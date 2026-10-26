package boot

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/teamsillybees/glean/internal/data"
	authmodule "github.com/teamsillybees/glean/internal/modules/auth"
	filemodule "github.com/teamsillybees/glean/internal/modules/file"
	httpdemomodule "github.com/teamsillybees/glean/internal/modules/httpdemo"
	taskdemomodule "github.com/teamsillybees/glean/internal/modules/taskdemo"
	usermodule "github.com/teamsillybees/glean/internal/modules/user"

	"github.com/samber/do"
	"github.com/stretchr/testify/require"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/observability"
)

// TestModuleProvideDoesNotCollide 验证 auth、user 和 file 模块使用具名依赖后不会在 DI 容器中冲突。
func TestModuleProvideDoesNotCollide(t *testing.T) {
	injector := do.New()

	require.NotPanics(t, func() {
		usermodule.Provide(injector)
		authmodule.Provide(injector)
		filemodule.Provide(injector, 10*1024*1024)
		httpdemomodule.Provide(injector)
		taskdemomodule.Provide(injector)
	})
}

// TestRegisterProvidersDoesNotConnectDuringRegistration 验证注册 provider 本身不触发数据库连接。
func TestRegisterProvidersDoesNotConnectDuringRegistration(t *testing.T) {
	injector := do.New()
	cfg := &Config{
		App: AppConfig{
			Name: "example-api",
			Slug: "example-api",
			Env:  "test",
		},
		Auth: AuthConfig{
			AllowMemoryTokenStore: true,
			AccessTokenTTL:        time.Minute,
			RefreshTokenTTL:       time.Hour,
			JWT: JWTConfig{
				Issuer: "example-api",
				Secret: "0123456789abcdef0123456789abcdef",
			},
			LoginProtection: platformauth.LoginProtectionConfig{
				Enabled:          true,
				AccountRateLimit: platformauth.LoginRateConfig{MaxAttempts: 10, Window: time.Minute},
				IPRateLimit:      platformauth.LoginRateConfig{MaxAttempts: 60, Window: time.Minute},
				Lockout:          platformauth.LoginLockConfig{MaxFailures: 5, FailureWindow: 15 * time.Minute, LockDuration: 15 * time.Minute},
			},
		},
		IDGen: IDGenConfig{Node: 1},
	}

	require.NotPanics(t, func() {
		registerProviders(injector, cfg, observability.BuildInfoVO{Version: "test"})
	})
}

