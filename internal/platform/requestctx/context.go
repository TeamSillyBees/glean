package requestctx

import "context"

// requestContextKey 是当前包内部使用的上下文 key 类型，避免与外部字符串 key 冲突。
type requestContextKey string

// 请求上下文 key 常量集中维护请求级数据的存储位置。
const (
	requestIDKey      requestContextKey = "request_id"
	traceIDKey        requestContextKey = "trace_id"
	userIDKey         requestContextKey = "user_id"
	rolesKey          requestContextKey = "roles"
	tenantIDKey       requestContextKey = "tenant_id"
	sessionIDKey      requestContextKey = "session_id"
	clientIPKey       requestContextKey = "client_ip"
	appIDKey          requestContextKey = "app_id"
	trustedProxiesKey requestContextKey = "trusted_proxies"
)

// Values 表示可随请求上下文传递的请求级数据。
type Values struct {
	RequestID string
	TraceID   string
	UserID    string
	Roles     []string
	TenantID  string
	SessionID string
	ClientIP  string
	AppID     string
}

// WithValues 将一组请求级数据写入上下文。
func WithValues(ctx context.Context, values Values) context.Context {
	ctx = WithRequestID(ctx, values.RequestID)
	ctx = WithTraceID(ctx, values.TraceID)
	ctx = WithUserID(ctx, values.UserID)
	ctx = WithRoles(ctx, values.Roles)
	ctx = WithTenantID(ctx, values.TenantID)
	ctx = WithSessionID(ctx, values.SessionID)
	ctx = WithClientIP(ctx, values.ClientIP)
	ctx = WithAppID(ctx, values.AppID)
	return ctx
}

// ValuesFromContext 从上下文中提取请求级数据。
func ValuesFromContext(ctx context.Context) Values {
	requestID, _ := RequestIDFromContext(ctx)
	traceID, _ := TraceIDFromContext(ctx)
	userID, _ := UserIDFromContext(ctx)
	roles, _ := RolesFromContext(ctx)
	tenantID, _ := TenantIDFromContext(ctx)
	sessionID, _ := SessionIDFromContext(ctx)
	clientIP, _ := ClientIPFromContext(ctx)
	appID, _ := AppIDFromContext(ctx)
	return Values{
		RequestID: requestID,
		TraceID:   traceID,
		UserID:    userID,
		Roles:     roles,
		TenantID:  tenantID,
		SessionID: sessionID,
		ClientIP:  clientIP,
		AppID:     appID,
	}
}

// WithRequestID 将 request_id 写入上下文。
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return withString(ctx, requestIDKey, requestID)
}

// RequestIDFromContext 从上下文中提取 request_id。
func RequestIDFromContext(ctx context.Context) (string, bool) {
	return stringFromContext(ctx, requestIDKey)
}

// WithTraceID 将 trace_id 写入上下文，方便日志、错误响应和下游 service 共用。
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return withString(ctx, traceIDKey, traceID)
}

// TraceIDFromContext 从上下文中提取 trace_id。
func TraceIDFromContext(ctx context.Context) (string, bool) {
	return stringFromContext(ctx, traceIDKey)
}

// WithUserID 将 user_id 写入上下文。
func WithUserID(ctx context.Context, userID string) context.Context {
	return withString(ctx, userIDKey, userID)
}

// UserIDFromContext 从上下文中提取 user_id。
func UserIDFromContext(ctx context.Context) (string, bool) {
	return stringFromContext(ctx, userIDKey)
}

// WithRoles 将 roles 写入上下文。
func WithRoles(ctx context.Context, roles []string) context.Context {
	return context.WithValue(ctx, rolesKey, append([]string(nil), roles...))
}

// RolesFromContext 从上下文中提取 roles。
func RolesFromContext(ctx context.Context) ([]string, bool) {
	if ctx == nil {
		return nil, false
	}
	roles, ok := ctx.Value(rolesKey).([]string)
	if !ok {
		return nil, false
	}
	return append([]string(nil), roles...), true
}

// WithTenantID 将 tenant_id 写入上下文。
func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return withString(ctx, tenantIDKey, tenantID)
}

// TenantIDFromContext 从上下文中提取 tenant_id。
func TenantIDFromContext(ctx context.Context) (string, bool) {
	return stringFromContext(ctx, tenantIDKey)
}

// WithSessionID 将 session_id 写入上下文。
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return withString(ctx, sessionIDKey, sessionID)
}

// SessionIDFromContext 从上下文中提取 session_id。
func SessionIDFromContext(ctx context.Context) (string, bool) {
	return stringFromContext(ctx, sessionIDKey)
}

// WithClientIP 将经可信代理规则解析后的客户端 IP 写入上下文。
func WithClientIP(ctx context.Context, clientIP string) context.Context {
	return withString(ctx, clientIPKey, clientIP)
}

// ClientIPFromContext 返回经可信代理规则解析后的客户端 IP。
func ClientIPFromContext(ctx context.Context) (string, bool) {
	return stringFromContext(ctx, clientIPKey)
}

// WithAppID 将 app_id 写入上下文。
func WithAppID(ctx context.Context, appID string) context.Context {
	return withString(ctx, appIDKey, appID)
}

// AppIDFromContext 从上下文中提取 app_id。
func AppIDFromContext(ctx context.Context) (string, bool) {
	return stringFromContext(ctx, appIDKey)
}

// WithTrustedProxies 将可信反向代理 IP 或 CIDR 白名单写入请求上下文。
func WithTrustedProxies(ctx context.Context, trustedProxies ...string) context.Context {
	return context.WithValue(ctx, trustedProxiesKey, append([]string(nil), trustedProxies...))
}

// TrustedProxiesFromContext 从请求上下文读取可信反向代理白名单。
func TrustedProxiesFromContext(ctx context.Context) ([]string, bool) {
	if ctx == nil {
		return nil, false
	}
	trustedProxies, ok := ctx.Value(trustedProxiesKey).([]string)
	if !ok {
		return nil, false
	}
	return append([]string(nil), trustedProxies...), true
}

func withString(ctx context.Context, key requestContextKey, value string) context.Context {
	return context.WithValue(ctx, key, value)
}

func stringFromContext(ctx context.Context, key requestContextKey) (string, bool) {
	if ctx == nil {
		return "", false
	}
	value, ok := ctx.Value(key).(string)
	return value, ok
}
