package apperrors

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
)

type typedCause struct {
	message string
}

func (e *typedCause) Error() string {
	return e.message
}

// TestWrapKeepsStandardErrorChain 验证 oops 包装后仍保留标准错误链路。
func TestWrapKeepsStandardErrorChain(t *testing.T) {
	cause := &typedCause{message: "driver: duplicate key"}
	err := Wrap(cause, CodeDBError, "create user failed")

	require.NotNil(t, errors.Unwrap(err))
	require.True(t, errors.Is(err, cause))

	var target *typedCause
	require.True(t, errors.As(err, &target))
	require.Same(t, cause, target)
}

// TestWrapContextWritesOopsMetadata 验证 WrapContext 会把内部元数据写入 oops。
func TestWrapContextWritesOopsMetadata(t *testing.T) {
	ctx := requestctx.WithTraceID(context.Background(), "trace-1")
	err := WrapContext(ctx, errors.New("redis timeout"), CodeCacheError, "read cache failed",
		WithCauseDomain(DomainCache),
		WithCauseHint(HintRedisTimeout),
		WithCauseAttr("operation", "get_user"),
		WithCauseAttr("token", "secret-token"),
	)

	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	require.Equal(t, CodeCacheError, CodeOf(err))
	require.Equal(t, DomainCache, oopsErr.Domain())
	require.Equal(t, HintRedisTimeout, oopsErr.Hint())
	require.Equal(t, "trace-1", oopsErr.Trace())
	require.Equal(t, "get_user", oopsErr.Context()["operation"])
	require.Equal(t, "secret-token", oopsErr.Context()["token"])
	require.Empty(t, PublicDetailsOf(err))
}

// TestNewSetsPublicForClientErrors 验证 4xx 源头错误默认将 message 作为公开消息。
func TestNewSetsPublicForClientErrors(t *testing.T) {
	err := New(CodeBadRequest, "invalid request")

	require.Equal(t, "invalid request", PublicMessageOf(err))
	require.Equal(t, CodeBadRequest, CodeOf(err))
}

// TestWrapDoesNotExposeInternalMessageAsPublic 验证 5xx 错误默认不把内部语义返回给用户。
func TestWrapDoesNotExposeInternalMessageAsPublic(t *testing.T) {
	err := Wrap(errors.New("driver: duplicate key"), CodeDBError, "create user failed")

	require.Contains(t, err.Error(), "duplicate key")
	require.Equal(t, "internal error", PublicMessageOf(err))
	require.Equal(t, CodeDBError, CodeOf(err))
}

// TestWrapExistingOopsDoesNotOverrideRootMetadata 确认二次包装不覆盖源头业务码和公开消息。
func TestWrapExistingOopsDoesNotOverrideRootMetadata(t *testing.T) {
	root := New(CodeBadRequest, "invalid request", WithDetail("field", "name"))

	err := Wrap(root, CodeInternalError, "service failed")

	require.Equal(t, CodeBadRequest, CodeOf(err))
	require.Equal(t, http.StatusBadRequest, StatusOf(err))
	require.Equal(t, "invalid request", PublicMessageOf(err))
	require.Equal(t, "name", PublicDetailsOf(err)["field"])
	require.Contains(t, err.Error(), "service failed")
	require.Contains(t, err.Error(), "invalid request")
}

// TestOopsStacktraceSkipsPackageHelpers 验证 internal/platform/errors 自身封装不会出现在 oops 栈顶。
func TestOopsStacktraceSkipsPackageHelpers(t *testing.T) {
	err := Wrap(errors.New("driver: duplicate key"), CodeDBError, "create user failed")
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)

	stack := oopsErr.Stacktrace()

	require.Contains(t, stack, "error_test.go")
	require.NotContains(t, stack, "error.go")
	require.NotContains(t, stack, "Wrap()")
}

// TestWithCallerSkipSkipsOuterHelper 验证二次错误 helper 可以继续跳过自身栈帧。
func TestWithCallerSkipSkipsOuterHelper(t *testing.T) {
	err := wrapWithOuterHelper()
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)

	stack := oopsErr.Stacktrace()

	require.Contains(t, stack, "TestWithCallerSkipSkipsOuterHelper")
	require.NotContains(t, stack, "wrapWithOuterHelper")
	require.NotContains(t, stack, "WrapContext")
}

// wrapWithOuterHelper 模拟业务侧再次封装 apperrors 的 helper。
func wrapWithOuterHelper() error {
	return WrapContext(context.Background(), errors.New("redis timeout"), CodeCacheError, "read cache failed", WithCallerSkip(1))
}
