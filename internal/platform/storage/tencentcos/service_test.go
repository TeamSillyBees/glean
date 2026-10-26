package tencentcos

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/storage"
)

func TestNewBuildsTencentServiceWithoutNetworkAccess(t *testing.T) {
	service, err := New(context.Background(), storage.Config{
		Enabled:  true,
		Provider: storage.ProviderTencentCOS,
		Bucket:   "example-bucket-1250000000",
		Tencent: storage.TencentConfig{
			Region:    "ap-guangzhou",
			SecretID:  "test-secret-id",
			SecretKey: "test-secret-key",
			UseHTTPS:  true,
		},
	})

	require.NoError(t, err)
	require.NotNil(t, service)
	require.Equal(t, "https://example-bucket-1250000000.cos.ap-guangzhou.myqcloud.com/path/file.txt", service.publicURL("example-bucket-1250000000", "path/file.txt"))
}

func TestNewRejectsMismatchedProvider(t *testing.T) {
	_, err := New(context.Background(), storage.Config{Enabled: true, Provider: storage.ProviderLocal})
	require.ErrorIs(t, err, storage.ErrInvalidConfig)
}
