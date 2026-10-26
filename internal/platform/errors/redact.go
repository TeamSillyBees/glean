package apperrors

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"
)

const redactedValue = "[REDACTED]"

var (
	sensitiveKeyParts = []string{
		"password",
		"passwd",
		"pwd",
		"token",
		"secret",
		"credential",
		"authorization",
		"accesskey",
		"secretkey",
		"privatekey",
		"session",
		"phone",
		"mobile",
		"idcard",
		"identitycard",
		"dsn",
		"datasourcename",
		"databaseurl",
		"connectionstring",
		"sql",
		"query",
		"objectkey",
		"osskey",
		"coskey",
		"s3key",
		"responsebody",
		"rawresponse",
	}
	sensitiveTextPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(authorization)\s*[:=]\s*(?:bearer\s+)?[^,\s;&]+`),
		regexp.MustCompile(`(?i)(password|passwd|pwd|token|secret|authorization|access[_-]?key|secret[_-]?key|dsn)\s*[:=]\s*[^,\s;&]+`),
	}
	urlCredentialPattern = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/@\s]+@`)
)

const maxSanitizeDepth = 32

var sanitizeTimeValueType = reflect.TypeOf(time.Time{})

type sanitizeVisit struct {
	kind   reflect.Kind
	typeOf reflect.Type
	ptr    uintptr
}

// SanitizeMap 复制并脱敏结构化字段，可用于 HTTP 响应 details 和日志 attrs。
func SanitizeMap(fields map[string]any) map[string]any {
	if fields == nil {
		return nil
	}
	return SanitizeValue("", fields).(map[string]any)
}

// SanitizeValue 按字段名和值类型递归脱敏敏感信息。
func SanitizeValue(key string, value any) any {
	return sanitizeReflectValue(key, reflect.ValueOf(value), 0, make(map[sanitizeVisit]struct{}))
}

func sanitizeReflectValue(key string, value reflect.Value, depth int, seen map[sanitizeVisit]struct{}) any {
	if isSensitiveKey(key) || depth >= maxSanitizeDepth {
		return redactedValue
	}
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil
		}
		return sanitizeReflectValue(key, value.Elem(), depth+1, seen)
	}
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return nil
	}
	if value.CanInterface() {
		if err, ok := value.Interface().(error); ok {
			return SanitizeText(err.Error())
		}
		if value.Type() == sanitizeTimeValueType {
			return value.Interface()
		}
	}
	if release, repeated := enterSanitizeValue(value, seen); repeated {
		return redactedValue
	} else if release != nil {
		defer release()
	}

	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return nil
		}
		return sanitizeReflectValue(key, value.Elem(), depth+1, seen)
	case reflect.Map:
		if value.IsNil() {
			return nil
		}
		result := make(map[string]any, value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			itemKey := sanitizeMapKey(iterator.Key())
			result[itemKey] = sanitizeReflectValue(itemKey, iterator.Value(), depth+1, seen)
		}
		return result
	case reflect.Struct:
		result := make(map[string]any, value.NumField())
		typeOf := value.Type()
		for index := 0; index < value.NumField(); index++ {
			field := typeOf.Field(index)
			fieldName, ok := sanitizeFieldName(field)
			if !ok {
				continue
			}
			result[fieldName] = sanitizeReflectValue(fieldName, value.Field(index), depth+1, seen)
		}
		return result
	case reflect.Slice, reflect.Array:
		if value.Kind() == reflect.Slice && value.IsNil() {
			return nil
		}
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return redactedValue
		}
		result := make([]any, value.Len())
		for index := 0; index < value.Len(); index++ {
			result[index] = sanitizeReflectValue("", value.Index(index), depth+1, seen)
		}
		return result
	case reflect.String:
		return SanitizeText(value.String())
	default:
		if value.CanInterface() {
			return value.Interface()
		}
		return fmt.Sprint(value)
	}
}

func enterSanitizeValue(value reflect.Value, seen map[sanitizeVisit]struct{}) (func(), bool) {
	switch value.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice:
		if value.IsNil() {
			return nil, false
		}
	default:
		return nil, false
	}
	visit := sanitizeVisit{kind: value.Kind(), typeOf: value.Type(), ptr: value.Pointer()}
	if _, ok := seen[visit]; ok {
		return nil, true
	}
	seen[visit] = struct{}{}
	return func() { delete(seen, visit) }, false
}

func sanitizeMapKey(value reflect.Value) string {
	if value.Kind() == reflect.String {
		return value.String()
	}
	if value.CanInterface() {
		return fmt.Sprint(value.Interface())
	}
	return fmt.Sprint(value)
}

func sanitizeFieldName(field reflect.StructField) (string, bool) {
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

// SanitizeText 脱敏常见 key=value 或 key:value 形式的敏感片段。
func SanitizeText(text string) string {
	sanitized := text
	for _, pattern := range sensitiveTextPatterns {
		sanitized = pattern.ReplaceAllStringFunc(sanitized, redactAssignment)
	}
	sanitized = urlCredentialPattern.ReplaceAllString(sanitized, "${1}"+redactedValue+"@")
	return sanitized
}

func redactAssignment(text string) string {
	for _, separator := range []string{":", "="} {
		if index := strings.Index(text, separator); index >= 0 {
			return strings.TrimSpace(text[:index]) + separator + redactedValue
		}
	}
	return redactedValue
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.NewReplacer("_", "", "-", "", ".", "", " ", "").Replace(normalized)
	for _, part := range sensitiveKeyParts {
		if strings.Contains(normalized, part) {
			return true
		}
	}
	return false
}
