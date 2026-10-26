package idgen

import (
	"encoding/json"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/require"
)

// TestParseIDAcceptsPositiveDecimalString 验证合法雪花 ID 字符串可以解析。
func TestParseIDAcceptsPositiveDecimalString(t *testing.T) {
	id, err := Parse("1771234567890123456")

	require.NoError(t, err)
	require.Equal(t, int64(1771234567890123456), id.Int64())
	require.Equal(t, "1771234567890123456", id.String())
}

// TestParseIDRejectsInvalidStrings 验证 ID 解析拒绝零值、前导零、负数和溢出值。
func TestParseIDRejectsInvalidStrings(t *testing.T) {
	for _, input := range []string{
		"",
		"0",
		"0123",
		"-1",
		"9223372036854775808",
		"17712345678901234567",
		"abc",
	} {
		_, err := Parse(input)
		require.Errorf(t, err, "Parse(%q) should fail", input)
	}
}

// TestIDMarshalJSONUsesString 验证 ID JSON 编码使用字符串避免前端精度丢失。
func TestIDMarshalJSONUsesString(t *testing.T) {
	payload, err := json.Marshal(struct {
		TaskID ID `json:"taskId"`
	}{
		TaskID: New(1771234567890123456),
	})

	require.NoError(t, err)
	require.JSONEq(t, `{"taskId":"1771234567890123456"}`, string(payload))
}

// TestIDUnmarshalJSONRequiresString 验证 ID JSON 解码只接受字符串。
func TestIDUnmarshalJSONRequiresString(t *testing.T) {
	var body struct {
		TaskID ID `json:"taskId"`
	}

	require.NoError(t, json.Unmarshal([]byte(`{"taskId":"1771234567890123456"}`), &body))
	require.Equal(t, New(1771234567890123456), body.TaskID)

	require.Error(t, json.Unmarshal([]byte(`{"taskId":1771234567890123456}`), &body))
	require.Error(t, json.Unmarshal([]byte(`{"taskId":"0"}`), &body))
}

// TestIDTextEncoding 验证 ID 文本编解码复用统一字符串规则。
func TestIDTextEncoding(t *testing.T) {
	id := New(1771234567890123456)

	text, err := id.MarshalText()
	require.NoError(t, err)
	require.Equal(t, "1771234567890123456", string(text))

	var parsed ID
	require.NoError(t, parsed.UnmarshalText(text))
	require.Equal(t, id, parsed)
	require.Error(t, parsed.UnmarshalText([]byte("0")))
}

// TestIDSchemaUsesStringPattern 验证 Huma OpenAPI schema 使用字符串和安全整数范围外示例。
func TestIDSchemaUsesStringPattern(t *testing.T) {
	schema := New(1).Schema(nil)

	require.Equal(t, huma.TypeString, schema.Type)
	require.Equal(t, `^[1-9][0-9]{0,18}$`, schema.Pattern)
	require.Equal(t, []any{"1771234567890123456"}, schema.Examples)
}
