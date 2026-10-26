package auth

import (
	"github.com/samber/do"
	"github.com/teamsillybees/glean/internal/accesscontrol"
	"github.com/teamsillybees/glean/internal/data/ent"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/logx"
)

const (
	authServiceServiceName = "auth.service"
	authHandlerServiceName = "auth.handler"
)

// Provide 使用 do 注册 auth 模块依赖。
func Provide(injector *do.Injector) {
	do.ProvideNamed(injector, authServiceServiceName, func(i *do.Injector) (*Service, error) {
		client := do.MustInvoke[*ent.Client](i)
		passwords := do.MustInvoke[*platformauth.BcryptPasswordManager](i)
		jwtManager := do.MustInvoke[*platformauth.JWTManager](i)
		loginGuard := do.MustInvoke[platformauth.LoginGuard](i)
		invalidator := do.MustInvoke[accesscontrol.Invalidator](i)
		logger := do.MustInvoke[*logx.Logger](i)
		return NewService(client, passwords, jwtManager, loginGuard, invalidator, logger)
	})
	do.ProvideNamed(injector, authHandlerServiceName, func(i *do.Injector) (*Handler, error) {
		service := do.MustInvokeNamed[*Service](i, authServiceServiceName)
		return NewHandler(service), nil
	})
	do.Provide(injector, func(i *do.Injector) (*Module, error) {
		handler := do.MustInvokeNamed[*Handler](i, authHandlerServiceName)
		return NewModule(handler), nil
	})
}
