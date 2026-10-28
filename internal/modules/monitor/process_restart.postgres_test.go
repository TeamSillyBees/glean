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
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/platform/entx"
	"github.com/teamsillybees/glean/internal/platform/httpclient"
	"github.com/teamsillybees/glean/internal/platform/idgen"
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
	db.SetMaxOpenConns(1)
	_, err = db.ExecContext(ctx, `SET search_path TO "`+schema+`"`)
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(entsql.NewDriver(dialect.Postgres, entsql.Conn{ExecQuerier: db})))
	client.Use(entx.AuditHook(entx.AuditHookOptions{}))
	client.Use(entx.RejectDeleteHook())
	ids, err := idgen.NewGenerator(61)
	require.NoError(t, err)
	response := `{"hitCount":2,"nextCursorMark":"saved-next","resultList":{"result":[{"id":"1","source":"MED","title":"Synthetic first page"}]}}`
	now := scenarioTime
	if phase == "resume" {
		response = `{"hitCount":2,"resultList":{"result":[{"id":"2","source":"MED","title":"Synthetic resumed page"}]}}`
		now = now.Add(4 * time.Second)
	}
	executor := &adapterExecutor{responses: []*httpclient.Response{jsonResponse(response)}}
	publisher := &flakyPublisher{}
	service, err := NewService(client, ids, RuntimeConfig(), []Source{NewEuropePMCSource(executor)}, MockReviewer{}, publisher)
	require.NoError(t, err)
	service.clock = func() time.Time { return now }
	provider := &mutableTemplates{items: []SearchTemplate{TopicSearchTemplate("custom", [][]string{{"original"}}, 1)}}
	service.templates = provider
	if phase == "checkpoint" {
		partial, err := service.RunOnce(ctx, now)
		require.NoError(t, err)
		require.Len(t, partial.Records, 1)
		require.NotEmpty(t, partial.Runs[0].Sources["europepmc"].Cursor)
		require.True(t, partial.Sources["europepmc"].LastAcquiredAt.IsZero())
		return // 测试子进程正常退出，此后没有共享内存、连接或服务对象。
	}
	require.Equal(t, "resume", phase)
	provider.err = errors.New("current topic configuration unavailable")
	before, err := service.Snapshot(ctx)
	require.NoError(t, err)
	require.Len(t, before.Runs, 1)
	require.Len(t, before.Records, 1)
	done, err := service.RunOnce(ctx, now)
	require.NoError(t, err)
	require.Equal(t, before.Runs[0].ID, done.Runs[0].ID)
	require.Equal(t, before.Runs[0].SearchTemplates, done.Runs[0].SearchTemplates)
	require.Equal(t, "completed", done.Runs[0].Status)
	require.Len(t, done.Records, 2)
	require.Equal(t, scenarioTime, done.Sources["europepmc"].AcquiredThrough)
	require.Equal(t, now, done.Sources["europepmc"].LastAcquiredAt)
	require.Equal(t, "saved-next", executor.requests[0].options.QueryParams.Get("cursorMark"))
	require.Contains(t, executor.requests[0].options.QueryParams.Get("query"), "original")
	require.Zero(t, provider.calls)
	replayed, err := service.RunOnce(ctx, now)
	require.NoError(t, err)
	require.Len(t, replayed.Records, 2)
	require.Len(t, executor.requests, 1)
}
