package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	atlas "ariga.io/atlas/sql/migrate"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/platform/entx"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
)

// catalogTestDriver 将每个服务事务映射为外层测试事务内的 savepoint，整个 schema 最终回滚。
type catalogTestDriver struct {
	*entsql.Driver
	tx       *sql.Tx
	sequence int
}

func (d *catalogTestDriver) Tx(ctx context.Context) (dialect.Tx, error) {
	d.sequence++
	name := fmt.Sprintf("catalog_case_%d", d.sequence)
	if _, err := d.tx.ExecContext(ctx, "SAVEPOINT "+name); err != nil {
		return nil, err
	}
	return &catalogTestSavepoint{ExecQuerier: d.Driver, tx: d.tx, name: name}, nil
}
func (d *catalogTestDriver) Close() error { return nil }

type catalogTestSavepoint struct {
	dialect.ExecQuerier
	tx   *sql.Tx
	name string
}

func (s *catalogTestSavepoint) Commit() error {
	_, err := s.tx.Exec("RELEASE SAVEPOINT " + s.name)
	return err
}
func (s *catalogTestSavepoint) Rollback() error {
	_, err := s.tx.Exec("ROLLBACK TO SAVEPOINT " + s.name)
	if err != nil {
		return err
	}
	_, err = s.tx.Exec("RELEASE SAVEPOINT " + s.name)
	return err
}

