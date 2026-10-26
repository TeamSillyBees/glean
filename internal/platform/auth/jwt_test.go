package auth

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
)

// fakeTokenStore 记录 JWTManager 对 TokenStore 的调用，便于断言 Redis TTL 和黑名单行为。
type fakeTokenStore struct {
	storedRefreshTokenID string
	storedRefreshRecord  RefreshTokenRecord
	storedRefreshTTL     time.Duration
	revokedRefreshIDs    []string
	blacklistedTokenID   string
	blacklistedTTL       time.Duration
	refreshValid         bool
	accessBlacklisted    bool
}

// StoreRefreshToken 保存 refresh token 写入参数。
func (f *fakeTokenStore) StoreRefreshToken(_ context.Context, tokenID string, record RefreshTokenRecord, ttl time.Duration) error {
	f.storedRefreshTokenID = tokenID
	f.storedRefreshRecord = record
	f.storedRefreshTTL = ttl
	return nil
}

// ValidateRefreshToken 模拟 Redis 中 refresh token 的存在性和归属校验。
func (f *fakeTokenStore) ValidateRefreshToken(_ context.Context, tokenID string) (RefreshTokenRecord, bool, error) {
	if !f.refreshValid {
		return RefreshTokenRecord{}, false, nil
	}
	return f.storedRefreshRecord, f.storedRefreshTokenID == tokenID, nil
}

// RevokeRefreshToken 记录被撤销的 refresh token ID。
func (f *fakeTokenStore) RevokeRefreshToken(_ context.Context, tokenID string) error {
	f.revokedRefreshIDs = append(f.revokedRefreshIDs, tokenID)
	if f.storedRefreshTokenID == tokenID {
		f.storedRefreshTokenID = ""
	}
	return nil
}

// ConsumeRefreshToken 模拟 refresh token 轮转时的一次性消费语义。
func (f *fakeTokenStore) ConsumeRefreshToken(_ context.Context, tokenID string) (RefreshTokenRecord, bool, error) {
	if !f.refreshValid {
		return RefreshTokenRecord{}, false, nil
	}
	if f.storedRefreshTokenID != tokenID {
		return RefreshTokenRecord{}, false, nil
	}
	record := f.storedRefreshRecord
	f.revokedRefreshIDs = append(f.revokedRefreshIDs, tokenID)
	f.storedRefreshTokenID = ""
	return record, true, nil
}

// RotateRefreshToken 模拟 refresh token 的原子校验、替换和旧 access token 吊销。
func (f *fakeTokenStore) RotateRefreshToken(
	_ context.Context,
	oldTokenID string,
	expected RefreshTokenRecord,
	newTokenID string,
	replacement RefreshTokenRecord,
	ttl time.Duration,
	blacklistTTL time.Duration,
) (bool, error) {
	if !f.refreshValid || f.storedRefreshTokenID != oldTokenID || !sameRefreshTokenRecord(f.storedRefreshRecord, expected) {
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

// BlacklistAccessToken 记录 access token 黑名单写入参数。
func (f *fakeTokenStore) BlacklistAccessToken(_ context.Context, tokenID string, ttl time.Duration) error {
	f.blacklistedTokenID = tokenID
	f.blacklistedTTL = ttl
	f.accessBlacklisted = true
	return nil
}

// IsAccessTokenBlacklisted 模拟 access token 黑名单命中判断。
func (f *fakeTokenStore) IsAccessTokenBlacklisted(_ context.Context, tokenID string) (bool, error) {
	return f.accessBlacklisted && f.blacklistedTokenID == tokenID, nil
}

// TestWithPrincipalInjectsRequestContextUserValues 验证登录身份会同步写入通用请求上下文。
func TestWithPrincipalInjectsRequestContextUserValues(t *testing.T) {
	ctx := requestctx.WithTraceID(context.Background(), "trace-1")
	ctx = WithPrincipal(ctx, Principal{
		UserID:    idgen.New(1001),
		SessionID: "session-1",
		Roles:     []string{"admin"},
		TenantID:  "tenant-1",
	})

	userID, ok := requestctx.UserIDFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "1001", userID)
	roles, ok := requestctx.RolesFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, []string{"admin"}, roles)
	tenantID, ok := requestctx.TenantIDFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "tenant-1", tenantID)
	sessionID, ok := requestctx.SessionIDFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "session-1", sessionID)
	traceID, ok := requestctx.TraceIDFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "trace-1", traceID)
}

