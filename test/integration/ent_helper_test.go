package integration_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/data"
	appent "github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

var testNow = time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)

func newMockEntClient(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *appent.Client) {
	t.Helper()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	generator, err := idgen.NewGenerator(1)
	require.NoError(t, err)
	return db, mock, data.NewEntClientFromDB(db, generator)
}

func sysUserRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"deleted_at",
		"created_at",
		"updated_at",
		"created_by",
		"updated_by",
		"username",
		"password_hash",
		"nickname",
		"phone",
		"email",
		"avatar_url",
		"is_super_admin",
		"is_enable",
		"session_version",
		"sort_id",
	})
}

func sysRoleRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"deleted_at",
		"created_at",
		"updated_at",
		"created_by",
		"updated_by",
		"code",
		"name",
		"remark",
		"is_builtin",
		"is_enable",
		"sort_id",
	})
}

func sysUserRoleRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"deleted_at",
		"user_id",
		"role_id",
		"created_by",
		"created_at",
	})
}
