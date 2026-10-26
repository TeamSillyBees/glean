package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
)

// TokenTypeAccess 表示短期访问令牌。
const TokenTypeAccess = "access"

// opaqueRefreshTokenBytes 是 refresh token 的随机字节长度，编码后不携带任何用户信息。
const opaqueRefreshTokenBytes = 32

const minimumJWTSecretBytes = 32

// JWTConfig 描述 JWT 服务的最小配置输入。
type JWTConfig struct {
	Issuer          string
	Secret          string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	Store           TokenStore
	Now             func() time.Time
}

// Principal 表示当前登录用户在请求链路中的最小身份载体。
type Principal struct {
	UserID         idgen.ID
	SessionID      string
	SessionVersion int64
	Roles          []string
	TenantID       string
	IsSuperAdmin   bool
}

// Claims 是应用统一的 JWT 声明。
type Claims struct {
	UserID         idgen.ID `json:"userId"`
	SessionID      string   `json:"sessionId"`
	SessionVersion int64    `json:"sessionVersion"`
	TokenType      string   `json:"tokenType"`
	jwt.RegisteredClaims
}

// TokenPair 表示一次签发返回的访问令牌与刷新令牌。
type TokenPair struct {
	AccessToken      string    `json:"accessToken"`
	RefreshToken     string    `json:"refreshToken"`
	AccessExpiresAt  time.Time `json:"accessExpiresAt"`
	RefreshExpiresAt time.Time `json:"refreshExpiresAt"`
}

// RefreshTokenRecord 是服务端保存的 refresh token 状态，客户端只持有 opaque token。
type RefreshTokenRecord struct {
	UserID          idgen.ID  `json:"userId"`
	SessionID       string    `json:"sessionId"`
	SessionVersion  int64     `json:"sessionVersion"`
	AccessTokenID   string    `json:"accessTokenId"`
	AccessExpiresAt time.Time `json:"accessExpiresAt"`
}

// JWTManager 负责签发 access JWT，并管理 opaque refresh token 的生命周期。
type JWTManager struct {
	issuer          string
	secret          []byte
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
	store           TokenStore
	now             func() time.Time
}

// JWT 错误哨兵用于让上层区分 token 本身无效、已被吊销和状态存储异常三类场景。
var (
	// ErrTokenInvalid 表示 token 本身签名、类型、时效或声明不合法。
	ErrTokenInvalid = errors.New("token invalid")
	// ErrTokenRevoked 表示 token 已被服务端主动吊销或 refresh token 已失效。
	ErrTokenRevoked = errors.New("token revoked")
	// ErrTokenStoreFailure 表示 Redis 等状态存储校验失败，无法安全完成 token 生命周期判断。
	ErrTokenStoreFailure = errors.New("token store failure")
)

// NewJWTManager 构造 JWT 管理器。
func NewJWTManager(cfg JWTConfig) (*JWTManager, error) {
	switch {
	case cfg.Issuer == "":
		return nil, errors.New("issuer 不能为空")
	case len([]byte(strings.TrimSpace(cfg.Secret))) < minimumJWTSecretBytes:
		return nil, fmt.Errorf("secret 长度不能少于 %d 字节", minimumJWTSecretBytes)
	case cfg.AccessTokenTTL <= 0:
		return nil, errors.New("access token ttl 必须大于 0")
	case cfg.RefreshTokenTTL <= 0:
		return nil, errors.New("refresh token ttl 必须大于 0")
	case cfg.RefreshTokenTTL <= cfg.AccessTokenTTL:
		return nil, errors.New("refresh token ttl 必须大于 access token ttl")
	default:
		if cfg.Now == nil {
			cfg.Now = time.Now
		}
		return &JWTManager{
			issuer:          cfg.Issuer,
			secret:          []byte(cfg.Secret),
			accessTokenTTL:  cfg.AccessTokenTTL,
			refreshTokenTTL: cfg.RefreshTokenTTL,
			store:           cfg.Store,
			now:             cfg.Now,
		}, nil
	}
}

