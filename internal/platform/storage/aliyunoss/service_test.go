package aliyunoss

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/storage"
)

func TestNewBuildsAliyunServiceWithoutNetworkAccess(t *testing.T) {
	service, err := New(context.Background(), storage.Config{
		Enabled:  true,
		Provider: storage.ProviderAliyunOSS,
		Bucket:   "example-bucket",
		Aliyun: storage.AliyunConfig{
			Endpoint:        "https://oss-cn-hangzhou.aliyuncs.com",
			AccessKeyID:     "test-access-key",
			AccessKeySecret: "test-secret-key",
			UseHTTPS:        true,
		},
	})

	require.NoError(t, err)
	require.NotNil(t, service)
	require.Equal(t, "https://example-bucket.oss-cn-hangzhou.aliyuncs.com/path/file.txt", service.publicURL("example-bucket", "path/file.txt"))
}

func TestNewRejectsMismatchedProvider(t *testing.T) {
	_, err := New(context.Background(), storage.Config{Enabled: true, Provider: storage.ProviderLocal})
	require.ErrorIs(t, err, storage.ErrInvalidConfig)
}