func TestNewJWTManagerRejectsShortSecret(t *testing.T) {
	manager, err := NewJWTManager(JWTConfig{
		Issuer:          "initra",
		Secret:          "too-short",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
	})

	require.Nil(t, manager)
	require.ErrorContains(t, err, "32 字节")
}

func TestJWTManagerRejectsZeroUserIDWhenIssuing(t *testing.T) {
	manager, err := NewJWTManager(JWTConfig{
		Issuer:          "initra",
		Secret:          "unit-test-secret-0123456789abcdef",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
	})
	require.NoError(t, err)

	_, err = manager.IssueTokenPair(context.Background(), Principal{})
	require.ErrorIs(t, err, ErrTokenInvalid)
}

// TestJWTManagerIssuesAndParsesTokens 验证 token pair 能正常签发、解析并缓存 refresh token。
func TestJWTManagerIssuesAndParsesTokens(t *testing.T) {
	store := &fakeTokenStore{refreshValid: true}
	manager, err := NewJWTManager(JWTConfig{
		Issuer:          "initra",
		Secret:          "unit-test-secret-0123456789abcdef",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		Store:           store,
	})
	require.NoError(t, err)

	pair, err := manager.IssueTokenPair(context.Background(), Principal{
		UserID:         idgen.New(1001),
		SessionVersion: 1,
		Roles:          []string{"admin"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, pair.AccessToken)
	require.NotEmpty(t, pair.RefreshToken)

	accessClaims, err := manager.ParseAccessToken(context.Background(), pair.AccessToken)
	require.NoError(t, err)
	require.Equal(t, idgen.New(1001), accessClaims.UserID)
	require.NotEmpty(t, accessClaims.SessionID)
	require.Equal(t, int64(1), accessClaims.SessionVersion)
	require.Equal(t, TokenTypeAccess, accessClaims.TokenType)
	require.NotEmpty(t, accessClaims.ID)
	unverified, _, err := new(jwt.Parser).ParseUnverified(pair.AccessToken, jwt.MapClaims{})
	require.NoError(t, err)
	require.NotContains(t, unverified.Claims.(jwt.MapClaims), "roles")
	require.NotContains(t, unverified.Claims.(jwt.MapClaims), "tenantId")

	_, _, err = new(jwt.Parser).ParseUnverified(pair.RefreshToken, &Claims{})
	require.Error(t, err)
	require.NotContains(t, pair.RefreshToken, ".")
	require.NotEqual(t, pair.RefreshToken, store.storedRefreshTokenID)
	require.Equal(t, idgen.New(1001), store.storedRefreshRecord.UserID)
	require.Equal(t, accessClaims.SessionID, store.storedRefreshRecord.SessionID)
	require.Equal(t, accessClaims.SessionVersion, store.storedRefreshRecord.SessionVersion)
	require.Equal(t, accessClaims.ID, store.storedRefreshRecord.AccessTokenID)
	require.Equal(t, pair.AccessExpiresAt.Unix(), store.storedRefreshRecord.AccessExpiresAt.Unix())
	require.Positive(t, store.storedRefreshTTL)
	require.Empty(t, store.blacklistedTokenID)
}

// TestJWTManagerRejectsExpiredAccessToken 验证过期 access token 无法通过解析。
func TestJWTManagerRejectsExpiredAccessToken(t *testing.T) {
	manager, err := NewJWTManager(JWTConfig{
		Issuer:          "initra",
		Secret:          "unit-test-secret-0123456789abcdef",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
	})
	require.NoError(t, err)

	token, err := manager.issue(Principal{UserID: idgen.New(1001)}, TokenTypeAccess, time.Now().Add(-time.Minute), time.Now().Add(-2*time.Minute))
	require.NoError(t, err)

	_, err = manager.ParseAccessToken(context.Background(), token)
	require.Error(t, err)
}

// TestJWTManagerRejectsAccessTokenWithoutExpiration 验证 JWT 必须显式携带 exp。
func TestJWTManagerRejectsAccessTokenWithoutExpiration(t *testing.T) {
	manager, err := NewJWTManager(JWTConfig{
		Issuer:          "initra",
		Secret:          "unit-test-secret-0123456789abcdef",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
	})
	require.NoError(t, err)

	claims := Claims{
		UserID:    idgen.New(1001),
		TokenType: TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:  "initra",
			Subject: "1001",
			ID:      "token-without-exp",
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("unit-test-secret-0123456789abcdef"))
	require.NoError(t, err)

	_, err = manager.ParseAccessToken(context.Background(), token)
	require.Error(t, err)
}

func TestJWTManagerRejectsAccessTokenWithZeroUserID(t *testing.T) {
	now := time.Now()
	manager, err := NewJWTManager(JWTConfig{
		Issuer:          "initra",
		Secret:          "unit-test-secret-0123456789abcdef",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		Now:             func() time.Time { return now },
	})
	require.NoError(t, err)

	claims := Claims{
		TokenType: TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "initra",
			Subject:   "0",
			ID:        "zero-user-token",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("unit-test-secret-0123456789abcdef"))
	require.NoError(t, err)

	_, err = manager.ParseAccessToken(context.Background(), token)
	require.ErrorIs(t, err, ErrTokenInvalid)
}

// TestJWTManagerRejectsBlacklistedAccessToken 验证服务端黑名单可以主动吊销 access token。
func TestJWTManagerRejectsBlacklistedAccessToken(t *testing.T) {
	store := &fakeTokenStore{refreshValid: true}
	manager, err := NewJWTManager(JWTConfig{
		Issuer:          "initra",
		Secret:          "unit-test-secret-0123456789abcdef",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		Store:           store,
	})
	require.NoError(t, err)

	pair, err := manager.IssueTokenPair(context.Background(), Principal{UserID: idgen.New(1001), SessionVersion: 1})
	require.NoError(t, err)

	claims, err := manager.parse(pair.AccessToken, TokenTypeAccess)
	require.NoError(t, err)
	store.blacklistedTokenID = claims.ID
	store.accessBlacklisted = true

	_, err = manager.ParseAccessToken(context.Background(), pair.AccessToken)
	require.Error(t, err)
}

// TestJWTManagerRejectsRefreshTokenMissingFromStore 验证未在状态存储中登记的 refresh token 会被拒绝。
func TestJWTManagerRejectsRefreshTokenMissingFromStore(t *testing.T) {
	store := &fakeTokenStore{refreshValid: false}
	manager, err := NewJWTManager(JWTConfig{
		Issuer:          "initra",
		Secret:          "unit-test-secret-0123456789abcdef",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		Store:           store,
	})
	require.NoError(t, err)

	pair, err := manager.IssueTokenPair(context.Background(), Principal{UserID: idgen.New(1001), SessionVersion: 1})
	require.NoError(t, err)

	_, err = manager.ValidateRefreshToken(context.Background(), pair.RefreshToken)
	require.Error(t, err)
}

// TestJWTManagerConsumeRefreshTokenRevokesPairedAccessToken 验证 refresh 与 access 一对一绑定，轮转时吊销旧 access jti。
func TestJWTManagerConsumeRefreshTokenRevokesPairedAccessToken(t *testing.T) {
	now := time.Now().Add(-30 * time.Minute)
	store := &fakeTokenStore{refreshValid: true}
	manager, err := NewJWTManager(JWTConfig{
		Issuer:          "initra",
		Secret:          "unit-test-secret-0123456789abcdef",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		Store:           store,
		Now: func() time.Time {
			return now
		},
	})
	require.NoError(t, err)

	pair, err := manager.IssueTokenPair(context.Background(), Principal{UserID: idgen.New(1001), SessionVersion: 1})
	require.NoError(t, err)
	accessClaims, err := manager.parse(pair.AccessToken, TokenTypeAccess)
	require.NoError(t, err)

	record, err := manager.ConsumeRefreshToken(context.Background(), pair.RefreshToken)
	require.NoError(t, err)
	require.Equal(t, idgen.New(1001), record.UserID)
	require.Equal(t, accessClaims.ID, store.blacklistedTokenID)
	require.Greater(t, store.blacklistedTTL, 50*time.Minute)

	_, err = manager.ParseAccessToken(context.Background(), pair.AccessToken)
	require.ErrorIs(t, err, ErrTokenRevoked)
}

func TestJWTManagerRotatesRefreshTokenAtomically(t *testing.T) {
	now := time.Now().Add(-30 * time.Minute)
	store := &fakeTokenStore{refreshValid: true}
	manager, err := NewJWTManager(JWTConfig{
		Issuer:          "initra",
		Secret:          "unit-test-secret-0123456789abcdef",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		Store:           store,
		Now:             func() time.Time { return now },
	})
	require.NoError(t, err)

	oldPair, err := manager.IssueTokenPair(context.Background(), Principal{UserID: idgen.New(1001), SessionVersion: 1})
	require.NoError(t, err)
	expected, err := manager.ValidateRefreshToken(context.Background(), oldPair.RefreshToken)
	require.NoError(t, err)

	newPair, err := manager.RotateRefreshToken(context.Background(), oldPair.RefreshToken, *expected, Principal{
		UserID:         idgen.New(1001),
		SessionID:      expected.SessionID,
		SessionVersion: expected.SessionVersion,
	})
	require.NoError(t, err)
	require.NotEmpty(t, newPair.RefreshToken)
	_, err = manager.ValidateRefreshToken(context.Background(), oldPair.RefreshToken)
	require.ErrorIs(t, err, ErrTokenRevoked)
	_, err = manager.ValidateRefreshToken(context.Background(), newPair.RefreshToken)
	require.NoError(t, err)
	_, err = manager.ParseAccessToken(context.Background(), oldPair.AccessToken)
	require.ErrorIs(t, err, ErrTokenRevoked)
}

// TestJWTManagerBlacklistAccessTokenStoresRemainingTTL 验证黑名单 TTL 使用 access token 剩余有效期。
func TestJWTManagerBlacklistAccessTokenStoresRemainingTTL(t *testing.T) {
	store := &fakeTokenStore{refreshValid: true}
	manager, err := NewJWTManager(JWTConfig{
		Issuer:          "initra",
		Secret:          "unit-test-secret-0123456789abcdef",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		Store:           store,
	})
	require.NoError(t, err)

	pair, err := manager.IssueTokenPair(context.Background(), Principal{UserID: idgen.New(1001), SessionVersion: 1})
	require.NoError(t, err)

	err = manager.BlacklistAccessToken(context.Background(), pair.AccessToken)
	require.NoError(t, err)
	require.NotEmpty(t, store.blacklistedTokenID)
	require.Positive(t, store.blacklistedTTL)
}

// TestJWTManagerUsesInjectedClockForRefreshTTL 验证 refresh token 缓存 TTL 基于注入时钟计算。
func TestJWTManagerUsesInjectedClockForRefreshTTL(t *testing.T) {
	now := time.Now().Add(-time.Hour)
	store := &fakeTokenStore{refreshValid: true}
	manager, err := NewJWTManager(JWTConfig{
		Issuer:          "initra",
		Secret:          "unit-test-secret-0123456789abcdef",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		Store:           store,
		Now: func() time.Time {
			return now
		},
	})
	require.NoError(t, err)

	_, err = manager.IssueTokenPair(context.Background(), Principal{UserID: idgen.New(1001), SessionVersion: 1})
	require.NoError(t, err)

	require.Equal(t, 24*time.Hour, store.storedRefreshTTL)
}

// TestJWTManagerUsesInjectedClockForBlacklistTTL 验证黑名单 TTL 基于注入时钟计算。
func TestJWTManagerUsesInjectedClockForBlacklistTTL(t *testing.T) {
	now := time.Now().Add(-30 * time.Minute)
	store := &fakeTokenStore{refreshValid: true}
	manager, err := NewJWTManager(JWTConfig{
		Issuer:          "initra",
		Secret:          "unit-test-secret-0123456789abcdef",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		Store:           store,
		Now: func() time.Time {
			return now
		},
	})
	require.NoError(t, err)

	pair, err := manager.IssueTokenPair(context.Background(), Principal{UserID: idgen.New(1001), SessionVersion: 1})
	require.NoError(t, err)

	err = manager.BlacklistAccessToken(context.Background(), pair.AccessToken)
	require.NoError(t, err)
	require.Greater(t, store.blacklistedTTL, 50*time.Minute)
}
