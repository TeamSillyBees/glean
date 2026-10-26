package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	apperrors "github.com/teamsillybees/glean/internal/platform/errors"
	"github.com/teamsillybees/glean/internal/platform/logx"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
)

// HTTP 头常量集中定义，避免不同中间件拼写不一致。
const (
	headerAuthorization = "Authorization"
	headerRequestID     = "X-Request-ID"
	headerTraceID       = "X-Trace-ID"
	routeSecurityKey    = "initra.route_security"
)

// routeSecurityResult 缓存单次请求的路由安全元信息，避免认证和授权中间件重复查表。
type routeSecurityResult struct {
	security RouteSecurity
	found    bool
}

// RequestContextMiddleware 为每个请求补齐 request_id 与 trace_id。
// 该中间件必须排在日志、CORS、认证与授权之前，确保即使请求被提前拒绝，也能留下完整链路信息。
func RequestContextMiddleware(trustedProxies ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := firstNonEmpty(c.GetHeader(headerRequestID), uuid.NewString())
		traceID := firstNonEmpty(c.GetHeader(headerTraceID), requestID)
		ctx := requestctx.WithRequestID(c.Request.Context(), requestID)
		ctx = requestctx.WithTraceID(ctx, traceID)
		ctx = requestctx.WithTrustedProxies(ctx, trustedProxies...)
		requestWithContext := c.Request.WithContext(ctx)
		ctx = requestctx.WithClientIP(ctx, requestctx.ClientIP(requestWithContext))
		c.Request = c.Request.WithContext(ctx)
		c.Header(headerRequestID, requestID)
		c.Header(headerTraceID, traceID)

		c.Next()
	}
}

// JWTMiddleware 在进入业务处理前完成 JWT 身份解析。
func JWTMiddleware(manager *JWTManager, resolver IdentityResolver, lookup RouteSecurityLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodOptions || shouldSkipAuth(c.Request.URL.Path) {
			c.Next()
			return
		}

		security, ok := resolveRouteSecurity(c, lookup)
		if !ok && isAPIRoute(c.Request.URL.Path) {
			writeError(c, apperrors.New(apperrors.CodeForbidden, "route security metadata is missing"))
			return
		}
		if ok {
			switch security.AccessMode {
			case AccessModePublic:
				c.Next()
				return
			case AccessModeAuthenticated, AccessModePermission:
			default:
				writeError(c, apperrors.New(apperrors.CodeForbidden, "route security metadata is incomplete"))
				return
			}
		}

		token, ok := bearerToken(c.GetHeader(headerAuthorization))
		if !ok {
			writeError(c, apperrors.New(apperrors.CodeUnauthorized, "authorization token is required"))
			return
		}

		if manager == nil || resolver == nil {
			writeError(c, apperrors.New(apperrors.CodeInternalError, "authorization identity service is unavailable"))
			return
		}

		claims, err := manager.ParseAccessToken(c.Request.Context(), token)
		if err != nil {
			if errors.Is(err, ErrTokenStoreFailure) {
				writeError(c, apperrors.WrapContext(c.Request.Context(), err, apperrors.CodeInternalError, "validate authorization token failed",
					apperrors.WithCauseDomain(apperrors.DomainAuth),
					apperrors.WithCauseHint(apperrors.HintJWTValidation),
				))
				return
			}
			writeError(c, apperrors.New(apperrors.CodeUnauthorized, "authorization token is invalid"))
			return
		}
		if claims.UserID <= 0 {
			writeError(c, apperrors.New(apperrors.CodeUnauthorized, "authorization token is invalid"))
			return
		}

		principal, found, err := resolver.ResolvePrincipal(c.Request.Context(), claims.UserID)
		if err != nil {
			writeError(c, apperrors.WrapContext(c.Request.Context(), err, apperrors.CodeInternalError, "resolve authorization identity failed",
				apperrors.WithCauseDomain(apperrors.DomainAuth),
			))
			return
		}
		if !found || principal.UserID != claims.UserID {
			writeError(c, apperrors.New(apperrors.CodeUnauthorized, "authorization identity is disabled or missing"))
			return
		}
		if principal.SessionVersion != claims.SessionVersion {
			writeError(c, apperrors.New(apperrors.CodeUnauthorized, "authorization session is invalid"))
			return
		}
		principal.SessionID = claims.SessionID
		c.Request = c.Request.WithContext(WithPrincipal(c.Request.Context(), principal))

		c.Next()
	}
}

// AuthorizationMiddleware 在身份校验后，基于 Casbin 策略继续完成授权判断。
func AuthorizationMiddleware(enforcer *casbin.SyncedEnforcer, lookup RouteSecurityLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodOptions || shouldSkipAuth(c.Request.URL.Path) {
			c.Next()
			return
		}

		security, ok := resolveRouteSecurity(c, lookup)
		if !ok {
			if isAPIRoute(c.Request.URL.Path) {
				writeError(c, apperrors.New(apperrors.CodeForbidden, "route security metadata is missing"))
				return
			}
			c.Next()
			return
		}
		switch security.AccessMode {
		case AccessModePublic, AccessModeAuthenticated:
			c.Next()
			return
		case AccessModePermission:
		default:
			writeError(c, apperrors.New(apperrors.CodeForbidden, "route security metadata is incomplete"))
			return
		}
		if strings.TrimSpace(security.Permission) == "" {
			writeError(c, apperrors.New(apperrors.CodeForbidden, "route security metadata is incomplete"))
			return
		}

		principal, ok := PrincipalFromContext(c.Request.Context())
		if !ok {
			writeError(c, apperrors.New(apperrors.CodeUnauthorized, "authorization identity is missing"))
			return
		}
		if principal.IsSuperAdmin {
			c.Next()
			return
		}
		if enforcer == nil {
			writeError(c, apperrors.New(apperrors.CodeInternalError, "authorization policy service is unavailable"))
			return
		}

		for _, role := range principal.Roles {
			allowed, err := enforcer.Enforce(role, security.Permission)
			if err != nil {
				writeError(c, apperrors.WrapContext(c.Request.Context(), err, apperrors.CodeInternalError, "authorize request failed",
					apperrors.WithCauseDomain(apperrors.DomainAuth),
				))
				return
			}
			if allowed {
				c.Next()
				return
			}
		}

		writeError(c, apperrors.New(apperrors.CodeForbidden, "permission denied"))
	}
}

