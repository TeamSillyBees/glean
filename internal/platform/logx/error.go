package logx

import (
	"errors"
	"fmt"
	"strings"

	"github.com/samber/oops"
	apperrors "github.com/teamsillybees/glean/internal/platform/errors"
	"go.uber.org/zap"
)

// ErrorInfo 是从普通 error 和 oops error 中提取出的日志模型。
type ErrorInfo struct {
	Message    string
	Type       string
	Code       string
	Public     string
	Status     int
	Domain     string
	Hint       string
	TraceID    string
	Tags       []string
	Context    map[string]any
	Details    map[string]any
	Cause      string
	Stacktrace string
	Object     map[string]any
}

// ErrorFields 将错误展开为完整结构化字段，供底层 zap 调用点复用。
func ErrorFields(err error) []Field {
	info := ExtractError(err, StackFull, RedactConfig{Enabled: true})
	return JSONLErrorFields(info, nil, StackFull, RedactConfig{Enabled: true})
}

// ExtractError 将任意错误归一化为可供 console/jsonl 渲染的日志模型。
func ExtractError(err error, stack StackMode, redact RedactConfig) ErrorInfo {
	if err == nil {
		return ErrorInfo{}
	}
	info := ErrorInfo{
		Message: RedactText(err.Error()),
		Type:    fmt.Sprintf("%T", err),
	}
	if cause := rootCause(err); cause != nil && cause != err {
		info.Cause = RedactText(cause.Error())
	}
	if oopsErr, ok := oops.AsOops(err); ok {
		info.Code = string(apperrors.CodeOf(err))
		info.Public = apperrors.PublicMessageOf(err)
		info.Status = apperrors.StatusOf(err)
		if details := apperrors.PublicDetailsOf(err); len(details) > 0 {
			info.Details = redactMap(details)
		}
		if domain := oopsErr.Domain(); domain != "" {
			info.Domain = domain
		}
		if hint := oopsErr.Hint(); hint != "" {
			info.Hint = hint
		}
		if trace := oopsErr.Trace(); trace != "" {
			info.TraceID = trace
		}
		if public := oopsErr.Public(); public != "" {
			info.Public = public
		}
		if tags := oopsErr.Tags(); len(tags) > 0 {
			info.Tags = append([]string(nil), tags...)
		}
		if attrs := apperrors.PublicContext(err); len(attrs) > 0 {
			info.Context = redactMap(attrs)
		}
		info.Object = oopsObject(oopsErr.ToMap(), info.Context)
		info.Stacktrace = renderStack(oopsErr.Stacktrace(), stack)
	}
	return info
}

// ConsoleErrorFields 返回面向人眼的精简错误字段。
func ConsoleErrorFields(info ErrorInfo, fields []Field, stack StackMode, redact RedactConfig) []Field {
	result := make([]Field, 0, len(fields)+8)
	result = append(result, consoleUserFields(fields, redact)...)
	appendStringField := func(key string, value string) {
		if strings.TrimSpace(value) != "" {
			result = append(result, zap.String(key, strings.TrimSpace(value)))
		}
	}
	appendStringField("trace_id", info.TraceID)
	appendStringField("error_code", info.Code)
	appendStringField("error_domain", info.Domain)
	appendStringField("error_public", info.Public)
	appendStringField("error", info.Message)
	if len(info.Context) > 0 {
		for key, value := range info.Context {
			if _, ok := consoleContextWhitelist[key]; ok {
				result = append(result, zap.Any(key, value))
			}
		}
	}
	if stack != StackNone {
		appendStringField("error_stacktrace", info.Stacktrace)
	}
	return RedactFields(result, redact)
}

// JSONLErrorFields 返回面向日志检索的完整错误字段。
func JSONLErrorFields(info ErrorInfo, fields []Field, stack StackMode, redact RedactConfig) []Field {
	result := make([]Field, 0, len(fields)+14)
	result = append(result, RedactFields(withoutReservedFields(fields, jsonLErrorReservedKeys(info, stack)), redact)...)
	appendStringField := func(key string, value string) {
		if strings.TrimSpace(value) != "" {
			result = append(result, zap.String(key, strings.TrimSpace(value)))
		}
	}
	appendStringField("trace_id", info.TraceID)
	appendStringField("error_code", info.Code)
	appendStringField("error_domain", info.Domain)
	if len(info.Tags) > 0 {
		result = append(result, zap.Strings("error_tags", info.Tags))
	}
	if info.Status > 0 {
		result = append(result, zap.Int("error_status", info.Status))
	}
	appendStringField("error_message", info.Message)
	appendStringField("error_public", info.Public)
	appendStringField("error_type", info.Type)
	appendStringField("error_hint", info.Hint)
	appendStringField("error_cause", info.Cause)
	if len(info.Details) > 0 {
		result = append(result, zap.Any("error_details", info.Details))
	}
	if len(info.Context) > 0 {
		result = append(result, zap.Any("error_context", info.Context))
	}
	if len(info.Object) > 0 {
		result = append(result, zap.Any("error", info.Object))
	} else {
		appendStringField("error", info.Message)
	}
	if stack != StackNone {
		appendStringField("error_stacktrace", info.Stacktrace)
	}
	return RedactFields(result, redact)
}

// jsonLErrorReservedKeys 返回 JSONL 错误模型会统一写入的字段，避免底层 zap 输出重复 key。
func jsonLErrorReservedKeys(info ErrorInfo, stack StackMode) map[string]struct{} {
	keys := make(map[string]struct{}, 14)
	addStringKey := func(key string, value string) {
		if strings.TrimSpace(value) != "" {
			keys[key] = struct{}{}
		}
	}
	addStringKey("trace_id", info.TraceID)
	addStringKey("error_code", info.Code)
	addStringKey("error_domain", info.Domain)
	if len(info.Tags) > 0 {
		keys["error_tags"] = struct{}{}
	}
	if info.Status > 0 {
		keys["error_status"] = struct{}{}
	}
	addStringKey("error_message", info.Message)
	addStringKey("error_public", info.Public)
	addStringKey("error_type", info.Type)
	addStringKey("error_hint", info.Hint)
	addStringKey("error_cause", info.Cause)
	if len(info.Details) > 0 {
		keys["error_details"] = struct{}{}
	}
	if len(info.Context) > 0 {
		keys["error_context"] = struct{}{}
	}
	if len(info.Object) > 0 || strings.TrimSpace(info.Message) != "" {
		keys["error"] = struct{}{}
	}
	if stack != StackNone && strings.TrimSpace(info.Stacktrace) != "" {
		keys["error_stacktrace"] = struct{}{}
	}
	return keys
}

// withoutReservedFields 删除会由错误模型统一写入的调用方字段。
func withoutReservedFields(fields []Field, reserved map[string]struct{}) []Field {
	if len(fields) == 0 || len(reserved) == 0 {
		return fields
	}
	filtered := make([]Field, 0, len(fields))
	for _, field := range fields {
		if _, ok := reserved[field.Key]; ok {
			continue
		}
		filtered = append(filtered, field)
	}
	return filtered
}

// rootCause 返回 errors.Unwrap 链末端的根错误。
func rootCause(err error) error {
	for err != nil {
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			return err
		}
		err = unwrapped
	}
	return nil
}

// oopsObject 返回脱敏后的 oops 原始结构，并用已脱敏上下文覆盖内部 context。
func oopsObject(payload map[string]any, context map[string]any) map[string]any {
	object := redactMap(payload)
	if len(context) == 0 {
		delete(object, "context")
		return object
	}
	object["context"] = context
	return object
}
