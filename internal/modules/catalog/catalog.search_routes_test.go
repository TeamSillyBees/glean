package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/entx"
	"github.com/teamsillybees/glean/internal/retrieval"
	"strings"
	"testing"
	"time"
)

// TestTopicRouteValidation 验证路线替代关键词、来源/日期校验及省略与清空语义。
func TestTopicRouteValidation(t *testing.T) {
	routes := []retrieval.Route{{ID: "main", Sources: []string{"pubmed"}, TermGroups: [][]string{{" alpha ", "alpha"}}, FieldMode: "title_abstract"}}
	input := TopicBody{Slug: "custom", Name: "Custom", SearchEnabled: true, SearchRoutes: &routes}
	require.NoError(t, normalizeTopic(&input))
	require.Equal(t, [][]string{{"alpha"}}, (*input.SearchRoutes)[0].TermGroups)
	for _, mutate := range []func(*retrieval.Route){func(r *retrieval.Route) { r.Sources = []string{"unknown"} }, func(r *retrieval.Route) { v := time.Now().Add(24 * time.Hour); r.BackfillFrom = &v }, func(r *retrieval.Route) { r.TermGroups = [][]string{{"alpha\n"}} }} {
		invalid := retrieval.NormalizeRoutes(routes)
		mutate(&invalid[0])
		input.SearchRoutes = &invalid
		require.Error(t, normalizeTopic(&input))
	}
	empty := []retrieval.Route{}
	require.Error(t, normalizeTopic(&TopicBody{Slug: "custom", Name: "Custom", SearchEnabled: true, SearchRoutes: &empty}))
	require.NoError(t, normalizeTopic(&TopicBody{Slug: "biomed-ai", Name: "Builtin", SearchEnabled: true, SearchRoutes: &empty}))
	for _, payload := range []string{`{"name":"Legacy"}`, `{"name":"Legacy","searchRoutes":null}`} {
		var input TopicBody
		require.NoError(t, json.Unmarshal([]byte(payload), &input))
		require.Nil(t, input.SearchRoutes)
	}
	var clear TopicBody
	require.NoError(t, json.Unmarshal([]byte(`{"name":"Clear","searchRoutes":[]}`), &clear))
	require.NotNil(t, clear.SearchRoutes)
	require.Empty(t, *clear.SearchRoutes)
}

// TestTopicRouteLockedUpdates 保留版本行锁且旧客户端省略路线仍可保存仅路线主题。
func TestTopicRouteLockedUpdates(t *testing.T) {
	for _, version := range []int{1, 2} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			service, mock := catalogMock(t)
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT .* FROM "sys_user" .* FOR SHARE`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
			mock.ExpectQuery(`SELECT .* FROM "glean_topic" .* FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"id", "slug", "version", "search_routes"}).AddRow(301, "custom", 2, `[{"id":"main","sources":["pubmed"],"termGroups":[["alpha"]]}]`))
			if version == 2 {
				mock.ExpectExec(`UPDATE "glean_topic" SET .*"search_routes"`).WillReturnError(fmt.Errorf("route update reached"))
			}
			mock.ExpectRollback()
			_, err := service.SaveTopic(managerContext(), "custom", TopicBody{Name: "Legacy", SearchEnabled: true, ExpectedVersion: version}, false)
			if version == 1 {
				require.Equal(t, 409, bizerrors.StatusOf(err))
			} else {
				require.ErrorContains(t, err, "route update reached")
			}
		})
	}
}

// TestDiscoveryDoesNotChangeSourceHash 把运行/查询路径从来源版本身份中排除。
func TestDiscoveryDoesNotChangeSourceHash(t *testing.T) {
	input := observationFixture()
	before, err := normalizeObservation(&input)
	require.NoError(t, err)
	input.Discoveries = []retrieval.Hit{{RunID: input.RunID, QueryID: "main", QueryVersion: "v1", PlanVersion: "p1", Source: input.Source, Query: "alpha", Mode: "boolean", DiscoveredAt: input.ObservedAt}}
	after, err := normalizeObservation(&input)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

// TestDiscoveryReplayPersistsQueryHit 验证旧来源版本仍追加新查询命中，重放不重复写入。
func TestDiscoveryReplayPersistsQueryHit(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			service, mock := catalogMock(t)
			input := observationFixture()
			input.DOI = ""
			input.Discoveries = []retrieval.Hit{{RunID: input.RunID, QueryID: "main", QueryVersion: "v1", PlanVersion: "plan1", Source: input.Source, Query: "alpha", Mode: "boolean", DiscoveredAt: input.ObservedAt}}
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT .*FROM "sys_user".* FOR SHARE`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
			mock.ExpectQuery(`SELECT .*FROM "glean_paper".* FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"id", "identity_key", "title"}).AddRow(11, "source:crossref:10.9999/source", "original"))
			mock.ExpectQuery(`SELECT .*FROM "glean_observation"`).WillReturnRows(sqlmock.NewRows([]string{"id", "paper_id", "review"}).AddRow(12, 11, `{}`))
			rows := sqlmock.NewRows([]string{"id"})
			if existing {
				rows.AddRow(15)
			}
			mock.ExpectQuery(`SELECT .*FROM "glean_search_hit"`).WithArgs(12, input.RunID, "main", "v1").WillReturnRows(rows)
			if !existing {
				mock.ExpectExec(`INSERT INTO "glean_search_hit"`).WillReturnResult(sqlmock.NewResult(15, 1))
			}
			mock.ExpectQuery(`SELECT .*FROM "glean_observation_match"`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(13))
			mock.ExpectQuery(`SELECT .*FROM "glean_membership"`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(14))
			mock.ExpectCommit()
			result, err := service.IngestObservation(entx.WithOperatorID(context.Background(), 7), input)
			require.NoError(t, err)
			require.False(t, result.Created)
		})
	}
}

