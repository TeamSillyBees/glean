package logx

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// RedactedValue 是日志脱敏后的统一占位值。
const RedactedValue = "[REDACTED]"

var (
	// defaultSensitiveKeys 是所有项目默认脱敏的字段名集合。
	defaultSensitiveKeys = []string{
		"password",
		"passwd",
		"pwd",
		"token",
		"accesstoken",
		"refreshtoken",
		"authorization",
		"cookie",
		"setcookie",
		"secret",
		"credential",
		"accesskey",
		"secretkey",
		"privatekey",
		"session",
		"phone",
		"mobile",
		"email",
		"idcard",
		"identitycard",
		"dsn",
		"datasourcename",
		"databaseurl",
		"connectionstring",
		"sql",
		"query",
		"body",
		"requestbody",
		"responsebody",
		"rawresponse",
	}
	// assignmentPatterns 匹配字符串中常见的敏感 key=value/key:value 片段。
	assignmentPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(authorization)\s*[:=]\s*(?:bearer\s+)?[^,\s;&]+`),
		regexp.MustCompile(`(?i)(password|passwd|pwd|token|secret|authorization|access[_-]?key|secret[_-]?key|dsn)\s*[:=]\s*[^,\s;&]+`),
	}
	urlCredentialPattern = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/@\s]+@`)
)

const maxRedactDepth = 32

var timeValueType = reflect.TypeOf(time.Time{})

type redactVisit struct {
	kind   reflect.Kind
	typeOf reflect.Type
	ptr    uintptr
}

// IsSensitiveKey 判断字段名是否属于敏感信息。
func IsSensitiveKey(key string, extraFields []string) bool {
	normalized := normalizeKey(key)
	if normalized == "" {
		return false
	}
	for _, candidate := range append(defaultSensitiveKeys, extraFields...) {
		candidate = normalizeKey(candidate)
		if candidate == "" {
			continue
		}
		if normalized == candidate || strings.Contains(normalized, candidate) {
			return true
		}
	}
	return false
}

// RedactValue 按字段名和值类型递归脱敏。
func RedactValue(key string, value any) any {
	return redactReflectValue(key, reflect.ValueOf(value), 0, make(map[redactVisit]struct{}))
}

func redactReflectValue(key string, value reflect.Value, depth int, seen map[redactVisit]struct{}) any {
	if IsSensitiveKey(key, nil) || depth >= maxRedactDepth {
		return RedactedValue
	}
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil
		}
		return redactReflectValue(key, value.Elem(), depth+1, seen)
	}
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return nil
	}
	if value.CanInterface() {
		if err, ok := value.Interface().(error); ok {
			return RedactText(err.Error())
		}
		if value.Type() == timeValueType {
			return value.Interface()
		}
		if stringer, ok := value.Interface().(fmt.Stringer); ok {
			return RedactText(stringer.String())
		}
	}
	if release, repeated := enterRedactValue(value, seen); repeated {
		return RedactedValue
	} else if release != nil {
		defer release()
	}

	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return nil
		}
		return redactReflectValue(key, value.Elem(), depth+1, seen)
	case reflect.Map:
		if value.IsNil() {
			return nil
		}
		result := make(map[string]any, value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			itemKey := reflectMapKey(iterator.Key())
			result[itemKey] = redactReflectValue(itemKey, iterator.Value(), depth+1, seen)
		}
		return result
	case reflect.Struct:
		result := make(map[string]any, value.NumField())
		typeOf := value.Type()
		for index := 0; index < value.NumField(); index++ {
			field := typeOf.Field(index)
			fieldName, ok := redactFieldName(field)
			if !ok {
				continue
			}
			result[fieldName] = redactReflectValue(fieldName, value.Field(index), depth+1, seen)
		}
		return result
	case reflect.Slice, reflect.Array:
		if value.Kind() == reflect.Slice && value.IsNil() {
			return nil
		}
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return RedactedValue
		}
		result := make([]any, value.Len())
		for index := 0; index < value.Len(); index++ {
			result[index] = redactReflectValue("", value.Index(index), depth+1, seen)
		}
		return result
	case reflect.String:
		return RedactText(value.String())
	default:
		if value.CanInterface() {
			return value.Interface()
		}
		return fmt.Sprint(value)
	}
}

