package redisx

import (
	"context"

	"github.com/redis/go-redis/v9"
	"github.com/samber/do"
	"github.com/teamsillybees/glean/internal/platform/logx"
)

// Register 将 Redis 客户端注册到 DI 容器，根据 redis.mode 配置自动选择
// standalone 或 sentinel 模式。业务代码只需依赖 UniversalClient 接口。
func Register(injector *do.Injector, cfg Config) {
	do.Provide(injector, func(i *do.Injector) (redis.UniversalClient, error) {
		logger := do.MustInvoke[*logx.Logger](i)
		return NewClient(context.Background(), cfg, logger)
	})
}
