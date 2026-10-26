package apperrors

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/samber/oops"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
)

const (
	contextKeyHTTPStatus    = "initra.http_status"
	contextKeyPublicDetails = "initra.public_details"
	defaultOopsCallerSkip   = 1
)

func init() {
	oops.SourceFragmentsHidden = true
	oops.FrameSkip("", "github.com/teamsillybees/glean/internal/platform/errors.New")
	oops.FrameSkip("", "github.com/teamsillybees/glean/internal/platform/errors.Wrap")
	oops.FrameSkip("", "github.com/teamsillybees/glean/internal/platform/errors.WrapContext")
}

// Option 调整 oops 错误的公开信息、HTTP 状态或内部排障上下文。
type Option func(*options)

type options struct {
	public string
	status int
	domain string
	hint   string
	trace  string
	tags   []string
	attrs  map[string]any
	detail map[string]any
	skip   int
}

// WithPublic 设置可直接返回给用户的公开错误消息。
func WithPublic(message string) Option {
	return func(opts *options) {
		opts.public = strings.TrimSpace(message)
	}
}

// WithDetail 为错误响应补充单个公开详情字段。
func WithDetail(key string, value any) Option {
	return func(opts *options) {
		key = strings.TrimSpace(key)
		if key == "" {
			return
		}
		if opts.detail == nil {
			opts.detail = map[string]any{}
		}
		opts.detail[key] = value
	}
}

// WithDetails 为错误响应补充一组公开详情字段。
func WithDetails(details map[string]any) Option {
	return func(opts *options) {
		if len(details) == 0 {
			return
		}
		if opts.detail == nil {
			opts.detail = map[string]any{}
		}
		for key, value := range details {
			key = strings.TrimSpace(key)
			if key != "" {
				opts.detail[key] = value
			}
		}
	}
}

// WithStatus 允许调用方覆盖默认 HTTP 状态码。
func WithStatus(status int) Option {
	return func(opts *options) {
		opts.status = status
	}
}

// WithCauseDomain 为 oops 错误补充内部错误域，仅进入日志，不进入 HTTP 响应。
func WithCauseDomain(domain string) Option {
	return func(opts *options) {
		opts.domain = strings.TrimSpace(domain)
	}
}

// WithCauseHint 为 oops 错误补充排障提示，仅进入日志，不进入 HTTP 响应。
func WithCauseHint(hint string) Option {
	return func(opts *options) {
		opts.hint = strings.TrimSpace(hint)
	}
}

// WithCauseTrace 为 oops 错误补充 trace id，仅进入日志，不进入 HTTP 响应。
func WithCauseTrace(traceID string) Option {
	return func(opts *options) {
		opts.trace = strings.TrimSpace(traceID)
	}
}

// WithCallerSkip 额外跳过调用方自有错误 helper 的栈帧。
func WithCallerSkip(skip int) Option {
	return func(opts *options) {
		if skip > 0 {
			opts.skip += skip
		}
	}
}

// WithTags 为 oops 错误补充检索标签，仅进入日志。
func WithTags(tags ...string) Option {
	return func(opts *options) {
		for _, tag := range tags {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				opts.tags = append(opts.tags, tag)
			}
		}
	}
}

// WithCauseAttr 为 oops 错误补充单个内部排障字段，仅进入日志，不进入 HTTP 响应。
func WithCauseAttr(key string, value any) Option {
	return func(opts *options) {
		key = strings.TrimSpace(key)
		if key == "" {
			return
		}
		if opts.attrs == nil {
			opts.attrs = map[string]any{}
		}
		opts.attrs[key] = value
	}
}

// WithCauseAttrs 为 oops 错误补充一组内部排障字段，仅进入日志，不进入 HTTP 响应。
func WithCauseAttrs(attrs map[string]any) Option {
	return func(opts *options) {
		if len(attrs) == 0 {
			return
		}
		if opts.attrs == nil {
			opts.attrs = map[string]any{}
		}
		for key, value := range attrs {
			key = strings.TrimSpace(key)
			if key != "" {
				opts.attrs[key] = value
			}
		}
	}
}

