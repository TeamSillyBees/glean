package user

import (
	"github.com/samber/do"
	"github.com/teamsillybees/glean/internal/accesscontrol"
	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/platform/auth"
	platformcache "github.com/teamsillybees/glean/internal/platform/cache"
)

const (
	userCacheServiceName   = "user.cache"
	userServiceServiceName = "user.service"
	userHandlerServiceName = "user.handler"
)

// Provide 使用 do 注册 user 模块依赖。
func Provide(injector *do.Injector) {
	do.ProvideNamed(injector, userCacheServiceName, func(i *do.Injector) (*UserCache, error) {
		manager := do.MustInvoke[*platformcache.Manager](i)
		return NewUserCache(manager), nil
	})
	do.ProvideNamed(injector, userServiceServiceName, func(i *do.Injector) (*Service, error) {
		client := do.MustInvoke[*ent.Client](i)
		cache := do.MustInvokeNamed[*UserCache](i, userCacheServiceName)
		passwords := do.MustInvoke[*auth.BcryptPasswordManager](i)
		invalidator := do.MustInvoke[accesscontrol.Invalidator](i)
		return NewService(client, cache, passwords, invalidator), nil
	})
	do.ProvideNamed(injector, userHandlerServiceName, func(i *do.Injector) (*Handler, error) {
		service := do.MustInvokeNamed[*Service](i, userServiceServiceName)
		return NewHandler(service), nil
	})
	do.Provide(injector, func(i *do.Injector) (*Module, error) {
		handler := do.MustInvokeNamed[*Handler](i, userHandlerServiceName)
		return NewModule(handler), nil
	})
}
