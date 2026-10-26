package idgen

import "github.com/samber/do"

// Register 将雪花算法 ID 生成器注册到 DI 容器。
func Register(injector *do.Injector, node int64) {
	generator, err := ConfigureDefault(node)
	do.Provide(injector, func(i *do.Injector) (*Generator, error) {
		if err != nil {
			return nil, err
		}
		return generator, nil
	})
}