// TestLoadConfigUsesRecommendedConfigShape 验证示例项目使用新的分组配置规范。
func TestLoadConfigUsesRecommendedConfigShape(t *testing.T) {
	configDir := t.TempDir()
	content := []byte(`
app:
  name: example-api
  slug: example-api
  version: 0.1.0
  instance_id: base-1

server:
  addr: ":8080"
  read_timeout: 10s
  write_timeout: 30s
  idle_timeout: 60s
  shutdown_timeout: 20s

database:
  host: "127.0.0.1"
  port: 5432
  user: "postgres"
  password: "postgres"
  dbname: "example"
  application_name: "example-api"
  connect_timeout: 5s
  max_open_conns: 20
  max_idle_conns: 10
  conn_max_idle_time: 15m
  conn_max_lifetime: 1h
  ping_timeout: 5s

redis:
  enabled: false
  addr: "127.0.0.1:6379"
  password: ""
  db: 0
  pool:
    size: 10

auth:
  enabled: true
  allow_memory_token_store: true
  access_token_ttl: 15m
  refresh_token_ttl: 720h
  jwt:
    issuer: example-api
    secret: "0123456789abcdef0123456789abcdef"

log:
  level: info
  console:
    enabled: true
    level: debug
    output: stderr
  jsonl:
    enabled: true
    level: info
    path: ./var/logs/app.jsonl
  redact:
    enabled: true
    fields: ["password", "token", "secret", "authorization", "dsn"]

observability:
  health:
    enabled: true

casbin:
  model_path: ./configs/rbac_model.conf

cache:
  local_ttl: 1m
  remote_ttl: 10m

storage:
  enabled: true
  provider: local
  presign_default_ttl: 15m
  local:
    root_dir: ./var/uploads
    temp_dir: .multipart
    generate_date_path: true
    allowed_extensions: ["txt", "md", "png", "jpg", "jpeg", "gif", "pdf"]
    max_size: 10485760

http_client:
  enabled: true
  timeout: 30s
  connect_timeout: 10s
  idle_conn_timeout: 90s
  max_idle_conns: 100
  max_idle_conns_per_host: 20
  max_response_body_size: 10485760
  proxy: http://127.0.0.1:7890
  services:
    httpbingo:
      base_url: https://httpbingo.org
      timeout: 10s
      proxy: http://127.0.0.1:7891
      headers:
        X-App-Id: example-api
      retry:
        enabled: true
        count: 2
        wait_time: 200ms
        max_wait_time: 2s

idgen:
  node: 1
`)
	override := []byte(`
app:
  instance_id: local-1

server:
  addr: ":18080"

auth:
  jwt:
    secret: "abcdef0123456789abcdef0123456789"
`)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), content, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.local.yaml"), override, 0o600))

	cfg, err := LoadConfig("local", configDir)

	require.NoError(t, err)
	require.Equal(t, "example-api", cfg.App.Name)
	require.Equal(t, "example-api", cfg.App.Slug)
	require.Equal(t, "local", cfg.App.Env)
	require.Equal(t, "0.1.0", cfg.App.Version)
	require.Equal(t, "local-1", cfg.App.InstanceID)
	require.Equal(t, ":18080", cfg.Server.Addr)
	require.Equal(t, 20*time.Second, cfg.Server.ShutdownTimeout)
	require.Equal(t, "example-api", cfg.Database.ApplicationName)
	require.Equal(t, 5*time.Second, cfg.Database.ConnectTimeout)
	require.Equal(t, 15*time.Minute, cfg.Database.ConnMaxIdleTime)
	require.Equal(t, time.Hour, cfg.Database.ConnMaxLifetime)
	require.Equal(t, 5*time.Second, cfg.Database.PingTimeout)
	require.Equal(t, "require", cfg.Database.SSLMode)
	require.False(t, cfg.Redis.Enabled)
	require.Equal(t, 10, cfg.Redis.Pool.Size)
	require.True(t, cfg.Auth.Enabled)
	require.True(t, cfg.Auth.AllowMemoryTokenStore)
	require.Equal(t, 720*time.Hour, cfg.Auth.RefreshTokenTTL)
	require.Equal(t, "example-api", cfg.Auth.JWT.Issuer)
	require.Equal(t, "abcdef0123456789abcdef0123456789", cfg.Auth.JWT.Secret)
	require.Equal(t, []string{"password", "token", "secret", "authorization", "dsn"}, cfg.Log.Redact.Fields)
	require.True(t, cfg.Storage.Enabled)
	require.Equal(t, "./var/uploads", cfg.Storage.Local.RootDir)
	require.Equal(t, int64(10*1024*1024), cfg.Storage.Local.MaxSize)
	require.True(t, cfg.HTTPClient.Enabled)
	require.Equal(t, 30*time.Second, cfg.HTTPClient.Timeout)
	require.Equal(t, "http://127.0.0.1:7890", cfg.HTTPClient.Proxy)
	require.Equal(t, "https://httpbingo.org", cfg.HTTPClient.Services["httpbingo"].BaseURL)
	require.Equal(t, "http://127.0.0.1:7891", cfg.HTTPClient.Services["httpbingo"].Proxy)
	require.Equal(t, "example-api", cfg.HTTPClient.Services["httpbingo"].Headers["x-app-id"])
	require.Equal(t, int64(1), cfg.IDGen.Node)

	cfg.HTTPClient.Proxy = "https://proxy-user:proxy-password@proxy.example.test"
	safe := cfg.SafeForLog()
	auth := safe["auth"].(map[string]any)
	jwt := auth["jwt"].(map[string]any)
	require.Equal(t, "***", jwt["secret"])
	require.NotContains(t, fmt.Sprint(safe["http_client"]), "proxy-password")
}

// TestConfigValidateRequiresExplicitIDNode 验证缺少实例唯一节点时启动校验失败。
func TestConfigValidateRequiresExplicitIDNode(t *testing.T) {
	cfg := validConfigForValidation()
	cfg.IDGen.Node = -1

	require.ErrorContains(t, cfg.Validate(), "必须显式配置")
}

func TestConfigValidateRejectsDevelopmentJWTSecretInProduction(t *testing.T) {
	cfg := validConfigForValidation()
	cfg.App.Env = "prod"
	cfg.Redis.Enabled = true
	cfg.Redis.Addr = "127.0.0.1:6379"
	cfg.Auth.AllowMemoryTokenStore = false
	cfg.Database.SSLMode = "verify-full"
	cfg.Auth.JWT.Secret = "local-only-change-me-0123456789abcdef"

	require.ErrorContains(t, cfg.Validate(), "示例 JWT secret")
}

