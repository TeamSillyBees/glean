package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/modules/auth"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

type noopAuthInvalidator struct{}

func (noopAuthInvalidator) NotifyChanged(context.Context, []idgen.ID, bool) error { return nil }

// TestAuthServiceFindByID 验证 auth 服务能通过 Ent 按用户 ID 读取身份并补齐角色编码。
func TestAuthServiceFindByID(t *testing.T) {
	db, mock, client := newMockEntClient(t)
	defer db.Close()

	manager, err := platformauth.NewJWTManager(platformauth.JWTConfig{
		Issuer:          "initra",
		Secret:          "integration-test-secret-0123456789abcdef",
		AccessTokenTTL:  time.Minute,
		RefreshTokenTTL: time.Hour,
	})
	require.NoError(t, err)
	guard, err := platformauth.NewMemoryLoginGuard(platformauth.LoginProtectionConfig{})
	require.NoError(t, err)
	svc, err := auth.NewService(client, stubPasswordManager{}, manager, guard, noopAuthInvalidator{}, nil)
	require.NoError(t, err)
	mock.ExpectQuery(`SELECT .*FROM "sys_user".*`).
		WillReturnRows(sysUserRows().AddRow(
			int64(1001), nil, testNow, testNow, int64(9001), int64(9001),
			"alice", "hashed:secret-123", "Alice", "13800000000", "alice@example.com",
			nil, true, true, int64(1), 1,
		))
	mock.ExpectQuery(`SELECT .*FROM "sys_role".*`).
		WillReturnRows(sqlmock.NewRows([]string{"code"}).AddRow("admin"))

	vo, err := svc.Me(context.Background(), idgen.New(1001))

	require.NoError(t, err)
	require.Equal(t, idgen.New(1001), vo.UserID)
	require.Equal(t, "alice", vo.Username)
	require.Equal(t, "Alice", vo.Nickname)
	require.Equal(t, []string{"admin"}, vo.RoleCodes)
	require.True(t, vo.IsSuperAdmin)
	require.True(t, vo.IsEnable)
	require.NoError(t, mock.ExpectationsWereMet())
}
