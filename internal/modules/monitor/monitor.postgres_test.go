package monitor

import (
	"context"
	"database/sql"
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
)

// TestMonitorPostgresDurableMockRun 在真实 PostgreSQL 迁移表上验证暂停、重建服务、恢复游标与 API 查询；整个临时 schema 最终回滚。
func TestMonitorPostgresDurableMockRun(t *testing.T) {
	dsn := os.Getenv("INITRA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("INITRA_TEST_DATABASE_URL required for real PostgreSQL monitor test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	schema := "glean_monitor_test_" + strings.ReplaceAll(time.Now().Format("20060102150405.000000000"), ".", "_")
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
	// 每次构建全新 Ent client；没有共享 Service.memory，读取必须来自真实 PostgreSQL JSONB。
	makeClient := func() *ent.Client {
		client := ent.NewClient(ent.Driver(entsql.NewDriver(dialect.Postgres, entsql.Conn{ExecQuerier: tx})))
		client.Use(entx.AuditHook(entx.AuditHookOptions{}))
		client.Use(entx.RejectDeleteHook())
		return client
	}
	ids, err := idgen.NewGenerator(34)
	require.NoError(t, err)
	firstClient := makeClient()
	_, err = firstClient.SysUser.Create().SetID(idgen.ID(7)).SetUsername("monitor-test-operator").SetPasswordHash("test-only-not-a-login-credential").Save(ctx)
	require.NoError(t, err)
	cfg := DefaultConfig()
	cfg.MaxPagesPerCall = 1
	first, err := NewService(firstClient, ids, cfg, DemoSources(scenarioTime, false), MockReviewer{}, MockPublisher{})
	require.NoError(t, err)
	actorCtx := entx.WithOperatorID(ctx, idgen.ID(7))
	partial, err := first.RunOnce(actorCtx, scenarioTime)
	require.NoError(t, err)
	require.Equal(t, "pending", partial.Runs[0].Status)
	require.Len(t, partial.Records, 1)
	require.True(t, partial.Sources["crossref"].AcquiredThrough.IsZero())
	firstRunID := partial.Runs[0].ID
	secondClient := makeClient()
	cfg.MaxPagesPerCall = 100
	restored, err := NewService(secondClient, ids, cfg, DemoSources(scenarioTime, false), MockReviewer{}, MockPublisher{})
	require.NoError(t, err)
	persisted, err := restored.Snapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, firstRunID, persisted.Runs[0].ID)
	require.Equal(t, 1, persisted.Runs[0].Sources["crossref"].TemplateIndex)
	require.Len(t, persisted.Records, 1)
	completed, err := restored.RunOnce(actorCtx, scenarioTime.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, "completed", completed.Runs[0].Status)
	require.Len(t, completed.Records, 4)
	require.Equal(t, scenarioTime, completed.Sources["crossref"].AcquiredThrough)
	incremental, err := RunPersistentDemo(actorCtx, secondClient, ids, scenarioTime, "incremental")
	require.NoError(t, err)
	require.Len(t, incremental.Runs, 2)
	require.Len(t, incremental.Records, 12)
	replayed, err := RunPersistentDemo(actorCtx, makeClient(), ids, scenarioTime, "incremental")
	require.NoError(t, err)
	require.Len(t, replayed.Runs, 2)
	require.Len(t, replayed.Records, 12, "repeated explicit seed is idempotent")
	apiService, err := NewService(makeClient(), ids, DefaultConfig(), DemoSources(scenarioTime, false), nil, nil)
	require.NoError(t, err)
	handler := NewHandler(apiService)
	runs, err := handler.runs(ctx, &runsRequest{})
	require.NoError(t, err)
	require.Equal(t, "OK", runs.Body.Code)
	require.Len(t, runs.Body.Data.Items, 2)
	require.Equal(t, 2, runs.Body.Data.Items[0].Number)
	sources, err := handler.sources(ctx, &sourcesRequest{})
	require.NoError(t, err)
	require.Len(t, sources.Body.Data.Items, 4)
	require.False(t, sources.Body.Data.Schedule.RealFetchEnabled)
	for _, checkpoint := range sources.Body.Data.Items {
		require.Equal(t, scenarioTime.Add(72*time.Hour), checkpoint.AcquiredThrough)
		require.Equal(t, checkpoint.AcquiredThrough, checkpoint.ReviewedThrough)
		require.Equal(t, checkpoint.AcquiredThrough, checkpoint.PublishedThrough)
	}
	for _, candidate := range replayed.Records {
		require.True(t, candidate.Review.IsMock)
		require.False(t, candidate.Review.ScientificReviewed)
		require.Equal(t, "pending", candidate.Review.InclusionStatus)
	}
	count, err := firstClient.GleanPaper.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count, "mock projection must never insert historical catalog papers")
	row, err := firstClient.GleanMonitorState.Query().Only(ctx)
	require.NoError(t, err)
	require.Equal(t, DefaultConfig().StateKey, row.Key)
	require.NotNil(t, row.CreatedBy)
	require.Equal(t, idgen.ID(7), *row.CreatedBy)
	require.Greater(t, row.Revision, 1)
	// PostgreSQL 返回 0 affected rows 时，服务必须拒绝过期写入而不是覆盖新状态。
	stale, err := restored.load(ctx)
	require.NoError(t, err)
	fresh, err := apiService.load(ctx)
	require.NoError(t, err)
	require.NoError(t, apiService.save(actorCtx, fresh))
	require.ErrorContains(t, restored.save(actorCtx, stale), "lease lost")
}
