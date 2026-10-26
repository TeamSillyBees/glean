package server

import (
	"github.com/casbin/casbin/v2"
	"github.com/samber/do"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/logx"
)

// Register 将 Web 应用注册到 DI 容器。
func Register(injector *do.Injector, opts Options) {
	do.Provide(injector, func(i *do.Injector) (*App, error) {
		logger := do.MustInvoke[*logx.Logger](i)
		jwtManager := do.MustInvoke[*platformauth.JWTManager](i)
		resolver := do.MustInvoke[platformauth.IdentityResolver](i)
		enforcer := do.MustInvoke[*casbin.SyncedEnforcer](i)
		return NewApp(opts, logger, jwtManager, resolver, enforcer)
	})
}
