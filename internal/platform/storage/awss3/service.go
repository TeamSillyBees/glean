package awss3

import (
	"context"
	"fmt"

	"github.com/teamsillybees/glean/internal/platform/storage"
	"github.com/teamsillybees/glean/internal/platform/storage/s3compat"
)

// New 创建 AWS S3 存储服务。
func New(ctx context.Context, cfg storage.Config) (*s3compat.Service, error) {
	if cfg.Provider == "" {
		cfg.Provider = storage.ProviderAWSS3
	}
	cfg = storage.ConfigWithDefaults(cfg)
	if cfg.Provider != storage.ProviderAWSS3 {
		return nil, fmt.Errorf("%w: awss3 provider 需要 storage.provider=aws_s3", storage.ErrInvalidConfig)
	}
	return s3compat.New(ctx, cfg)
}