// TestDiscoveryDetailReadbackBounds 验证多路线可见且100条上限不会静默截断。
func TestDiscoveryDetailReadbackBounds(t *testing.T) {
	for _, count := range []int{2, 101} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			service, mock := catalogMock(t)
			at := time.Now().UTC()
			mock.ExpectQuery(`SELECT .*FROM "glean_observation"`).WillReturnRows(sqlmock.NewRows([]string{"id", "paper_id", "observed_at", "metadata", "review"}).AddRow(12, 11, at, `{"source":"pubmed"}`, `{}`))
			mock.ExpectQuery(`SELECT .*FROM "glean_observation_match"`).WillReturnRows(sqlmock.NewRows([]string{"id", "observation_id", "topic"}).AddRow(13, 12, "biomed-ai"))
			rows := sqlmock.NewRows([]string{"id", "observation_id", "run_id", "query_id", "query_version", "plan_version", "source", "query_text", "mode", "discovered_at"})
			for i := 0; i < count; i++ {
				rows.AddRow(100+i, 12, 7001, fmt.Sprint("route-", i), "v1", "plan1", "pubmed", "alpha", "boolean", at)
			}
			mock.ExpectQuery(`SELECT .*FROM "glean_search_hit".*ORDER BY.*LIMIT 101`).WithArgs(12).WillReturnRows(rows)
			items := []PaperVO{{ID: 11}}
			require.NoError(t, service.attachObservations(context.Background(), items, true))
			require.Len(t, items[0].Observations[0].Discoveries, min(count, 100))
			require.Equal(t, count > 100, items[0].Observations[0].DiscoveriesHasMore)
			require.Equal(t, "route-0", items[0].Observations[0].Discoveries[0].QueryID)
		})
	}
}

// TestCompiledRouteFitsDiscoveryProvenance 保留最大合法路线的完整编译文本和Unicode转义膨胀。
func TestCompiledRouteFitsDiscoveryProvenance(t *testing.T) {
	for _, term := range []string{strings.Repeat("a", 300), strings.Repeat(`"\`, 150), strings.Repeat("\u00ad", 150)} {
		terms := make([]string, 50)
		for i := range terms {
			terms[i] = term
		}
		route := retrieval.Route{ID: "maximal", Sources: []string{"europepmc"}, TermGroups: [][]string{terms}, FieldMode: "title_abstract"}
		require.NoError(t, retrieval.ValidateRoutes([]retrieval.Route{route}, time.Now()))
		compiled, err := retrieval.Compile("europepmc", route.TermGroups, route.FieldMode)
		require.NoError(t, err)
		require.Greater(t, len(compiled.Query), 20000)
		input := observationFixture()
		input.Source = "europepmc"
		input.Discoveries = []retrieval.Hit{{RunID: input.RunID, QueryID: "maximal", QueryVersion: "v1", PlanVersion: "plan1", Source: input.Source, Query: compiled.Query, Mode: compiled.Mode, DiscoveredAt: input.ObservedAt}}
		_, err = normalizeObservation(&input)
		require.NoError(t, err)
		require.Equal(t, compiled.Query, input.Discoveries[0].Query)
	}
	input := observationFixture()
	input.Discoveries = []retrieval.Hit{{RunID: input.RunID, QueryID: "oversized", QueryVersion: "v1", PlanVersion: "plan1", Source: input.Source, Query: strings.Repeat("a", 128*1024+1), Mode: "boolean", DiscoveredAt: input.ObservedAt}}
	_, err := normalizeObservation(&input)
	require.Error(t, err)
}