// IssueTokenPair 为指定身份生成 access JWT 与 opaque refresh token。
func (m *JWTManager) IssueTokenPair(ctx context.Context, principal Principal) (TokenPair, error) {
	pair, record, err := m.newTokenPair(principal)
	if err != nil {
		return TokenPair{}, err
	}
	if m.store != nil {
		if err := m.store.StoreRefreshToken(ctx, refreshTokenFingerprint(pair.RefreshToken), record, m.refreshTokenTTL); err != nil {
			return TokenPair{}, fmt.Errorf("%w: store refresh token: %v", ErrTokenStoreFailure, err)
		}
	}
	return pair, nil
}

func (m *JWTManager) newTokenPair(principal Principal) (TokenPair, RefreshTokenRecord, error) {
	if principal.UserID <= 0 {
		return TokenPair{}, RefreshTokenRecord{}, fmt.Errorf("%w: user id is missing", ErrTokenInvalid)
	}
	if principal.SessionVersion <= 0 {
		return TokenPair{}, RefreshTokenRecord{}, fmt.Errorf("%w: session version is invalid", ErrTokenInvalid)
	}
	if strings.TrimSpace(principal.SessionID) == "" {
		principal.SessionID = uuid.NewString()
	}
	now := m.now()

	accessExpiresAt := now.Add(m.accessTokenTTL)
	refreshExpiresAt := now.Add(m.refreshTokenTTL)

	accessTokenID := uuid.NewString()
	accessToken, err := m.issueWithID(principal, TokenTypeAccess, accessTokenID, accessExpiresAt, now)
	if err != nil {
		return TokenPair{}, RefreshTokenRecord{}, err
	}

	refreshToken, err := newOpaqueRefreshToken()
	if err != nil {
		return TokenPair{}, RefreshTokenRecord{}, err
	}

	pair := TokenPair{
		AccessToken:      accessToken,
		RefreshToken:     refreshToken,
		AccessExpiresAt:  accessExpiresAt,
		RefreshExpiresAt: refreshExpiresAt,
	}
	record := RefreshTokenRecord{
		UserID:          principal.UserID,
		SessionID:       principal.SessionID,
		SessionVersion:  principal.SessionVersion,
		AccessTokenID:   accessTokenID,
		AccessExpiresAt: accessExpiresAt,
	}
	return pair, record, nil
}

// ParseAccessToken 解析并验证访问令牌。
func (m *JWTManager) ParseAccessToken(ctx context.Context, token string) (*Claims, error) {
	claims, err := m.parse(token, TokenTypeAccess)
	if err != nil {
		return nil, err
	}
	if m.store == nil {
		return claims, nil
	}

	blacklisted, err := m.store.IsAccessTokenBlacklisted(ctx, claims.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: check access token blacklist: %v", ErrTokenStoreFailure, err)
	}
	if blacklisted {
		return nil, ErrTokenRevoked
	}
	return claims, nil
}

// ValidateRefreshToken 校验 opaque refresh token 是否仍在状态存储中有效。
func (m *JWTManager) ValidateRefreshToken(ctx context.Context, token string) (*RefreshTokenRecord, error) {
	if m.store == nil {
		return nil, fmt.Errorf("%w: refresh token store is not configured", ErrTokenStoreFailure)
	}

	record, valid, err := m.store.ValidateRefreshToken(ctx, refreshTokenFingerprint(token))
	if err != nil {
		return nil, fmt.Errorf("%w: validate refresh token: %v", ErrTokenStoreFailure, err)
	}
	if !valid {
		return nil, ErrTokenRevoked
	}
	if err := validateRefreshTokenRecord(record); err != nil {
		return nil, err
	}
	return &record, nil
}

