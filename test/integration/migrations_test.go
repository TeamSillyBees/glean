package integration_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	atlas "ariga.io/atlas/sql/migrate"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/data/ent/migrate"
	"golang.org/x/crypto/bcrypt"

	_ "github.com/lib/pq"
)

// TestMigrationHistoryHasNoPhysicalForeignKeys 验证迁移校验和及整个历史中的外键禁用约束。
func TestMigrationHistoryHasNoPhysicalForeignKeys(t *testing.T) {
	dir, err := atlas.NewLocalDir(filepath.Join("..", "..", "db", "migrations"))
	require.NoError(t, err)
	require.NoError(t, atlas.Validate(dir))
	files, err := dir.Files()
	require.NoError(t, err)
	require.NotEmpty(t, files)
	foreignKey := regexp.MustCompile(`(?i)\bFOREIGN\s+KEY\b|\bREFERENCES\b`)
	for _, file := range files {
		require.Falsef(t, foreignKey.Match(file.Bytes()), "%s contains a physical foreign key", file.Name())
	}
}

// TestLogicalRelationIndexes 验证逻辑关系仍有索引覆盖，且非唯一索引不会被更长索引完全覆盖。
func TestLogicalRelationIndexes(t *testing.T) {
	for _, table := range migrate.Tables {
		for _, relation := range table.ForeignKeys {
			covered := false
			for _, index := range table.Indexes {
				if len(index.Columns) >= len(relation.Columns) {
					match := true
					for i, column := range relation.Columns {
						match = match && column.Name == index.Columns[i].Name
					}
					covered = covered || match
				}
			}
			require.Truef(t, covered, "%s relation %s lacks an index", table.Name, relation.Symbol)
		}
		for _, index := range table.Indexes {
			if index.Unique {
				continue
			}
			for _, other := range table.Indexes {
				if index == other || len(other.Columns) < len(index.Columns) {
					continue
				}
				prefix := true
				for i, column := range index.Columns {
					prefix = prefix && column.Name == other.Columns[i].Name
				}
				require.Falsef(t, prefix, "%s index %s is covered by %s", table.Name, index.Name, other.Name)
			}
		}
	}
}

// TestMigrationsAndSeedOnPostgres 在显式指定的测试库中使用事务隔离 schema，验证真实 SQL 和种子幂等性。
func TestMigrationsAndSeedOnPostgres(t *testing.T) {
	databaseURL := os.Getenv("INITRA_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("设置 INITRA_TEST_DATABASE_URL 后执行 PostgreSQL 迁移与 seed 集成验证")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	// schema 名只使用测试进程生成的数字；整个 schema 连同测试数据在回滚时删除。
	schemaName := "template_test_" + time.Now().Format("20060102150405.000000000")
	schemaName = strings.ReplaceAll(schemaName, ".", "_")
	_, err = tx.ExecContext(ctx, `CREATE SCHEMA "`+schemaName+`"`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `SET LOCAL search_path TO "`+schemaName+`"`)
	require.NoError(t, err)
	dir, err := atlas.NewLocalDir(filepath.Join("..", "..", "db", "migrations"))
	require.NoError(t, err)
	files, err := dir.Files()
	require.NoError(t, err)
	for _, file := range files {
		_, err = tx.ExecContext(ctx, string(file.Bytes()))
		require.NoError(t, err, file.Name())
	}
	seed, err := os.ReadFile(filepath.Join("..", "..", "db", "seeds", "001_seed_admin.sql"))
	require.NoError(t, err)
	hash, err := bcrypt.GenerateFromPassword([]byte("integration-test-password"), bcrypt.MinCost)
	require.NoError(t, err)
	seedSQL := strings.ReplaceAll(string(seed), "__INITRA_ADMIN_PASSWORD_HASH__", string(hash))
	for range 2 {
		_, err = tx.ExecContext(ctx, seedSQL)
		require.NoError(t, err, "重复 seed 应保留已有管理员和授权关系")
	}
	var username, passwordHash string
	var sessionVersion int64
	var superAdmin bool
	err = tx.QueryRowContext(ctx, "SELECT username, password_hash, session_version, is_super_admin FROM sys_user WHERE username = 'admin'").Scan(&username, &passwordHash, &sessionVersion, &superAdmin)
	require.NoError(t, err)
	require.Equal(t, "admin", username)
	require.EqualValues(t, 1, sessionVersion)
	require.True(t, superAdmin)
	require.Equal(t, string(hash), passwordHash, "再次 seed 不得重置密码")
	var foreignKeys, roleAssignments int
	err = tx.QueryRowContext(ctx, "SELECT count(*) FROM pg_constraint WHERE connamespace = current_schema()::regnamespace AND contype = 'f'").Scan(&foreignKeys)
	require.NoError(t, err)
	require.Zero(t, foreignKeys)
	err = tx.QueryRowContext(ctx, "SELECT count(*) FROM sys_user_role WHERE user_id = (SELECT id FROM sys_user WHERE username = 'admin')").Scan(&roleAssignments)
	require.NoError(t, err)
	require.Equal(t, 1, roleAssignments)
}
