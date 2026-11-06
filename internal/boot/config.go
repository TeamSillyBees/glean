package boot

import (
	"fmt"
	"strings"
	"time"

	"github.com/teamsillybees/glean/internal/data"
	monitormodule "github.com/teamsillybees/glean/internal/modules/monitor"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	platformconfig "github.com/teamsillybees/glean/internal/platform/config"
	"github.com/teamsillybees/glean/internal/platform/httpclient"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/logx"
	"github.com/teamsillybees/glean/internal/platform/redisx"
	platformserver "github.com/teamsillybees/glean/internal/platform/server"
	platformstorage "github.com/teamsillybees/glean/internal/platform/storage"
	"github.com/teamsillybees/glean/internal/platform/task"
)

// Config 是应用配置聚合根。
type Config struct {
	App           AppConfig              `mapstructure:"app"`
	Server        ServerConfig           `mapstructure:"server"`
	Database      data.DatabaseConfig    `mapstructure:"database"`
	Redis         redisx.Config          `mapstructure:"redis"`
	Auth          AuthConfig             `mapstructure:"auth"`
	Log           logx.Config            `mapstructure:"log"`
	Observability ObservabilityConfig    `mapstructure:"observability"`
	Casbin        CasbinConfig           `mapstructure:"casbin"`
	Cache         CacheConfig            `mapstructure:"cache"`
	IDGen         IDGenConfig            `mapstructure:"idgen"`
	Storage       platformstorage.Config `mapstructure:"storage"`
	HTTPClient    httpclient.Config      `mapstructure:"http_client"`
	Task          task.Config            `mapstructure:"task"`
	Monitor       MonitorConfig          `mapstructure:"monitor"`
}

// MonitorConfig 描述显式启用的真实来源调度；默认不启动任何采集定时器。
type MonitorConfig struct {
	Models           monitormodule.ModelPipelineConfig `mapstructure:"models"`
	SchedulerEnabled bool                              `mapstructure:"scheduler_enabled"`
	Sources          []string                          `mapstructure:"sources"`
	MaxPagesPerCall  int                               `mapstructure:"max_pages_per_call"`
	Contact          string                            `mapstructure:"contact"`
	OperatorID       string                            `mapstructure:"operator_id"`
}

// AppConfig 描述应用基础元信息。
type AppConfig struct {
	Name       string `mapstructure:"name"`
	Slug       string `mapstructure:"slug"`
	Env        string `mapstructure:"env"`
	Version    string `mapstructure:"version"`
	InstanceID string `mapstructure:"instance_id"`
}

