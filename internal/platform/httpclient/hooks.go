package httpclient

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/teamsillybees/glean/internal/platform/requestctx"
)

// RequestHook 在请求发送前接收最终的标准库 HTTP Request，可用于动态 Token、请求签名和 Header 注入。
// Hook 不应读取或记录敏感正文；返回错误会中止请求。
type RequestHook func(*http.Request) error

// FactoryOption 定制 Factory 创建的服务 Client。
type FactoryOption func(*factoryOptions)

type factoryOptions struct {
	hooks map[string][]RequestHook
	err   error
}

// WithServiceHooks 为指定远程服务注册请求 Hook，按声明顺序执行。
func WithServiceHooks(serviceName string, hooks ...RequestHook) FactoryOption {
	return func(options *factoryOptions) {
		serviceName = strings.TrimSpace(serviceName)
		if serviceName == "" {
			options.err = fmt.Errorf("%w: request hook service name cannot be empty", ErrInvalidConfig)
			return
		}
		for _, hook := range hooks {
			if hook != nil {
				options.hooks[serviceName] = append(options.hooks[serviceName], hook)
			}
		}
	}
}

func applyFactoryOptions(options []FactoryOption) (factoryOptions, error) {
	result := factoryOptions{hooks: make(map[string][]RequestHook)}
	for _, option := range options {
		if option != nil {
			option(&result)
		}
	}
	return result, result.err
}

type hookTransport struct {
	base  http.RoundTripper
	hooks []RequestHook
}

// RoundTrip 在最终请求发送前透传上下文标识并依次执行服务 Hook。
func (t *hookTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	cloned.Header = request.Header.Clone()
	propagateRequestContext(cloned)
	for _, hook := range t.hooks {
		if err := hook(cloned); err != nil {
			return nil, &requestHookError{cause: err}
		}
	}
	return t.base.RoundTrip(cloned)
}

// CloseIdleConnections 关闭底层 Transport 的空闲连接。
func (t *hookTransport) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func propagateRequestContext(request *http.Request) {
	if request.Header.Get("X-Trace-ID") == "" {
		if traceID, ok := requestctx.TraceIDFromContext(request.Context()); ok {
			request.Header.Set("X-Trace-ID", traceID)
		}
	}
	if request.Header.Get("X-Request-ID") == "" {
		if requestID, ok := requestctx.RequestIDFromContext(request.Context()); ok {
			request.Header.Set("X-Request-ID", requestID)
		}
	}
}

type requestHookError struct {
	cause error
}

// Error 返回请求 Hook 执行失败信息。
func (e *requestHookError) Error() string {
	return "http client request hook failed: " + e.cause.Error()
}

// Unwrap 返回 Hook 原始错误。
func (e *requestHookError) Unwrap() error {
	return e.cause
}
