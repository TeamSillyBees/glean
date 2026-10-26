package response_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
	"github.com/teamsillybees/glean/internal/platform/response"
)

// TestOKExtractsTraceIDFromContext 验证标准成功响应会自动从 context 提取 trace_id。
func TestOKExtractsTraceIDFromContext(t *testing.T) {
	ctx := requestctx.WithTraceID(context.Background(), "trace-1")

	body := response.OK(ctx, map[string]string{"status": "ok"})

	require.Equal(t, "OK", body.Code)
	require.Equal(t, "success", body.Message)
	require.Equal(t, "trace-1", body.TraceID)
	require.Equal(t, "ok", body.Data["status"])
}

// TestOKAllowsMissingTraceID 验证没有 trace_id 的 context 不影响成功响应构造。
func TestOKAllowsMissingTraceID(t *testing.T) {
	body := response.OK(context.Background(), "pong")

	require.Equal(t, "pong", body.Data)
	require.Empty(t, body.TraceID)
}
