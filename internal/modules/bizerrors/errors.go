package bizerrors

import (
	"context"
	"net/http"

	apperrors "github.com/teamsillybees/glean/internal/platform/errors"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// 示例业务错误码独立于框架公共错误码，避免业务语义泄漏到 pkg API。
const (
	// CodeUserNotFound 表示指定用户不存在。
	CodeUserNotFound apperrors.Code = "USER_NOT_FOUND"
	// CodeLoginFailed 表示登录凭证校验失败。
	CodeLoginFailed apperrors.Code = "LOGIN_FAILED"
)

// Option 表示业务错误构造选项。
type Option = apperrors.Option

// WithDetail 为业务错误补充单个详情字段。
// Details 面向客户端，只能放允许返回给前端的信息。
func WithDetail(key string, value any) Option {
	return apperrors.WithDetail(key, value)
}

// WithCauseAttr 为底层 cause 补充内部排障字段，只进入日志，不进入 HTTP 响应。
func WithCauseAttr(key string, value any) Option {
	return apperrors.WithCauseAttr(key, value)
}

// BadRequest 创建统一的请求参数错误。
func BadRequest(message string, opts ...Option) error {
	return newError(apperrors.CodeBadRequest, message, opts...)
}

// Unauthorized 创建统一的未授权错误。
func Unauthorized(message string, opts ...Option) error {
	return newError(apperrors.CodeUnauthorized, message, opts...)
}

// Internal 创建统一的服务端内部错误。
func Internal(message string, opts ...Option) error {
	return newError(apperrors.CodeInternalError, message, opts...)
}

// WrapBadRequestContext 将底层错误封装为请求参数错误，并自动写入 trace 元数据。
func WrapBadRequestContext(ctx context.Context, err error, message string, opts ...Option) error {
	return wrapContext(ctx, err, apperrors.CodeBadRequest, message, nil, opts...)
}

// WrapNotFoundContext 将底层错误封装为资源不存在错误，并自动写入 trace 元数据。
func WrapNotFoundContext(ctx context.Context, err error, message string, opts ...Option) error {
	return wrapContext(ctx, err, apperrors.CodeNotFound, message, nil, opts...)
}

// WrapInternalContext 将底层错误封装为服务端内部错误，并自动写入 trace 元数据。
func WrapInternalContext(ctx context.Context, err error, message string, opts ...Option) error {
	return wrapContext(ctx, err, apperrors.CodeInternalError, message, nil, opts...)
}

// WrapDBContext 将底层错误封装为数据库错误，并自动写入 trace 元数据。
func WrapDBContext(ctx context.Context, err error, message string, opts ...Option) error {
	return wrapContext(ctx, err, apperrors.CodeDBError, message, dbDefaults(), opts...)
}

// WrapCacheContext 将底层错误封装为缓存错误，并自动写入 trace 元数据。
func WrapCacheContext(ctx context.Context, err error, message string, opts ...Option) error {
	return wrapContext(ctx, err, apperrors.CodeCacheError, message, cacheDefaults(), opts...)
}

// WrapStorageContext 将底层错误封装为对象存储错误，并自动写入 trace 元数据。
func WrapStorageContext(ctx context.Context, err error, message string, opts ...Option) error {
	return wrapContext(ctx, err, apperrors.CodeInternalError, message, storageDefaults(), opts...)
}

// WrapHTTPClientContext 将底层错误封装为下游 HTTP 调用错误，并自动写入 trace 元数据。
func WrapHTTPClientContext(ctx context.Context, err error, message string, opts ...Option) error {
	return wrapContext(ctx, err, apperrors.CodeInternalError, message, httpClientDefaults(), opts...)
}

// WrapTaskContext 将底层错误封装为任务队列错误，并自动写入 trace 元数据。
func WrapTaskContext(ctx context.Context, err error, message string, opts ...Option) error {
	return wrapContext(ctx, err, apperrors.CodeInternalError, message, taskDefaults(), opts...)
}

// LoginFailed 创建统一的登录失败错误。
func LoginFailed() error {
	return newError(CodeLoginFailed, "login failed", apperrors.WithStatus(http.StatusUnauthorized))
}

// UserNotFound 创建统一的用户不存在错误。
func UserNotFound(userID idgen.ID) error {
	return newError(
		CodeUserNotFound,
		"user not found",
		apperrors.WithStatus(http.StatusNotFound),
		apperrors.WithDetail("userId", userID),
	)
}

// newError 创建业务源头错误，并跳过 bizerrors 自身 helper。
func newError(code apperrors.Code, message string, opts ...Option) error {
	return apperrors.New(code, message, withDefaults(nil, opts)...)
}

// wrapContext 包装底层错误并写入 trace 元数据，统一应用领域默认元数据和调用帧跳过策略。
func wrapContext(ctx context.Context, err error, code apperrors.Code, message string, defaults []Option, opts ...Option) error {
	return apperrors.WrapContext(ctx, err, code, message, withDefaults(defaults, opts)...)
}

// withDefaults 合并领域默认元数据和调用方选项，并跳过当前业务错误 helper 与私有转发层。
func withDefaults(defaults []Option, opts []Option) []Option {
	merged := make([]Option, 0, len(defaults)+len(opts)+1)
	merged = append(merged, defaults...)
	merged = append(merged, opts...)
	merged = append(merged, apperrors.WithCallerSkip(2))
	return merged
}

// dbDefaults 返回数据库错误的默认日志元数据。
func dbDefaults() []Option {
	return []Option{
		apperrors.WithCauseDomain(apperrors.DomainDB),
		apperrors.WithCauseHint(apperrors.HintDBConnection),
	}
}

// cacheDefaults 返回缓存错误的默认日志元数据。
func cacheDefaults() []Option {
	return []Option{
		apperrors.WithCauseDomain(apperrors.DomainCache),
		apperrors.WithCauseHint(apperrors.HintRedisTimeout),
	}
}

// storageDefaults 返回对象存储错误的默认日志元数据。
func storageDefaults() []Option {
	return []Option{
		apperrors.WithCauseDomain(apperrors.DomainStorage),
		apperrors.WithCauseHint(apperrors.HintStorageUpload),
	}
}

// httpClientDefaults 返回下游 HTTP 调用错误的默认日志元数据。
func httpClientDefaults() []Option {
	return []Option{
		apperrors.WithCauseDomain(apperrors.DomainHTTPClient),
		apperrors.WithCauseHint(apperrors.HintHTTPClientCall),
	}
}

// taskDefaults 返回任务队列错误的默认日志元数据。
func taskDefaults() []Option {
	return []Option{
		apperrors.WithCauseDomain(apperrors.DomainTask),
	}
}
