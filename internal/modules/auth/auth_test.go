package auth

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/data"
	appent "github.com/teamsillybees/glean/internal/data/ent"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

var errLoginFailed = errors.New("login failed")
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
		"id", "deleted_at", "created_at", "updated_at", "created_by", "updated_by",
		"username", "password_hash", "nickname", "phone", "email", "avatar_url",
		"is_super_admin", "is_enable", "session_version", "sort_id",
	})
}

type fakePasswordVerifier struct{}

func (fakePasswordVerifier) Hash(password string) (string, error) {
	return "hashed:" + password, nil
}

type fakeAuthorizationInvalidator struct {
	calls   int
	userIDs []idgen.ID
}

type recordingLoginGuard struct {
	checkErr     error
	checkCalls   int
	failureCalls int
	resetCalls   int
}

func (g *recordingLoginGuard) Check(context.Context, string, string) error {
	g.checkCalls++
	return g.checkErr
}

func (g *recordingLoginGuard) RecordFailure(context.Context, string) error {
	g.failureCalls++
	return nil
}

func (g *recordingLoginGuard) Reset(context.Context, string) error {
	g.resetCalls++
	return nil
}

func (f *fakeAuthorizationInvalidator) NotifyChanged(_ context.Context, userIDs []idgen.ID, _ bool) error {
	f.calls++
	f.userIDs = append([]idgen.ID(nil), userIDs...)
	return nil
}

func newAuthServiceForTest(t *testing.T, client *appent.Client, tokens tokenManager) *Service {
	t.Helper()
	guard, err := platformauth.NewMemoryLoginGuard(platformauth.LoginProtectionConfig{})
	require.NoError(t, err)
	service, err := NewService(client, fakePasswordVerifier{}, tokens, guard, &fakeAuthorizationInvalidator{}, nil)
	require.NoError(t, err)
	return service
}

func (fakePasswordVerifier) Compare(hash string, password string) error {
	if hash != "hashed:"+password {
		return errLoginFailed
	}
	return nil
}

type fakeTokenStore struct {
	storedRefreshTokenID string
	storedRefreshRecord  platformauth.RefreshTokenRecord
	storedRefreshTTL     time.Duration
	revokedRefreshIDs    []string
	blacklistedTokenID   string
	blacklistedTTL       time.Duration
	refreshValid         bool
	accessBlacklisted    bool
}

func (f *fakeTokenStore) StoreRefreshToken(_ context.Context, tokenID string, record platformauth.RefreshTokenRecord, ttl time.Duration) error {
	f.storedRefreshTokenID = tokenID
	f.storedRefreshRecord = record
	f.storedRefreshTTL = ttl
	return nil
}

func (f *fakeTokenStore) ValidateRefreshToken(_ context.Context, tokenID string) (platformauth.RefreshTokenRecord, bool, error) {
	if !f.refreshValid {
		return platformauth.RefreshTokenRecord{}, false, nil
	}
	return f.storedRefreshRecord, f.storedRefreshTokenID == tokenID, nil
}

func (f *fakeTokenStore) ConsumeRefreshToken(_ context.Context, tokenID string) (platformauth.RefreshTokenRecord, bool, error) {
	if !f.refreshValid {
		return platformauth.RefreshTokenRecord{}, false, nil
	}
	if f.storedRefreshTokenID != tokenID {
		return platformauth.RefreshTokenRecord{}, false, nil
	}
	record := f.storedRefreshRecord
	f.revokedRefreshIDs = append(f.revokedRefreshIDs, tokenID)
	f.storedRefreshTokenID = ""
	return record, true, nil
}

func (f *fakeTokenStore) RotateRefreshToken(
	_ context.Context,
	oldTokenID string,
	expected platformauth.RefreshTokenRecord,
	newTokenID string,
	replacement platformauth.RefreshTokenRecord,
	ttl time.Duration,
	blacklistTTL time.Duration,
) (bool, error) {
	if !f.refreshValid || f.storedRefreshTokenID != oldTokenID ||
		f.storedRefreshRecord.UserID != expected.UserID ||
		f.storedRefreshRecord.AccessTokenID != expected.AccessTokenID ||
		!f.storedRefreshRecord.AccessExpiresAt.Equal(expected.AccessExpiresAt) {
		return false, nil
	}
	f.revokedRefreshIDs = append(f.revokedRefreshIDs, oldTokenID)
	f.storedRefreshTokenID = newTokenID
	f.storedRefreshRecord = replacement
	f.storedRefreshTTL = ttl
	if blacklistTTL > 0 {
		f.blacklistedTokenID = expected.AccessTokenID
		f.blacklistedTTL = blacklistTTL
		f.accessBlacklisted = true
	}
	return true, nil
}

func (f *fakeTokenStore) RevokeRefreshToken(_ context.Context, tokenID string) error {
	f.revokedRefreshIDs = append(f.revokedRefreshIDs, tokenID)
	if f.storedRefreshTokenID == tokenID {
		f.storedRefreshTokenID = ""
	}
	return nil
}

func (f *fakeTokenStore) BlacklistAccessToken(_ context.Context, tokenID string, ttl time.Duration) error {
	f.blacklistedTokenID = tokenID
	f.blacklistedTTL = ttl
	f.accessBlacklisted = true
	return nil
}

func (f *fakeTokenStore) IsAccessTokenBlacklisted(_ context.Context, tokenID string) (bool, error) {
	return f.accessBlacklisted && f.blacklistedTokenID == tokenID, nil
}

