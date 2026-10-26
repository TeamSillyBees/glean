package monitor_test

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
	"github.com/teamsillybees/glean/internal/boot"
	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/modules/catalog"
	"github.com/teamsillybees/glean/internal/modules/monitor"
	"github.com/teamsillybees/glean/internal/platform/entx"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
)

type livePipelineDriver struct {
	*entsql.Driver
	tx       *sql.Tx
	sequence int
}

func (d *livePipelineDriver) Tx(ctx context.Context) (dialect.Tx, error) {
	d.sequence++
	name := fmt.Sprintf("monitor_case_%d", d.sequence)
	if _, err := d.tx.ExecContext(ctx, "SAVEPOINT "+name); err != nil {
		return nil, err
	}
	return &livePipelineSavepoint{ExecQuerier: d.Driver, tx: d.tx, name: name}, nil
}
func (d *livePipelineDriver) Close() error { return nil }

type livePipelineSavepoint struct {
	dialect.ExecQuerier
	tx   *sql.Tx
	name string
}

func (s *livePipelineSavepoint) Commit() error {
	_, err := s.tx.Exec("RELEASE SAVEPOINT " + s.name)
	return err
}
func (s *livePipelineSavepoint) Rollback() error {
	if _, err := s.tx.Exec("ROLLBACK TO SAVEPOINT " + s.name); err != nil {
		return err
	}
	_, err := s.tx.Exec("RELEASE SAVEPOINT " + s.name)
	return err
}

// pipelineSourceFixture 仅替代 CI 的外部网络，经过真实 Service、真实目录发布器和 PostgreSQL；生产装配从不使用它。
type pipelineRegistryFixture struct{}

func (pipelineRegistryFixture) CoreJournals(context.Context) ([]monitor.JournalEntry, error) {
	return []monitor.JournalEntry{{ISSNs: []string{"0028-0836"}, MetricYear: 2025, MetricSource: "CI fixture only"}}, nil
}

type pipelineSourceFixture struct {
	record  monitor.Record
	records []monitor.Record
	calls   int
}

func (s *pipelineSourceFixture) Name() string { return "crossref" }
func (s *pipelineSourceFixture) Fetch(ctx context.Context, request monitor.FetchRequest) (monitor.Page, error) {
	s.calls++
	if err := ctx.Err(); err != nil {
		return monitor.Page{}, err
	}
	if request.Template.Topic != "biomed-ai" || request.Cursor != "" {
		return monitor.Page{Complete: true}, nil
	}
	records := s.records
	if records == nil {
		records = []monitor.Record{s.record}
	}
	return monitor.Page{Records: records, NextCursor: "fixture-page-2"}, nil
}

