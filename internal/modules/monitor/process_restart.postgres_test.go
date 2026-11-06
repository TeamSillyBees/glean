package monitor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	atlas "ariga.io/atlas/sql/migrate"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/platform/entx"
	"github.com/teamsillybees/glean/internal/platform/httpclient"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/retrieval"
)

// TestMonitorPostgresProcessRestart 使用两个独立测试子进程证明已提交的分页检查点可在进程退出后恢复；仅假 HTTP，无外部 API。
func TestMonitorPostgresProcessRestart(t *testing.T) {
	dsn := os.Getenv("INITRA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("INITRA_TEST_DATABASE_URL required for real PostgreSQL process restart test")
	}
	if phase := os.Getenv("GLEAN_TEST_RESTART_PHASE"); phase != "" {
		runRestartProcess(t, dsn, phase, os.Getenv("GLEAN_TEST_RESTART_SCHEMA"))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	// 不能使用外层未提交事务：两个真实进程必须读到已提交 schema 和 JSONB。
	schema := fmt.Sprintf("glean_restart_%d", time.Now().UnixNano())
	_, err = db.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`)
	require.NoError(t, err)
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		_, err := db.ExecContext(cleanup, `DROP SCHEMA "`+schema+`" CASCADE`)
		require.NoError(t, err)
	}()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
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
	require.NoError(t, tx.Commit())
	for _, phase := range []string{"checkpoint", "resume"} {
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMonitorPostgresProcessRestart$", "-test.count=1")
		command.Env = append(os.Environ(), "GLEAN_TEST_RESTART_PHASE="+phase, "GLEAN_TEST_RESTART_SCHEMA="+schema)
		output, err := command.CombinedOutput()
		require.NoError(t, err, "restart phase %s failed: %s", phase, output)
	}
}

func runRestartProcess(t *testing.T, dsn, phase, schema string) {
	t.Helper()
	require.True(t, strings.HasPrefix(schema, "glean_restart_"))
	for _, c := range strings.TrimPrefix(schema, "glean_restart_") {
		require.True(t, c >= '0' && c <= '9')
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	// 每个 phase 固定在一个真实事务，事务池无需提供会话粘性。仅本函数提交父事务。
	tx, err := beginRestartProcessTx(ctx, db, schema)
	require.NoError(t, err)
	defer tx.Rollback()
	client := ent.NewClient(ent.Driver(&monitorTestTxDriver{Driver: entsql.NewDriver(dialect.Postgres, entsql.Conn{ExecQuerier: tx}), tx: tx}))
	client.Use(entx.AuditHook(entx.AuditHookOptions{}))
	client.Use(entx.RejectDeleteHook())
	ids, err := idgen.NewGenerator(61)
	require.NoError(t, err)
	response := restartRoutePage("checkpoint")
	now := scenarioTime
	if phase == "resume" {
		response = restartRoutePage("resume")
		now = now.Add(4 * time.Second)
	}
	executor := &adapterExecutor{responses: []*httpclient.Response{jsonResponse(response)}}
	publisher := &flakyPublisher{}
	service, err := NewService(client, ids, RuntimeConfig(), []Source{NewEuropePMCSource(executor)}, MockReviewer{}, publisher)
	require.NoError(t, err)
	service.clock = func() time.Time { return now }
	provider := &mutableTemplates{items: []SearchTemplate{restartRouteTemplate()}}
	service.templates = provider
	if phase == "checkpoint" {
		partial, err := service.RunOnce(ctx, now)
		require.NoError(t, err)
		require.Len(t, partial.Records, 1)
		require.NotEmpty(t, partial.Runs[0].Sources["europepmc"].Cursor)
		require.True(t, partial.Sources["europepmc"].LastAcquiredAt.IsZero())
		assertRestartRouteProgress(t, partial, false)
		require.NoError(t, tx.Commit()) // 恢复进程只能读取此处真实提交的检查点。
		return                          // 测试子进程正常退出，此后没有共享内存、连接或服务对象。
	}
	require.Equal(t, "resume", phase)
	provider.err = errors.New("current topic configuration unavailable")
	before, err := service.Snapshot(ctx)
	require.NoError(t, err)
	require.Len(t, before.Runs, 1)
	require.Len(t, before.Records, 1)
	beforeQuery := assertRestartRouteProgress(t, before, false)
	done, err := service.RunOnce(ctx, now)
	require.NoError(t, err)
	require.Equal(t, before.Runs[0].ID, done.Runs[0].ID)
	require.Equal(t, before.Runs[0].SearchTemplates, done.Runs[0].SearchTemplates)
	require.Equal(t, "completed", done.Runs[0].Status)
	require.Len(t, done.Records, 2)
	doneQuery := assertRestartRouteProgress(t, done, true)
	require.Equal(t, beforeQuery.QueryID, doneQuery.QueryID)
	require.Equal(t, beforeQuery.QueryVersion, doneQuery.QueryVersion)
	require.Equal(t, beforeQuery.CoverageWindow, doneQuery.CoverageWindow)
	for key, candidate := range before.Records {
		require.Contains(t, done.Records, key, "repeated source result retains the original candidate key")
		require.Equal(t, candidate.ID, done.Records[key].ID)
	}
	require.Equal(t, scenarioTime, done.Sources["europepmc"].AcquiredThrough)
	require.Equal(t, now, done.Sources["europepmc"].LastAcquiredAt)
	require.Equal(t, "saved-next", executor.requests[0].options.QueryParams.Get("cursorMark"))
	require.Contains(t, executor.requests[0].options.QueryParams.Get("query"), `TITLE:"original"`)
	require.Contains(t, executor.requests[0].options.QueryParams.Get("query"), `ABSTRACT:"original"`)
	require.Zero(t, provider.calls)
	replayed, err := service.RunOnce(ctx, now)
	require.NoError(t, err)
	require.Len(t, replayed.Records, 2)
	require.Equal(t, doneQuery, assertRestartRouteProgress(t, replayed, true))
	require.Len(t, executor.requests, 1)
	require.NoError(t, tx.Commit())
}

// beginRestartProcessTx 将所有 phase 读写约束到本测试 schema；禁止依赖会话 SET 或 public 回退。
func beginRestartProcessTx(ctx context.Context, db *sql.DB, schema string) (*sql.Tx, error) {
	suffix, ok := strings.CutPrefix(schema, "glean_restart_")
	if !ok || suffix == "" {
		return nil, fmt.Errorf("invalid restart test schema")
	}
	for _, c := range suffix {
		if c < '0' || c > '9' {
			return nil, fmt.Errorf("invalid restart test schema")
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `SET LOCAL search_path TO "`+schema+`"`); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	var actual string
	if err = tx.QueryRowContext(ctx, "SELECT current_schema()").Scan(&actual); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if actual != schema {
		_ = tx.Rollback()
		return nil, fmt.Errorf("restart test schema isolation failed")
	}
	return tx, nil
}

// TestRestartProcessTransactionIsolation 离线验证事务级 schema 设置、提交与失败回滚，不连接 PostgreSQL。
func TestRestartProcessTransactionIsolation(t *testing.T) {
	const schema = "glean_restart_12345"
	t.Run("phase transaction commits explicitly", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()
		mock.ExpectBegin()
		mock.ExpectExec(`SET LOCAL search_path TO "glean_restart_12345"`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(`SELECT current_schema\(\)`).WillReturnRows(sqlmock.NewRows([]string{"current_schema"}).AddRow(schema))
		mock.ExpectCommit()
		tx, err := beginRestartProcessTx(context.Background(), db, schema)
		require.NoError(t, err)
		require.NoError(t, tx.Commit())
		require.NoError(t, mock.ExpectationsWereMet())
	})
	for _, tc := range []struct {
		name                 string
		setError, queryError bool
		actual               string
	}{
		{name: "SET LOCAL failure", setError: true},
		{name: "schema assertion failure", queryError: true},
		{name: "public fallback rejected", actual: "public"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			mock.ExpectBegin()
			set := mock.ExpectExec(`SET LOCAL search_path TO "glean_restart_12345"`)
			if tc.setError {
				set.WillReturnError(errors.New("fixture set failure"))
			} else {
				set.WillReturnResult(sqlmock.NewResult(0, 0))
				query := mock.ExpectQuery(`SELECT current_schema\(\)`)
				if tc.queryError {
					query.WillReturnError(errors.New("fixture query failure"))
				} else {
					query.WillReturnRows(sqlmock.NewRows([]string{"current_schema"}).AddRow(tc.actual))
				}
			}
			mock.ExpectRollback()
			tx, err := beginRestartProcessTx(context.Background(), db, schema)
			require.Error(t, err)
			require.Nil(t, tx)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
	t.Run("invalid schema rejected before connection", func(t *testing.T) {
		for _, schema := range []string{"public", "glean_restart_", `glean_restart_1"; DROP SCHEMA public;--`} {
			tx, err := beginRestartProcessTx(context.Background(), nil, schema)
			require.Error(t, err)
			require.Nil(t, tx)
		}
	})
}

