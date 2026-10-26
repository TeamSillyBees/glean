package logx

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type redactCredentialFixture struct {
	Password string `json:"password"`
	Label    string `json:"label"`
}

type redactNodeFixture struct {
	Name string             `json:"name"`
	Next *redactNodeFixture `json:"next"`
}

type redactConfigFixture struct {
	DataSourceName string                             `mapstructure:"data_source_name"`
	Nested         *redactCredentialFixture           `json:"nested"`
	Services       map[string]redactCredentialFixture `json:"services"`
	CreatedAt      time.Time                          `json:"created_at"`
	Cycle          *redactNodeFixture                 `json:"cycle"`
	Deep           *redactNodeFixture                 `json:"deep"`
}

type credentialStringer string

func (s credentialStringer) String() string {
	return string(s)
}

// TestRedactFieldsMasksSensitiveKeys 验证结构化敏感字段会被统一脱敏。
func TestRedactFieldsMasksSensitiveKeys(t *testing.T) {
	fields := RedactFields([]zap.Field{
		zap.String("password", "plain-password"),
		zap.String("authorization", "Bearer plain-token"),
		zap.String("username", "alice"),
	}, RedactConfig{Enabled: true})

	got := stringFields(fields)
	require.Equal(t, RedactedValue, got["password"])
	require.Equal(t, RedactedValue, got["authorization"])
	require.Equal(t, "alice", got["username"])
}

// TestRedactValueSanitizesNestedData 验证 map 和 slice 中的敏感字段会递归脱敏。
func TestRedactValueSanitizesNestedData(t *testing.T) {
	got := RedactValue("payload", map[string]any{
		"token": "secret-token",
		"profile": map[string]any{
			"email":    "alice@example.com",
			"nickname": "alice",
		},
	}).(map[string]any)

	require.Equal(t, RedactedValue, got["token"])
	require.Equal(t, RedactedValue, got["profile"].(map[string]any)["email"])
	require.Equal(t, "alice", got["profile"].(map[string]any)["nickname"])
}

// TestRedactTextMasksAssignments 验证字符串中的敏感赋值片段会被脱敏。
func TestRedactTextMasksAssignments(t *testing.T) {
	got := RedactText("dsn=postgres://user:pass@localhost token:abc username=alice proxy=https://user:url-pass@proxy.example.test")

	require.Contains(t, got, "dsn="+RedactedValue)
	require.Contains(t, got, "token:"+RedactedValue)
	require.Contains(t, got, "username=alice")
	require.NotContains(t, got, "abc")
	require.NotContains(t, got, "postgres://")
	require.NotContains(t, got, "url-pass")
}

func TestRedactFieldsSanitizesCompositeHelpers(t *testing.T) {
	fields := RedactFields([]Field{
		Strings("headers", []string{"Authorization: Bearer list-token"}),
		Any("payload", []byte("password=byte-secret")),
		Any("endpoint", credentialStringer("https://user:stringer-secret@api.example.test")),
	}, RedactConfig{Enabled: true})
	printable := fmt.Sprint(fields)

	require.NotContains(t, printable, "list-token")
	require.NotContains(t, printable, "byte-secret")
	require.NotContains(t, printable, "stringer-secret")
	require.Contains(t, printable, RedactedValue)
}

func TestRedactValueHandlesStructPointersTypedMapsAndCycles(t *testing.T) {
	cycle := &redactNodeFixture{Name: "cycle"}
	cycle.Next = cycle
	deep := &redactNodeFixture{Name: "root"}
	cursor := deep
	for index := 0; index < maxRedactDepth+2; index++ {
		cursor.Next = &redactNodeFixture{Name: "next"}
		cursor = cursor.Next
	}
	now := time.Now().UTC()

	got := RedactValue("config", redactConfigFixture{
		DataSourceName: "postgres://user:password@localhost/database",
		Nested:         &redactCredentialFixture{Password: "nested-secret", Label: "safe"},
		Services:       map[string]redactCredentialFixture{"primary": {Password: "map-secret", Label: "visible"}},
		CreatedAt:      now,
		Cycle:          cycle,
		Deep:           deep,
	}).(map[string]any)

	require.Equal(t, RedactedValue, got["data_source_name"])
	require.Equal(t, RedactedValue, got["nested"].(map[string]any)["password"])
	require.Equal(t, "safe", got["nested"].(map[string]any)["label"])
	primary := got["services"].(map[string]any)["primary"].(map[string]any)
	require.Equal(t, RedactedValue, primary["password"])
	require.Equal(t, "visible", primary["label"])
	require.Equal(t, now, got["created_at"])
	encoded, err := json.Marshal(got)
	require.NoError(t, err)
	require.Contains(t, string(encoded), RedactedValue)
}

// stringFields 提取测试中需要断言的字符串字段。
func stringFields(fields []zap.Field) map[string]string {
	result := make(map[string]string, len(fields))
	for _, field := range fields {
		if field.Type == zapcore.StringType {
			result[field.Key] = field.String
		}
	}
	return result
}
