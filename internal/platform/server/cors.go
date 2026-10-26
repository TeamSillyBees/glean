package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// CORSConfig 描述浏览器跨域访问白名单。
type CORSConfig struct {
	Enabled          bool          `mapstructure:"enabled"`
	AllowedOrigins   []string      `mapstructure:"allowed_origins"`
	AllowedMethods   []string      `mapstructure:"allowed_methods"`
	AllowedHeaders   []string      `mapstructure:"allowed_headers"`
	ExposedHeaders   []string      `mapstructure:"exposed_headers"`
	AllowCredentials bool          `mapstructure:"allow_credentials"`
	MaxAge           time.Duration `mapstructure:"max_age"`
}

// Validate 校验跨域白名单；共享环境禁止任意来源、方法或请求头。
func (c CORSConfig) Validate(env string) error {
	if !c.Enabled {
		return nil
	}
	if err := validateCORSOrigins(c.AllowedOrigins); err != nil {
		return err
	}
	switch {
	case len(nonEmptyStrings(c.AllowedOrigins)) == 0:
		return fmt.Errorf("server.cors.allowed_origins 不能为空")
	case len(nonEmptyStrings(c.AllowedMethods)) == 0:
		return fmt.Errorf("server.cors.allowed_methods 不能为空")
	case len(nonEmptyStrings(c.AllowedHeaders)) == 0:
		return fmt.Errorf("server.cors.allowed_headers 不能为空")
	case c.MaxAge < 0:
		return fmt.Errorf("server.cors.max_age 不能小于 0")
	case c.AllowCredentials && containsFold(c.AllowedOrigins, "*"):
		return fmt.Errorf("server.cors.allow_credentials 启用时不能允许任意来源")
	case isSharedEnvironment(env) && containsFold(c.AllowedOrigins, "*"):
		return fmt.Errorf("非 dev/local/test 环境禁止 server.cors.allowed_origins 使用通配符")
	case isSharedEnvironment(env) && containsFold(c.AllowedMethods, "*"):
		return fmt.Errorf("非 dev/local/test 环境禁止 server.cors.allowed_methods 使用通配符")
	case isSharedEnvironment(env) && containsFold(c.AllowedHeaders, "*"):
		return fmt.Errorf("非 dev/local/test 环境禁止 server.cors.allowed_headers 使用通配符")
	default:
		return nil
	}
}

func validateCORSOrigins(origins []string) error {
	for _, origin := range nonEmptyStrings(origins) {
		if origin == "*" {
			continue
		}
		parsed, err := url.Parse(origin)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
			parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("server.cors.allowed_origins %q 必须是 http(s) origin，且不能包含凭据、路径、查询或片段", origin)
		}
	}
	return nil
}

// CORSMiddleware 根据配置响应浏览器跨域请求和预检请求。
func CORSMiddleware(config CORSConfig) gin.HandlerFunc {
	allowedOrigins := nonEmptyStrings(config.AllowedOrigins)
	allowedMethods := upperStrings(config.AllowedMethods)
	allowedHeaders := nonEmptyStrings(config.AllowedHeaders)
	exposedHeaders := nonEmptyStrings(config.ExposedHeaders)

	return func(c *gin.Context) {
		if !config.Enabled {
			c.Next()
			return
		}

		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if origin == "" {
			c.Next()
			return
		}
		allowOrigin, allowed := matchedOrigin(origin, allowedOrigins)
		if !allowed {
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.Next()
			return
		}

		c.Writer.Header().Add("Vary", "Origin")
		c.Header("Access-Control-Allow-Origin", allowOrigin)
		if config.AllowCredentials {
			c.Header("Access-Control-Allow-Credentials", "true")
		}
		if len(exposedHeaders) > 0 {
			c.Header("Access-Control-Expose-Headers", strings.Join(exposedHeaders, ", "))
		}
		if c.Request.Method != http.MethodOptions {
			c.Next()
			return
		}

		requestedMethod := strings.ToUpper(strings.TrimSpace(c.GetHeader("Access-Control-Request-Method")))
		if requestedMethod == "" || !containsFold(allowedMethods, requestedMethod) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		if !requestedHeadersAllowed(c.GetHeader("Access-Control-Request-Headers"), allowedHeaders) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Writer.Header().Add("Vary", "Access-Control-Request-Method")
		c.Writer.Header().Add("Vary", "Access-Control-Request-Headers")
		c.Header("Access-Control-Allow-Methods", strings.Join(allowedMethods, ", "))
		c.Header("Access-Control-Allow-Headers", strings.Join(allowedHeaders, ", "))
		if config.MaxAge > 0 {
			c.Header("Access-Control-Max-Age", strconv.FormatInt(int64(config.MaxAge/time.Second), 10))
		}
		c.AbortWithStatus(http.StatusNoContent)
	}
}

func matchedOrigin(origin string, allowed []string) (string, bool) {
	for _, candidate := range allowed {
		if candidate == "*" {
			return "*", true
		}
		if candidate == origin {
			return origin, true
		}
	}
	return "", false
}

func requestedHeadersAllowed(header string, allowed []string) bool {
	if strings.TrimSpace(header) == "" || containsFold(allowed, "*") {
		return true
	}
	for _, requested := range strings.Split(header, ",") {
		if !containsFold(allowed, strings.TrimSpace(requested)) {
			return false
		}
	}
	return true
}

func nonEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func upperStrings(values []string) []string {
	result := nonEmptyStrings(values)
	for index := range result {
		result[index] = strings.ToUpper(result[index])
	}
	return result
}

func containsFold(values []string, expected string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(expected)) {
			return true
		}
	}
	return false
}

func isSharedEnvironment(env string) bool {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "dev", "local", "test":
		return false
	default:
		return true
	}
}