// RotateRefreshToken 原子地以已校验旧记录替换 refresh token，并吊销配对的旧 access token。
func (m *JWTManager) RotateRefreshToken(ctx context.Context, token string, expected RefreshTokenRecord, principal Principal) (TokenPair, error) {
	if m.store == nil {
		return TokenPair{}, fmt.Errorf("%w: refresh token store is not configured", ErrTokenStoreFailure)
	}
	if err := validateRefreshTokenRecord(expected); err != nil {
		return TokenPair{}, err
	}
	if principal.UserID <= 0 || principal.UserID != expected.UserID {
		return TokenPair{}, fmt.Errorf("%w: refresh token user mismatch", ErrTokenInvalid)
	}
	if principal.SessionID != expected.SessionID || principal.SessionVersion != expected.SessionVersion {
		return TokenPair{}, fmt.Errorf("%w: refresh token session mismatch", ErrTokenInvalid)
	}

	pair, replacement, err := m.newTokenPair(principal)
	if err != nil {
		return TokenPair{}, err
	}
	rotated, err := m.store.RotateRefreshToken(
		ctx,
		refreshTokenFingerprint(token),
		expected,
		refreshTokenFingerprint(pair.RefreshToken),
		replacement,
		m.refreshTokenTTL,
		expected.AccessExpiresAt.Sub(m.now()),
	)
	if err != nil {
		return TokenPair{}, fmt.Errorf("%w: rotate refresh token: %v", ErrTokenStoreFailure, err)
	}
	if !rotated {
		return TokenPair{}, ErrTokenRevoked
	}
	return pair, nil
}

// ConsumeRefreshToken 原子消费 opaque refresh token；需要签发替代 token 时应使用 RotateRefreshToken。
func (m *JWTManager) ConsumeRefreshToken(ctx context.Context, token string) (*RefreshTokenRecord, error) {
	if m.store == nil {
		return nil, fmt.Errorf("%w: refresh token store is not configured", ErrTokenStoreFailure)
	}

	record, valid, err := m.store.ConsumeRefreshToken(ctx, refreshTokenFingerprint(token))
	if err != nil {
		return nil, fmt.Errorf("%w: consume refresh token: %v", ErrTokenStoreFailure, err)
	}
	if !valid {
		return nil, ErrTokenRevoked
	}
	if err := validateRefreshTokenRecord(record); err != nil {
		return nil, err
	}
	if err := m.blacklistAccessTokenID(ctx, record.AccessTokenID, record.AccessExpiresAt); err != nil {
		return nil, err
	}
	return &record, nil
}

// BlacklistAccessToken 把 access token 加入 Redis 黑名单，TTL 等于该 token 剩余寿命。
func (m *JWTManager) BlacklistAccessToken(ctx context.Context, token string) error {
	if m.store == nil {
		return fmt.Errorf("%w: access token blacklist store is not configured", ErrTokenStoreFailure)
	}

	claims, err := m.parse(token, TokenTypeAccess)
	if err != nil {
		return err
	}
	if claims.ExpiresAt == nil {
		return fmt.Errorf("%w: access token expires_at is missing", ErrTokenInvalid)
	}

	if err := m.blacklistAccessTokenID(ctx, claims.ID, claims.ExpiresAt.Time); err != nil {
		return err
	}
	return nil
}

// blacklistAccessTokenID 使用 access token 剩余有效期写入 jti denylist。
func (m *JWTManager) blacklistAccessTokenID(ctx context.Context, tokenID string, expiresAt time.Time) error {
	if tokenID == "" {
		return fmt.Errorf("%w: access token id is missing", ErrTokenInvalid)
	}
	if m.store == nil {
		return fmt.Errorf("%w: access token blacklist store is not configured", ErrTokenStoreFailure)
	}

	ttl := expiresAt.Sub(m.now())
	if ttl <= 0 {
		return nil
	}
	if err := m.store.BlacklistAccessToken(ctx, tokenID, ttl); err != nil {
		return fmt.Errorf("%w: blacklist access token: %v", ErrTokenStoreFailure, err)
	}
	return nil
}

// issue 根据统一 Claims 结构签发指定类型 token。
// 调用方必须传入同一个 now 值，确保 iat、nbf 和 exp 基于一致时钟计算。
func (m *JWTManager) issue(principal Principal, tokenType string, expiresAt, now time.Time) (string, error) {
	return m.issueWithID(principal, tokenType, uuid.NewString(), expiresAt, now)
}

