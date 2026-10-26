package httpclient

import (
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultTimeout             = 30 * time.Second
	defaultConnectTimeout      = 10 * time.Second
	defaultIdleConnTimeout     = 90 * time.Second
	defaultMaxIdleConns        = 100
	defaultMaxIdleConnsPerHost = 20
	defaultRetryWaitTime       = 500 * time.Millisecond
	defaultRetryMaxWaitTime    = 5 * time.Second
	defaultMaxResponseBodySize = int64(10 * 1024 * 1024)
	maskedValue                = "***"
)

// AuthType 表示远程服务的静态认证方式。动态凭证或请求签名应使用 RequestHook。
type AuthType string

const (
	// AuthTypeNone 表示不添加认证信息。
	AuthTypeNone AuthType = "none"
	// AuthTypeBearer 表示使用 Authorization: Bearer <token> 认证。
	AuthTypeBearer AuthType = "bearer"
	// AuthTypeBasic 表示使用 HTTP Basic Auth 认证。
	AuthTypeBasic AuthType = "basic"
	// AuthTypeAPIKey 表示在指定 Header 中放置 API Key。
	AuthTypeAPIKey AuthType = "api_key"
	// AuthTypeCustomHeader 表示添加一组静态认证 Header。
	AuthTypeCustomHeader AuthType = "custom_header"
)

// Config 描述 HTTP Client 全局配置。除 Enabled 和 Services 外的字段都有安全默认值，
// 普通业务配置通常只需要声明 services.<name>.base_url。
type Config struct {
	Enabled             bool                     `mapstructure:"enabled"`
	Timeout             time.Duration            `mapstructure:"timeout"`
	ConnectTimeout      time.Duration            `mapstructure:"connect_timeout"`
	IdleConnTimeout     time.Duration            `mapstructure:"idle_conn_timeout"`
	MaxIdleConns        int                      `mapstructure:"max_idle_conns"`
	MaxIdleConnsPerHost int                      `mapstructure:"max_idle_conns_per_host"`
	MaxResponseBodySize int64                    `mapstructure:"max_response_body_size"`
	Proxy               string                   `mapstructure:"proxy"`
	Services            map[string]ServiceConfig `mapstructure:"services"`
	RetryStatusCodes    []int                    `mapstructure:"retry_status_codes"`
	RetryMethods        []string                 `mapstructure:"retry_methods"`
}

// ServiceConfig 描述单个远程 HTTP 服务配置。
type ServiceConfig struct {
	BaseURL             string            `mapstructure:"base_url"`
	Timeout             time.Duration     `mapstructure:"timeout"`
	Headers             map[string]string `mapstructure:"headers"`
	Auth                AuthConfig        `mapstructure:"auth"`
	Retry               RetryConfig       `mapstructure:"retry"`
	MaxResponseBodySize int64             `mapstructure:"max_response_body_size"`
	Proxy               string            `mapstructure:"proxy"`
}

// AuthConfig 描述远程服务的静态认证配置。
type AuthConfig struct {
	Type     AuthType          `mapstructure:"type"`
	Token    string            `mapstructure:"token"`
	Username string            `mapstructure:"username"`
	Password string            `mapstructure:"password"`
	Header   string            `mapstructure:"header"`
	Value    string            `mapstructure:"value"`
	Headers  map[string]string `mapstructure:"headers"`
}

// RetryConfig 描述远程服务重试配置。POST 和 PATCH 仅在单次请求显式标记安全时重试。
type RetryConfig struct {
	Enabled          bool          `mapstructure:"enabled"`
	Count            int           `mapstructure:"count"`
	WaitTime         time.Duration `mapstructure:"wait_time"`
	MaxWaitTime      time.Duration `mapstructure:"max_wait_time"`
	RetryStatusCodes []int         `mapstructure:"retry_status_codes"`
	RetryMethods     []string      `mapstructure:"retry_methods"`
	RetryAll5xx      bool          `mapstructure:"retry_all_5xx"`
}

// Validate 校验 HTTP Client 配置。未启用或没有远程服务时配置有效，便于项目按需接入。
func (c Config) Validate() error {
	if !c.Enabled || len(c.Services) == 0 {
		return nil
	}
	cfg := c.withDefaults()
	if cfg.Timeout <= 0 {
		return fmt.Errorf("%w: http_client.timeout 必须大于 0", ErrInvalidConfig)
	}
	if cfg.ConnectTimeout <= 0 {
		return fmt.Errorf("%w: http_client.connect_timeout 必须大于 0", ErrInvalidConfig)
	}
	if cfg.IdleConnTimeout < 0 {
		return fmt.Errorf("%w: http_client.idle_conn_timeout 不能为负数", ErrInvalidConfig)
	}
	if cfg.MaxIdleConns <= 0 {
		return fmt.Errorf("%w: http_client.max_idle_conns 必须大于 0", ErrInvalidConfig)
	}
	if cfg.MaxIdleConnsPerHost <= 0 {
		return fmt.Errorf("%w: http_client.max_idle_conns_per_host 必须大于 0", ErrInvalidConfig)
	}
	if err := validateBodyLimit("http_client.max_response_body_size", cfg.MaxResponseBodySize); err != nil {
		return err
	}
	if err := validateProxyURL("http_client.proxy", cfg.Proxy); err != nil {
		return err
	}
	for name, service := range cfg.Services {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("%w: http_client.services 服务名不能为空", ErrInvalidConfig)
		}
		if err := validateServiceConfig(name, service.withDefaults(cfg)); err != nil {
			return err
		}
	}
	return nil
}

