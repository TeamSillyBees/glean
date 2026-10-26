package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
)

// staticRouteSecurityLookup 始终返回同一份安全元信息，用于 JWT 中间件单元测试。
type staticRouteSecurityLookup struct {
	security RouteSecurity
}

// Lookup 返回预置安全元信息。
func (s staticRouteSecurityLookup) Lookup(method string, path string) (RouteSecurity, bool) {
	return s.security, true
}

// missingRouteSecurityLookup 模拟路由未登记安全元信息的危险场景。
type missingRouteSecurityLookup struct{}

// Lookup 始终返回未命中。
func (missingRouteSecurityLookup) Lookup(method string, path string) (RouteSecurity, bool) {
	return RouteSecurity{}, false
}

// countingRouteSecurityLookup 统计路由安全元信息查询次数，用于验证认证授权链路不会重复查表。
type countingRouteSecurityLookup struct {
	security RouteSecurity
	count    int
}

// Lookup 返回预置安全元信息并累加查询次数。
func (s *countingRouteSecurityLookup) Lookup(method string, path string) (RouteSecurity, bool) {
	s.count++
	return s.security, true
}

// TestJWTMiddlewareRejectsBlacklistedToken 验证 JWT 中间件拒绝已被黑名单吊销的 token。
func TestJWTMiddlewareRejectsBlacklistedToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := &fakeTokenStore{refreshValid: true}
	manager, err := NewJWTManager(JWTConfig{
		Issuer:          "initra",
		Secret:          "middleware-test-secret-0123456789abcdef",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		Store:           store,
	})
	require.NoError(t, err)

	pair, err := manager.IssueTokenPair(t.Context(), Principal{UserID: idgen.New(1001), SessionVersion: 1, Roles: []string{"admin"}})
	require.NoError(t, err)

	claims, err := manager.parse(pair.AccessToken, TokenTypeAccess)
	require.NoError(t, err)
	store.blacklistedTokenID = claims.ID
	store.accessBlacklisted = true

	engine := gin.New()
	engine.Use(JWTMiddleware(manager, activeResolver("admin"), staticRouteSecurityLookup{
		security: RouteSecurity{AccessMode: AccessModePermission, Permission: "system:user:read"},
	}))
	engine.GET("/protected", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	rec := httptest.NewRecorder()

	engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestJWTMiddlewareRejectsTokenWithZeroUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now()
	manager, err := NewJWTManager(JWTConfig{
		Issuer:          "initra",
		Secret:          "middleware-test-secret-0123456789abcdef",
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
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("middleware-test-secret-0123456789abcdef"))
	require.NoError(t, err)

	engine := gin.New()
	engine.Use(JWTMiddleware(manager, activeResolver(), staticRouteSecurityLookup{
		security: RouteSecurity{AccessMode: AccessModeAuthenticated},
	}))
	engine.GET("/api/v1/me", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestJWTMiddlewareRejectsDisabledCurrentIdentity 验证旧 token 不能绕过用户禁用或删除状态。
func TestJWTMiddlewareRejectsDisabledCurrentIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager, err := NewJWTManager(JWTConfig{
		Issuer:          "initra",
		Secret:          "middleware-test-secret-0123456789abcdef",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
	})
	require.NoError(t, err)
	pair, err := manager.IssueTokenPair(t.Context(), Principal{UserID: idgen.New(1001), SessionVersion: 1, Roles: []string{"admin"}})
	require.NoError(t, err)

	resolver := IdentityResolverFunc(func(context.Context, idgen.ID) (Principal, bool, error) {
		return Principal{}, false, nil
	})
	engine := gin.New()
	engine.Use(JWTMiddleware(manager, resolver, staticRouteSecurityLookup{security: RouteSecurity{AccessMode: AccessModeAuthenticated}}))
	engine.GET("/api/v1/me", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestJWTMiddlewareRejectsStaleSessionVersion 验证 logout-all 或改密后旧 access token 会被当前身份版本拒绝。
func TestJWTMiddlewareRejectsStaleSessionVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager, err := NewJWTManager(JWTConfig{
		Issuer:          "initra",
		Secret:          "middleware-test-secret-0123456789abcdef",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
	})
	require.NoError(t, err)
	pair, err := manager.IssueTokenPair(t.Context(), Principal{UserID: idgen.New(1001), SessionVersion: 1})
	require.NoError(t, err)
	resolver := IdentityResolverFunc(func(context.Context, idgen.ID) (Principal, bool, error) {
		return Principal{UserID: idgen.New(1001), SessionVersion: 2}, true, nil
	})

	engine := gin.New()
	engine.Use(JWTMiddleware(manager, resolver, staticRouteSecurityLookup{security: RouteSecurity{AccessMode: AccessModeAuthenticated}}))
	engine.GET("/api/v1/me", func(c *gin.Context) { c.Status(http.StatusOK) })
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestAuthorizationMiddlewareRejectsUnregisteredAPIRoute 验证 /api 路由缺少安全元信息时默认拒绝。
func TestAuthorizationMiddlewareRejectsUnregisteredAPIRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		ctx := WithPrincipal(c.Request.Context(), Principal{
			UserID: idgen.New(1001),
			Roles:  []string{"admin"},
		})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	engine.Use(AuthorizationMiddleware(nil, missingRouteSecurityLookup{}))
	engine.GET("/api/v1/unregistered", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/unregistered", nil)
	rec := httptest.NewRecorder()

	engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
}

// TestJWTMiddlewareRejectsMissingTokenForAuthenticatedRoute 验证 authenticated 路由仍必须提供登录态。
func TestJWTMiddlewareRejectsMissingTokenForAuthenticatedRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(JWTMiddleware(nil, nil, staticRouteSecurityLookup{
		security: RouteSecurity{AccessMode: AccessModeAuthenticated},
	}))
	engine.GET("/api/v1/me", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	rec := httptest.NewRecorder()

	engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestAuthorizationMiddlewareAllowsAuthenticatedRouteWithoutPermission 验证 authenticated 路由不进入 Casbin 授权。
func TestAuthorizationMiddlewareAllowsAuthenticatedRouteWithoutPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		ctx := WithPrincipal(c.Request.Context(), Principal{
			UserID: idgen.New(1001),
			Roles:  []string{"viewer"},
		})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	engine.Use(AuthorizationMiddleware(nil, staticRouteSecurityLookup{
		security: RouteSecurity{AccessMode: AccessModeAuthenticated},
	}))
	engine.GET("/api/v1/me", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	rec := httptest.NewRecorder()

	engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
}

// TestAuthorizationMiddlewareRejectsUnknownAccessMode 验证未知访问模式会被默认拒绝。
func TestAuthorizationMiddlewareRejectsUnknownAccessMode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		ctx := WithPrincipal(c.Request.Context(), Principal{
			UserID: idgen.New(1001),
			Roles:  []string{"admin"},
		})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	engine.Use(AuthorizationMiddleware(nil, staticRouteSecurityLookup{
		security: RouteSecurity{},
	}))
	engine.GET("/api/v1/unknown", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/unknown", nil)
	rec := httptest.NewRecorder()

	engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestAuthorizationMiddlewareRejectsZeroUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		ctx := requestctx.WithUserID(c.Request.Context(), "0")
		ctx = requestctx.WithRoles(ctx, []string{"admin"})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	engine.Use(AuthorizationMiddleware(nil, staticRouteSecurityLookup{
		security: RouteSecurity{AccessMode: AccessModePermission, Permission: "system:user:read"},
	}))
	engine.GET("/api/v1/users", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestAuthMiddlewareReusesRouteSecurityLookup 验证 JWT 与授权中间件在同一请求中复用路由安全元信息。
func TestAuthMiddlewareReusesRouteSecurityLookup(t *testing.T) {
	gin.SetMode(gin.TestMode)

	lookup := &countingRouteSecurityLookup{security: RouteSecurity{AccessMode: AccessModePublic}}
	engine := gin.New()
	engine.Use(JWTMiddleware(nil, nil, lookup))
	engine.Use(AuthorizationMiddleware(nil, lookup))
	engine.GET("/api/v1/public", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/public", nil)
	rec := httptest.NewRecorder()

	engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, 1, lookup.count)
}

// TestRequestContextMiddlewareStoresTrustedClientIP 验证登录防护读取的是可信代理规则解析后的来源 IP。
func TestRequestContextMiddlewareStoresTrustedClientIP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(RequestContextMiddleware("10.0.0.0/8"))
	engine.GET("/client-ip", func(c *gin.Context) {
		clientIP, ok := requestctx.ClientIPFromContext(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, "203.0.113.9", clientIP)
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/client-ip", nil)
	req.RemoteAddr = "10.0.0.2:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)
}

func activeResolver(roles ...string) IdentityResolver {
	return IdentityResolverFunc(func(_ context.Context, userID idgen.ID) (Principal, bool, error) {
		return Principal{UserID: userID, SessionVersion: 1, Roles: append([]string(nil), roles...)}, true, nil
	})
}