// ServerConfig 描述 HTTP Server 的监听地址、超时和关闭参数。
type ServerConfig struct {
	Addr            string                    `mapstructure:"addr"`
	ReadTimeout     time.Duration             `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration             `mapstructure:"write_timeout"`
	IdleTimeout     time.Duration             `mapstructure:"idle_timeout"`
	ShutdownTimeout time.Duration             `mapstructure:"shutdown_timeout"`
	TrustedProxies  []string                  `mapstructure:"trusted_proxies"`
	CORS            platformserver.CORSConfig `mapstructure:"cors"`
	Docs            platformserver.DocsConfig `mapstructure:"docs"`
}

// AuthConfig 描述认证与令牌配置。
type AuthConfig struct {
	Enabled               bool                               `mapstructure:"enabled"`
	AllowMemoryTokenStore bool                               `mapstructure:"allow_memory_token_store"`
	AccessTokenTTL        time.Duration                      `mapstructure:"access_token_ttl"`
	RefreshTokenTTL       time.Duration                      `mapstructure:"refresh_token_ttl"`
	JWT                   JWTConfig                          `mapstructure:"jwt"`
	LoginProtection       platformauth.LoginProtectionConfig `mapstructure:"login_protection"`
}

// JWTConfig 描述 JWT 签发所需配置。
type JWTConfig struct {
	Issuer string `mapstructure:"issuer"`
	Secret string `mapstructure:"secret"`
}

// ObservabilityConfig 描述已接入运行时的可观测性能力开关。
type ObservabilityConfig struct {
	Health FeatureConfig `mapstructure:"health"`
}

// FeatureConfig 描述单项能力是否启用。
type FeatureConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

// CasbinConfig 描述权限模型路径；策略唯一来源为业务数据库。
type CasbinConfig struct {
	ModelPath string `mapstructure:"model_path"`
}

// CacheConfig 描述多级缓存的默认 TTL。
type CacheConfig struct {
	LocalTTL  time.Duration `mapstructure:"local_ttl"`
	RemoteTTL time.Duration `mapstructure:"remote_ttl"`
}

// IDGenConfig 描述雪花算法节点配置。
type IDGenConfig struct {
	Node int64 `mapstructure:"node"`
}

// LoadConfig 加载并校验应用配置。
func LoadConfig(env string, configDir string) (*Config, error) {
	return platformconfig.LoadInto[Config](platformconfig.LoaderOptions{
		Env:       env,
		ConfigDir: configDir,
		Defaults:  configDefaults(),
	})
}

// SafeForLog 返回脱敏后的配置副本，可安全用于结构化日志打印。
func (c *Config) SafeForLog() map[string]any {
	return platformconfig.Sanitize(c, c.Log.Redact.Fields)
}

// Validate 对启动所需关键配置进行兜底校验。
func (c *Config) Validate() error {
	if err := c.Monitor.Models.Validate(); err != nil {
		return err
	}
	if err := c.Redis.Validate(); err != nil {
		return err
	}
	if err := c.Server.CORS.Validate(c.App.Env); err != nil {
		return err
	}
	if err := c.Server.Docs.Validate(c.App.Env); err != nil {
		return err
	}
	if err := c.Auth.LoginProtection.Validate(); err != nil {
		return err
	}
	// monitor 的单一主开关集中启用任务依赖；不要求操作员猜测三个底层开关。
	if c.Monitor.SchedulerEnabled {
		c.Task.Enabled = true
		c.Task.Worker.Enabled = true
		c.Task.Scheduler.Enabled = true
		c.Task.Scheduler.Timezone = "UTC"
	}
	taskConfig := c.Task.Normalize()
	if c.Monitor.OperatorID != "" {
		if _, err := idgen.Parse(c.Monitor.OperatorID); err != nil {
			return fmt.Errorf("monitor.operator_id must be a valid explicit user ID")
		}
	}
	if c.Monitor.SchedulerEnabled && c.Monitor.OperatorID == "" {
		return fmt.Errorf("monitor.operator_id is required when scheduler is enabled")
	}
	if c.Monitor.MaxPagesPerCall < 0 || c.Monitor.MaxPagesPerCall > 100 {
		return fmt.Errorf("monitor.max_pages_per_call must be 1..100 (zero uses default)")
	}
	if strings.ContainsAny(c.Monitor.Contact, "\r\n") {
		return fmt.Errorf("monitor.contact must not contain header control characters")
	}
	supported := map[string]bool{"crossref": true, "pubmed": true, "arxiv": true, "biorxiv": true, "medrxiv": true, "europepmc": true}
	seen := map[string]bool{}
	for _, source := range c.Monitor.Sources {
		if !supported[source] || seen[source] {
			return fmt.Errorf("monitor.sources contains unknown or duplicate source")
		}
		seen[source] = true
	}

	switch {
	case c.App.Name == "":
		return fmt.Errorf("app.name 不能为空")
	case !isSafeAppSlug(c.App.Slug):
		return fmt.Errorf("app.slug 必须是长度不超过 63 的小写字母、数字或中划线组合，且不能以中划线开头或结尾")
	case c.App.Env == "":
		return fmt.Errorf("app.env 不能为空")
	case c.Server.Addr == "":
		return fmt.Errorf("server.addr 不能为空")
	case c.Server.ReadTimeout <= 0:
		return fmt.Errorf("server.read_timeout 必须大于 0")
	case c.Server.WriteTimeout <= 0:
		return fmt.Errorf("server.write_timeout 必须大于 0")
	case c.Server.IdleTimeout <= 0:
		return fmt.Errorf("server.idle_timeout 必须大于 0")
	case c.Server.ShutdownTimeout <= 0:
		return fmt.Errorf("server.shutdown_timeout 必须大于 0")
	case taskConfig.Enabled && taskConfig.Worker.Enabled && c.Server.ShutdownTimeout < taskConfig.Worker.ShutdownTimeout:
		return fmt.Errorf("server.shutdown_timeout 不能小于 task.worker.shutdown_timeout")
	case c.Database.Host == "":
		return fmt.Errorf("database.host 不能为空")
	case c.Database.Port <= 0 || c.Database.Port > 65535:
		return fmt.Errorf("database.port 必须在 1 到 65535 之间")
	case c.Database.User == "":
		return fmt.Errorf("database.user 不能为空")
	case c.Database.DBName == "":
		return fmt.Errorf("database.dbname 不能为空")
	case c.Database.ConnectTimeout <= 0:
		return fmt.Errorf("database.connect_timeout 必须大于 0")
	case c.Database.MaxOpenConns <= 0:
		return fmt.Errorf("database.max_open_conns 必须大于 0")
	case c.Database.MaxIdleConns < 0:
		return fmt.Errorf("database.max_idle_conns 不能小于 0")
	case c.Database.MaxIdleConns > c.Database.MaxOpenConns:
		return fmt.Errorf("database.max_idle_conns 不能大于 database.max_open_conns")
	case c.Database.ConnMaxIdleTime <= 0:
		return fmt.Errorf("database.conn_max_idle_time 必须大于 0")
	case c.Database.ConnMaxLifetime <= 0:
		return fmt.Errorf("database.conn_max_lifetime 必须大于 0")
	case c.Database.PingTimeout <= 0:
		return fmt.Errorf("database.ping_timeout 必须大于 0")
	case !isValidDatabaseSSLMode(c.Database.SSLMode):
		return fmt.Errorf("database.ssl_mode 必须是 disable、allow、prefer、require、verify-ca 或 verify-full")
	case requiresSecureInfrastructure(c.App.Env) && !isSecureDatabaseSSLMode(c.Database.SSLMode):
		return fmt.Errorf("非 dev/local/test 环境的 database.ssl_mode 必须是 verify-full")
	case requiresSecureInfrastructure(c.App.Env) && !c.Redis.Enabled:
		return fmt.Errorf("非 dev/local/test 环境的认证必须启用 Redis 共享 token store")
	case requiresSecureInfrastructure(c.App.Env) && c.Auth.AllowMemoryTokenStore:
		return fmt.Errorf("非 dev/local/test 环境禁止启用 auth.allow_memory_token_store")
	case requiresSecureInfrastructure(c.App.Env) && !c.Auth.LoginProtection.Enabled:
		return fmt.Errorf("非 dev/local/test 环境必须启用 auth.login_protection")
	case !c.Redis.Enabled && !c.Auth.AllowMemoryTokenStore:
		return fmt.Errorf("Redis 关闭时必须显式启用 auth.allow_memory_token_store")
	case !c.Auth.Enabled:
		return fmt.Errorf("auth.enabled 当前必须为 true")
	case c.Auth.JWT.Issuer == "":
		return fmt.Errorf("auth.jwt.issuer 不能为空")
	case requiresSecureInfrastructure(c.App.Env) && isDevelopmentJWTSecret(c.Auth.JWT.Secret):
		return fmt.Errorf("非 dev/local/test 环境禁止使用示例 JWT secret")
	case len([]byte(strings.TrimSpace(c.Auth.JWT.Secret))) < 32:
		return fmt.Errorf("auth.jwt.secret 长度不能少于 32 字节")
	case c.Auth.AccessTokenTTL <= 0:
		return fmt.Errorf("auth.access_token_ttl 必须大于 0")
	case c.Auth.RefreshTokenTTL <= 0:
		return fmt.Errorf("auth.refresh_token_ttl 必须大于 0")
	case c.Auth.RefreshTokenTTL <= c.Auth.AccessTokenTTL:
		return fmt.Errorf("auth.refresh_token_ttl 必须大于 auth.access_token_ttl")
	case c.Casbin.ModelPath == "":
		return fmt.Errorf("casbin.model_path 不能为空")
	case c.IDGen.Node < 0 || c.IDGen.Node > 1023:
		return fmt.Errorf("idgen.node 必须显式配置为 0 到 1023 之间的实例唯一值")
	}
	if err := c.Storage.Validate(); err != nil {
		return err
	}
	if err := c.HTTPClient.Validate(); err != nil {
		return err
	}
	if err := taskConfig.Validate(); err != nil {
		return err
	}
	return nil
}

// configDefaults 统一声明应用配置默认值。
func configDefaults() map[string]any {
	return map[string]any{
		"app.version":                         "dev",
		"app.slug":                            "",
		"app.instance_id":                     "local-1",
		"server.addr":                         ":8080",
		"server.read_timeout":                 "10s",
		"server.write_timeout":                "30s",
		"server.idle_timeout":                 "60s",
		"server.shutdown_timeout":             "45s",
		"server.trusted_proxies":              []string{},
		"server.cors.enabled":                 true,
		"server.cors.allowed_origins":         []string{"http://localhost:3000", "http://127.0.0.1:3000"},
		"server.cors.allowed_methods":         []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		"server.cors.allowed_headers":         []string{"Authorization", "Content-Type", "X-Request-ID", "X-Trace-ID"},
		"server.cors.exposed_headers":         []string{"X-Request-ID", "X-Trace-ID"},
		"server.cors.allow_credentials":       false,
		"server.cors.max_age":                 "10m",
		"server.docs.enabled":                 true,
		"database.host":                       "",
		"database.port":                       5432,
		"database.user":                       "",
		"database.password":                   "",
		"database.dbname":                     "",
		"database.application_name":           "",
		"database.ssl_mode":                   "require",
		"database.ssl_root_cert":              "",
		"database.connect_timeout":            "5s",
		"database.max_open_conns":             20,
		"database.max_idle_conns":             10,
		"database.conn_max_idle_time":         "15m",
		"database.conn_max_lifetime":          "1h",
		"database.ping_timeout":               "5s",
		"redis.enabled":                       false,
		"redis.addr":                          "127.0.0.1:6379",
		"redis.password":                      "",
		"redis.db":                            0,
		"redis.pool.size":                     10,
		"redis.observability.metrics_enabled": true,
		"redis.observability.tracing_enabled": false,
		"auth.enabled":                        true,
		"auth.allow_memory_token_store":       false,
		"auth.access_token_ttl":               "15m",
		"auth.refresh_token_ttl":              "720h",
		"auth.jwt.issuer":                     "",
		"auth.jwt.secret":                     "",
		"auth.login_protection.enabled":       true,
		"auth.login_protection.account_rate_limit.max_attempts": 10,
		"auth.login_protection.account_rate_limit.window":       "1m",
		"auth.login_protection.ip_rate_limit.max_attempts":      60,
		"auth.login_protection.ip_rate_limit.window":            "1m",
		"auth.login_protection.lockout.max_failures":            5,
		"auth.login_protection.lockout.failure_window":          "15m",
		"auth.login_protection.lockout.lock_duration":           "15m",
		"log.level":                               "info",
		"log.console.enabled":                     true,
		"log.console.level":                       "debug",
		"log.console.stack":                       string(logx.StackShort),
		"log.console.color":                       true,
		"log.console.output":                      "stderr",
		"log.jsonl.enabled":                       true,
		"log.jsonl.level":                         "info",
		"log.jsonl.stack":                         string(logx.StackFull),
		"log.jsonl.path":                          logx.DefaultJSONLPath,
		"log.jsonl.rotation.enabled":              false,
		"log.jsonl.rotation.date_format":          logx.DefaultRotationDateFormat,
		"log.jsonl.rotation.max_size_mb":          0,
		"log.redact.enabled":                      true,
		"log.redact.fields":                       []string{"password", "token", "secret", "authorization", "dsn"},
		"observability.health.enabled":            true,
		"casbin.model_path":                       "",
		"cache.local_ttl":                         "1m",
		"cache.remote_ttl":                        "10m",
		"idgen.node":                              int64(-1),
		"storage.enabled":                         true,
		"storage.provider":                        string(platformstorage.ProviderLocal),
		"storage.presign_default_ttl":             "15m",
		"storage.local.root_dir":                  "./var/uploads",
		"storage.local.temp_dir":                  ".multipart",
		"storage.local.generate_date_path":        true,
		"storage.local.allowed_extensions":        []string{"txt", "md", "png", "jpg", "jpeg", "gif", "pdf"},
		"storage.local.max_size":                  int64(10 * 1024 * 1024),
		"http_client.enabled":                     true,
		"http_client.services.httpbingo.base_url": "https://httpbingo.org",
		"monitor.scheduler_enabled":               false,
		"monitor.sources":                         []string{"crossref", "pubmed", "arxiv", "biorxiv", "europepmc", "medrxiv"},
		"monitor.max_pages_per_call":              12,
		"monitor.contact":                         "",
		"monitor.operator_id":                     "",
		"monitor.models.enabled":                  false,
		"monitor.models.auto_display":             false,

		"monitor.models.budget.max_calls":           100,
		"monitor.models.budget.max_reserved_tokens": 1000000,
		"monitor.models.budget.max_input_bytes":     131072,
		"monitor.models.budget.max_output_tokens":   4096,

		"monitor.models.jev.endpoint":             "",
		"monitor.models.jev.model":                "",
		"monitor.models.jev.api_key_env":          "INITRA_JEV_API_KEY",
		"monitor.models.jev.timeout":              "60s",
		"monitor.models.generative.endpoint":      "",
		"monitor.models.generative.model":         "",
		"monitor.models.generative.api_key_env":   "INITRA_GENERATIVE_API_KEY",
		"monitor.models.generative.timeout":       "60s",
		"task.enabled":                            true,
		"task.backend":                            string(task.BackendAsynq),
		"task.redis.enabled":                      true,
		"task.redis.mode":                         string(redisx.ModeStandalone),
		"task.redis.addr":                         "127.0.0.1:6379",
		"task.redis.db":                           2,
		"task.redis.pool.size":                    10,
		"task.publisher.default_queue":            task.QueueDefault,
		"task.publisher.default_max_retry":        3,
		"task.publisher.default_timeout":          "5m",
		"task.publisher.default_retention":        "24h",
		"task.publisher.enforce_biz_key":          true,
		"task.worker.enabled":                     false,
		"task.worker.concurrency":                 10,
		"task.worker.shutdown_timeout":            "30s",
		"task.worker.health_check_interval":       "15s",
		"task.worker.delayed_task_check_interval": "5s",
		"task.worker.task_check_interval":         "1s",
		"task.worker.strict_priority":             false,
		"task.worker.queues.critical":             6,
		"task.worker.queues.default":              3,
		"task.worker.queues.low":                  1,
		"task.worker.retry.strategy":              string(task.RetryStrategyOfficial),
		"task.scheduler.enabled":                  false,
		"task.scheduler.sync_interval":            "3m",
		"task.scheduler.timezone":                 "Asia/Shanghai",
	}
}

func isSafeAppSlug(slug string) bool {
	slug = strings.TrimSpace(slug)
	if slug == "" || len(slug) > 63 || slug[0] == '-' || slug[len(slug)-1] == '-' {
		return false
	}
	for _, char := range slug {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func isValidDatabaseSSLMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
		return true
	default:
		return false
	}
}

func isSecureDatabaseSSLMode(mode string) bool {
	return strings.EqualFold(strings.TrimSpace(mode), "verify-full")
}

func requiresSecureInfrastructure(env string) bool {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "dev", "local", "test":
		return false
	default:
		return true
	}
}

func isDevelopmentJWTSecret(secret string) bool {
	secret = strings.ToLower(strings.TrimSpace(secret))
	return strings.HasPrefix(secret, "change-me") ||
		strings.HasPrefix(secret, "local-only-change-me-") ||
		strings.HasPrefix(secret, "dev-only-change-me-") ||
		strings.HasPrefix(secret, "test-only-change-me-")
}
