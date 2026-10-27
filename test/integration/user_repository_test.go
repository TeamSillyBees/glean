package integration_test

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/modules/user"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/entx"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/pagination"
)

// stubPasswordManager 在集成测试中提供简单的密码哈希，不连接真实 bcrypt。
type stubPasswordManager struct{}

func (stubPasswordManager) Hash(password string) (string, error) {
	return "hashed:" + password, nil
}
func (stubPasswordManager) Compare(hash string, password string) error {
	return nil
}

// stubUserCache 在集成测试中始终模拟缓存未命中。
type stubUserCache struct{}

func (stubUserCache) Get(context.Context, idgen.ID) (*user.User, bool, error) {
	return nil, false, nil
}
func (stubUserCache) Set(context.Context, *user.User) error  { return nil }
func (stubUserCache) Delete(context.Context, idgen.ID) error { return nil }

// TestUserServiceCreateUsesEntHooks 验证创建用户后自动填充 ID 和审计字段。
func TestUserServiceCreateUsesEntHooks(t *testing.T) {
	db, mock, client := newMockEntClient(t)
	defer db.Close()

	service := user.NewService(client, stubUserCache{}, stubPasswordManager{})
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "sys_user".*ORDER BY "sys_user"\."id" ASC FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "is_super_admin", "is_enable"}).AddRow(int64(9001), true, true))
	mock.ExpectQuery(`SELECT .*FROM "sys_user".*`).WithArgs(int64(9001)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(9001)))
	mock.ExpectQuery(`SELECT .*FROM "sys_role".*FOR SHARE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "code"}).AddRow(int64(1002), "admin"))
	mock.ExpectExec(`INSERT INTO "sys_user" \(.*"id".*\)`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "sys_user_role".*`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT .*FROM "sys_user_role".*`).WillReturnRows(sysUserRoleRows())
	mock.ExpectExec(`INSERT INTO "sys_user_role" \(.*"id".*\)`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	isEnable := true
	ctx := platformauth.WithPrincipal(context.Background(), platformauth.Principal{UserID: idgen.New(9001), IsSuperAdmin: true})
	vo, err := service.Create(ctx, user.CreateUserBody{
		Username:     "alice",
		Password:     "secret-123",
		Nickname:     "Alice",
		Phone:        "13800000000",
		Email:        "alice@example.com",
		RoleCodes:    []string{"admin"},
		IsSuperAdmin: true,
		IsEnable:     &isEnable,
		SortID:       1,
	})

	require.NoError(t, err)
	require.NotZero(t, vo.ID)
	require.Equal(t, "alice", vo.Username)
	require.Equal(t, "Alice", vo.Nickname)
	require.Equal(t, []string{"admin"}, vo.RoleCodes)
	require.True(t, vo.IsSuperAdmin)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestUserServiceFindByID 验证 user 服务能按 ID 查询用户并补齐角色编码。
func TestUserServiceFindByID(t *testing.T) {
	db, mock, client := newMockEntClient(t)
	defer db.Close()

	service := user.NewService(client, stubUserCache{}, stubPasswordManager{})
	mock.ExpectQuery(`SELECT .*FROM "sys_user".*`).
		WillReturnRows(sysUserRows().AddRow(
			int64(1001), nil, testNow, testNow, int64(9001), int64(9001),
			"alice", "hashed:secret-123", "Alice", "13800000000", "alice@example.com",
			"https://example.com/avatar.png", true, true, int64(1), 1,
		))
	mock.ExpectQuery(`SELECT .*FROM "sys_role".*`).
		WillReturnRows(sqlmock.NewRows([]string{"code"}).AddRow("admin").AddRow("viewer"))

	vo, err := service.Get(context.Background(), idgen.New(1001))

	require.NoError(t, err)
	require.Equal(t, idgen.New(1001), vo.ID)
	require.Equal(t, "Alice", vo.Nickname)
	require.Equal(t, []string{"admin", "viewer"}, vo.RoleCodes)
	require.True(t, vo.IsSuperAdmin)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestUserServicePageLoadsRolesInBatch 验证分页查询和批量角色加载。
func TestUserServicePageLoadsRolesInBatch(t *testing.T) {
	db, mock, client := newMockEntClient(t)
	defer db.Close()

	service := user.NewService(client, stubUserCache{}, stubPasswordManager{})
	mock.ExpectQuery(`SELECT COUNT.*FROM "sys_user".*`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT .*FROM "sys_user".*`).
		WillReturnRows(sysUserRows().AddRow(
			int64(1001), nil, testNow, testNow, int64(9001), int64(9001),
			"alice", "hashed:secret-123", "Alice", nil, nil, nil, false, true, int64(1), 1,
		))
	mock.ExpectQuery(`SELECT .*FROM "sys_role".*`).
		WillReturnRows(sysRoleRows().AddRow(
			int64(1002), nil, testNow, testNow, int64(0), int64(0),
			"viewer", "只读用户", nil, true, true, 10,
		))
	mock.ExpectQuery(`SELECT .*FROM "sys_user_role".*`).
		WillReturnRows(sysUserRoleRows().AddRow(int64(2001), nil, int64(1001), int64(1002), int64(9001), testNow))

	pageVO, err := service.Page(context.Background(), user.PageUsersQuery{
		PageQuery: pagination.PageQuery{Page: 1, PageSize: 20},
	})

	require.NoError(t, err)
	require.Equal(t, int32(1), pageVO.Total)
	require.Len(t, pageVO.Items, 1)
	require.Equal(t, []string{"viewer"}, pageVO.Items[0].RoleCodes)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestUserServiceDeleteSoftDeletesUserAndRoles 验证删除用户先锁定父记录，再同步软删除关系。
func TestUserServiceDeleteSoftDeletesUserAndRoles(t *testing.T) {
	db, mock, client := newMockEntClient(t)
	defer db.Close()

	service := user.NewService(client, stubUserCache{}, stubPasswordManager{})
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "sys_user".*ORDER BY "sys_user"\."id" ASC FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "is_super_admin", "is_enable"}).AddRow(int64(9001), true, true))
	mock.ExpectQuery(`SELECT .*FROM "sys_user".*FOR UPDATE`).
		WillReturnRows(sysUserRows().AddRow(
			int64(1001), nil, testNow, testNow, int64(9001), int64(9001),
			"alice", "hashed:secret-123", "Alice", nil, nil, nil, false, true, int64(1), 1,
		))
	mock.ExpectExec(`UPDATE "sys_user".*`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "sys_user_role".*`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	ctx := platformauth.WithPrincipal(context.Background(), platformauth.Principal{UserID: idgen.New(9001)})
	err := service.Delete(ctx, idgen.New(1001))

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestEntClientRejectsPhysicalDelete 验证 Ent 物理删除被全局 hook 拦截。
func TestEntClientRejectsPhysicalDelete(t *testing.T) {
	db, _, client := newMockEntClient(t)
	defer db.Close()

	err := client.SysUser.DeleteOneID(idgen.New(1001)).Exec(context.Background())

	require.Error(t, err)
	require.ErrorIs(t, err, entx.ErrPhysicalDeleteRejected)
}
