package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	apperrors "github.com/teamsillybees/glean/internal/platform/errors"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/logx"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
)

// TestNewAppAllowsCORSPreflightBeforeAuth 验证 CORS 预检在 JWT 校验前被正确放行。
func TestNewAppAllowsCORSPreflightBeforeAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	app, err := NewApp(Options{
		Title: "initra", Version: "test", Env: "test",
		CORS: CORSConfig{
			Enabled:        true,
			AllowedOrigins: []string{"https://admin.example.test"},
			AllowedMethods: []string{http.MethodGet, http.MethodOptions},
			AllowedHeaders: []string{"Authorization"},
		},
	}, nil, nil, nil, nil)
	require.NoError(t, err)

	app.Registry.Register(http.MethodGet, "/api/v1/protected", platformauth.RouteSecurity{
		AccessMode: platformauth.AccessModePermission,
		Permission: "system:user:read",
	})
	app.Engine.GET("/api/v1/protected", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/protected", nil)
	req.Header.Set("Origin", "https://admin.example.test")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	rec := httptest.NewRecorder()

	app.Engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, "https://admin.example.test", rec.Header().Get("Access-Control-Allow-Origin"))
}

// TestNewAppLogsUnauthorizedRequests 验证认证失败请求也会经过请求日志中间件。
func TestNewAppLogsUnauthorizedRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger, logPath := newTestLogger(t)
	manager, err := platformauth.NewJWTManager(platformauth.JWTConfig{
		Issuer:          "initra",
		Secret:          "server-test-secret-at-least-32-bytes",
		AccessTokenTTL:  time.Minute,
		RefreshTokenTTL: time.Hour,
	})
	require.NoError(t, err)

	app, err := NewApp(Options{Title: "initra", Version: "test", Env: "test"}, logger, manager, nil, nil)
	require.NoError(t, err)

	app.Registry.Register(http.MethodGet, "/api/v1/protected", platformauth.RouteSecurity{
		AccessMode: platformauth.AccessModePermission,
		Permission: "system:user:read",
	})
	app.Engine.GET("/api/v1/protected", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/protected", nil)
	rec := httptest.NewRecorder()

	app.Engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.NoError(t, logger.Close())
	body := readTextFile(t, logPath)
	require.Contains(t, body, "http request failed")
	require.Contains(t, body, `"error_code":"UNAUTHORIZED"`)
	require.Equal(t, 1, strings.Count(body, `"request_id"`))
	require.Equal(t, 1, strings.Count(body, `"trace_id"`))
}