func restartRouteTemplate() SearchTemplate {
	template := TopicSearchTemplate("custom", nil, 1)
	template.Routes = []retrieval.Route{{ID: "title-abstract", Sources: []string{"europepmc"}, FieldMode: "title_abstract", TermGroups: [][]string{{"original"}}}}
	return template
}

func restartRoutePage(phase string) string {
	if phase == "checkpoint" {
		return `{"hitCount":3,"nextCursorMark":"saved-next","resultList":{"result":[{"id":"1","source":"MED","title":"Synthetic first page"}]}}`
	}
	// 相同来源记录在恢复页再次返回：发现路径与抓取时间不能制造第三个候选。
	return `{"hitCount":3,"resultList":{"result":[{"id":"1","source":"MED","title":"Synthetic first page"},{"id":"2","source":"MED","title":"Synthetic resumed page"}]}}`
}

func assertRestartRouteProgress(t *testing.T, snapshot Snapshot, completed bool) QueryProgress {
	t.Helper()
	require.Len(t, snapshot.Runs, 1)
	run := snapshot.Runs[0]
	progress := run.Sources["europepmc"]
	require.NotNil(t, progress)
	require.Len(t, progress.Queries, 1)
	query := progress.Queries["custom-configured/title-abstract"]
	require.NotNil(t, query)
	require.Equal(t, "custom-configured/title-abstract", query.QueryID)
	require.NotEmpty(t, query.QueryVersion)
	require.Equal(t, Window{Start: scenarioTime.Add(-30 * 24 * time.Hour), End: scenarioTime}, query.CoverageWindow)
	require.Contains(t, query.Query, `TITLE:"original"`)
	require.Contains(t, query.Query, `ABSTRACT:"original"`)
	requests, observed, candidates, partitions, status := 1, 1, 1, 0, "partial_budget"
	if completed {
		requests, observed, candidates, partitions, status = 2, 3, 2, 1, "completed"
	}
	require.Equal(t, requests, query.Requests)
	require.Equal(t, requests, query.Pages)
	require.Equal(t, observed, query.Observed)
	require.Equal(t, candidates, query.NewCandidates)
	require.Equal(t, partitions, query.CompletedPartitions)
	require.Equal(t, status, query.Status)
	for _, candidate := range snapshot.Records {
		require.Len(t, candidate.Record.Discoveries, 1, "same query/run discovery is idempotent across pages")
		hit := candidate.Record.Discoveries[0]
		require.Equal(t, run.ID, hit.RunID)
		require.Equal(t, run.TemplateVersion, hit.PlanVersion)
		require.Equal(t, query.QueryID, hit.QueryID)
		require.Equal(t, query.QueryVersion, hit.QueryVersion)
		require.Equal(t, query.Query, hit.Query)
		require.Equal(t, "europepmc", hit.Source)
		require.False(t, hit.DiscoveredAt.IsZero())
		withoutHits := candidate.Record
		withoutHits.Discoveries = nil
		identity, hash := recordIdentity(candidate.Record)
		bareIdentity, bareHash := recordIdentity(withoutHits)
		require.Equal(t, bareIdentity, identity)
		require.Equal(t, bareHash, hash, "discovery provenance is excluded from content identity")
	}
	return *query
}

