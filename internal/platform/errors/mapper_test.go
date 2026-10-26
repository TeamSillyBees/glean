package apperrors

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestToHTTPMapsOopsError 验证 oops 错误能映射为统一 HTTP 错误响应。
func TestToHTTPMapsOopsError(t *testing.T) {
	err := New(
		CodeNotFound,
		"user not found",
		WithDetail("userId", int64(1001)),
	)

	status, body := ToHTTP(err, "trace-001")

	require.Equal(t, http.StatusNotFound, status)
	require.Equal(t, "NOT_FOUND", body.Code)
	require.Equal(t, "user not found", body.Message)
	require.Equal(t, "trace-001", body.TraceID)
	require.Equal(t, int64(1001), body.Details["userId"])
}

// TestToHTTPDoesNotReturnInternalCause 验证 HTTP 响应不会暴露底层 cause 文本。
func TestToHTTPDoesNotReturnInternalCause(t *testing.T) {
	err := Wrap(errors.New("driver: duplicate key"), CodeDBError, "create user failed")

	status, body := ToHTTP(err, "trace-001")
	payload, marshalErr := json.Marshal(body)

	require.NoError(t, marshalErr)
	require.Equal(t, http.StatusInternalServerError, status)
	require.Equal(t, "internal error", body.Message)
	require.NotContains(t, string(payload), "duplicate key")
}

// TestToHTTPSanitizesSensitiveDetails 验证误放入 Details 的敏感字段不会明文进入 HTTP 响应。
func TestToHTTPSanitizesSensitiveDetails(t *testing.T) {
	err := New(
		CodeBadRequest,
		"invalid request",
		WithDetail("password", "secret-password"),
		WithDetail("safe", "visible"),
	)

	_, body := ToHTTP(err, "trace-001")
	payload, marshalErr := json.Marshal(body)

	require.NoError(t, marshalErr)
	require.Equal(t, redactedValue, body.Details["password"])
	require.Equal(t, "visible", body.Details["safe"])
	require.NotContains(t, string(payload), "secret-password")
}