// TestNewAppLogsHumaHandlerServerError 验证 Huma handler 返回的 5xx 错误会记录内部错误链。
func TestNewAppLogsHumaHandlerServerError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger, logPath := newTestLogger(t)

	app, err := NewApp(Options{Title: "initra", Version: "test", Env: "test"}, logger, nil, nil, nil)
	require.NoError(t, err)

	app.Registry.Register(http.MethodGet, "/api/v1/fail", platformauth.RouteSecurity{AccessMode: platformauth.AccessModePublic})
	huma.Register(app.API, huma.Operation{
		OperationID: "fail",
		Method:      http.MethodGet,
		Path:        "/api/v1/fail",
	}, func(ctx context.Context, input *struct{}) (*struct{}, error) {
		return nil, apperrors.Wrap(errors.New("driver: duplicate key"), apperrors.CodeDBError, "create user failed")
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/fail", nil)
	rec := httptest.NewRecorder()

	app.Engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.NotContains(t, rec.Body.String(), "duplicate key")
	require.NoError(t, logger.Close())
	body := readTextFile(t, logPath)
	require.Contains(t, body, "http request failed")
	require.Contains(t, body, `"error_code":"DB_ERROR"`)
	require.Contains(t, body, `"error_message":"create user failed: driver: duplicate key"`)
	require.Contains(t, body, `"error_cause":"driver: duplicate key"`)
	require.Contains(t, body, `"error_stacktrace"`)
}

// TestNewAppAcceptsValidJWTForProtectedAPIRoute 验证受保护 /api 路由会完成 JWT 校验与 Casbin 授权。
func TestNewAppAcceptsValidJWTForProtectedAPIRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	manager, err := platformauth.NewJWTManager(platformauth.JWTConfig{
		Issuer:          "initra",
		Secret:          "server-test-secret-at-least-32-bytes",
		AccessTokenTTL:  time.Minute,
		RefreshTokenTTL: time.Hour,
	})
	require.NoError(t, err)

	modelPath := writeWebCasbinModel(t)
	enforcer, err := platformauth.NewEnforcer(modelPath, platformauth.PolicyLoaderFunc(func(context.Context) ([]platformauth.PolicyRule, error) {
		return []platformauth.PolicyRule{{RoleCode: "admin", PermissionCode: "system:user:read"}}, nil
	}))
	require.NoError(t, err)

	app, err := NewApp(Options{Title: "initra", Version: "test", Env: "test"}, nil, manager, resolverWithRoles("admin"), enforcer)
	require.NoError(t, err)

	app.Registry.Register(http.MethodGet, "/api/v1/protected", platformauth.RouteSecurity{
		AccessMode: platformauth.AccessModePermission,
		Permission: "system:user:read",
	})
	app.Engine.GET("/api/v1/protected", func(c *gin.Context) {
		userID, ok := requestctx.UserIDFromContext(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, "1001", userID)
		roles, ok := requestctx.RolesFromContext(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, []string{"admin"}, roles)
		c.Status(http.StatusNoContent)
	})

	pair, err := manager.IssueTokenPair(t.Context(), platformauth.Principal{
		UserID:         idgen.New(1001),
		SessionVersion: 1,
		Roles:          []string{"revoked-token-role"},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/protected", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	rec := httptest.NewRecorder()

	app.Engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
}

// TestNewAppAllowsAuthenticatedAPIRouteWithoutCasbinPolicy 验证 authenticated 路由只要求登录态，不依赖 Casbin 授权。
func TestNewAppAllowsAuthenticatedAPIRouteWithoutCasbinPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)

	manager, err := platformauth.NewJWTManager(platformauth.JWTConfig{
		Issuer:          "initra",
		Secret:          "server-test-secret-at-least-32-bytes",
		AccessTokenTTL:  time.Minute,
		RefreshTokenTTL: time.Hour,
	})
	require.NoError(t, err)

	app, err := NewApp(Options{Title: "initra", Version: "test", Env: "test"}, nil, manager, resolverWithRoles("viewer"), nil)
	require.NoError(t, err)

	app.Registry.Register(http.MethodGet, "/api/v1/me", platformauth.RouteSecurity{
		AccessMode: platformauth.AccessModeAuthenticated,
	})
	app.Engine.GET("/api/v1/me", func(c *gin.Context) {
		userID, ok := requestctx.UserIDFromContext(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, "1001", userID)
		c.Status(http.StatusNoContent)
	})

	pair, err := manager.IssueTokenPair(t.Context(), platformauth.Principal{
		UserID:         idgen.New(1001),
		SessionVersion: 1,
		Roles:          []string{"viewer"},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	rec := httptest.NewRecorder()

	app.Engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
}

// TestNewAppInjectsAuthenticatedContextIntoHumaHandler 验证登录后的 Huma ctx 包含身份和 trace 信息。
func TestNewAppInjectsAuthenticatedContextIntoHumaHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger, logPath := newTestLogger(t)
	manager, err := platformauth.NewJWTManager(platformauth.JWTConfig{
		Issuer:          "initra",
		Secret:          "server-test-secret-at-least-32-bytes",
		AccessTokenTTL:  time.Minute,
		RefreshTokenTTL: time.Hour,
	})
	require.NoError(t, err)

	app, err := NewApp(Options{Title: "initra", Version: "test", Env: "test"}, logger, manager, resolverWithRoles("viewer"), nil)
	require.NoError(t, err)

	app.Registry.Register(http.MethodGet, "/api/v1/context", platformauth.RouteSecurity{
		AccessMode: platformauth.AccessModeAuthenticated,
	})
	huma.Register(app.API, huma.Operation{
		OperationID: "context",
		Method:      http.MethodGet,
		Path:        "/api/v1/context",
	}, func(ctx context.Context, input *struct{}) (*struct{}, error) {
		userID, ok := requestctx.UserIDFromContext(ctx)
		require.True(t, ok)
		require.Equal(t, "1001", userID)
		roles, ok := requestctx.RolesFromContext(ctx)
		require.True(t, ok)
		require.Equal(t, []string{"viewer"}, roles)
		tenantID, ok := requestctx.TenantIDFromContext(ctx)
		require.True(t, ok)
		require.Equal(t, "tenant-1", tenantID)
		traceID, ok := requestctx.TraceIDFromContext(ctx)
		require.True(t, ok)
		require.Equal(t, "trace-1", traceID)
		return &struct{}{}, nil
	})

	pair, err := manager.IssueTokenPair(t.Context(), platformauth.Principal{
		UserID:         idgen.New(1001),
		SessionVersion: 1,
		Roles:          []string{"viewer"},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/context", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	req.Header.Set("X-Request-ID", "req-1")
	req.Header.Set("X-Trace-ID", "trace-1")
	rec := httptest.NewRecorder()

	app.Engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, "req-1", rec.Header().Get("X-Request-ID"))
	require.Equal(t, "trace-1", rec.Header().Get("X-Trace-ID"))

	require.NoError(t, logger.Close())
	body := readTextFile(t, logPath)
	require.Contains(t, body, "http request completed")
	require.Contains(t, body, `"request_id":"req-1"`)
	require.Contains(t, body, `"trace_id":"trace-1"`)
	require.Contains(t, body, `"user_id":"1001"`)
}

// TestNewAppAllowsPublicAPIRouteWithoutToken 验证 public 模式的 /api 路由不会触发 JWT 校验。
func TestNewAppAllowsPublicAPIRouteWithoutToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	app, err := NewApp(Options{Title: "initra", Version: "test", Env: "test"}, nil, nil, nil, nil)
	require.NoError(t, err)

	app.Registry.Register(http.MethodGet, "/api/v1/public", platformauth.RouteSecurity{AccessMode: platformauth.AccessModePublic})
	app.Engine.GET("/api/v1/public", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/public", nil)
	rec := httptest.NewRecorder()

	app.Engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
}

// TestNewAppRejectsAPIRouteMissingSecurityMetadata 验证 /api 路由缺少安全元信息时默认拒绝。
func TestNewAppRejectsAPIRouteMissingSecurityMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	app, err := NewApp(Options{Title: "initra", Version: "test", Env: "test"}, nil, nil, nil, nil)
	require.NoError(t, err)

	app.Engine.GET("/api/v1/unregistered", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/unregistered", nil)
	rec := httptest.NewRecorder()

	app.Engine.ServeHTTP(rec, req)

	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, "FORBIDDEN", body.Code)
	require.Equal(t, "route security metadata is missing", body.Message)
}

// TestRouteRegistryMatchesGinColonParams 验证 Gin 参数路径能匹配 Huma/OpenAPI 参数路径。
func TestRouteRegistryMatchesGinColonParams(t *testing.T) {
	registry := NewRouteRegistry()
	expected := platformauth.RouteSecurity{
		AccessMode: platformauth.AccessModePermission,
		Permission: "system:user:read",
	}

	registry.Register(http.MethodGet, "/api/v1/users/{id}", expected)

	actual, ok := registry.Lookup(http.MethodGet, "/api/v1/users/:id")
	require.True(t, ok)
	require.Equal(t, expected, actual)
}

// writeWebCasbinModel 为 Web 链路测试写入最小 Casbin 模型。
func writeWebCasbinModel(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	modelPath := filepath.Join(dir, "rbac_model.conf")
	model := `
[request_definition]
r = sub, perm

[policy_definition]
p = sub, perm

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = r.sub == p.sub && r.perm == p.perm
`

	require.NoError(t, os.WriteFile(modelPath, []byte(strings.TrimSpace(model)), 0o600))
	return modelPath
}

func resolverWithRoles(roles ...string) platformauth.IdentityResolver {
	return platformauth.IdentityResolverFunc(func(_ context.Context, userID idgen.ID) (platformauth.Principal, bool, error) {
		return platformauth.Principal{
			UserID:         userID,
			SessionVersion: 1,
			Roles:          append([]string(nil), roles...),
			TenantID:       "tenant-1",
		}, true, nil
	})
}

// newTestLogger 创建只写入临时 JSONL 文件的测试 logger。
func newTestLogger(t *testing.T) (*logx.Logger, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.jsonl")
	logger, err := logx.NewLogger(logx.Config{
		Console: logx.ConsoleConfig{Enabled: false},
		JSONL:   logx.JSONLConfig{Enabled: true, Level: "debug", Path: path},
		Redact:  logx.RedactConfig{Enabled: true},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = logger.Close() })
	return logger, path
}

// readTextFile 读取测试日志文件内容。
func readTextFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(content)
}