// New 创建带稳定业务码的 oops 源头错误。
func New(code Code, message string, opts ...Option) error {
	resolved := resolveOptions(code, message, opts...)
	return applyCallerSkip(applyOptions(oops.Code(code), resolved), resolved).New(message)
}

// Wrap 使用 oops 包装底层错误；底层错误已有业务码时只追加当前层语义。
func Wrap(err error, code Code, message string, opts ...Option) error {
	if err == nil {
		return New(code, message, withCallerSkip(opts, 1)...)
	}
	hasCode := HasCode(err)
	resolved := resolveOptionsWithDefaults(code, message, !hasCode, opts...)
	return applyCallerSkip(applyOptions(builderForCode(code, !hasCode), resolved), resolved).Wrapf(err, "%s", message)
}

// WrapContext 使用 oops 包装底层错误，并自动从 context 中提取 trace id。
func WrapContext(ctx context.Context, err error, code Code, message string, opts ...Option) error {
	opts = appendTraceOption(ctx, opts)
	opts = withCallerSkip(opts, 1)
	return Wrap(err, code, message, opts...)
}

// AsOops 尝试从错误链中提取 oops 错误。
func AsOops(err error) (oops.OopsError, bool) {
	return oops.AsOops(err)
}

// HasCode 判断错误链中是否已经包含稳定业务码。
func HasCode(err error) bool {
	oopsErr, ok := oops.AsOops(err)
	return ok && oopsErr.Code() != nil && strings.TrimSpace(fmt.Sprint(oopsErr.Code())) != ""
}

// CodeOf 返回错误链中的业务码；缺失时返回 INTERNAL_ERROR。
func CodeOf(err error) Code {
	if oopsErr, ok := oops.AsOops(err); ok {
		if code := codeFromAny(oopsErr.Code()); code != "" {
			return code
		}
	}
	return CodeInternalError
}

// StatusOf 返回错误对应的 HTTP 状态码。
func StatusOf(err error) int {
	if oopsErr, ok := oops.AsOops(err); ok {
		if status, ok := statusFromContext(oopsErr.Context()); ok {
			return status
		}
	}
	return statusOf(CodeOf(err))
}

// PublicMessageOf 返回允许展示给用户的错误消息。
func PublicMessageOf(err error) string {
	status := StatusOf(err)
	defaultMessage := "internal error"
	if status < http.StatusInternalServerError {
		defaultMessage = "request failed"
	}
	return oops.GetPublic(err, defaultMessage)
}

// PublicDetailsOf 返回允许进入 HTTP 响应的公开详情。
func PublicDetailsOf(err error) map[string]any {
	oopsErr, ok := oops.AsOops(err)
	if !ok {
		return nil
	}
	details, ok := detailsFromContext(oopsErr.Context())
	if !ok {
		return nil
	}
	return SanitizeMap(details)
}

// PublicContext 返回过滤掉框架保留字段后的 oops context，供边界日志使用。
func PublicContext(err error) map[string]any {
	oopsErr, ok := oops.AsOops(err)
	if !ok {
		return nil
	}
	attrs := oopsErr.Context()
	filtered := make(map[string]any, len(attrs))
	for key, value := range attrs {
		if key == contextKeyHTTPStatus || key == contextKeyPublicDetails {
			continue
		}
		filtered[key] = value
	}
	if len(filtered) == 0 {
		return nil
	}
	return SanitizeMap(filtered)
}

// Is 保留标准库 errors.Is 的包内便捷入口，便于调用方不直接依赖 oops 实现细节。
func Is(err error, target error) bool {
	return errors.Is(err, target)
}