func TestConfigValidateRejectsShortJWTSecret(t *testing.T) {
	cfg := validConfigForValidation()
	cfg.Auth.JWT.Secret = "too-short"

	require.ErrorContains(t, cfg.Validate(), "32 字节")
}

// TestConfigValidateRequiresVerifiedTLSInSharedEnvironments 验证共享环境必须同时校验证书与主机名。
func TestConfigValidateRequiresVerifiedTLSInSharedEnvironments(t *testing.T) {
	for _, env := range []string{"prod", "production", "prd", "staging", "uat", "preview"} {
		for _, mode := range []string{"disable", "allow", "prefer", "require", "verify-ca"} {
			t.Run(env+"/"+mode, func(t *testing.T) {
				cfg := validConfigForValidation()
				cfg.App.Env = env
				cfg.Database.SSLMode = mode

				require.ErrorContains(t, cfg.Validate(), "必须是 verify-full")
			})
		}
	}
}

// TestConfigValidateRequiresSharedTokenStoreInSharedEnvironments 验证除本地开发和测试外的环境不能退化为进程内状态。
func TestConfigValidateRequiresSharedTokenStoreInSharedEnvironments(t *testing.T) {
	for _, env := range []string{"prod", "production", "prd", "staging", "uat", "preview"} {
		t.Run(env, func(t *testing.T) {
			cfg := validConfigForValidation()
			cfg.App.Env = env
			cfg.Database.SSLMode = "verify-full"

			require.ErrorContains(t, cfg.Validate(), "Redis 共享 token store")
		})
	}
}

// TestConfigValidateAllowsLocalSecurityExceptions 验证只有明确的本地开发和测试环境可使用弱 TLS 与内存 token store。
func TestConfigValidateAllowsLocalSecurityExceptions(t *testing.T) {
	for _, env := range []string{"dev", "local", "test"} {
		t.Run(env, func(t *testing.T) {
			cfg := validConfigForValidation()
			cfg.App.Env = env

			require.NoError(t, cfg.Validate())
		})
	}
}

// TestConfigValidateRequiresWorkerShutdownBudget 验证应用关闭预算足以覆盖 Worker 的优雅关闭时间。
func TestConfigValidateRequiresWorkerShutdownBudget(t *testing.T) {
	cfg := validConfigForValidation()
	cfg.Task.Enabled = true
	cfg.Task.Worker.Enabled = true
	cfg.Task.Worker.ShutdownTimeout = 2 * time.Second
	cfg.Server.ShutdownTimeout = time.Second

	require.ErrorContains(t, cfg.Validate(), "server.shutdown_timeout 不能小于 task.worker.shutdown_timeout")

	cfg.Server.ShutdownTimeout = 2 * time.Second
	require.NoError(t, cfg.Validate())
}

func validConfigForValidation() *Config {
	return &Config{
		App: AppConfig{Name: "example-api", Slug: "example-api", Env: "test"},
		Server: ServerConfig{
			Addr:            ":8080",
			ReadTimeout:     time.Second,
			WriteTimeout:    time.Second,
			IdleTimeout:     time.Second,
			ShutdownTimeout: time.Second,
		},
		Database: data.DatabaseConfig{
			Host:            "127.0.0.1",
			Port:            5432,
			User:            "postgres",
			DBName:          "example",
			SSLMode:         "disable",
			ConnectTimeout:  5 * time.Second,
			MaxOpenConns:    20,
			MaxIdleConns:    10,
			ConnMaxIdleTime: 15 * time.Minute,
			ConnMaxLifetime: time.Hour,
			PingTimeout:     5 * time.Second,
		},
		Auth: AuthConfig{
			Enabled:               true,
			AllowMemoryTokenStore: true,
			AccessTokenTTL:        time.Minute,
			RefreshTokenTTL:       time.Hour,
			JWT: JWTConfig{
				Issuer: "example-api",
				Secret: "0123456789abcdef0123456789abcdef",
			},
			LoginProtection: platformauth.LoginProtectionConfig{
				Enabled:          true,
				AccountRateLimit: platformauth.LoginRateConfig{MaxAttempts: 10, Window: time.Minute},
				IPRateLimit:      platformauth.LoginRateConfig{MaxAttempts: 60, Window: time.Minute},
				Lockout:          platformauth.LoginLockConfig{MaxFailures: 5, FailureWindow: 15 * time.Minute, LockDuration: 15 * time.Minute},
			},
		},
		Casbin: CasbinConfig{ModelPath: "model.conf"},
		IDGen:  IDGenConfig{Node: 1},
	}
}