// SafeForLog 返回可安全写入日志的脱敏配置。
func (c Config) SafeForLog() map[string]any {
	cfg := c.withDefaults()
	services := make(map[string]any, len(cfg.Services))
	for name, service := range cfg.Services {
		services[name] = sanitizeServiceConfig(service.withDefaults(cfg))
	}
	return map[string]any{
		"enabled":                 cfg.Enabled,
		"timeout":                 cfg.Timeout,
		"connect_timeout":         cfg.ConnectTimeout,
		"idle_conn_timeout":       cfg.IdleConnTimeout,
		"max_idle_conns":          cfg.MaxIdleConns,
		"max_idle_conns_per_host": cfg.MaxIdleConnsPerHost,
		"max_response_body_size":  cfg.MaxResponseBodySize,
		"proxy":                   sanitizeProxyURL(cfg.Proxy),
		"retry_status_codes":      cfg.RetryStatusCodes,
		"retry_methods":           cfg.RetryMethods,
		"services":                services,
	}
}

func (c Config) withDefaults() Config {
	if c.Timeout == 0 {
		c.Timeout = defaultTimeout
	}
	if c.ConnectTimeout == 0 {
		c.ConnectTimeout = defaultConnectTimeout
	}
	if c.IdleConnTimeout == 0 {
		c.IdleConnTimeout = defaultIdleConnTimeout
	}
	if c.MaxIdleConns == 0 {
		c.MaxIdleConns = defaultMaxIdleConns
	}
	if c.MaxIdleConnsPerHost == 0 {
		c.MaxIdleConnsPerHost = defaultMaxIdleConnsPerHost
	}
	if c.MaxResponseBodySize == 0 {
		c.MaxResponseBodySize = defaultMaxResponseBodySize
	}
	if len(c.RetryStatusCodes) == 0 {
		c.RetryStatusCodes = defaultRetryStatusCodes()
	}
	if len(c.RetryMethods) == 0 {
		c.RetryMethods = defaultRetryMethods()
	}
	return c
}

func (c ServiceConfig) withDefaults(global Config) ServiceConfig {
	if c.Timeout == 0 {
		c.Timeout = global.Timeout
	}
	if c.Auth.Type == "" {
		c.Auth.Type = AuthTypeNone
	}
	if c.MaxResponseBodySize == 0 {
		c.MaxResponseBodySize = global.MaxResponseBodySize
	}
	if c.Proxy == "" {
		c.Proxy = global.Proxy
	}
	if c.Retry.Enabled {
		if c.Retry.Count == 0 {
			c.Retry.Count = 3
		}
		if c.Retry.WaitTime == 0 {
			c.Retry.WaitTime = defaultRetryWaitTime
		}
		if c.Retry.MaxWaitTime == 0 {
			c.Retry.MaxWaitTime = defaultRetryMaxWaitTime
		}
		if len(c.Retry.RetryStatusCodes) == 0 {
			c.Retry.RetryStatusCodes = global.RetryStatusCodes
		}
		if len(c.Retry.RetryMethods) == 0 {
			c.Retry.RetryMethods = global.RetryMethods
		}
	}
	return c
}

func validateServiceConfig(name string, cfg ServiceConfig) error {
	prefix := "http_client.services." + name
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		return fmt.Errorf("%w: %s.base_url 不能为空", ErrInvalidConfig, prefix)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%w: %s.base_url 必须是合法的 http 或 https 地址", ErrInvalidConfig, prefix)
	}
	if cfg.Timeout <= 0 {
		return fmt.Errorf("%w: %s.timeout 必须大于 0", ErrInvalidConfig, prefix)
	}
	if err := validateBodyLimit(prefix+".max_response_body_size", cfg.MaxResponseBodySize); err != nil {
		return err
	}
	if err := validateProxyURL(prefix+".proxy", cfg.Proxy); err != nil {
		return err
	}
	if err := validateAuthConfig(prefix+".auth", cfg.Auth); err != nil {
		return err
	}
	return validateRetryConfig(prefix+".retry", cfg.Retry)
}

