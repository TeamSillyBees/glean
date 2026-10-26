package provider

import (
	"context"

	"github.com/samber/do"
	"github.com/teamsillybees/glean/internal/platform/storage"
)

// Register 将存储服务注册到 DI 容器，根据 storage.provider 配置自动选择底层实现。
// 业务代码只需依赖 storage.Service 接口，不感知具体云厂商 SDK。
func Register(injector *do.Injector, cfg storage.Config) {
	do.Provide(injector, func(i *do.Injector) (storage.Service, error) {
		return New(context.Background(), cfg)
	})
}
