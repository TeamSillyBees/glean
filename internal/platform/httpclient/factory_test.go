package httpclient

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/logx"
)

func TestFactoryCachesAndClearsClient(t *testing.T) {
	factory := newTestFactory(t, "http://example.test", ServiceConfig{})

	first, err := factory.Get("svc")
	require.NoError(t, err)
	second, err := factory.Get("svc")
	require.NoError(t, err)
	require.Same(t, first, second)

	factory.Clear("svc")
	third, err := factory.Get("svc")
	require.NoError(t, err)
	require.NotSame(t, first, third)

	factory.ClearAll()
	fourth, err := factory.Get("svc")
	require.NoError(t, err)
	require.NotSame(t, third, fourth)
}

func TestFactoryGetRejectsUnknownService(t *testing.T) {
	factory := newTestFactory(t, "http://example.test", ServiceConfig{})

	_, err := factory.Get("missing")

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrServiceNotFound))
}

func TestFactoryGetRejectsDisabledConfig(t *testing.T) {
	factory, err := NewFactory(Config{Enabled: false}, logx.NewNop())
	require.NoError(t, err)

	_, err = factory.Get("svc")

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrDisabled))
}

func newTestFactory(t *testing.T, baseURL string, override ServiceConfig) *Factory {
	t.Helper()
	service := ServiceConfig{
		BaseURL: baseURL,
	}
	if override.Auth.Type != "" {
		service.Auth = override.Auth
	}
	if override.Headers != nil {
		service.Headers = override.Headers
	}
	if override.Retry.Enabled {
		service.Retry = override.Retry
	}
	if override.Timeout != 0 {
		service.Timeout = override.Timeout
	}
	if override.Proxy != "" {
		service.Proxy = override.Proxy
	}
	factory, err := NewFactory(Config{
		Enabled: true,
		Services: map[string]ServiceConfig{
			"svc": service,
		},
	}, logx.NewNop())
	require.NoError(t, err)
	return factory
}