func TestServiceLoginReturnsTokenPairForValidCredentials(t *testing.T) {
	store := &fakeTokenStore{refreshValid: true}
	tokenManager, err := platformauth.NewJWTManager(platformauth.JWTConfig{
		Issuer:          "initra",
		Secret:          "auth-test-secret-0123456789abcdef",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		Store:           store,
	})
	require.NoError(t, err)

	db, mock, client := newMockEntClient(t)
	defer db.Close()

	mock.ExpectQuery(`SELECT .*FROM "sys_user".*`).
		WillReturnRows(sysUserRows().AddRow(
			int64(1001), nil, testNow, testNow, int64(9001), int64(9001),
			"alice", "hashed:secret-123", "Alice", nil, nil, nil, false, true, int64(1), 1,
		))
	mock.ExpectQuery(`SELECT .*FROM "sys_role".*`).
		WillReturnRows(sqlmock.NewRows([]string{"code"}).AddRow("admin"))

	service := newAuthServiceForTest(t, client, tokenManager)
	guard := &recordingLoginGuard{}
	service.loginGuard = guard

	vo, err := service.Login(context.Background(), LoginBody{
		Username: "alice",
		Password: "secret-123",
	})
	require.NoError(t, err)
	require.Equal(t, "Alice", vo.User.Nickname)
	require.Equal(t, []string{"admin"}, vo.User.RoleCodes)
	require.NotEmpty(t, vo.AccessToken)
	require.NotEmpty(t, vo.RefreshToken)
	require.NotEmpty(t, store.storedRefreshTokenID)
	require.Empty(t, store.blacklistedTokenID)
	require.Equal(t, 1, guard.checkCalls)
	require.Zero(t, guard.failureCalls)
	require.Equal(t, 1, guard.resetCalls)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestServiceLoginReturnsUniformFailureWhenGuardRejects 验证限流或锁定不会暴露账号是否存在。
func TestServiceLoginReturnsUniformFailureWhenGuardRejects(t *testing.T) {
	db, _, client := newMockEntClient(t)
	t.Cleanup(func() { _ = db.Close() })
	service := newAuthServiceForTest(t, client, &fakeSessionTokenManager{})
	guard := &recordingLoginGuard{checkErr: platformauth.ErrLoginLocked}
	service.loginGuard = guard

	_, err := service.Login(t.Context(), LoginBody{Username: "alice", Password: "secret-123"})
	require.ErrorContains(t, err, "login failed")
	require.Equal(t, 1, guard.checkCalls)
	require.Zero(t, guard.failureCalls)
	require.Zero(t, guard.resetCalls)
}

func TestServiceRefreshIssuesNewAccessToken(t *testing.T) {
	store := &fakeTokenStore{refreshValid: true}
	tokenManager, err := platformauth.NewJWTManager(platformauth.JWTConfig{
		Issuer:          "initra",
		Secret:          "auth-test-secret-0123456789abcdef",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		Store:           store,
	})
	require.NoError(t, err)

	db, mock, client := newMockEntClient(t)
	defer db.Close()

	mock.ExpectQuery(`SELECT .*FROM "sys_user".*`).
		WillReturnRows(sysUserRows().AddRow(
			int64(1001), nil, testNow, testNow, int64(9001), int64(9001),
			"alice", "hashed:secret-123", "Alice", nil, nil, nil, false, true, int64(1), 1,
		))
	mock.ExpectQuery(`SELECT .*FROM "sys_role".*`).
		WillReturnRows(sqlmock.NewRows([]string{"code"}).AddRow("admin"))

	service := newAuthServiceForTest(t, client, tokenManager)
	pair, err := tokenManager.IssueTokenPair(context.Background(), platformauth.Principal{
		UserID:         idgen.New(1001),
		SessionVersion: 1,
		Roles:          []string{"old-role"},
	})
	require.NoError(t, err)

	vo, err := service.Refresh(context.Background(), RefreshBody{RefreshToken: pair.RefreshToken})
	require.NoError(t, err)
	require.NotEmpty(t, vo.AccessToken)
	require.NotEmpty(t, vo.RefreshToken)

	claims, err := tokenManager.ParseAccessToken(context.Background(), vo.AccessToken)
	require.NoError(t, err)
	require.Equal(t, idgen.New(1001), claims.UserID)

	_, err = tokenManager.ValidateRefreshToken(context.Background(), pair.RefreshToken)
	require.Error(t, err)

	_, err = tokenManager.ValidateRefreshToken(context.Background(), vo.RefreshToken)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestServiceRefreshDoesNotConsumeTokenWhenUserLookupFails(t *testing.T) {
	store := &fakeTokenStore{refreshValid: true}
	tokenManager, err := platformauth.NewJWTManager(platformauth.JWTConfig{
		Issuer:          "initra",
		Secret:          "auth-test-secret-0123456789abcdef",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		Store:           store,
	})
	require.NoError(t, err)

	db, mock, client := newMockEntClient(t)
	defer db.Close()
	mock.ExpectQuery(`SELECT .*FROM "sys_user".*`).
		WillReturnRows(sysUserRows())

	service := newAuthServiceForTest(t, client, tokenManager)
	pair, err := tokenManager.IssueTokenPair(context.Background(), platformauth.Principal{UserID: idgen.New(1001), SessionVersion: 1})
	require.NoError(t, err)

	_, err = service.Refresh(context.Background(), RefreshBody{RefreshToken: pair.RefreshToken})
	require.Error(t, err)
	_, err = tokenManager.ValidateRefreshToken(context.Background(), pair.RefreshToken)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
