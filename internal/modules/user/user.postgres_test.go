package user

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/teamsillybees/glean/internal/platform/server"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	atlas "ariga.io/atlas/sql/migrate"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	appent "github.com/teamsillybees/glean/internal/data/ent"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// userPostgresDriver 为每个真实独立事务指定测试 schema，绝不改变连接池的全局 search_path。
type userPostgresDriver struct {
	*entsql.Driver
	schema string
	ready  chan int
}

func (d *userPostgresDriver) Tx(ctx context.Context) (dialect.Tx, error) {
	tx, err := d.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `SET LOCAL search_path TO "`+d.schema+`"`); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `SET LOCAL lock_timeout = '10s'`); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if d.ready != nil {
		var pid int
		if err = tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		d.ready <- pid
	}
	return &userPostgresTx{ExecQuerier: entsql.NewDriver(dialect.Postgres, entsql.Conn{ExecQuerier: tx}), tx: tx}, nil
}

type userPostgresTx struct {
	dialect.ExecQuerier
	tx *sql.Tx
}

func (t *userPostgresTx) Commit() error   { return t.tx.Commit() }
func (t *userPostgresTx) Rollback() error { return t.tx.Rollback() }

// TestUserAdministrationPostgres 验证两个管理员互相禁用或降级时真实事务串行化、重新鉴权且至少保留一位管理员。
func TestUserAdministrationPostgres(t *testing.T) {
	dsn := os.Getenv("INITRA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("INITRA_TEST_DATABASE_URL required for real PostgreSQL test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	db.SetMaxOpenConns(6)
	schema := fmt.Sprintf("glean_user_admin_test_%d", time.Now().UnixNano())
	_, err = db.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`)
	require.NoError(t, err)
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, cleanupErr := db.ExecContext(cleanupCtx, `DROP SCHEMA "`+schema+`" CASCADE`)
		require.NoError(t, cleanupErr)
	}()
	// schema 必须提交以供不同连接读取；清理只删除本测试生成的唯一 schema。
	setup, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer setup.Rollback()
	_, err = setup.ExecContext(ctx, `SET LOCAL search_path TO "`+schema+`"`)
	require.NoError(t, err)
	dir, err := atlas.NewLocalDir(filepath.Join("..", "..", "..", "db", "migrations"))
	require.NoError(t, err)
	files, err := dir.Files()
	require.NoError(t, err)
	for _, file := range files {
		_, err = setup.ExecContext(ctx, string(file.Bytes()))
		require.NoError(t, err, file.Name())
	}
	require.NoError(t, setup.Commit())

	for _, operation := range []string{"disable", "demote"} {
		t.Run(operation, func(t *testing.T) {
			ctx, cancelCase := context.WithCancel(ctx)
			defer cancelCase()
			_, err = db.ExecContext(ctx, `DELETE FROM "`+schema+`".sys_user`)
			require.NoError(t, err)
			_, err = db.ExecContext(ctx, `INSERT INTO "`+schema+`".sys_user(id,username,password_hash,is_super_admin,is_enable,created_at,updated_at) VALUES (1001,'admin-a','test-only-not-a-password',true,true,now(),now()),(1002,'admin-b','test-only-not-a-password',true,true,now(),now())`)
			require.NoError(t, err)
			// 外部锁让两个请求确实同时到达管理员锁，而不是偶然串行执行。
			blocker, err := db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer blocker.Rollback()
			_, err = blocker.ExecContext(ctx, `SELECT id FROM "`+schema+`".sys_user WHERE id=1001 FOR UPDATE`)
			require.NoError(t, err)
			ready := make(chan int, 2)
			driver := &userPostgresDriver{Driver: entsql.OpenDB(dialect.Postgres, db), schema: schema, ready: ready}
			service := NewService(appent.NewClient(appent.Driver(driver)), &fakeUserCache{}, &fakePasswordManager{})
			results := make(chan error, 2)
			no := false
			body := UpdateUserBody{IsEnable: &no}
			if operation == "demote" {
				body = UpdateUserBody{IsSuperAdmin: &no}
			}
			var workers sync.WaitGroup
			defer func() { cancelCase(); _ = blocker.Rollback(); workers.Wait() }()
			for _, actor := range []idgen.ID{1001, 1002} {
				workers.Add(1)
				go func(actor idgen.ID) {
					defer workers.Done()
					requestCtx := platformauth.WithPrincipal(ctx, platformauth.Principal{UserID: actor, IsSuperAdmin: true})
					_, updateErr := service.Update(requestCtx, idgen.New(2003-int64(actor)), body)
					results <- updateErr
				}(actor)
			}
			pids := make([]int, 0, 2)
			for len(pids) < 2 {
				select {
				case pid := <-ready:
					pids = append(pids, pid)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			require.Eventually(t, func() bool {
				var waiting int
				queryErr := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE pid IN ($1,$2) AND wait_event_type='Lock'`, pids[0], pids[1]).Scan(&waiting)
				return queryErr == nil && waiting == 2
			}, 5*time.Second, 10*time.Millisecond, "both independent requests must contend on the administrator lock")
			require.NoError(t, blocker.Commit())
			successes := 0
			for range 2 {
				select {
				case updateErr := <-results:
					if updateErr == nil {
						successes++
					} else {
						require.ErrorContains(t, updateErr, "requires an active super administrator")
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			require.Equal(t, 1, successes, "exactly one cross-account mutation succeeds")
			var remaining int
			err = db.QueryRowContext(ctx, `SELECT count(*) FROM "`+schema+`".sys_user WHERE deleted_at IS NULL AND is_enable AND is_super_admin`).Scan(&remaining)
			require.NoError(t, err)
			require.Equal(t, 1, remaining)
		})
	}
	t.Run("create ordinary account over HTTP with default viewer", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `INSERT INTO "`+schema+`".sys_role
            (id,code,name,is_builtin,is_enable,created_at,updated_at)
            VALUES (1000000000102,'viewer','只读用户',true,true,now(),now())`)
		require.NoError(t, err)
		var actor int64
		require.NoError(t, db.QueryRowContext(ctx, `SELECT id FROM "`+schema+`".sys_user WHERE is_enable AND is_super_admin`).Scan(&actor))
		generator, err := idgen.NewGenerator(1)
		require.NoError(t, err)
		idgen.SetDefaultGenerator(generator)
		driver := &userPostgresDriver{Driver: entsql.OpenDB(dialect.Postgres, db), schema: schema}
		service := NewService(appent.NewClient(appent.Driver(driver)), &fakeUserCache{}, &fakePasswordManager{})
		_, api := humatest.New(t)
		NewModule(NewHandler(service)).Register(api, server.NewRouteRegistry())
		actorCtx := platformauth.WithPrincipal(ctx, platformauth.Principal{UserID: idgen.ID(actor), IsSuperAdmin: true})
		res := api.DoCtx(actorCtx, http.MethodPost, "/api/v1/users", map[string]any{
			"username": "http-created-reader", "password": "test-only-password", "nickname": "HTTP 读者", "isEnable": true, "isSuperAdmin": false,
		})
		require.Equal(t, http.StatusOK, res.Code, res.Body.String())
		var envelope struct {
			Data UserVO `json:"data"`
		}
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &envelope))
		require.Equal(t, "http-created-reader", envelope.Data.Username)
		require.False(t, envelope.Data.IsSuperAdmin)
		require.True(t, envelope.Data.IsEnable)
		require.Equal(t, []string{"viewer"}, envelope.Data.RoleCodes)
		var count int
		require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM "`+schema+`".sys_user u JOIN "`+schema+`".sys_user_role ur ON u.id=ur.user_id JOIN "`+schema+`".sys_role r ON ur.role_id=r.id WHERE u.username='http-created-reader' AND r.code='viewer' AND ur.deleted_at IS NULL AND u.is_enable AND NOT u.is_super_admin`).Scan(&count))
		require.Equal(t, 1, count, "ordinary account and default viewer membership must both persist")
	})

}
