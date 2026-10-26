package httpclient

import (
	"github.com/go-resty/resty/v2"
)

// applyStaticAuth 根据认证配置把静态凭证写入当前请求。动态凭证和签名由 RequestHook 处理。
func applyStaticAuth(r *resty.Request, cfg AuthConfig) error {
	switch cfg.Type {
	case "", AuthTypeNone:
		return nil
	case AuthTypeBearer:
		r.SetAuthScheme("Bearer").SetAuthToken(cfg.Token)
	case AuthTypeBasic:
		r.SetBasicAuth(cfg.Username, cfg.Password)
	case AuthTypeAPIKey:
		r.SetHeader(cfg.Header, cfg.Value)
	case AuthTypeCustomHeader:
		if cfg.Header != "" {
			r.SetHeader(cfg.Header, cfg.Value)
		}
		for key, value := range cfg.Headers {
			r.SetHeader(key, value)
		}
	default:
		return &Error{
			Kind:    ErrorKindInternal,
			Code:    "unsupported_auth_type",
			Message: "不支持的 HTTP Client 认证类型",
		}
	}
	return nil
}