func enterRedactValue(value reflect.Value, seen map[redactVisit]struct{}) (func(), bool) {
	switch value.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice:
		if value.IsNil() {
			return nil, false
		}
	default:
		return nil, false
	}
	visit := redactVisit{kind: value.Kind(), typeOf: value.Type(), ptr: value.Pointer()}
	if _, ok := seen[visit]; ok {
		return nil, true
	}
	seen[visit] = struct{}{}
	return func() { delete(seen, visit) }, false
}

func reflectMapKey(value reflect.Value) string {
	if value.Kind() == reflect.String {
		return value.String()
	}
	if value.CanInterface() {
		return fmt.Sprint(value.Interface())
	}
	return fmt.Sprint(value)
}

func redactFieldName(field reflect.StructField) (string, bool) {
	if field.PkgPath != "" {
		return "", false
	}
	for _, tagName := range []string{"json", "mapstructure", "yaml"} {
		tag := strings.Split(field.Tag.Get(tagName), ",")[0]
		if tag == "-" {
			return "", false
		}
		if tag != "" {
			return tag, true
		}
	}
	return field.Name, true
}

// RedactFields 对 zap 字段执行集中脱敏。
func RedactFields(fields []Field, cfg RedactConfig) []Field {
	if !cfg.Enabled || len(fields) == 0 {
		return fields
	}
	result := make([]Field, len(fields))
	copy(result, fields)
	for index, field := range result {
		if IsSensitiveKey(field.Key, cfg.Fields) {
			result[index] = zap.String(field.Key, RedactedValue)
			continue
		}
		switch field.Type {
		case zapcore.StringType:
			result[index] = zap.String(field.Key, RedactText(field.String))
		case zapcore.ErrorType:
			if err, ok := field.Interface.(error); ok && err != nil {
				result[index] = zap.String(field.Key, RedactText(err.Error()))
			}
		case zapcore.ReflectType:
			result[index] = zap.Any(field.Key, RedactValue(field.Key, field.Interface))
		case zapcore.StringerType:
			if stringer, ok := field.Interface.(fmt.Stringer); ok && stringer != nil {
				result[index] = zap.String(field.Key, RedactText(stringer.String()))
			}
		case zapcore.BinaryType, zapcore.ByteStringType:
			result[index] = zap.String(field.Key, RedactedValue)
		}
	}
	return result
}

// RedactText 脱敏字符串中的常见敏感赋值片段。
func RedactText(text string) string {
	result := text
	for _, pattern := range assignmentPatterns {
		result = pattern.ReplaceAllStringFunc(result, redactAssignment)
	}
	result = urlCredentialPattern.ReplaceAllString(result, "${1}"+RedactedValue+"@")
	return result
}

// redactCore 是包裹 zapcore.Core 的脱敏适配器。
type redactCore struct {
	zapcore.Core
	cfg RedactConfig
}

// With 返回携带已脱敏固定字段的新 core。
func (c *redactCore) With(fields []zapcore.Field) zapcore.Core {
	return &redactCore{Core: c.Core.With(RedactFields(fields, c.cfg)), cfg: c.cfg}
}

// Check 将启用级别的日志条目绑定到当前 core。
func (c *redactCore) Check(entry zapcore.Entry, checked *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return checked.AddCore(entry, c)
	}
	return checked
}

// Write 在写入底层 core 前对字段做最终脱敏。
func (c *redactCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	return c.Core.Write(entry, RedactFields(fields, c.cfg))
}

// redactMap 对 map 字段做递归脱敏。
func redactMap(fields map[string]any) map[string]any {
	if fields == nil {
		return nil
	}
	return RedactValue("", fields).(map[string]any)
}

// redactAssignment 替换单个敏感赋值片段的值部分。
func redactAssignment(text string) string {
	colon := strings.Index(text, ":")
	equal := strings.Index(text, "=")
	switch {
	case equal >= 0 && (colon < 0 || equal < colon):
		return strings.TrimSpace(text[:equal]) + "=" + RedactedValue
	case colon >= 0:
		return strings.TrimSpace(text[:colon]) + ":" + RedactedValue
	default:
		return RedactedValue
	}
}

// normalizeKey 将字段名规整为只含小写字母和数字的形式。
func normalizeKey(key string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(fmt.Sprint(key))) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}
