package logx

import (
	"github.com/samber/do"
)

// Register 将 logx Logger 注册到 DI 容器。
func Register(injector *do.Injector, cfg Config) {
	do.Provide(injector, func(i *do.Injector) (*Logger, error) {
		return NewLogger(cfg)
	})
}
