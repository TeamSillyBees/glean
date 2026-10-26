package idgen

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
)

const (
	// idPattern 是对外 JSON/OpenAPI ID 字符串格式，禁止零值、负数和前导零。
	idPattern = `^[1-9][0-9]{0,18}$`
	// idExample 是 Huma OpenAPI 文档使用的雪花 ID 示例值。
	idExample = "1771234567890123456"
)

var idPatternRegexp = regexp.MustCompile(idPattern)

// humaIDParam 是 Huma path/query 参数解析时使用的严格文本接收器。
type humaIDParam struct{}

// UnmarshalText 校验 Huma 参数原始文本是否为合法业务 ID。
func (*humaIDParam) UnmarshalText(data []byte) error {
	if _, err := Parse(string(data)); err != nil {
		return err
	}
	return nil
}

// ID 表示业务系统中的雪花 ID。
//
// ID 在 Go 内部以 int64 存储，在 JSON 和 Huma OpenAPI 中统一表现为字符串，
// 避免前端 JavaScript 在安全整数范围外丢失精度。
type ID int64

// New 将 int64 包装为业务 ID。
func New(v int64) ID {
	return ID(v)
}

// Parse 将十进制字符串解析为业务 ID。
func Parse(s string) (ID, error) {
	if !idPatternRegexp.MatchString(s) {
		return 0, fmt.Errorf("id must match %s", idPattern)
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse id: %w", err)
	}
	if v <= 0 {
		return 0, fmt.Errorf("id must be positive")
	}
	return ID(v), nil
}

// Int64 返回 ID 的底层 int64 值。
func (id ID) Int64() int64 {
	return int64(id)
}

// String 返回 ID 的十进制字符串。
func (id ID) String() string {
	return strconv.FormatInt(id.Int64(), 10)
}

// MarshalJSON 将 ID 编码为 JSON 字符串。
func (id ID) MarshalJSON() ([]byte, error) {
	return json.Marshal(id.String())
}

// UnmarshalJSON 从 JSON 字符串解析 ID。
func (id *ID) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("id must be a JSON string: %w", err)
	}
	parsed, err := Parse(value)
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}

// MarshalText 将 ID 编码为文本。
func (id ID) MarshalText() ([]byte, error) {
	return []byte(id.String()), nil
}

// UnmarshalText 从文本解析 ID。
func (id *ID) UnmarshalText(data []byte) error {
	parsed, err := Parse(string(data))
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}

// Receiver 为 Huma 参数解析提供字符串接收器，避免底层 int64 被按 number schema 校验。
func (*ID) Receiver() reflect.Value {
	return reflect.New(reflect.TypeOf(humaIDParam{})).Elem()
}

// OnParamSet 在 Huma 参数解析成功后把原始字符串写回业务 ID。
func (id *ID) OnParamSet(isSet bool, parsed any) {
	if !isSet {
		*id = 0
		return
	}
	value, ok := parsed.(string)
	if !ok {
		return
	}
	parsedID, err := Parse(value)
	if err != nil {
		return
	}
	*id = parsedID
}

// Schema 返回业务 ID 在 Huma OpenAPI 中的字符串 schema。
func (ID) Schema(r huma.Registry) *huma.Schema {
	return &huma.Schema{
		Type:     huma.TypeString,
		Pattern:  idPattern,
		Examples: []any{idExample},
	}
}
