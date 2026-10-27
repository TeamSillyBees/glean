package user

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/data"
	appent "github.com/teamsillybees/glean/internal/data/ent"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
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

// TestUserManagementRejectsPrivilegeEscalation 验证普通用户不可提升权限或修改管理员。
func TestUserManagementRejectsPrivilegeEscalation(t *testing.T) {
	yes := true
	roles := []string{"admin"}
	for _, tc := range []struct {
		name  string
		admin bool
		body  UpdateUserBody
	}{
		{"promote", false, UpdateUserBody{IsSuperAdmin: &yes}},
		{"assign role", false, UpdateUserBody{RoleCodes: &roles}},
		{"edit administrator", true, UpdateUserBody{Nickname: new("changed")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, mock := adminMockClient(t)
			mock.ExpectBegin()
			expectAdminLock(mock)
			mock.ExpectQuery(`SELECT .* FROM "sys_user" WHERE .* FOR UPDATE`).WithArgs(int64(1002)).WillReturnRows(sqlmock.NewRows([]string{"id", "is_super_admin", "is_enable"}).AddRow(1002, tc.admin, true))
			mock.ExpectRollback()
			ctx := platformauth.WithPrincipal(context.Background(), platformauth.Principal{UserID: idgen.New(1001)})
			service := NewService(client, &fakeUserCache{}, &fakePasswordManager{})
			_, err := service.Update(ctx, idgen.New(1002), tc.body)
			require.ErrorContains(t, err, "requires an active super administrator")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestCreatePrivilegedUserRequiresAdministrator(t *testing.T) {
	for _, body := range []CreateUserBody{
		{Username: "alice", Password: "secret", IsSuperAdmin: true},
		{Username: "alice", Password: "secret", RoleCodes: []string{"admin"}},
	} {
		client, mock := adminMockClient(t)
		mock.ExpectBegin()
		expectAdminLock(mock)
		mock.ExpectRollback()
		service := NewService(client, &fakeUserCache{}, &fakePasswordManager{})
		ctx := platformauth.WithPrincipal(context.Background(), platformauth.Principal{UserID: idgen.New(1001)})
		_, err := service.Create(ctx, body)
		require.ErrorContains(t, err, "requires an active super administrator")
		require.NoError(t, mock.ExpectationsWereMet())
	}
}

func TestUserManagementProtectsSelfBeforeDatabase(t *testing.T) {
	service := NewService(nil, &fakeUserCache{}, &fakePasswordManager{})
	ctx := platformauth.WithPrincipal(context.Background(), platformauth.Principal{UserID: idgen.New(1001), IsSuperAdmin: true})
	no := false
	for _, body := range []UpdateUserBody{{IsEnable: &no}, {IsSuperAdmin: &no}} {
		_, err := service.Update(ctx, idgen.New(1001), body)
		require.ErrorContains(t, err, "your own account")
	}
	require.ErrorContains(t, service.Delete(ctx, idgen.New(1001)), "your own account")
	ordinary := platformauth.WithPrincipal(context.Background(), platformauth.Principal{UserID: idgen.New(1001)})
	require.NoError(t, protectSelf(ordinary, idgen.New(1001), false, true, false))
}

func TestAdministratorIdentityRevalidatedInsideTransaction(t *testing.T) {
	client, mock := adminMockClient(t)
	ctx := platformauth.WithPrincipal(context.Background(), platformauth.Principal{UserID: idgen.New(1001), IsSuperAdmin: true})
	mock.ExpectQuery(`SELECT .* FROM "sys_user" WHERE`).WithArgs(int64(1001)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	require.ErrorContains(t, requireSuperAdmin(ctx, client), "requires an active super administrator")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestDeleteLocksAdministratorsBeforeTarget 验证固定管理员锁顺序先于目标锁，避免并发互删死锁。
func TestDeleteLocksAdministratorsBeforeTarget(t *testing.T) {
	client, mock := adminMockClient(t)
	mock.ExpectBegin()
	expectAdminLock(mock)
	mock.ExpectQuery(`SELECT .* FROM "sys_user" WHERE .* FOR UPDATE`).WithArgs(int64(1002)).WillReturnRows(sqlmock.NewRows([]string{"id", "is_super_admin", "is_enable"}).AddRow(1002, true, true))
	mock.ExpectRollback()
	service := NewService(client, &fakeUserCache{}, &fakePasswordManager{})
	ctx := platformauth.WithPrincipal(context.Background(), platformauth.Principal{UserID: idgen.New(1001)})
	require.ErrorContains(t, service.Delete(ctx, idgen.New(1002)), "requires an active super administrator")
	require.NoError(t, mock.ExpectationsWereMet())
}

func adminMockClient(t *testing.T) (*appent.Client, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	generator, err := idgen.NewGenerator(1)
	require.NoError(t, err)
	return data.NewEntClientFromDB(db, generator), mock
}

func expectAdminLock(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT .* FROM "sys_user" WHERE .* ORDER BY "sys_user"\."id" ASC FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"id", "is_super_admin", "is_enable"}).AddRow(1003, true, true))
}

// TestOrdinaryUserBasicEditPreservesRoles 验证普通授权用户仍可编辑基本资料，且未提交角色时不重写授权关系。
func TestOrdinaryUserBasicEditPreservesRoles(t *testing.T) {
	client, mock := adminMockClient(t)
	mock.ExpectBegin()
	expectAdminLock(mock)
	cols := []string{"id", "nickname", "is_super_admin", "is_enable"}
	mock.ExpectQuery(`SELECT .* FROM "sys_user" WHERE .* FOR UPDATE`).WithArgs(int64(1002)).WillReturnRows(sqlmock.NewRows(cols).AddRow(1002, "before", false, true))
	mock.ExpectExec(`UPDATE "sys_user" SET`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT .* FROM "sys_user" WHERE`).WithArgs(int64(1002)).WillReturnRows(sqlmock.NewRows(cols).AddRow(1002, "after", false, true))
	mock.ExpectQuery(`SELECT .* FROM "sys_user" WHERE`).WithArgs(int64(1002)).WillReturnRows(sqlmock.NewRows(cols).AddRow(1002, "after", false, true))
	mock.ExpectQuery(`SELECT .* FROM "sys_role"`).WillReturnRows(sqlmock.NewRows([]string{"code"}).AddRow("existing-role"))
	mock.ExpectCommit()
	service := NewService(client, &fakeUserCache{}, &fakePasswordManager{})
	ctx := platformauth.WithPrincipal(context.Background(), platformauth.Principal{UserID: idgen.New(1001)})
	got, err := service.Update(ctx, idgen.New(1002), UpdateUserBody{Nickname: new("after"), IsSuperAdmin: new(false)})
	require.NoError(t, err)
	require.Equal(t, "after", got.Nickname)
	require.Equal(t, []string{"existing-role"}, got.RoleCodes)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAdministratorValidationAcceptsOnlyCurrentRecord(t *testing.T) {
	client, mock := adminMockClient(t)
	require.ErrorContains(t, requireSuperAdmin(context.Background(), client), "requires an active super administrator")
	ctx := platformauth.WithPrincipal(context.Background(), platformauth.Principal{UserID: idgen.New(1001), IsSuperAdmin: true})
	mock.ExpectQuery(`SELECT .* FROM "sys_user" WHERE`).WithArgs(int64(1001)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1001))
	require.NoError(t, requireSuperAdmin(ctx, client))
	require.NoError(t, mock.ExpectationsWereMet())
}
