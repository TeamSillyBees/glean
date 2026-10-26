package awss3

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/storage"
)

func TestNewBuildsAWSServiceWithoutNetworkAccess(t *testing.T) {
	service, err := New(context.Background(), storage.Config{
		Enabled: true,
		Bucket:  "example-bucket",
		S3: storage.S3Config{
			Region:          "us-east-1",
			Endpoint:        "https://s3.amazonaws.com",
			AccessKeyID:     "test-access-key",
			SecretAccessKey: "test-secret-key",
			UseHTTPS:        true,
		},
	})

	require.NoError(t, err)
	require.NotNil(t, service)
}

func TestNewRejectsMismatchedProvider(t *testing.T) {
	_, err := New(context.Background(), storage.Config{Enabled: true, Provider: storage.ProviderLocal})
	require.ErrorIs(t, err, storage.ErrInvalidConfig)
}
