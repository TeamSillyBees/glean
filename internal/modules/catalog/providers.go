package catalog

import (
	"github.com/samber/do"
	"github.com/teamsillybees/glean/internal/data/ent"
)

// Provide 显式注册目录依赖，业务服务不持有依赖容器。
func Provide(injector *do.Injector) {
	do.Provide(injector, func(i *do.Injector) (*Service, error) { return NewService(do.MustInvoke[*ent.Client](i)), nil })
	do.Provide(injector, func(i *do.Injector) (*Handler, error) { return NewHandler(do.MustInvoke[*Service](i)), nil })
	do.Provide(injector, func(i *do.Injector) (*Module, error) { return NewModule(do.MustInvoke[*Handler](i)), nil })
}
