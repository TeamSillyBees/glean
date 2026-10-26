package logx

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	apperrors "github.com/teamsillybees/glean/internal/platform/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// TestExtractErrorIncludesOopsFields 验证 oops 错误会被统一提取。
func TestExtractErrorIncludesOopsFields(t *testing.T) {
	err := apperrors.Wrap(errors.New("driver: duplicate key"), apperrors.CodeDBError, "create user failed",
		apperrors.WithCauseDomain(apperrors.DomainDB),
		apperrors.WithCauseTrace("trace-1"),
		apperrors.WithCauseAttr("operation", "insert_user"),
		apperrors.WithCauseAttr("password", "secret-password"),
		apperrors.WithDetail("field", "email"),
	)

	got := ExtractError(err, StackFull, RedactConfig{Enabled: true})

	require.Equal(t, "DB_ERROR", got.Code)
	require.Contains(t, got.Message, "create user failed")
	require.Contains(t, got.Message, "duplicate key")
	require.Equal(t, apperrors.DomainDB, got.Domain)
	require.Equal(t, "trace-1", got.TraceID)
	require.Equal(t, "insert_user", got.Context["operation"])
	require.Equal(t, RedactedValue, got.Context["password"])
	require.Equal(t, "email", got.Details["field"])
	require.NotContains(t, got.Context, "initra.http_status")
	require.NotContains(t, got.Context, "initra.public_details")
	require.NotEmpty(t, got.Stacktrace)
	require.NotEmpty(t, got.Object)
	objectContext, ok := got.Object["context"].(map[string]any)
	require.True(t, ok)
	require.NotContains(t, objectContext, "initra.http_status")
	require.NotContains(t, objectContext, "initra.public_details")
}

// TestExtractErrorHandlesPlainError 验证普通 error 也能输出稳定错误字段。
func TestExtractErrorHandlesPlainError(t *testing.T) {
	got := ExtractError(errors.New("plain failure"), StackFull, RedactConfig{Enabled: true})

	require.Equal(t, "plain failure", got.Message)
	require.Empty(t, got.Code)
	require.Empty(t, got.Stacktrace)
	require.Contains(t, got.Type, "errorString")
}

// TestJSONLErrorFieldsPromotesIndexFields 验证 jsonl 错误字段会提升检索字段到顶层。
func TestJSONLErrorFieldsPromotesIndexFields(t *testing.T) {
	info := ErrorInfo{
		Message: "create user failed",
		Code:    "DB_ERROR",
		Domain:  apperrors.DomainDB,
		TraceID: "trace-1",
		Tags:    []string{"db"},
		Context: map[string]any{"operation": "insert_user"},
	}
	core, logs := observer.New(zap.ErrorLevel)
	logger := zap.New(core)

	logger.Error("request failed", JSONLErrorFields(info, []zap.Field{zap.String("path", "/api/users")}, StackFull, RedactConfig{Enabled: true})...)

	fields := logs.All()[0].ContextMap()
	require.Equal(t, "DB_ERROR", fields["error_code"])
	require.Equal(t, apperrors.DomainDB, fields["error_domain"])
	require.Equal(t, "trace-1", fields["trace_id"])
	require.Equal(t, []any{"db"}, fields["error_tags"])
	require.Equal(t, "/api/users", fields["path"])
	require.Equal(t, map[string]any{"operation": "insert_user"}, fields["error_context"])
}

// TestConsoleErrorFieldsKeepsWhitelist 验证 console 错误字段只保留白名单上下文。
func TestConsoleErrorFieldsKeepsWhitelist(t *testing.T) {
	info := ErrorInfo{
		Message: "create user failed",
		Code:    "DB_ERROR",
		TraceID: "trace-1",
		Context: map[string]any{
			"operation": "insert_user",
			"sql":       "select * from users",
		},
	}
	core, logs := observer.New(zap.ErrorLevel)
	logger := zap.New(core)

	logger.Error("request failed", ConsoleErrorFields(info, []zap.Field{
		zap.String("path", "/api/users"),
		zap.String("raw_path", "/api/users?password=secret"),
	}, StackShort, RedactConfig{Enabled: true})...)

	fields := logs.All()[0].ContextMap()
	require.Equal(t, "DB_ERROR", fields["error_code"])
	require.Equal(t, "trace-1", fields["trace_id"])
	require.Equal(t, "/api/users", fields["path"])
	require.Equal(t, "insert_user", fields["operation"])
	require.NotContains(t, fields, "raw_path")
	require.NotContains(t, fields, "sql")
}