// TestCatalogPostgresEndToEnd 在真实 PostgreSQL 验证迁移、幂等导入、去重、筛选及个人状态隔离。
func TestCatalogPostgresEndToEnd(t *testing.T) {
	dsn := os.Getenv("INITRA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("INITRA_TEST_DATABASE_URL required for real PostgreSQL test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	schema := "glean_catalog_test_" + strings.ReplaceAll(time.Now().Format("20060102150405.000000000"), ".", "_")
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
		require.NoError(t, err, file.Name())
	}
	generator, err := idgen.ConfigureDefault(31)
	require.NoError(t, err)
	_ = generator
	driver := &catalogTestDriver{Driver: entsql.NewDriver(dialect.Postgres, entsql.Conn{ExecQuerier: tx}), tx: tx}
	client := ent.NewClient(ent.Driver(driver))
	client.Use(entx.RejectDeleteHook())
	for _, id := range []idgen.ID{7, 8} {
		_, err = client.SysUser.Create().SetID(id).SetUsername("catalog-user-" + id.String()).SetPasswordHash("test-only-not-a-login-credential").Save(ctx)
		require.NoError(t, err)
	}
	service := NewService(client)
	formal := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	original := ImportRecord{Topic: "biomed-ai", OriginalID: "bio-1", Title: "100% literal clinical agent", DOI: "10.1234/Stable", Year: 2026, Venue: "Evidence Journal", PublicationType: "article", FormalOnlineDate: &formal, DateProvenance: "publisher formal-online evidence", Tier: "historical", PrimaryCategory: "Oncology", Categories: []string{"Oncology"}, Dimensions: map[string][]string{"technical_tasks": {"planning"}, "ai_methods": {"transformer"}, "x' OR TRUE --": {"safe"}}, Raw: map[string]any{"original": "preserve"}, Evidence: []SourceEvidence{{Source: "publisher", DateType: "formal-online", Date: formal.Format(time.RFC3339), Raw: map[string]any{}}}}
	first, err := service.Import(ctx, original)
	require.NoError(t, err)
	require.True(t, first.Created)
	original.Title = "must not replace source title"
	original.Raw = map[string]any{"original": "changed"}
	again, err := service.Import(ctx, original)
	require.NoError(t, err)
	require.False(t, again.Created)
	require.Equal(t, first.PaperID, again.PaperID)
	second := original
	second.Topic = "llm-agent"
	second.OriginalID = "llm-1"
	second.ArxivID = "2401.12345v2"
	_, err = service.Import(ctx, second)
	require.NoError(t, err)
	user7 := requestctx.WithUserID(ctx, "7")
	user8 := requestctx.WithUserID(ctx, "8")
	page, err := service.Papers(user7, PaperQuery{})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	require.Len(t, page.Items, 1)
	require.Equal(t, "100% literal clinical agent", page.Items[0].Title)
	require.Len(t, page.Items[0].Memberships, 2)
	detail, err := service.Paper(user7, first.PaperID)
	require.NoError(t, err)
	require.Equal(t, "preserve", detail.Memberships[0].Raw["original"])
	require.Equal(t, "2401.12345", detail.ArxivID)
	require.NotNil(t, detail.FormalOnlineDate)
	require.Len(t, detail.Evidence, 1)
	filtered, err := service.Papers(user7, PaperQuery{Topic: "biomed-ai", Task: "planning", Method: "transformer", Q: "100%", Dimension: `{"x' OR TRUE --":"safe"}`})
	require.NoError(t, err)
	require.Equal(t, 1, filtered.Total)
	facets, err := service.Facets(user7, PaperQuery{Topic: "biomed-ai"})
	require.NoError(t, err)
	require.Equal(t, []FacetVO{{Value: "transformer", Count: 1}}, facets.Methods)
	require.Equal(t, []FacetVO{{Value: "planning", Count: 1}}, facets.Tasks)
	_, err = service.SavePaperState(user7, first.PaperID, PaperStateVO{Hidden: true, ReadStatus: "reading"})
	require.NoError(t, err)
	hidden, err := service.Papers(user7, PaperQuery{})
	require.NoError(t, err)
	require.Zero(t, hidden.Total)
	visibleOther, err := service.Papers(user8, PaperQuery{})
	require.NoError(t, err)
	require.Equal(t, 1, visibleOther.Total)
	only, err := service.Papers(user7, PaperQuery{Hidden: "only", ReadStatus: "reading"})
	require.NoError(t, err)
	require.Equal(t, 1, only.Total)
	_, err = service.SavePreferences(user7, PreferenceVO{Filters: map[string]any{"topic": "biomed-ai"}})
	require.NoError(t, err)
	prefs, err := service.Preferences(user7)
	require.NoError(t, err)
	require.Equal(t, "biomed-ai", prefs.Filters["topic"])
	otherPrefs, err := service.Preferences(user8)
	require.NoError(t, err)
	require.Empty(t, otherPrefs.Filters)
	for _, version := range []string{"2025", "2026", "2026"} {
		require.NoError(t, service.RegisterVenue(ctx, VenueVO{Name: "Evidence Journal", RegistryVersion: version, QualificationStatus: "pending"}))
	}
	venues, err := service.Venues(ctx, "Evidence", "")
	require.NoError(t, err)
	require.Len(t, venues, 2)

	// 真实来源样本直接经过同一服务写入并重放，覆盖 DTO 与 PostgreSQL JSONB 数组筛选。
	fixture := baselineFixtureRecords(t)
	fixtureIDs := map[string]idgen.ID{}
	for _, record := range fixture {
		result, err := service.Import(ctx, record)
		require.NoError(t, err)
		require.True(t, result.Created)
		fixtureIDs[record.Topic+":"+record.OriginalID] = result.PaperID
	}
	for _, record := range fixture {
		result, err := service.Import(ctx, record)
		require.NoError(t, err)
		require.False(t, result.Created)
		require.Equal(t, fixtureIDs[record.Topic+":"+record.OriginalID], result.PaperID)
	}
	checkedTopics := map[string]bool{}
	for _, record := range fixture {
		if checkedTopics[record.Topic] {
			continue
		}
		checkedTopics[record.Topic] = true
		for _, key := range []string{"paper_types", "biomedical_domains", "display_scope", "technical_tasks", "ai_leadership", "display_review_status", "relation_direction", "scope", "llm_role", "display_type", "display_scale", "display_tasks", "display_roles"} {
			values := record.Dimensions[key]
			if len(values) == 0 {
				continue
			}
			encoded, err := json.Marshal(map[string]string{key: values[0]})
			require.NoError(t, err)
			page, err := service.Papers(user8, PaperQuery{Topic: record.Topic, Dimension: string(encoded), PageSize: 100})
			require.NoError(t, err)
			found := false
			for _, paper := range page.Items {
				if paper.ID == fixtureIDs[record.Topic+":"+record.OriginalID] {
					found = true
				}
			}
			require.True(t, found, "literal JSONB array filter must find original source record: %s/%s", record.Topic, key)
		}
	}
	require.Error(t, client.GleanPaper.DeleteOneID(first.PaperID).Exec(ctx), "historical records must reject physical deletion")
}
