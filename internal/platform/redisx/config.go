package redisx

import (
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const maskedValue = "***"

// Mode 表示 Redis 连接模式；当前仅支持单机和 Sentinel。
type Mode string

const (
	// ModeStandalone 表示直连单 Redis 实例。
	ModeStandalone Mode = "standalone"
	// ModeSentinel 表示通过 Redis Sentinel 发现主节点。
	ModeSentinel Mode = "sentinel"
)

// Config 描述 Redis 客户端连接、连接池、安全和观测配置。
type Config struct {
	Enabled       bool                `mapstructure:"enabled"`
	Mode          Mode                `mapstructure:"mode"`
	Addr          string              `mapstructure:"addr"`
	Username      string              `mapstructure:"username"`
	Password      string              `mapstructure:"password"`
	DB            int                 `mapstructure:"db"`
	ClientName    string              `mapstructure:"client_name"`
	Pool          PoolConfig          `mapstructure:"pool"`
	Timeout       TimeoutConfig       `mapstructure:"timeout"`
	Retry         RetryConfig         `mapstructure:"retry"`
	TLS           TLSConfig           `mapstructure:"tls"`
	Sentinel      SentinelConfig      `mapstructure:"sentinel"`
	Observability ObservabilityConfig `mapstructure:"observability"`
	Log           LogConfig           `mapstructure:"log"`
}

// PoolConfig 描述 Redis 连接池参数。
type PoolConfig struct {
	Size            int           `mapstructure:"size"`
	MinIdleConns    int           `mapstructure:"min_idle_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	MaxActiveConns  int           `mapstructure:"max_active_conns"`
	Timeout         time.Duration `mapstructure:"timeout"`
	ConnMaxIdleTime time.Duration `mapstructure:"conn_max_idle_time"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
	FIFO            bool          `mapstructure:"fifo"`
}

// TimeoutConfig 描述 Redis 建连与读写超时。
type TimeoutConfig struct {
	Dial  time.Duration `mapstructure:"dial"`
	Read  time.Duration `mapstructure:"read"`
	Write time.Duration `mapstructure:"write"`
}

// RetryConfig 描述 Redis 命令失败重试参数。
type RetryConfig struct {
	MaxRetries      int           `mapstructure:"max_retries"`
	MinRetryBackoff time.Duration `mapstructure:"min_retry_backoff"`
	MaxRetryBackoff time.Duration `mapstructure:"max_retry_backoff"`
}

// TLSConfig 描述 Redis TLS 连接参数。
type TLSConfig struct {
	Enabled            bool   `mapstructure:"enabled"`
	ServerName         string `mapstructure:"server_name"`
	InsecureSkipVerify bool   `mapstructure:"insecure_skip_verify"`
}

// SentinelConfig 描述 Redis Sentinel 连接参数。
type SentinelConfig struct {
	MasterName       string   `mapstructure:"master_name"`
	Addrs            []string `mapstructure:"addrs"`
	Username         string   `mapstructure:"username"`
	Password         string   `mapstructure:"password"`
	RouteByLatency   bool     `mapstructure:"route_by_latency"`
	RouteRandomly    bool     `mapstructure:"route_randomly"`
	ReplicaOnly      bool     `mapstructure:"replica_only"`
	UseDisconnected  bool     `mapstructure:"use_disconnected_replicas"`
	DisableDiscovery bool     `mapstructure:"disable_discovery"`
}

// ObservabilityConfig 描述 Redis OpenTelemetry 开关。
type ObservabilityConfig struct {
	TracingEnabled bool `mapstructure:"tracing_enabled"`
	MetricsEnabled bool `mapstructure:"metrics_enabled"`
}

// LogConfig 描述 Redis 命令日志 hook 参数。
type LogConfig struct {
	Enabled       bool          `mapstructure:"enabled"`
	SlowEnabled   bool          `mapstructure:"slow_enabled"`
	ErrorEnabled  bool          `mapstructure:"error_enabled"`
	SlowThreshold time.Duration `mapstructure:"slow_threshold"`
}

// Validate 校验 Redis 配置，显式拒绝 cluster 模式。
func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	cfg := c.withDefaults()
	switch cfg.Mode {
	case ModeStandalone:
		if strings.TrimSpace(cfg.Addr) == "" {
			return fmt.Errorf("redis.addr 不能为空")
		}
	case ModeSentinel:
		if strings.TrimSpace(cfg.Sentinel.MasterName) == "" {
			return fmt.Errorf("redis.sentinel.master_name 不能为空")
		}
		if len(cfg.Sentinel.Addrs) == 0 {
			return fmt.Errorf("redis.sentinel.addrs 不能为空")
		}
	default:
		return fmt.Errorf("redis mode %q 不受支持，cluster 模式未启用", cfg.Mode)
	}
	if cfg.DB < 0 {
		return fmt.Errorf("redis.db 不能小于 0")
	}
	if cfg.Pool.Size <= 0 {
		return fmt.Errorf("redis.pool.size 必须大于 0")
	}
	if cfg.Timeout.Dial < 0 || cfg.Timeout.Read < 0 || cfg.Timeout.Write < 0 {
		return fmt.Errorf("redis timeout 不能为负数")
	}
	if cfg.Pool.Timeout < 0 || cfg.Pool.ConnMaxIdleTime < 0 || cfg.Pool.ConnMaxLifetime < 0 {
		return fmt.Errorf("redis pool timeout 不能为负数")
	}
	return nil
}

