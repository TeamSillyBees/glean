package s3compat

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/storage"
)

func TestNewSupportsAWSAndS3CompatibleProviders(t *testing.T) {
	for _, provider := range []storage.Provider{storage.ProviderAWSS3, storage.ProviderS3Compatible} {
		t.Run(string(provider), func(t *testing.T) {
			cfg := storage.Config{
				Enabled:  true,
				Provider: provider,
				Bucket:   "example-bucket",
				S3: storage.S3Config{
					Region:          "us-east-1",
					Endpoint:        "https://s3.example.test",
					AccessKeyID:     "test-access-key",
					SecretAccessKey: "test-secret-key",
					UsePathStyle:    true,
					UseHTTPS:        true,
				},
			}

			service, err := New(context.Background(), cfg)
			require.NoError(t, err)
			require.NotNil(t, service)
			require.Equal(t, "https://s3.example.test/example-bucket/path/file.txt", service.publicURL("example-bucket", "path/file.txt"))
		})
	}
}

func TestBucketAndKeyRejectsTraversal(t *testing.T) {
	service, err := New(context.Background(), storage.Config{
		Enabled:  true,
		Provider: storage.ProviderS3Compatible,
		Bucket:   "example-bucket",
		S3: storage.S3Config{
			Region:          "us-east-1",
			Endpoint:        "https://s3.example.test",
			AccessKeyID:     "test-access-key",
			SecretAccessKey: "test-secret-key",
		},
	})
	require.NoError(t, err)

	_, _, err = service.bucketAndKey("", "../secret.txt")
	require.ErrorIs(t, err, storage.ErrInvalidKey)
}
