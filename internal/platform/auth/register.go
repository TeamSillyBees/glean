package auth

import (
	"fmt"
	"strings"

	"github.com/casbin/casbin/v2"
	"github.com/samber/do"
	"github.com/teamsillybees/glean/internal/platform/redisx"
)

// RegisterOptions 描述认证相关组件注册所需的输入。
type RegisterOptions struct {
	AppName          string
	Env              string
	PasswordCost     int
	JWT              JWTConfig
	RedisEnabled     bool
	AllowMemoryStore bool
	CasbinModelPath  string
	LoginProtection  LoginProtectionConfig
}

// Register 将密码管理器、JWT 管理器和 Casbin Enforcer 注册到 DI 容器。
func Register(injector *do.Injector, opts RegisterOptions) {
	do.Provide(injector, func(i *do.Injector) (*BcryptPasswordManager, error) {
		return NewBcryptPasswordManager(opts.PasswordCost), nil
	})
	do.Provide(injector, func(i *do.Injector) (*JWTManager, error) {
		cfg := opts.JWT
		store, err := tokenStoreFromInjector(i, opts)
		if err != nil {
			return nil, err
		}
		cfg.Store = store
		return NewJWTManager(cfg)
	})
	do.Provide(injector, func(i *do.Injector) (*casbin.SyncedEnforcer, error) {
		loader, err := do.Invoke[PolicyLoader](i)
		if err != nil {
			return nil, fmt.Errorf("解析数据库权限策略加载器失败: %w", err)
		}
		return NewEnforcer(opts.CasbinModelPath, loader)
	})
	do.Provide(injector, func(i *do.Injector) (LoginGuard, error) {
		return loginGuardFromInjector(i, opts)
	})
}

func loginGuardFromInjector(injector *do.Injector, opts RegisterOptions) (LoginGuard, error) {
	if !opts.LoginProtection.Enabled {
		return noopLoginGuard{}, nil
	}
	if !opts.RedisEnabled {
		if !opts.AllowMemoryStore || !isMemoryStoreAllowedEnvironment(opts.Env) {
			return nil, fmt.Errorf("login protection 必须使用 Redis，进程内实现只允许用于显式启用的 dev、local 或 test 环境")
		}
		return NewMemoryLoginGuard(opts.LoginProtection)
	}
	client, err := do.Invoke[redisx.UniversalClient](injector)
	if err != nil {
		return nil, fmt.Errorf("解析 login protection Redis client 失败: %w", err)
	}
	return NewRedisLoginGuard(opts.AppName, opts.Env, client, opts.LoginProtection)
}

func tokenStoreFromInjector(injector *do.Injector, opts RegisterOptions) (TokenStore, error) {
	if !opts.RedisEnabled {
		if !opts.AllowMemoryStore {
			return nil, fmt.Errorf("auth memory token store 未显式启用；请配置 Redis 共享存储")
		}
		if !isMemoryStoreAllowedEnvironment(opts.Env) {
			return nil, fmt.Errorf("auth memory token store 只允许用于 dev、local 或 test 环境")
		}
		return NewMemoryTokenStore(), nil
	}
	client, err := do.Invoke[redisx.UniversalClient](injector)
	if err != nil {
		return nil, fmt.Errorf("解析 auth Redis client 失败: %w", err)
	}
	if client == nil {
		return nil, fmt.Errorf("auth Redis client 不能为空")
	}
	store, err := NewRedisTokenStoreWithEnv(opts.AppName, opts.Env, client)
	if err != nil {
		return nil, fmt.Errorf("创建 auth Redis token store 失败: %w", err)
	}
	return store, nil
}

func isMemoryStoreAllowedEnvironment(env string) bool {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "dev", "local", "test":
		return true
	default:
		return false
	}
}