// TestMonitorRealPipelinePostgres 验证来源页在未完成分页时进入真实目录，mock LLM 不污染真实论文标记，重建与版本更新保持幂等。
func TestMonitorRealPipelinePostgres(t *testing.T) {
	dsn := os.Getenv("INITRA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("INITRA_TEST_DATABASE_URL required for real PostgreSQL pipeline test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	schema := "glean_live_pipeline_" + strings.ReplaceAll(time.Now().Format("20060102150405.000000000"), ".", "_")
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
	ids, err := idgen.ConfigureDefault(63)
	require.NoError(t, err)
	driver := &livePipelineDriver{Driver: entsql.NewDriver(dialect.Postgres, entsql.Conn{ExecQuerier: tx}), tx: tx}
	client := ent.NewClient(ent.Driver(driver))
	client.Use(entx.AuditHook(entx.AuditHookOptions{}))
	client.Use(entx.RejectDeleteHook())
	_, err = client.SysUser.Create().SetID(7).SetUsername("live-pipeline-test-operator").SetPasswordHash("test-only-not-a-login-credential").Save(ctx)
	require.NoError(t, err)
	catalogService := catalog.NewService(client)
	publisher := boot.NewCatalogMonitorPublisher(catalogService, 7)
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	source := &pipelineSourceFixture{record: monitor.Record{Source: "crossref", SourceID: "10.0000/ci-pipeline", DOI: "10.0000/ci-pipeline", Title: "CI fixture: clinical machine learning", Abstract: "Network fixture for transaction-isolated integration only.", URL: "https://doi.org/10.0000/ci-pipeline", SourceURL: "https://api.crossref.org/works/10.0000/ci-pipeline", PublicationType: "journal-article", Version: "1", Topics: []string{"biomed-ai"}, SourceUpdatedAt: at.Add(-time.Hour), Raw: json.RawMessage(`{"fixture":true,"version":1}`), PublicationEvents: []monitor.PublicationEvent{{Kind: "online", Date: at.Add(-24 * time.Hour), Evidence: "CI publisher metadata fixture"}}}}
	cfg := monitor.RuntimeConfig()
	cfg.MaxPagesPerCall = 1
	service, err := monitor.NewService(client, ids, cfg, []monitor.Source{source}, monitor.MockReviewer{}, publisher, pipelineRegistryFixture{})
	require.NoError(t, err)
	actorCtx := entx.WithOperatorID(ctx, 7)
	first, err := service.RunOnce(actorCtx, at)
	require.NoError(t, err)
	require.True(t, first.Sources["crossref"].AcquiredThrough.IsZero())
	require.Equal(t, 1, first.Runs[0].Sources["crossref"].Projected)
	stateRow, err := client.GleanMonitorState.Query().Only(ctx)
	require.NoError(t, err)
	require.NotNil(t, stateRow.UpdatedBy)
	require.Equal(t, idgen.ID(7), *stateRow.UpdatedBy, "lease release retains the audit actor")
	userCtx := requestctx.WithUserID(ctx, "7")
	page, err := catalogService.Papers(userCtx, catalog.PaperQuery{Topic: "biomed-ai", CollectionScope: "pending"})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total, "query interface sees real persisted observation before full source pagination")
	paperID := page.Items[0].ID
	detail, err := catalogService.Paper(userCtx, paperID)
	require.NoError(t, err)
	require.False(t, detail.IsMock)
	require.NotEmpty(t, detail.Observations)
	require.NotNil(t, detail.Review)
	require.True(t, detail.Review.IsMock)
	require.False(t, detail.Review.ScientificReviewed)
	// 丢弃 Service 后重新构造：游标与真实目录行均来自 PostgreSQL，不是内存发布器。
	service, err = monitor.NewService(client, ids, cfg, []monitor.Source{source}, monitor.MockReviewer{}, publisher, pipelineRegistryFixture{})
	require.NoError(t, err)
	for i := 1; i <= 7; i++ {
		_, err = service.RunOnce(actorCtx, at.Add(time.Duration(i)*4*time.Second))
		require.NoError(t, err)
	}
	completed, err := service.Snapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, "completed", completed.Runs[0].Status)
	require.Equal(t, at, completed.Sources["crossref"].AcquiredThrough)
	for _, candidate := range completed.Records {
		require.False(t, candidate.Record.IsMock)
		require.True(t, candidate.Review.IsMock)
		require.False(t, candidate.Review.ScientificReviewed)
		require.Equal(t, "pending", candidate.Review.InclusionStatus)
		require.True(t, candidate.Compacted)
		require.Empty(t, candidate.Record.Raw)
	}
	source.record.Version = "2"
	source.record.Raw = json.RawMessage(`{"fixture":true,"version":2}`)
	source.record.SourceUpdatedAt = at.Add(71 * time.Hour)
	_, err = service.RunOnce(actorCtx, at.Add(72*time.Hour))
	require.NoError(t, err)
	page, err = catalogService.Papers(userCtx, catalog.PaperQuery{Topic: "biomed-ai", CollectionScope: "pending"})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	require.Equal(t, paperID, page.Items[0].ID)
	detail, err = catalogService.Paper(userCtx, paperID)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(detail.Observations), 2, "source versions remain independently queryable")
	for i := 1; i <= 7; i++ {
		_, err = service.RunOnce(actorCtx, at.Add(72*time.Hour+time.Duration(i)*4*time.Second))
		require.NoError(t, err)
	}
	// 一个跨载体身份冲突不能回滚同页两条良好记录；原始冲突证据保留待人工处理。
	_, err = catalogService.IngestObservation(actorCtx, catalog.ObservationInput{RunID: ids.NextID(), Source: "arxiv", SourceID: "2610.99999", ArxivID: "2610.99999", Version: "1", Title: "CI fixture: identity collision anchor", ObservedAt: time.Now().UTC(), Topics: []string{"biomed-ai"}, TemplateVersion: monitor.TemplateVersion, Raw: json.RawMessage(`{"fixture":"collision-anchor"}`), Review: catalog.ReviewVO{Provider: "deterministic-mock", IsMock: true, InclusionStatus: "pending"}})
	require.NoError(t, err)
	source.records = []monitor.Record{}
	for i := 0; i < 3; i++ {
		r := source.record
		r.SourceID = fmt.Sprintf("10.0000/ci-isolated-%d", i)
		r.DOI = r.SourceID
		r.Title = fmt.Sprintf("CI fixture: valid clinical record %d", i)
		r.SourceUpdatedAt = at.Add(143 * time.Hour)
		r.Raw = json.RawMessage(fmt.Sprintf(`{"fixture":"quarantine-batch","item":%d}`, i))
		if i == 0 {
			r.DOI = source.record.DOI
			r.ArxivID = "2610.99999"
		}
		source.records = append(source.records, r)
	}
	partial, err := service.RunOnce(actorCtx, at.Add(144*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 2, partial.Runs[len(partial.Runs)-1].Sources["crossref"].Projected)
	require.Equal(t, 1, partial.Sources["crossref"].Quarantined)
	service, err = monitor.NewService(client, ids, cfg, []monitor.Source{source}, monitor.MockReviewer{}, publisher, pipelineRegistryFixture{})
	require.NoError(t, err)
	for i := 1; i <= 7; i++ {
		_, err = service.RunOnce(actorCtx, at.Add(144*time.Hour+time.Duration(i)*4*time.Second))
		require.NoError(t, err)
	}
	recovered, err := service.Snapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, "completed_with_quarantine", recovered.Runs[len(recovered.Runs)-1].Status)
	require.Equal(t, at.Add(144*time.Hour), recovered.Sources["crossref"].HandledThrough)
	require.Equal(t, at.Add(72*time.Hour), recovered.Sources["crossref"].PublishedThrough)
	found := false
	for _, candidate := range recovered.Records {
		if candidate.Quarantine != nil {
			found = true
			require.Equal(t, "identity_conflict", candidate.Quarantine.Reason)
			require.NotEmpty(t, candidate.Record.Raw)
			require.False(t, candidate.Published)
		}
	}
	require.True(t, found)
	beforeCount, err := client.GleanObservation.Query().Count(ctx)
	require.NoError(t, err)
	_, err = service.RunOnce(actorCtx, at.Add(144*time.Hour+32*time.Second))
	require.NoError(t, err)
	afterCount, err := client.GleanObservation.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount, "resume creates no duplicate observations")
	page, err = catalogService.Papers(userCtx, catalog.PaperQuery{Topic: "biomed-ai", CollectionScope: "pending"})
	require.NoError(t, err)
	require.Equal(t, 4, page.Total, "two original identities and two good new papers survive one quarantined conflict")
}