// SafeForLog 返回脱敏后的 Redis 配置副本，可安全写入结构化日志。
func (c Config) SafeForLog() map[string]any {
	cfg := c.withDefaults()
	password := ""
	if cfg.Password != "" {
		password = maskedValue
	}
	sentinelPassword := ""
	if cfg.Sentinel.Password != "" {
		sentinelPassword = maskedValue
	}
	return map[string]any{
		"enabled":     cfg.Enabled,
		"mode":        cfg.Mode,
		"addr":        cfg.Addr,
		"username":    cfg.Username,
		"password":    password,
		"db":          cfg.DB,
		"client_name": cfg.ClientName,
		"pool": map[string]any{
			"size":               cfg.Pool.Size,
			"min_idle_conns":     cfg.Pool.MinIdleConns,
			"max_idle_conns":     cfg.Pool.MaxIdleConns,
			"max_active_conns":   cfg.Pool.MaxActiveConns,
			"timeout":            cfg.Pool.Timeout,
			"conn_max_idle_time": cfg.Pool.ConnMaxIdleTime,
			"conn_max_lifetime":  cfg.Pool.ConnMaxLifetime,
			"fifo":               cfg.Pool.FIFO,
		},
		"timeout": map[string]any{
			"dial":  cfg.Timeout.Dial,
			"read":  cfg.Timeout.Read,
			"write": cfg.Timeout.Write,
		},
		"retry": map[string]any{
			"max_retries":       cfg.Retry.MaxRetries,
			"min_retry_backoff": cfg.Retry.MinRetryBackoff,
			"max_retry_backoff": cfg.Retry.MaxRetryBackoff,
		},
		"tls": map[string]any{
			"enabled":              cfg.TLS.Enabled,
			"server_name":          cfg.TLS.ServerName,
			"insecure_skip_verify": cfg.TLS.InsecureSkipVerify,
		},
		"sentinel": map[string]any{
			"master_name":               cfg.Sentinel.MasterName,
			"addrs":                     cfg.Sentinel.Addrs,
			"username":                  cfg.Sentinel.Username,
			"password":                  sentinelPassword,
			"route_by_latency":          cfg.Sentinel.RouteByLatency,
			"route_randomly":            cfg.Sentinel.RouteRandomly,
			"replica_only":              cfg.Sentinel.ReplicaOnly,
			"use_disconnected_replicas": cfg.Sentinel.UseDisconnected,
			"disable_discovery":         cfg.Sentinel.DisableDiscovery,
		},
		"observability": map[string]any{
			"tracing_enabled": cfg.Observability.TracingEnabled,
			"metrics_enabled": cfg.Observability.MetricsEnabled,
		},
		"log": map[string]any{
			"enabled":        cfg.Log.Enabled,
			"slow_enabled":   cfg.Log.SlowEnabled,
			"error_enabled":  cfg.Log.ErrorEnabled,
			"slow_threshold": cfg.Log.SlowThreshold,
		},
	}
}

