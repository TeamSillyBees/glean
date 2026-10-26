package auth

import (
	"testing"
	"time"

	"github.com/samber/do"
	"github.com/stretchr/testify/require"
)

func TestRegisterProvidesAuthComponents(t *testing.T) {
	injector := do.New()
	Register(injector, RegisterOptions{
		AppName:          "initra",
		Env:              "test",
		PasswordCost:     0,
		AllowMemoryStore: true,
		JWT: JWTConfig{
			Issuer:          "initra",
			Secret:          "register-test-secret-0123456789abcdef",
			AccessTokenTTL:  time.Minute,
			RefreshTokenTTL: time.Hour,
		},
	})

	passwordManager := do.MustInvoke[*BcryptPasswordManager](injector)
	jwtManager := do.MustInvoke[*JWTManager](injector)

	require.NotNil(t, passwordManager)
	require.NotNil(t, jwtManager)
}

// TestRegisterRejectsImplicitMemoryTokenStore 验证未显式允许时不会退化为进程内 token store。
func TestRegisterRejectsImplicitMemoryTokenStore(t *testing.T) {
	injector := do.New()
	Register(injector, RegisterOptions{
		AppName: "initra",
		Env:     "dev",
		JWT: JWTConfig{
			Issuer:          "initra",
			Secret:          "register-test-secret-0123456789abcdef",
			AccessTokenTTL:  time.Minute,
			RefreshTokenTTL: time.Hour,
		},
	})

	manager, err := do.Invoke[*JWTManager](injector)

	require.Nil(t, manager)
	require.ErrorContains(t, err, "未显式启用")
}

// TestRegisterAllowsMemoryTokenStoreOnlyInLocalEnvironments 验证显式内存 store 只在本地开发和测试环境开放。
func TestRegisterAllowsMemoryTokenStoreOnlyInLocalEnvironments(t *testing.T) {
	for _, env := range []string{"dev", "local", "test"} {
		t.Run(env, func(t *testing.T) {
			store, err := tokenStoreFromInjector(do.New(), RegisterOptions{
				Env:              env,
				AllowMemoryStore: true,
			})

			require.NoError(t, err)
			require.NotNil(t, store)
		})
	}
}

// TestRegisterRejectsMemoryTokenStoreInSharedEnvironments 验证所有其他环境均必须使用共享存储。
func TestRegisterRejectsMemoryTokenStoreInSharedEnvironments(t *testing.T) {
	for _, env := range []string{"prod", "production", "prd", "staging", "uat", "preview", ""} {
		name := env
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			store, err := tokenStoreFromInjector(do.New(), RegisterOptions{
				Env:              env,
				AllowMemoryStore: true,
			})

			require.Nil(t, store)
			require.ErrorContains(t, err, "只允许用于 dev、local 或 test 环境")
		})
	}
}
