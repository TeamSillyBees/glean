package provider

import (
	"context"
	"fmt"

	"github.com/teamsillybees/glean/internal/platform/storage"
	"github.com/teamsillybees/glean/internal/platform/storage/aliyunoss"
	"github.com/teamsillybees/glean/internal/platform/storage/awss3"
	"github.com/teamsillybees/glean/internal/platform/storage/local"
	"github.com/teamsillybees/glean/internal/platform/storage/s3compat"
	"github.com/teamsillybees/glean/internal/platform/storage/tencentcos"
)

// New 根据配置创建统一存储服务；未启用时返回 nil。
func New(ctx context.Context, cfg storage.Config) (storage.Service, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	cfg = storage.ConfigWithDefaults(cfg)
	switch cfg.Provider {
	case storage.ProviderLocal:
		return local.New(cfg)
	case storage.ProviderAWSS3:
		return awss3.New(ctx, cfg)
	case storage.ProviderS3Compatible:
		return s3compat.New(ctx, cfg)
	case storage.ProviderAliyunOSS:
		return aliyunoss.New(ctx, cfg)
	case storage.ProviderTencentCOS:
		return tencentcos.New(ctx, cfg)
	default:
		return nil, fmt.Errorf("%w: storage.provider %q 不受支持", storage.ErrInvalidConfig, cfg.Provider)
	}
}

// NewSTS 根据配置创建临时授权服务；未启用 STS 时返回 nil。
func NewSTS(ctx context.Context, cfg storage.Config) (storage.STSService, error) {
	if !cfg.Enabled || !cfg.STS.Enabled {
		return nil, nil
	}
	cfg = storage.ConfigWithDefaults(cfg)
	switch cfg.Provider {
	case storage.ProviderAWSS3:
		return awss3.NewSTS(ctx, cfg)
	case storage.ProviderAliyunOSS:
		return aliyunoss.NewSTS(ctx, cfg)
	case storage.ProviderTencentCOS:
		return tencentcos.NewSTS(ctx, cfg)
	case storage.ProviderLocal, storage.ProviderS3Compatible:
		return nil, storage.ErrUnsupported
	default:
		return nil, fmt.Errorf("%w: storage.provider %q 不受支持", storage.ErrInvalidConfig, cfg.Provider)
	}
}
