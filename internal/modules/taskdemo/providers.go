package taskdemo

import (
	"github.com/samber/do"
	"github.com/teamsillybees/glean/internal/platform/task"
)

const (
	taskDemoServiceName = "taskdemo.service"
	taskDemoHandlerName = "taskdemo.handler"
)

// Provide 使用 do 注册 taskdemo 示例模块依赖。
func Provide(injector *do.Injector) {
	do.ProvideNamed(injector, taskDemoServiceName, func(i *do.Injector) (*Service, error) {
		publisher := do.MustInvoke[task.Publisher](i)
		return NewService(publisher), nil
	})
	do.ProvideNamed(injector, taskDemoHandlerName, func(i *do.Injector) (*Handler, error) {
		service := do.MustInvokeNamed[*Service](i, taskDemoServiceName)
		return NewHandler(service), nil
	})
	do.Provide(injector, func(i *do.Injector) (*Module, error) {
		handler := do.MustInvokeNamed[*Handler](i, taskDemoHandlerName)
		return NewModule(handler), nil
	})
}
