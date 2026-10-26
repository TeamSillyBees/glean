package boot

import (
	"database/sql"

	"github.com/samber/do"
	"github.com/teamsillybees/glean/internal/accesscontrol"
	"github.com/teamsillybees/glean/internal/data"
	"github.com/teamsillybees/glean/internal/data/ent"
	authmodule "github.com/teamsillybees/glean/internal/modules/auth"
	catalogmodule "github.com/teamsillybees/glean/internal/modules/catalog"
	filemodule "github.com/teamsillybees/glean/internal/modules/file"
	httpdemomodule "github.com/teamsillybees/glean/internal/modules/httpdemo"
	monitormodule "github.com/teamsillybees/glean/internal/modules/monitor"
	rbacmodule "github.com/teamsillybees/glean/internal/modules/rbac"
	taskdemomodule "github.com/teamsillybees/glean/internal/modules/taskdemo"
	usermodule "github.com/teamsillybees/glean/internal/modules/user"
	"github.com/teamsillybees/glean/internal/platform/auth"
	platformcache "github.com/teamsillybees/glean/internal/platform/cache"
	platformdatabase "github.com/teamsillybees/glean/internal/platform/database"
	"github.com/teamsillybees/glean/internal/platform/httpclient"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/logx"
	"github.com/teamsillybees/glean/internal/platform/observability"
	"github.com/teamsillybees/glean/internal/platform/redisx"
	"github.com/teamsillybees/glean/internal/platform/server"
	platformstorage "github.com/teamsillybees/glean/internal/platform/storage"
	storageprovider "github.com/teamsillybees/glean/internal/platform/storage/provider"
	"github.com/teamsillybees/glean/internal/platform/task/asynqadapter"
)

func registerProviders(injector *do.Injector, cfg *Config, buildInfo observability.BuildInfoVO) {
	logx.Register(injector, cfg.Log)
	httpclient.Register(injector, cfg.HTTPClient)
	redisx.Register(injector, cfg.Redis)
	asynqadapter.Register(injector, cfg.Task)
	platformcache.Register(injector, platformcache.Config{
		AppName:       cfg.App.Slug,
		LocalTTL:      cfg.Cache.LocalTTL,
		RemoteTTL:     cfg.Cache.RemoteTTL,
		RemoteEnabled: cfg.Redis.Enabled,
	})
	idgen.Register(injector, cfg.IDGen.Node)
	storageprovider.Register(injector, cfg.Storage)
	platformdatabase.Register(injector, data.SQLDBConfig(cfg.Database))
	do.Provide(injector, func(i *do.Injector) (*ent.Client, error) {
		generator := do.MustInvoke[*idgen.Generator](i)
		db := do.MustInvoke[*sql.DB](i)
		return data.NewEntClientFromDB(db, generator), nil
	})
	accesscontrol.Provide(injector, accesscontrol.Options{
		AppName:      cfg.App.Slug,
		Env:          cfg.App.Env,
		InstanceID:   cfg.App.InstanceID,
		CacheTTL:     cfg.Cache.RemoteTTL,
		RedisEnabled: cfg.Redis.Enabled,
	})
	auth.Register(injector, auth.RegisterOptions{
		AppName:          cfg.App.Slug,
		Env:              cfg.App.Env,
		RedisEnabled:     cfg.Redis.Enabled,
		AllowMemoryStore: cfg.Auth.AllowMemoryTokenStore,
		JWT: auth.JWTConfig{
			Issuer:          cfg.Auth.JWT.Issuer,
			Secret:          cfg.Auth.JWT.Secret,
			AccessTokenTTL:  cfg.Auth.AccessTokenTTL,
			RefreshTokenTTL: cfg.Auth.RefreshTokenTTL,
		},
		CasbinModelPath: cfg.Casbin.ModelPath,
		LoginProtection: cfg.Auth.LoginProtection,
	})
	server.Register(injector, server.Options{
		Title:          cfg.App.Name,
		Version:        buildInfo.Version,
		Env:            cfg.App.Env,
		TrustedProxies: append([]string(nil), cfg.Server.TrustedProxies...),
		CORS:           cfg.Server.CORS,
		Docs:           cfg.Server.Docs,
	})
}

func registerModules(injector *do.Injector, cfg *Config) {
	// 新增业务模块时，先在 internal/modules/<module>/providers.go 暴露 Provide，
	// 再在这里把模块依赖注册进同一个 do 容器。
	usermodule.Provide(injector)
	authmodule.Provide(injector)
	filemodule.Provide(injector, storageproviderMaxFileSize(cfg))
	httpdemomodule.Provide(injector)
	taskdemomodule.Provide(injector)
	rbacmodule.Provide(injector)
	catalogmodule.Provide(injector)
	monitormodule.Provide(injector)
}

func storageproviderMaxFileSize(cfg *Config) int64 {
	return platformstorage.ConfigWithDefaults(cfg.Storage).Local.MaxSize
}

func registerRoutes(
	injector *do.Injector,
	webApp *server.App,
	buildInfo observability.BuildInfoVO,
	readiness *observability.ReadinessRegistry,
) {
	cfg := do.MustInvoke[*Config](injector)
	if cfg.Observability.Health.Enabled {
		observability.NewModule(buildInfo, readiness).Register(webApp.API, webApp.Registry)
	}

	// 新增业务模块完成依赖注册后，需要在这里解析 Module 并调用 Register。
	// Register 内部应同时注册 Huma operation 与 RouteSecurity，避免 /api 路由因缺少安全元信息被拒绝。
	do.MustInvoke[*usermodule.Module](injector).Register(webApp.API, webApp.Registry)
	do.MustInvoke[*authmodule.Module](injector).Register(webApp.API, webApp.Registry)
	do.MustInvoke[*filemodule.Module](injector).Register(webApp.API, webApp.Registry)
	do.MustInvoke[*httpdemomodule.Module](injector).Register(webApp.API, webApp.Registry)
	do.MustInvoke[*taskdemomodule.Module](injector).Register(webApp.API, webApp.Registry)
	do.MustInvoke[*rbacmodule.Module](injector).Register(webApp.API, webApp.Registry)
	do.MustInvoke[*catalogmodule.Module](injector).Register(webApp.API, webApp.Registry)
	do.MustInvoke[*monitormodule.Module](injector).Register(webApp.API, webApp.Registry)
}
