package monitor

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	atlas "ariga.io/atlas/sql/migrate"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/platform/entx"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// budgetSchemaDriver 在每个并发事务中声明隔离 schema，不依赖连接池的会话粘性。
type budgetSchemaDriver struct {
	*entsql.Driver
	db     *sql.DB
	schema string
}

func (d *budgetSchemaDriver) Tx(ctx context.Context) (dialect.Tx, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `SET LOCAL search_path TO "`+d.schema+`"`); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return &entsql.Tx{Conn: entsql.Conn{ExecQuerier: tx}, Tx: tx}, nil
}

// TestModelBudgetPostgresConcurrentAndRestart 验证多个服务争用同一额度，重建服务仍不能恢复已消费额度。
func TestModelBudgetPostgresConcurrentAndRestart(t *testing.T) {
	dsn := os.Getenv("INITRA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("INITRA_TEST_DATABASE_URL required for real PostgreSQL budget test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	schema := fmt.Sprintf("glean_budget_%d", time.Now().UnixNano())
	_, err = db.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`)
	require.NoError(t, err)
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
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
	ids, err := idgen.NewGenerator(62)
	require.NoError(t, err)
	build := func() *modelBudget {
		client := ent.NewClient(ent.Driver(&budgetSchemaDriver{Driver: entsql.OpenDB(dialect.Postgres, db), db: db, schema: schema}))
		client.Use(entx.AuditHook(entx.AuditHookOptions{}))
		client.Use(entx.RejectDeleteHook())
		return &modelBudget{service: &Service{client: client, ids: ids}, limits: ModelBudgetConfig{MaxCalls: 5, MaxReservedTokens: 1000000, MaxInputBytes: 1024, MaxOutputTokens: 128}}
	}
	ctx = entx.WithOperatorID(ctx, 7)
	var successes atomic.Int32
	errors := make(chan error, 20)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := build().reserve(ctx, 100)
			if err == nil {
				successes.Add(1)
			} else {
				errors <- err
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errors)
	require.EqualValues(t, 5, successes.Load(), "concurrent services must share the hard call reservation cap")
	for err := range errors {
		require.ErrorContains(t, err, "model_budget_exhausted")
	}
	var raw []byte
	require.NoError(t, db.QueryRowContext(ctx, `SELECT snapshot FROM "`+schema+`".glean_monitor_state WHERE key=$1`, modelBudgetKey).Scan(&raw))
	var usage ModelBudgetUsage
	require.NoError(t, json.Unmarshal(raw, &usage))
	require.Equal(t, 5, usage.Calls)
	require.Equal(t, 5*(100+4096+128), usage.ReservedTokens)
	require.ErrorContains(t, build().reserve(ctx, 1), "model_budget_exhausted", "rebuilding never resets the database counter")
	tooSmall := build()
	tooSmall.limits.MaxCalls = 100
	tooSmall.limits.MaxReservedTokens = usage.ReservedTokens
	require.ErrorContains(t, tooSmall.reserve(ctx, 1), "model_budget_exhausted", "token reservation is independent of call count")
}
