package provider

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/storage"
	"github.com/teamsillybees/glean/internal/platform/storage/local"
)

func TestNewReturnsNilWhenDisabled(t *testing.T) {
	service, err := New(context.Background(), storage.Config{})
	require.NoError(t, err)
	require.Nil(t, service)
}

func TestNewCreatesLocalProvider(t *testing.T) {
	service, err := New(context.Background(), storage.Config{
		Enabled:  true,
		Provider: storage.ProviderLocal,
		Local: storage.LocalConfig{
			RootDir: t.TempDir(),
		},
	})
	require.NoError(t, err)
	require.IsType(t, &local.Service{}, service)
}

func TestNewSTSReturnsUnsupportedForLocalProvider(t *testing.T) {
	stsService, err := NewSTS(context.Background(), storage.Config{
		Enabled:  true,
		Provider: storage.ProviderLocal,
		STS: storage.STSConfig{
			Enabled: true,
		},
	})
	require.ErrorIs(t, err, storage.ErrUnsupported)
	require.Nil(t, stsService)
}
