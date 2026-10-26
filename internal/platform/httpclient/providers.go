package httpclient

import (
	"fmt"

	"github.com/samber/do"
	"github.com/teamsillybees/glean/internal/platform/logx"
)

const clientNamePrefix = "httpclient.client."

// ClientName 返回指定服务名在 DI 容器中的 HTTP Client 命名依赖。
func ClientName(serviceName string) string {
	return clientNamePrefix + serviceName
}

// Register 将 HTTP Client 工厂和已配置服务的命名 Client 注册到 DI 容器。
//
// 调用方需要先向容器注册 *logx.Logger。业务模块优先使用 Provide 将命名服务
// 注入 Executor；只有依赖 Client 专有能力时才显式解析 ClientName。
func Register(injector *do.Injector, cfg Config, options ...FactoryOption) {
	do.Provide(injector, func(i *do.Injector) (*Factory, error) {
		logger := do.MustInvoke[*logx.Logger](i)
		return NewFactory(cfg, logger, options...)
	})
	for serviceName := range cfg.Services {
		registerClient(injector, serviceName)
	}
}

// Provide 注册只依赖一个远程服务 Executor 的业务组件。
// 业务类型在不同 package 中天然具有不同 DI 类型名，因此无需额外维护 providerName。
func Provide[T any](injector *do.Injector, serviceName string, constructor func(Executor) *T) {
	do.Provide(injector, func(i *do.Injector) (*T, error) {
		client, err := do.InvokeNamed[*Client](i, ClientName(serviceName))
		if err != nil {
			return nil, fmt.Errorf("resolve HTTP client %s: %w", serviceName, err)
		}
		return constructor(client), nil
	})
}

// ProvideE 注册构造过程可能失败、且只依赖一个远程服务 Executor 的业务组件。
func ProvideE[T any](injector *do.Injector, serviceName string, constructor func(Executor) (*T, error)) {
	do.Provide(injector, func(i *do.Injector) (*T, error) {
		client, err := do.InvokeNamed[*Client](i, ClientName(serviceName))
		if err != nil {
			return nil, fmt.Errorf("resolve HTTP client %s: %w", serviceName, err)
		}
		return constructor(client)
	})
}

func registerClient(injector *do.Injector, serviceName string) {
	do.ProvideNamed(injector, ClientName(serviceName), func(i *do.Injector) (*Client, error) {
		factory := do.MustInvoke[*Factory](i)
		return factory.Get(serviceName)
	})
}