func appendTraceOption(ctx context.Context, opts []Option) []Option {
	if traceID, ok := requestctx.TraceIDFromContext(ctx); ok && strings.TrimSpace(traceID) != "" {
		return append(opts, WithCauseTrace(traceID))
	}
	return opts
}

// withCallerSkip 在保留原有选项的基础上追加调用帧跳过配置。
func withCallerSkip(opts []Option, skip int) []Option {
	if skip <= 0 {
		return opts
	}
	next := make([]Option, 0, len(opts)+1)
	next = append(next, opts...)
	next = append(next, WithCallerSkip(skip))
	return next
}

func resolveOptions(code Code, message string, opts ...Option) options {
	return resolveOptionsWithDefaults(code, message, true, opts...)
}

func resolveOptionsWithDefaults(code Code, message string, defaults bool, opts ...Option) options {
	resolved := options{}
	for _, opt := range opts {
		if opt != nil {
			opt(&resolved)
		}
	}
	if defaults && resolved.status == 0 {
		resolved.status = statusOf(code)
	}
	if defaults && resolved.public == "" && resolved.status < http.StatusInternalServerError {
		resolved.public = strings.TrimSpace(message)
	}
	return resolved
}

func builderForCode(code Code, useCode bool) oops.OopsErrorBuilder {
	if useCode {
		return oops.Code(code)
	}
	return oops.With()
}

func applyOptions(builder oops.OopsErrorBuilder, opts options) oops.OopsErrorBuilder {
	if opts.domain != "" {
		builder = builder.In(opts.domain)
	}
	if opts.hint != "" {
		builder = builder.Hint(opts.hint)
	}
	if opts.trace != "" {
		builder = builder.Trace(opts.trace)
	}
	if opts.public != "" {
		builder = builder.Public(opts.public)
	}
	if len(opts.tags) > 0 {
		builder = builder.Tags(opts.tags...)
	}
	attrs := make(map[string]any, len(opts.attrs)+2)
	for key, value := range opts.attrs {
		attrs[key] = value
	}
	if opts.status > 0 {
		attrs[contextKeyHTTPStatus] = opts.status
	}
	if len(opts.detail) > 0 {
		attrs[contextKeyPublicDetails] = opts.detail
	}
	if len(attrs) > 0 {
		builder = builder.With(attrPairs(attrs)...)
	}
	return builder
}

// applyCallerSkip 统一应用 oops 调用帧跳过策略，避免日志栈展示错误封装层。
func applyCallerSkip(builder oops.OopsErrorBuilder, opts options) oops.OopsErrorBuilder {
	return builder.CallerSkip(defaultOopsCallerSkip + opts.skip)
}

func attrPairs(attrs map[string]any) []any {
	pairs := make([]any, 0, len(attrs)*2)
	for key, value := range attrs {
		pairs = append(pairs, key, value)
	}
	return pairs
}

func codeFromAny(value any) Code {
	switch typed := value.(type) {
	case nil:
		return ""
	case Code:
		return typed
	case string:
		return Code(strings.TrimSpace(typed))
	default:
		return Code(strings.TrimSpace(fmt.Sprint(typed)))
	}
}

func statusFromContext(attrs map[string]any) (int, bool) {
	value, ok := attrs[contextKeyHTTPStatus]
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case int:
		return typed, typed > 0
	case int64:
		return int(typed), typed > 0
	case float64:
		return int(typed), typed > 0
	default:
		return 0, false
	}
}

func detailsFromContext(attrs map[string]any) (map[string]any, bool) {
	value, ok := attrs[contextKeyPublicDetails]
	if !ok {
		return nil, false
	}
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case map[string]string:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = item
		}
		return result, true
	default:
		return nil, false
	}
}

// statusOf 返回错误码对应的 HTTP 状态码，未知错误码默认视为服务端错误。
func statusOf(code Code) int {
	if status, ok := defaultStatuses[code]; ok {
		return status
	}
	return http.StatusInternalServerError
}