func validateAuthConfig(prefix string, cfg AuthConfig) error {
	switch cfg.Type {
	case AuthTypeNone:
		return nil
	case AuthTypeBearer:
		if strings.TrimSpace(cfg.Token) == "" {
			return fmt.Errorf("%w: %s.token 不能为空", ErrInvalidConfig, prefix)
		}
	case AuthTypeBasic:
		if strings.TrimSpace(cfg.Username) == "" {
			return fmt.Errorf("%w: %s.username 不能为空", ErrInvalidConfig, prefix)
		}
	case AuthTypeAPIKey:
		if strings.TrimSpace(cfg.Header) == "" {
			return fmt.Errorf("%w: %s.header 不能为空", ErrInvalidConfig, prefix)
		}
		if strings.TrimSpace(cfg.Value) == "" {
			return fmt.Errorf("%w: %s.value 不能为空", ErrInvalidConfig, prefix)
		}
	case AuthTypeCustomHeader:
		if strings.TrimSpace(cfg.Header) == "" && len(cfg.Headers) == 0 {
			return fmt.Errorf("%w: %s.header 或 headers 不能为空", ErrInvalidConfig, prefix)
		}
		if strings.TrimSpace(cfg.Header) != "" && strings.TrimSpace(cfg.Value) == "" {
			return fmt.Errorf("%w: %s.value 不能为空", ErrInvalidConfig, prefix)
		}
	default:
		return fmt.Errorf("%w: %s.type %q 不受支持", ErrInvalidConfig, prefix, cfg.Type)
	}
	return nil
}

func validateRetryConfig(prefix string, cfg RetryConfig) error {
	if !cfg.Enabled {
		return nil
	}
	if cfg.Count < 0 {
		return fmt.Errorf("%w: %s.count 不能为负数", ErrInvalidConfig, prefix)
	}
	if cfg.WaitTime < 0 {
		return fmt.Errorf("%w: %s.wait_time 不能为负数", ErrInvalidConfig, prefix)
	}
	if cfg.MaxWaitTime < 0 {
		return fmt.Errorf("%w: %s.max_wait_time 不能为负数", ErrInvalidConfig, prefix)
	}
	if cfg.MaxWaitTime > 0 && cfg.WaitTime > cfg.MaxWaitTime {
		return fmt.Errorf("%w: %s.wait_time 不能大于 max_wait_time", ErrInvalidConfig, prefix)
	}
	for _, code := range cfg.RetryStatusCodes {
		if code < http.StatusContinue || code > 999 {
			return fmt.Errorf("%w: %s.retry_status_codes 包含非法状态码 %d", ErrInvalidConfig, prefix, code)
		}
	}
	return nil
}

func validateProxyURL(name string, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("%w: %s 不是合法代理地址: %w", ErrInvalidConfig, name, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%w: %s 必须包含 scheme 和 host", ErrInvalidConfig, name)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "socks5":
		return nil
	default:
		return fmt.Errorf("%w: %s scheme %q 不受支持", ErrInvalidConfig, name, parsed.Scheme)
	}
}

func validateBodyLimit(name string, value int64) error {
	if value < 0 {
		return fmt.Errorf("%w: %s 不能为负数", ErrInvalidConfig, name)
	}
	if value > int64(math.MaxInt) {
		return fmt.Errorf("%w: %s 超出当前平台 int 上限", ErrInvalidConfig, name)
	}
	return nil
}

func sanitizeServiceConfig(cfg ServiceConfig) map[string]any {
	return map[string]any{
		"base_url":               cfg.BaseURL,
		"timeout":                cfg.Timeout,
		"headers":                sanitizeHeaderMap(cfg.Headers),
		"auth":                   sanitizeAuthConfig(cfg.Auth),
		"retry":                  cfg.Retry,
		"max_response_body_size": cfg.MaxResponseBodySize,
		"proxy":                  sanitizeProxyURL(cfg.Proxy),
	}
}

func sanitizeAuthConfig(cfg AuthConfig) map[string]any {
	return map[string]any{
		"type":     cfg.Type,
		"token":    maskIfSet(cfg.Token),
		"username": cfg.Username,
		"password": maskIfSet(cfg.Password),
		"header":   cfg.Header,
		"value":    maskIfSet(cfg.Value),
		"headers":  sanitizeHeaderMap(cfg.Headers),
	}
}

func sanitizeHeaderMap(headers map[string]string) map[string]string {
	sanitized := make(map[string]string, len(headers))
	for key, value := range headers {
		if isSensitiveHeader(key) {
			sanitized[key] = maskedValue
			continue
		}
		sanitized[key] = value
	}
	return sanitized
}

func sanitizeProxyURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return maskedValue
	}
	if parsed.User != nil {
		if _, ok := parsed.User.Password(); ok {
			prefix := parsed.Scheme + "://"
			username := parsed.User.Username()
			parsed.User = nil
			return prefix + username + ":" + maskedValue + "@" + strings.TrimPrefix(parsed.String(), prefix)
		}
	}
	return parsed.String()
}

func maskIfSet(value string) string {
	if value == "" {
		return ""
	}
	return maskedValue
}
