package httpdemo

import (
	"github.com/samber/do"
	"github.com/teamsillybees/glean/internal/platform/httpclient"
)

const httpBingoServiceName = "httpbingo"

// Provide 使用 do 注册 httpdemo 示例模块依赖。
func Provide(injector *do.Injector) {
	httpclient.Provide(injector, httpBingoServiceName, NewService)
	do.Provide(injector, func(i *do.Injector) (*Handler, error) {
		service := do.MustInvoke[*Service](i)
		return NewHandler(service), nil
	})
	do.Provide(injector, func(i *do.Injector) (*Module, error) {
		handler := do.MustInvoke[*Handler](i)
		return NewModule(handler), nil
	})
}