// TestRestartRouteFixtureOffline 检查同一跨进程用例的协议、覆盖计数和去重；不代表真实进程或 PostgreSQL 验收。
func TestRestartRouteFixtureOffline(t *testing.T) {
	executor := &adapterExecutor{responses: []*httpclient.Response{jsonResponse(restartRoutePage("checkpoint")), jsonResponse(restartRoutePage("resume"))}}
	ids, err := idgen.NewGenerator(61)
	require.NoError(t, err)
	cfg := RuntimeConfig()
	cfg.MaxPagesPerCall = 1
	service, err := NewOfflineService(ids, cfg, []Source{NewEuropePMCSource(executor)}, MockReviewer{}, &flakyPublisher{})
	require.NoError(t, err)
	now := scenarioTime
	service.clock = func() time.Time { return now }
	provider := &mutableTemplates{items: []SearchTemplate{restartRouteTemplate()}}
	service.templates = provider
	before, err := service.RunOnce(context.Background(), now)
	require.NoError(t, err)
	beforeQuery := assertRestartRouteProgress(t, before, false)
	provider.err = errors.New("configuration unavailable during frozen resume")
	now = now.Add(4 * time.Second)
	after, err := service.RunOnce(context.Background(), now)
	require.NoError(t, err)
	afterQuery := assertRestartRouteProgress(t, after, true)
	require.Equal(t, beforeQuery.QueryID, afterQuery.QueryID)
	require.Equal(t, beforeQuery.QueryVersion, afterQuery.QueryVersion)
	require.Equal(t, beforeQuery.CoverageWindow, afterQuery.CoverageWindow)
	require.Len(t, after.Records, 2)
	for key, candidate := range before.Records {
		require.Contains(t, after.Records, key)
		require.Equal(t, candidate.ID, after.Records[key].ID)
	}
	require.Equal(t, 1, provider.calls)
	require.Equal(t, "saved-next", executor.requests[1].options.QueryParams.Get("cursorMark"))
}
