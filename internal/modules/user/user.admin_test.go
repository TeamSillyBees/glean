package user

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/data"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// TestEnsureAnotherSuperAdminProtectsLastAdministrator 验证最后一个有效超级管理员不可被移除。
func TestEnsureAnotherSuperAdminProtectsLastAdministrator(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	generator, err := idgen.NewGenerator(1)
	require.NoError(t, err)
	client := data.NewEntClientFromDB(db, generator)

	mock.ExpectQuery(`SELECT COUNT\("sys_user"\."id"\) FROM "sys_user"`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	err = ensureAnotherSuperAdmin(context.Background(), client, idgen.New(1001))
	require.ErrorContains(t, err, "last active super administrator")
	require.NoError(t, mock.ExpectationsWereMet())
}
