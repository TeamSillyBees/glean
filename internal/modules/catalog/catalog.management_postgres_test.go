package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	atlas "ariga.io/atlas/sql/migrate"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/gleanpaper"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/entx"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
	"github.com/teamsillybees/glean/internal/retrieval"
)

// TestCatalogManagementPostgres 验证真实数据库的覆盖层、主题、审计、隔离、版本以及来源不变性。
func TestCatalogManagementPostgres(t *testing.T) {
	dsn := os.Getenv("INITRA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("INITRA_TEST_DATABASE_URL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	schema := "glean_management_test_" + strings.ReplaceAll(time.Now().Format("20060102150405.000000000"), ".", "_")
	_, err = tx.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `SET LOCAL search_path TO "`+schema+`"`)
	require.NoError(t, err)
	dir, err := atlas.NewLocalDir(filepath.Join("..", "..", "..", "db", "migrations"))
	require.NoError(t, err)
	files, err := dir.Files()
	require.NoError(t, err)
	for _, file := range files {
		_, err = tx.ExecContext(ctx, string(file.Bytes()))
		require.NoError(t, err)
	}
	_, err = idgen.ConfigureDefault(57)
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(&catalogTestDriver{Driver: entsql.NewDriver(dialect.Postgres, entsql.Conn{ExecQuerier: tx}), tx: tx}))
	client.Use(entx.RejectDeleteHook())

	for _, uid := range []idgen.ID{7, 8} {
		_, err = client.SysUser.Create().SetID(uid).SetUsername("management-user-" + uid.String()).SetPasswordHash("test-only").SetIsSuperAdmin(uid == 7).Save(ctx)
		require.NoError(t, err)
	}
	service := NewService(client)
	manager := platformauth.WithPrincipal(ctx, platformauth.Principal{UserID: 7, IsSuperAdmin: true})
	viewer := requestctx.WithUserID(ctx, "8")
	forgedManager := platformauth.WithPrincipal(ctx, platformauth.Principal{UserID: 8, IsSuperAdmin: true})
	_, err = service.SaveTopic(forgedManager, "", TopicBody{Slug: "forged", Name: "forged"}, true)
	require.Equal(t, 403, bizerrors.StatusOf(err), "database role recheck rejects stale/elevated claims")

	original := ImportRecord{Topic: "biomed-ai", OriginalID: "baseline-1", Title: "Original title", Year: 2020, Venue: "Old journal", DOI: "10.1234/unchanged", Dimensions: map[string][]string{"collection": {"正式纳入"}}, Raw: map[string]any{"title": "immutable source title"}}
	imported, err := service.Import(ctx, original)
	require.NoError(t, err)
	original.Topic = "llm-agent"
	original.OriginalID = "baseline-2"
	_, err = service.Import(ctx, original)
	require.NoError(t, err)
	second, err := service.Import(ctx, ImportRecord{Topic: "llm-agent", OriginalID: "second-distinct", Title: "B middle paper", Year: 2025, Venue: "Middle journal", DOI: "10.1234/second"})
	require.NoError(t, err)
	before, err := service.Topics(ctx)
	require.NoError(t, err)
	require.Len(t, before, 3)
	require.Equal(t, 1, before[0].CuratedCount)
	// 一次保存即生效，不改变另一个主题的结论。
	excluded, err := service.SaveReview(manager, imported.PaperID, "biomed-ai", ReviewBody{Status: "excluded", Evidence: screeningFixture("biomed-ai", "excluded"), Note: "人工范围判断", Labels: []string{"人工标注"}})
	require.NoError(t, err)
	require.Equal(t, 1, excluded.Version)
	page, err := service.Papers(viewer, PaperQuery{Topic: "biomed-ai", CollectionScope: "curated"})
	require.NoError(t, err)
	require.Zero(t, page.Total)
	page, err = service.Papers(viewer, PaperQuery{Topic: "biomed-ai", CollectionScope: "excluded", Dimension: `{"manual_labels":"人工标注"}`})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	detail, err := service.Paper(viewer, imported.PaperID)
	require.NoError(t, err)
	require.Equal(t, "excluded", detail.Memberships[0].ManualReview.Status)
	require.Nil(t, detail.Memberships[1].ManualReview)
	require.Equal(t, "immutable source title", detail.Memberships[0].Raw["title"])
	_, err = service.SaveReview(manager, imported.PaperID, "biomed-ai", ReviewBody{Status: "included", Note: "transaction fixture", Evidence: screeningFixture("biomed-ai", "included")})
	require.Equal(t, 409, bizerrors.StatusOf(err))
	included, err := service.SaveReview(manager, imported.PaperID, "biomed-ai", ReviewBody{Status: "included", Note: "transaction fixture", Evidence: screeningFixture("biomed-ai", "included"), ExpectedVersion: 1})
	require.NoError(t, err)
	require.Equal(t, 2, included.Version)
	page, err = service.Papers(viewer, PaperQuery{Topic: "biomed-ai", CollectionScope: "curated"})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	// 覆盖书目必须同时影响搜索、排序、筛选和 facets，canonical 来源保持原样。
	title := "A Corrected 100% title"
	venue := "Corrected journal"
	year := 2030
	abstract := "Corrected abstract"
	authors := []string{"Corrected Author"}
	correction, err := service.SaveCorrection(manager, imported.PaperID, CorrectionBody{Fields: CorrectionFields{Title: &title, Year: &year, Venue: &venue, Abstract: &abstract, Authors: &authors}, Reason: "publisher verification"})
	require.NoError(t, err)
	require.Equal(t, 1, correction.Version)
	canonical, err := client.GleanPaper.Query().Where(gleanpaper.ID(imported.PaperID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "Original title", canonical.Title)
	require.Equal(t, 2020, canonical.Year)
	page, err = service.Papers(viewer, PaperQuery{Q: "100%", Year: 2030, Venue: venue, Sort: "title"})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	require.Equal(t, title, page.Items[0].Title)
	require.Equal(t, authors, page.Items[0].Authors)
	ordered, err := service.Papers(viewer, PaperQuery{Sort: "newest"})
	require.NoError(t, err)
	require.Equal(t, imported.PaperID, ordered.Items[0].ID)
	ordered, err = service.Papers(viewer, PaperQuery{Sort: "oldest"})
	require.NoError(t, err)
	require.Equal(t, second.PaperID, ordered.Items[0].ID)
	ordered, err = service.Papers(viewer, PaperQuery{Sort: "title"})
	require.NoError(t, err)
	require.Equal(t, imported.PaperID, ordered.Items[0].ID)
	// 真实审计插入失败必须撤销覆盖层更新，旧版本仍可继续编辑。
	_, err = tx.ExecContext(ctx, `CREATE FUNCTION reject_catalog_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'audit deliberately unavailable'; END $$; CREATE TRIGGER reject_catalog_audit BEFORE INSERT ON glean_catalog_change FOR EACH ROW EXECUTE FUNCTION reject_catalog_audit();`)
	require.NoError(t, err)
	_, err = service.SaveCorrection(manager, imported.PaperID, CorrectionBody{ExpectedVersion: 1, Reason: "must roll back"})
	require.ErrorContains(t, err, "audit deliberately unavailable")
	_, err = tx.ExecContext(ctx, `DROP TRIGGER reject_catalog_audit ON glean_catalog_change; DROP FUNCTION reject_catalog_audit();`)
	require.NoError(t, err)
	stillCorrected, err := service.Paper(viewer, imported.PaperID)
	require.NoError(t, err)
	require.Equal(t, 1, stillCorrected.Correction.Version)
	require.Equal(t, title, stillCorrected.Title)

	facets, err := service.Facets(viewer, PaperQuery{Year: 2030})
	require.NoError(t, err)
	require.Equal(t, []FacetVO{{Value: "2030", Count: 1}}, facets.Years)
	require.Equal(t, []FacetVO{{Value: venue, Count: 1}}, facets.Venues)
	_, err = service.SaveCorrection(manager, imported.PaperID, CorrectionBody{})
	require.Equal(t, 409, bizerrors.StatusOf(err))
	// 新主题保存后直接可见、可查询并接收采集结果。
	topic, err := service.SaveTopic(manager, "", TopicBody{Slug: "custom-topic", Name: "自定义主题", Description: "主题描述", Rule: "人工纳入规则", SearchEnabled: true, TermGroups: [][]string{{"keyword"}}}, true)
	require.NoError(t, err)
	require.Equal(t, 1, topic.Version)
	_, err = service.SaveTopic(manager, "", TopicBody{Slug: "custom-topic", Name: "duplicate"}, true)
	require.Equal(t, 409, bizerrors.StatusOf(err))
	configs, err := service.SearchTopicConfigs(ctx)
	require.NoError(t, err)
	require.Len(t, configs, 4)
	require.Equal(t, "custom-topic", configs[3].Slug)

	routes := []retrieval.Route{{ID: "main", Sources: []string{"pubmed"}, TermGroups: [][]string{{"alpha"}}}}
	topic, err = service.SaveTopic(manager, "custom-topic", TopicBody{Name: "Routes only", SearchEnabled: true, SearchRoutes: &routes, ExpectedVersion: 1}, false)
	require.NoError(t, err)
	require.Equal(t, routes, topic.SearchRoutes)
	topic, err = service.SaveTopic(manager, "custom-topic", TopicBody{Name: "Legacy client", SearchEnabled: true, ExpectedVersion: 2}, false)
	require.NoError(t, err)
	require.Equal(t, routes, topic.SearchRoutes)
	emptyRoutes := []retrieval.Route{}
	_, err = service.SaveTopic(manager, "custom-topic", TopicBody{Name: "Invalid clear", SearchEnabled: true, SearchRoutes: &emptyRoutes, ExpectedVersion: 3}, false)
	require.Equal(t, 400, bizerrors.StatusOf(err))
	topic, err = service.SaveTopic(manager, "custom-topic", TopicBody{Name: "Legacy keywords", SearchEnabled: true, SearchRoutes: &emptyRoutes, TermGroups: [][]string{{"keyword"}}, ExpectedVersion: 3}, false)
	require.NoError(t, err)
	require.Empty(t, topic.SearchRoutes)
	changes, err := client.GleanCatalogChange.Query().All(ctx)
	require.NoError(t, err)
	foundRoutesAudit := false
	for _, change := range changes {
		if change.Topic == "custom-topic" && change.After["name"] == "Legacy client" {
			require.NotEmpty(t, change.Before["searchRoutes"])
			require.NotEmpty(t, change.After["searchRoutes"])
			foundRoutesAudit = true
		}
	}
	require.True(t, foundRoutesAudit)
	page, err = service.Papers(viewer, PaperQuery{Topic: "custom-topic"})
	require.NoError(t, err)
	require.Zero(t, page.Total)
	input := ObservationInput{RunID: 7001, Source: "fixture", SourceID: "custom-1", Title: "Custom pending paper", DOI: "10.1234/custom", ObservedAt: time.Now().UTC(), Topics: []string{"custom-topic"}, Raw: json.RawMessage(`{"title":"immutable observed source"}`), Review: ReviewVO{IsMock: true}}
	observation, err := service.IngestObservation(entx.WithOperatorID(ctx, 7), input)
	require.NoError(t, err)
	page, err = service.Papers(viewer, PaperQuery{Topic: "custom-topic", CollectionScope: "pending"})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	_, err = service.SaveReview(manager, observation.PaperID, "custom-topic", ReviewBody{Status: "included", Note: "transaction fixture", Evidence: screeningFixture("custom-topic", "included")})
	require.NoError(t, err)
	input.ObservedAt = input.ObservedAt.Add(time.Minute)
	_, err = service.IngestObservation(entx.WithOperatorID(ctx, 7), input)
	require.NoError(t, err)
	page, err = service.Papers(viewer, PaperQuery{Topic: "custom-topic", CollectionScope: "curated"})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	// 私人隐藏不影响其他用户，且不会改变共享审核。
	_, err = service.SavePaperState(manager, observation.PaperID, PaperStateVO{ReadStatus: "read", Hidden: true})
	require.NoError(t, err)
	page, err = service.Papers(manager, PaperQuery{Topic: "custom-topic"})
	require.NoError(t, err)
	require.Zero(t, page.Total)
	page, err = service.Papers(viewer, PaperQuery{Topic: "custom-topic"})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	require.Equal(t, "unread", page.Items[0].ReadStatus)
	// 保存空覆盖集恢复原始书目，历史仍记录两个版本。
	_, err = service.SaveCorrection(manager, imported.PaperID, CorrectionBody{ExpectedVersion: 1, Reason: "恢复来源"})
	require.NoError(t, err)
	detail, err = service.Paper(viewer, imported.PaperID)
	require.NoError(t, err)
	require.Equal(t, "Original title", detail.Title)
	require.Equal(t, 2, detail.Correction.Version)
	history, err := service.History(viewer, imported.PaperID)
	require.NoError(t, err)
	require.Len(t, history, 4)
	require.NotNil(t, history[0].ActorID)
	require.Equal(t, idgen.ID(7), *history[0].ActorID)
	// 内置主题可首次覆盖名称但不丢失历史计数或原模板。
	builtin, err := service.SaveTopic(manager, "biomed-ai", TopicBody{Name: "Biomedical AI", SearchEnabled: true}, false)
	require.NoError(t, err)
	require.Equal(t, 1, builtin.Version)
	after, err := service.Topics(ctx)
	require.NoError(t, err)
	require.Len(t, after, 4)
	require.Equal(t, before[0].MembershipCount, after[0].MembershipCount)
	require.Equal(t, 1, after[0].CuratedCount)
	require.Empty(t, after[0].TermGroups)
	// 导出不经过界面隐藏/纳入筛选；同论文三个 profile 的不同决定必须全部保留。
	input.Topics = []string{"custom-topic", "llm-agent", "biomed-worldmodel"}
	_, err = service.IngestObservation(entx.WithOperatorID(ctx, 7), input)
	require.NoError(t, err)
	_, err = service.SaveReview(manager, observation.PaperID, "llm-agent", ReviewBody{Status: "excluded", Note: "transaction-only explicit exclusion", Evidence: screeningFixture("llm-agent", "excluded")})
	require.NoError(t, err)
	exported, err := service.RetrievalExportPage(manager, input.RunID, 0, 1)
	require.NoError(t, err)
	require.Len(t, exported, 1, "hidden records still belong in the audit")
	require.Equal(t, map[string]any{"hidden": true, "reading": "read"}, exported[0].Record["personal_state"])
	decisions := map[string]string{}
	for _, screening := range exported[0].Screening {
		decisions[screening["profile_id"].(string)] = screening["decision"].(string)
	}
	require.Equal(t, map[string]string{"custom-topic": "included", "llm-agent": "excluded", "biomed-worldmodel": "pending"}, decisions)
	require.NotEmpty(t, exported[0].Record["field_sources"])
	require.NotEmpty(t, exported[0].IdentityEdges)
	require.NotContains(t, exported[0].Record, "abstract")
	next, err := service.RetrievalExportPage(manager, input.RunID, exported[0].ID, 1)
	require.NoError(t, err)
	require.Empty(t, next, "stable ID cursor must not repeat the previous record")
	otherExport, err := service.RetrievalExportPage(viewer, input.RunID, 0, 1)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"hidden": false, "reading": "unread"}, otherExport[0].Record["personal_state"])
}