func (c Config) withDefaults() Config {
	if c.Mode == "" {
		c.Mode = ModeStandalone
	}
	if c.Pool.Size == 0 {
		c.Pool.Size = 10
	}
	if c.Timeout.Dial == 0 {
		c.Timeout.Dial = 5 * time.Second
	}
	if c.Timeout.Read == 0 {
		c.Timeout.Read = 3 * time.Second
	}
	if c.Timeout.Write == 0 {
		c.Timeout.Write = 3 * time.Second
	}
	if c.Log.SlowThreshold == 0 {
		c.Log.SlowThreshold = 200 * time.Millisecond
	}
	return c
}

func (c Config) redisOptions() *redis.Options {
	cfg := c.withDefaults()
	return &redis.Options{
		Addr:            cfg.Addr,
		ClientName:      cfg.ClientName,
		Username:        cfg.Username,
		Password:        cfg.Password,
		DB:              cfg.DB,
		PoolFIFO:        cfg.Pool.FIFO,
		PoolSize:        cfg.Pool.Size,
		PoolTimeout:     cfg.Pool.Timeout,
		MinIdleConns:    cfg.Pool.MinIdleConns,
		MaxIdleConns:    cfg.Pool.MaxIdleConns,
		MaxActiveConns:  cfg.Pool.MaxActiveConns,
		ConnMaxIdleTime: cfg.Pool.ConnMaxIdleTime,
		ConnMaxLifetime: cfg.Pool.ConnMaxLifetime,
		DialTimeout:     cfg.Timeout.Dial,
		ReadTimeout:     cfg.Timeout.Read,
		WriteTimeout:    cfg.Timeout.Write,
		MaxRetries:      cfg.Retry.MaxRetries,
		MinRetryBackoff: cfg.Retry.MinRetryBackoff,
		MaxRetryBackoff: cfg.Retry.MaxRetryBackoff,
		TLSConfig:       cfg.tlsConfig(),
	}
}

func (c Config) failoverOptions() *redis.FailoverOptions {
	cfg := c.withDefaults()
	return &redis.FailoverOptions{
		MasterName:              cfg.Sentinel.MasterName,
		SentinelAddrs:           cfg.Sentinel.Addrs,
		ClientName:              cfg.ClientName,
		Username:                cfg.Username,
		Password:                cfg.Password,
		SentinelUsername:        cfg.Sentinel.Username,
		SentinelPassword:        cfg.Sentinel.Password,
		DB:                      cfg.DB,
		RouteByLatency:          cfg.Sentinel.RouteByLatency,
		RouteRandomly:           cfg.Sentinel.RouteRandomly,
		ReplicaOnly:             cfg.Sentinel.ReplicaOnly,
		UseDisconnectedReplicas: cfg.Sentinel.UseDisconnected,
		PoolFIFO:                cfg.Pool.FIFO,
		PoolSize:                cfg.Pool.Size,
		PoolTimeout:             cfg.Pool.Timeout,
		MinIdleConns:            cfg.Pool.MinIdleConns,
		MaxIdleConns:            cfg.Pool.MaxIdleConns,
		MaxActiveConns:          cfg.Pool.MaxActiveConns,
		ConnMaxIdleTime:         cfg.Pool.ConnMaxIdleTime,
		ConnMaxLifetime:         cfg.Pool.ConnMaxLifetime,
		DialTimeout:             cfg.Timeout.Dial,
		ReadTimeout:             cfg.Timeout.Read,
		WriteTimeout:            cfg.Timeout.Write,
		MaxRetries:              cfg.Retry.MaxRetries,
		MinRetryBackoff:         cfg.Retry.MinRetryBackoff,
		MaxRetryBackoff:         cfg.Retry.MaxRetryBackoff,
		TLSConfig:               cfg.tlsConfig(),
	}
}

func (c Config) tlsConfig() *tls.Config {
	cfg := c.withDefaults()
	if !cfg.TLS.Enabled {
		return nil
	}
	return &tls.Config{
		ServerName:         cfg.TLS.ServerName,
		InsecureSkipVerify: cfg.TLS.InsecureSkipVerify, //nolint:gosec // 由调用方配置，常用于内网自签证书。
		MinVersion:         tls.VersionTLS12,
	}
}
