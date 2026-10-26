package requestctx

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	defaultFormMemory = 32 << 20

	headerAcceptLanguage  = "Accept-Language"
	headerForwarded       = "Forwarded"
	headerForwardedHost   = "X-Forwarded-Host"
	headerForwardedProto  = "X-Forwarded-Proto"
	headerOriginalForward = "X-Forwarded-Scheme"
	headerOrigin          = "Origin"
)

// QueryParam 从请求 URL 中提取 query 参数。
func QueryParam(r *http.Request, name string) string {
	if r == nil || r.URL == nil {
		return ""
	}
	return r.URL.Query().Get(name)
}

// PathParam 从请求路径参数中提取值。
func PathParam(r *http.Request, name string) string {
	if r == nil {
		return ""
	}
	return r.PathValue(name)
}

// HeaderValue 从请求头中提取值。
func HeaderValue(r *http.Request, name string) string {
	if r == nil {
		return ""
	}
	return r.Header.Get(name)
}

// CookieValue 从请求 Cookie 中提取值。
func CookieValue(r *http.Request, name string) string {
	if r == nil {
		return ""
	}
	cookie, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// FormParam 从请求表单中提取值。
func FormParam(r *http.Request, name string) string {
	if r == nil {
		return ""
	}
	if r.Form == nil && r.PostForm == nil && r.MultipartForm == nil {
		_ = r.ParseMultipartForm(defaultFormMemory)
	}
	if r.PostForm != nil {
		if value := r.PostForm.Get(name); value != "" {
			return value
		}
	}
	if r.MultipartForm != nil && r.MultipartForm.Value != nil {
		if values := r.MultipartForm.Value[name]; len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

// AcceptLanguage 从 Accept-Language 请求头中提取语言偏好。
func AcceptLanguage(r *http.Request) string {
	return HeaderValue(r, headerAcceptLanguage)
}

// Language 优先从自定义语言头提取语言，未命中时回退到 Accept-Language。
func Language(r *http.Request, headerNames ...string) string {
	for _, name := range headerNames {
		if value := strings.TrimSpace(HeaderValue(r, name)); value != "" {
			return value
		}
	}
	return AcceptLanguage(r)
}

// Origin 从 Origin 请求头中提取请求来源。
func Origin(r *http.Request) string {
	return HeaderValue(r, headerOrigin)
}

// Host 提取请求 Host；只有请求来自显式可信代理时才采纳转发头。
func Host(r *http.Request, trustedProxies ...string) string {
	if r == nil {
		return ""
	}
	if trustsForwardedHeaders(r, trustedProxies) {
		if value := validHost(firstForwardedParam(r.Header.Values(headerForwarded), "host")); value != "" {
			return value
		}
		if value := validHost(firstCSV(HeaderValue(r, headerForwardedHost))); value != "" {
			return value
		}
	}
	if value := validHost(r.Host); value != "" {
		return value
	}
	if r.URL != nil {
		return validHost(r.URL.Host)
	}
	return ""
}

// Scheme 提取 HTTP(S) 协议；只有请求来自显式可信代理时才采纳转发头。
func Scheme(r *http.Request, trustedProxies ...string) string {
	if r == nil {
		return ""
	}
	if r.URL != nil {
		if scheme := validHTTPScheme(r.URL.Scheme); scheme != "" {
			return scheme
		}
	}
	if trustsForwardedHeaders(r, trustedProxies) {
		if scheme := validHTTPScheme(firstForwardedParam(r.Header.Values(headerForwarded), "proto")); scheme != "" {
			return scheme
		}
		if scheme := validHTTPScheme(firstCSV(HeaderValue(r, headerForwardedProto))); scheme != "" {
			return scheme
		}
		if scheme := validHTTPScheme(firstCSV(HeaderValue(r, headerOriginalForward))); scheme != "" {
			return scheme
		}
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// BaseURL 根据请求协议和 Host 构造基础 URL。
func BaseURL(r *http.Request, trustedProxies ...string) string {
	host := Host(r, trustedProxies...)
	if host == "" {
		return ""
	}
	return Scheme(r, trustedProxies...) + "://" + host
}

func validHTTPScheme(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "http":
		return "http"
	case "https":
		return "https"
	default:
		return ""
	}
}

func validHost(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 255 || strings.ContainsAny(value, " \t\r\n/\\@") {
		return ""
	}
	parsed, err := url.Parse("http://" + value)
	if err != nil || parsed.User != nil || parsed.Host != value || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	if parsed.Hostname() == "" {
		return ""
	}
	if port := parsed.Port(); port != "" {
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return ""
		}
	}
	return value
}
