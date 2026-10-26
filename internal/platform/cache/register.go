package cache

import (
	"github.com/samber/do"
	"github.com/teamsillybees/glean/internal/platform/redisx"
)

// Register 将缓存管理器注册到 DI 容器；启用远端缓存时会从容器中解析 Redis 客户端。
func Register(injector *do.Injector, cfg Config) {
	do.Provide(injector, func(i *do.Injector) (*Manager, error) {
		var client redisx.UniversalClient
		if cfg.RemoteEnabled {
			client = do.MustInvoke[redisx.UniversalClient](i)
		}
		return NewManager(cfg, client), nil
	})
}
