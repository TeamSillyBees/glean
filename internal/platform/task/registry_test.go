package task

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/logx"
)

func TestRegistryRejectsDuplicate(t *testing.T) {
	registry := NewRegistry()

	err := registry.Register("demo:send_email", HandlerFunc(func(context.Context, Task) error { return nil }))
	require.NoError(t, err)

	err = registry.Register("demo:send_email", HandlerFunc(func(context.Context, Task) error { return nil }))
	require.Error(t, err)
}

func TestRegistryMergesMetadataAndValidatesBizKey(t *testing.T) {
	registry := NewRegistry(BizKeyValidationMiddleware())
	err := registry.Register("demo:send_email",
		HandlerFunc(func(context.Context, Task) error { return nil }),
		WithRegisterModule("demo"),
		WithRegisterOwner("platform"),
		WithRegisterBizKeyRequired(true),
	)
	require.NoError(t, err)

	handler, ok := registry.Handler("demo:send_email")
	require.True(t, ok)

	err = handler.HandleTask(context.Background(), Task{Type: "demo:send_email"})

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrMissingBizKey))
}

func TestRecoverMiddlewareConvertsPanicToError(t *testing.T) {
	handler := RecoverMiddleware(logx.NewNop())(HandlerFunc(func(context.Context, Task) error {
		panic("boom")
	}))

	err := handler.HandleTask(context.Background(), Task{Type: "demo:send_email"})

	require.ErrorContains(t, err, "boom")
}
