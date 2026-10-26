package bizerrors

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	apperrors "github.com/teamsillybees/glean/internal/platform/errors"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/logx"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
)

// TestLoginFailed 使用独立业务码表达统一的登录失败语义。
func TestLoginFailed(t *testing.T) {
	err := LoginFailed()

	require.Equal(t, CodeLoginFailed, apperrors.CodeOf(err))
	require.Equal(t, http.StatusUnauthorized, apperrors.StatusOf(err))
	require.Equal(t, "login failed", apperrors.PublicMessageOf(err))
}

// TestUserNotFound 确认用户不存在错误携带用户 ID 详情。
func TestUserNotFound(t *testing.T) {
	err := UserNotFound(idgen.New(1001))

	require.Equal(t, CodeUserNotFound, apperrors.CodeOf(err))
	require.Equal(t, http.StatusNotFound, apperrors.StatusOf(err))
	require.Equal(t, idgen.New(1001), apperrors.PublicDetailsOf(err)["userId"])
}

// TestWrapInternalContext 确认业务错误封装保留底层错误链和详情。
func TestWrapInternalContext(t *testing.T) {
	cause := errors.New("network failed")

	err := WrapInternalContext(context.Background(), cause, "call remote service failed", WithDetail("service", "httpbingo"))

	require.Equal(t, apperrors.CodeInternalError, apperrors.CodeOf(err))
	require.True(t, errors.Is(err, cause))
	require.Equal(t, "httpbingo", apperrors.PublicDetailsOf(err)["service"])
}

// TestWrapDBContext 确认数据库错误会写入 trace、domain 和 hint 等内部 cause metadata。
func TestWrapDBContext(t *testing.T) {
	ctx := requestctx.WithTraceID(context.Background(), "trace-1")
	cause := errors.New("driver: duplicate key")

	err := WrapDBContext(ctx, cause, "query user failed", WithCauseAttr("sql", "select * from users"))
	info := logx.ExtractError(err, logx.StackFull, logx.RedactConfig{Enabled: true})

	require.Equal(t, apperrors.DomainDB, info.Domain)
	require.Equal(t, apperrors.HintDBConnection, info.Hint)
	require.Equal(t, "trace-1", info.TraceID)
	require.Equal(t, "[REDACTED]", info.Context["sql"])
	require.Empty(t, apperrors.PublicDetailsOf(err))
	require.Contains(t, info.Stacktrace, "errors_test.go")
	require.NotContains(t, info.Stacktrace, "bizerrors/errors.go")
	require.NotContains(t, info.Stacktrace, "internal/platform/errors/error.go")
}