// issueWithID 根据统一 Claims 结构签发指定类型 token，并使用调用方提供的 jti。
func (m *JWTManager) issueWithID(principal Principal, tokenType string, tokenID string, expiresAt, now time.Time) (string, error) {
	if principal.UserID <= 0 {
		return "", fmt.Errorf("%w: user id is missing", ErrTokenInvalid)
	}
	claims := Claims{
		UserID:         principal.UserID,
		SessionID:      principal.SessionID,
		SessionVersion: principal.SessionVersion,
		TokenType:      tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   principal.UserID.String(),
			ID:        tokenID,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.secret)
}

// newOpaqueRefreshToken 生成不携带声明的随机 refresh token。
func newOpaqueRefreshToken() (string, error) {
	randomBytes := make([]byte, opaqueRefreshTokenBytes)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(randomBytes), nil
}

// refreshTokenFingerprint 返回 refresh token 的稳定指纹，避免把明文 token 放进 Redis key。
func refreshTokenFingerprint(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

// validateRefreshTokenRecord 防止损坏的 Redis 记录绕过 refresh/access 一对一绑定。
func validateRefreshTokenRecord(record RefreshTokenRecord) error {
	if record.UserID <= 0 {
		return fmt.Errorf("%w: refresh token user id is missing", ErrTokenInvalid)
	}
	if record.AccessTokenID == "" {
		return fmt.Errorf("%w: refresh token access token id is missing", ErrTokenInvalid)
	}
	if strings.TrimSpace(record.SessionID) == "" {
		return fmt.Errorf("%w: refresh token session id is missing", ErrTokenInvalid)
	}
	if record.SessionVersion <= 0 {
		return fmt.Errorf("%w: refresh token session version is invalid", ErrTokenInvalid)
	}
	if record.AccessExpiresAt.IsZero() {
		return fmt.Errorf("%w: refresh token access expiration is missing", ErrTokenInvalid)
	}
	return nil
}

// parse 校验签名、签发方、算法、过期时间和 token 类型，并返回业务 Claims。
// 这里强制使用注入时钟，避免测试时钟和真实时钟混用造成生命周期判断漂移。
func (m *JWTManager) parse(token string, expectedType string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(
		token,
		&Claims{},
		func(token *jwt.Token) (any, error) {
			return m.secret, nil
		},
		jwt.WithIssuer(m.issuer),
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithTimeFunc(m.now),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTokenInvalid, err)
	}

	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, fmt.Errorf("%w: token 无效", ErrTokenInvalid)
	}
	if claims.TokenType != expectedType {
		return nil, fmt.Errorf("%w: token type mismatch: expect=%s actual=%s", ErrTokenInvalid, expectedType, claims.TokenType)
	}
	if claims.ID == "" {
		return nil, fmt.Errorf("%w: token id is missing", ErrTokenInvalid)
	}
	if claims.UserID <= 0 {
		return nil, fmt.Errorf("%w: user id is missing", ErrTokenInvalid)
	}
	if strings.TrimSpace(claims.SessionID) == "" {
		return nil, fmt.Errorf("%w: session id is missing", ErrTokenInvalid)
	}
	if claims.SessionVersion <= 0 {
		return nil, fmt.Errorf("%w: session version is invalid", ErrTokenInvalid)
	}
	if claims.Subject != claims.UserID.String() {
		return nil, fmt.Errorf("%w: token subject mismatch", ErrTokenInvalid)
	}
	return claims, nil
}

// WithPrincipal 将登录用户信息写入上下文，便于后续中间件和 service 统一读取。
func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	ctx = context.WithValue(ctx, principalContextKey{}, principal)
	if principal.UserID > 0 {
		ctx = requestctx.WithUserID(ctx, principal.UserID.String())
	}
	ctx = requestctx.WithRoles(ctx, principal.Roles)
	if principal.SessionID != "" {
		ctx = requestctx.WithSessionID(ctx, principal.SessionID)
	}
	if principal.TenantID != "" {
		ctx = requestctx.WithTenantID(ctx, principal.TenantID)
	}
	return ctx
}

type principalContextKey struct{}

// PrincipalFromContext 返回请求时解析得到的当前有效身份。
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok && principal.UserID > 0
}
