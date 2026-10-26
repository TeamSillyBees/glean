package rbac

import (
	"github.com/samber/do"
	"github.com/teamsillybees/glean/internal/accesscontrol"
	"github.com/teamsillybees/glean/internal/data/ent"
)

const (
	serviceName = "rbac.service"
	handlerName = "rbac.handler"
)

// Provide 注册 RBAC 管理模块。
func Provide(injector *do.Injector) {
	do.ProvideNamed(injector, serviceName, func(i *do.Injector) (*Service, error) {
		return NewService(do.MustInvoke[*ent.Client](i), do.MustInvoke[accesscontrol.Invalidator](i)), nil
	})
	do.ProvideNamed(injector, handlerName, func(i *do.Injector) (*Handler, error) {
		return NewHandler(do.MustInvokeNamed[*Service](i, serviceName)), nil
	})
	do.Provide(injector, func(i *do.Injector) (*Module, error) {
		return NewModule(do.MustInvokeNamed[*Handler](i, handlerName)), nil
	})
}