// RequestLogxMiddleware 输出结构化请求日志。
func RequestLogxMiddleware(logger *logx.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		if logger == nil {
			return
		}

		fields := []logx.Field{
			logx.String("path", c.Request.URL.Path),
			logx.String("method", c.Request.Method),
			logx.Int("status", c.Writer.Status()),
			logx.Int64("latency_ms", time.Since(start).Milliseconds()),
		}

		if userID, ok := requestctx.UserIDFromContext(c.Request.Context()); ok && strings.TrimSpace(userID) != "" {
			fields = append(fields, logx.String("user_id", userID))
		}

		if err := ginLastError(c); err != nil {
			logger.Error(c.Request.Context(), "http request failed", err, append(fields, ginErrorFields(c)...)...)
		} else {
			logger.Info(c.Request.Context(), "http request completed", fields...)
		}
	}
}

// RecoveryLogxMiddleware 捕获 panic 并统一转换成标准错误响应。
func RecoveryLogxMiddleware(logger *logx.Logger) gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		if logger != nil {
			err := apperrors.WrapContext(c.Request.Context(), fmt.Errorf("panic recovered: %v", recovered), apperrors.CodeInternalError, "internal error",
				apperrors.WithCauseDomain(apperrors.DomainServer),
				apperrors.WithCauseAttr("panic", fmt.Sprint(recovered)),
			)
			logger.Error(c.Request.Context(), "panic recovered", err,
				logx.String("request_id", requestIDFromContext(c.Request.Context())),
				logx.String("path", c.Request.URL.Path),
				logx.String("method", c.Request.Method),
			)
		}
		writeError(c, apperrors.New(apperrors.CodeInternalError, "internal error"))
	})
}

// resolveRouteSecurity 获取路由安全元信息，并在同一请求内复用查询结果。
func resolveRouteSecurity(c *gin.Context, lookup RouteSecurityLookup) (RouteSecurity, bool) {
	if cached, exists := c.Get(routeSecurityKey); exists {
		if result, ok := cached.(routeSecurityResult); ok {
			return result.security, result.found
		}
	}
	if lookup == nil {
		return RouteSecurity{}, false
	}

	routePath := c.FullPath()
	if routePath == "" {
		routePath = c.Request.URL.Path
	}
	security, ok := lookup.Lookup(c.Request.Method, routePath)
	c.Set(routeSecurityKey, routeSecurityResult{security: security, found: ok})
	return security, ok
}

// shouldSkipAuth 判断无需认证的系统级公开路径。
// 这些路径必须保持较小集合，业务公开接口应通过 RouteSecurity.AccessMode 显式声明。
func shouldSkipAuth(path string) bool {
	return path == "/health" ||
		path == "/ready" ||
		path == "/version" ||
		strings.HasPrefix(path, "/docs") ||
		strings.HasPrefix(path, "/openapi") ||
		strings.HasPrefix(path, "/schemas")
}

// isAPIRoute 判断请求是否属于业务 API 命名空间，供 fail-closed 策略统一使用。
func isAPIRoute(path string) bool {
	return strings.HasPrefix(path, "/api/")
}

// bearerToken 从 Authorization 头中提取 Bearer token，并拒绝空 token 或错误认证方案。
func bearerToken(header string) (string, bool) {
	parts := strings.SplitN(strings.TrimSpace(header), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return "", false
	}
	return strings.TrimSpace(parts[1]), true
}

// writeError 将平台错误统一转换为带 traceId 的 JSON 响应。
func writeError(c *gin.Context, err error) {
	if err != nil {
		_ = c.Error(err)
	}
	traceID, _ := requestctx.TraceIDFromContext(c.Request.Context())
	status, body := apperrors.ToHTTP(err, traceID)
	c.AbortWithStatusJSON(status, body)
}

func requestIDFromContext(ctx context.Context) string {
	requestID, _ := requestctx.RequestIDFromContext(ctx)
	return requestID
}

func ginErrorFields(c *gin.Context) []logx.Field {
	err := ginLastError(c)
	if err == nil {
		return nil
	}

	fields := []logx.Field{
		logx.Int("error_count", len(c.Errors)),
	}
	if len(c.Errors) > 1 {
		messages := make([]string, 0, len(c.Errors))
		for _, item := range c.Errors {
			if item != nil && item.Err != nil {
				messages = append(messages, item.Err.Error())
			}
		}
		fields = append(fields, logx.Strings("errors", messages))
	}
	return fields
}

func ginLastError(c *gin.Context) error {
	if c == nil || len(c.Errors) == 0 {
		return nil
	}
	last := c.Errors.Last()
	if last == nil {
		return nil
	}
	return last.Err
}

// firstNonEmpty 返回第一个非空字符串，用于请求 ID 和 trace ID 的兜底选择。
func firstNonEmpty(candidates ...string) string {
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) != "" {
			return candidate
		}
	}
	return ""
}
