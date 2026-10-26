package auth

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

func testUserRow(sessionVersion int64) *sqlmock.Rows {
	return sysUserRows().AddRow(
		int64(1001), nil, testNow, testNow, int64(9001), int64(9001),
		"alice", "hashed:secret-123", "Alice", nil, nil, nil, false, true, sessionVersion, 1,
	)
}

// TestServiceLogoutConsumesRefreshAndRevokesAccess 验证当前会话退出同时消费 refresh token 与吊销配对 access token。
func TestServiceLogoutConsumesRefreshAndRevokesAccess(t *testing.T) {
	store := &fakeTokenStore{refreshValid: true}
	manager, err := platformauth.NewJWTManager(platformauth.JWTConfig{
		Issuer:          "initra",
		Secret:          "auth-test-secret-0123456789abcdef",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		Store:           store,
	})
	require.NoError(t, err)
	db, _, client := newMockEntClient(t)
	t.Cleanup(func() { _ = db.Close() })
	service := newAuthServiceForTest(t, client, manager)
	pair, err := manager.IssueTokenPair(t.Context(), platformauth.Principal{UserID: idgen.New(1001), SessionVersion: 1})
	require.NoError(t, err)
	record, err := manager.ValidateRefreshToken(t.Context(), pair.RefreshToken)
	require.NoError(t, err)

	err = service.Logout(t.Context(), platformauth.Principal{
		UserID:         record.UserID,
		SessionID:      record.SessionID,
		SessionVersion: record.SessionVersion,
	}, LogoutBody{RefreshToken: pair.RefreshToken})
	require.NoError(t, err)
	_, err = manager.ValidateRefreshToken(t.Context(), pair.RefreshToken)
	require.ErrorIs(t, err, platformauth.ErrTokenRevoked)
	_, err = manager.ParseAccessToken(t.Context(), pair.AccessToken)
	require.ErrorIs(t, err, platformauth.ErrTokenRevoked)
}

// TestServiceRefreshRejectsStaleSessionVersion 验证 logout-all 或改密后旧 refresh token 不会轮转。
func TestServiceRefreshRejectsStaleSessionVersion(t *testing.T) {
	store := &fakeTokenStore{refreshValid: true}
	manager, err := platformauth.NewJWTManager(platformauth.JWTConfig{
		Issuer:          "initra",
		Secret:          "auth-test-secret-0123456789abcdef",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		Store:           store,
	})
	require.NoError(t, err)
	db, mock, client := newMockEntClient(t)
	t.Cleanup(func() { _ = db.Close() })
	service := newAuthServiceForTest(t, client, manager)
	pair, err := manager.IssueTokenPair(t.Context(), platformauth.Principal{UserID: idgen.New(1001), SessionVersion: 1})
	require.NoError(t, err)
	mock.ExpectQuery(`SELECT .*FROM "sys_user".*`).WillReturnRows(testUserRow(2))
	mock.ExpectQuery(`SELECT .*FROM "sys_role".*`).WillReturnRows(sqlmock.NewRows([]string{"code"}))

	_, err = service.Refresh(t.Context(), RefreshBody{RefreshToken: pair.RefreshToken})
	require.Error(t, err)
	require.ErrorContains(t, err, "refresh token is invalid")
	_, err = manager.ValidateRefreshToken(t.Context(), pair.RefreshToken)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestServiceLogoutAllIncrementsVersionAndInvalidatesIdentity 验证全部退出在事务提交后主动清理身份缓存。
func TestServiceLogoutAllIncrementsVersionAndInvalidatesIdentity(t *testing.T) {
	db, mock, client := newMockEntClient(t)
	t.Cleanup(func() { _ = db.Close() })
	guard, err := platformauth.NewMemoryLoginGuard(platformauth.LoginProtectionConfig{})
	require.NoError(t, err)
	invalidator := &fakeAuthorizationInvalidator{}
	service, err := NewService(client, fakePasswordVerifier{}, &fakeSessionTokenManager{}, guard, invalidator, nil)
	require.NoError(t, err)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "sys_user".*FOR UPDATE`).WillReturnRows(testUserRow(1))
	mock.ExpectExec(`UPDATE "sys_user" SET .*"session_version"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT .*FROM "sys_user" WHERE "id" =`).WillReturnRows(testUserRow(2))
	mock.ExpectCommit()

	err = service.LogoutAll(t.Context(), platformauth.Principal{UserID: idgen.New(1001), SessionVersion: 1})
	require.NoError(t, err)
	require.Equal(t, 1, invalidator.calls)
	require.Equal(t, []idgen.ID{idgen.New(1001)}, invalidator.userIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestServiceChangePasswordUpdatesHashAndRevokesAllSessions 验证改密和会话版本递增位于同一事务。
func TestServiceChangePasswordUpdatesHashAndRevokesAllSessions(t *testing.T) {
	db, mock, client := newMockEntClient(t)
	t.Cleanup(func() { _ = db.Close() })
	guard, err := platformauth.NewMemoryLoginGuard(platformauth.LoginProtectionConfig{})
	require.NoError(t, err)
	invalidator := &fakeAuthorizationInvalidator{}
	service, err := NewService(client, fakePasswordVerifier{}, &fakeSessionTokenManager{}, guard, invalidator, nil)
	require.NoError(t, err)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "sys_user".*FOR UPDATE`).WillReturnRows(testUserRow(1))
	mock.ExpectExec(`UPDATE "sys_user" SET .*"password_hash".*"session_version"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT .*FROM "sys_user" WHERE "id" =`).WillReturnRows(testUserRow(2))
	mock.ExpectCommit()

	err = service.ChangePassword(t.Context(), platformauth.Principal{UserID: idgen.New(1001), SessionVersion: 1}, ChangePasswordBody{
		CurrentPassword: "secret-123",
		NewPassword:     "new-secret-456",
	})
	require.NoError(t, err)
	require.Equal(t, 1, invalidator.calls)
	require.NoError(t, mock.ExpectationsWereMet())
}

type fakeSessionTokenManager struct{}

func (*fakeSessionTokenManager) IssueTokenPair(context.Context, platformauth.Principal) (platformauth.TokenPair, error) {
	return platformauth.TokenPair{}, nil
}

func (*fakeSessionTokenManager) ValidateRefreshToken(context.Context, string) (*platformauth.RefreshTokenRecord, error) {
	return nil, platformauth.ErrTokenRevoked
}

func (*fakeSessionTokenManager) RotateRefreshToken(context.Context, string, platformauth.RefreshTokenRecord, platformauth.Principal) (platformauth.TokenPair, error) {
	return platformauth.TokenPair{}, platformauth.ErrTokenRevoked
}

func (*fakeSessionTokenManager) ConsumeRefreshToken(context.Context, string) (*platformauth.RefreshTokenRecord, error) {
	return nil, platformauth.ErrTokenRevoked
}
