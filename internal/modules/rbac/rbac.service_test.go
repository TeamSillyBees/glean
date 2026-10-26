package rbac

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/data"
	appent "github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

func newRBACMockClient(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *appent.Client) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	generator, err := idgen.NewGenerator(1)
	require.NoError(t, err)
	return db, mock, data.NewEntClientFromDB(db, generator)
}

func rbacRoleRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "code", "name", "remark", "is_builtin", "is_enable", "sort_id",
		"deleted_at", "created_at", "updated_at", "created_by", "updated_by",
	})
}

// TestRolesByCodesLocksLogicalParents 验证逻辑外键写入前会共享锁定有效角色。
func TestRolesByCodesLocksLogicalParents(t *testing.T) {
	db, mock, client := newRBACMockClient(t)
	t.Cleanup(func() { _ = db.Close() })
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT .*FROM "sys_role".*ORDER BY "sys_role"\."id" ASC FOR SHARE`).
		WithArgs("admin").
		WillReturnRows(rbacRoleRows().AddRow(
			int64(1001), "admin", "管理员", nil, true, true, int32(0),
			nil, now, now, nil, nil,
		))

	roles, err := rolesByCodes(context.Background(), client, []string{"admin"})

	require.NoError(t, err)
	require.Len(t, roles, 1)
	require.Equal(t, "admin", roles[0].Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestFindRoleForUpdateLocksBeforeParentMutation 验证父记录状态变更前使用排他行锁。
func TestFindRoleForUpdateLocksBeforeParentMutation(t *testing.T) {
	db, mock, client := newRBACMockClient(t)
	t.Cleanup(func() { _ = db.Close() })
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT .*FROM "sys_role".*FOR UPDATE`).
		WithArgs(int64(1001)).
		WillReturnRows(rbacRoleRows().AddRow(
			int64(1001), "admin", "管理员", nil, true, true, int32(0),
			nil, now, now, nil, nil,
		))

	role, err := (&Service{}).findRoleForUpdate(context.Background(), client, idgen.New(1001))

	require.NoError(t, err)
	require.Equal(t, "admin", role.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}
