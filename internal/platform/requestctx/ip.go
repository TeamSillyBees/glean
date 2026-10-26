package requestctx

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

const (
	headerXForwardedFor = "X-Forwarded-For"
	headerXRealIP       = "X-Real-IP"
)

type trustedProxySet struct {
	addrs    []netip.Addr
	prefixes []netip.Prefix
}

// ClientIP 提取客户端 IP。只有 RemoteAddr 命中可信代理列表时才信任转发头。
func ClientIP(r *http.Request, trustedProxies ...string) string {
	if r == nil {
		return ""
	}
	remoteIP, ok := ParseIP(r.RemoteAddr)
	if !ok {
		return ""
	}

	trusted := newTrustedProxySet(requestTrustedProxies(r, trustedProxies))
	if !trusted.contains(remoteIP) {
		return remoteIP.String()
	}

	forwarded := forwardedIPValues(r)
	for i := len(forwarded) - 1; i >= 0; i-- {
		ip, ok := ParseIP(forwarded[i])
		if !ok {
			continue
		}
		if !trusted.contains(ip) {
			return ip.String()
		}
	}
	for _, value := range forwarded {
		if ip, ok := ParseIP(value); ok {
			return ip.String()
		}
	}
	return remoteIP.String()
}

func requestTrustedProxies(r *http.Request, explicit []string) []string {
	if len(explicit) > 0 {
		return explicit
	}
	if r == nil {
		return nil
	}
	trustedProxies, _ := TrustedProxiesFromContext(r.Context())
	return trustedProxies
}

func trustsForwardedHeaders(r *http.Request, explicit []string) bool {
	if r == nil {
		return false
	}
	remoteIP, ok := ParseIP(r.RemoteAddr)
	if !ok {
		return false
	}
	return newTrustedProxySet(requestTrustedProxies(r, explicit)).contains(remoteIP)
}

// ParseIP 解析 IPv4 或 IPv6 地址，并兼容带端口、IPv6 方括号和空白的输入。
func ParseIP(value string) (netip.Addr, bool) {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"`)
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "unknown") {
		return netip.Addr{}, false
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	} else if strings.HasPrefix(value, "[") {
		if end := strings.Index(value, "]"); end > 0 {
			value = value[1:end]
		}
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

// NormalizeIP 解析并返回规范化后的 IP 字符串，非法输入返回空字符串。
func NormalizeIP(value string) string {
	addr, ok := ParseIP(value)
	if !ok {
		return ""
	}
	return addr.String()
}

// IPInRange 判断 IP 是否位于指定 IP 或 CIDR 范围内。
func IPInRange(ip string, target string) bool {
	addr, ok := ParseIP(ip)
	if !ok {
		return false
	}
	target = strings.TrimSpace(target)
	if strings.Contains(target, "/") {
		prefix, err := netip.ParsePrefix(target)
		if err != nil {
			return false
		}
		return prefix.Masked().Contains(addr)
	}
	targetAddr, ok := ParseIP(target)
	return ok && targetAddr == addr
}

func newTrustedProxySet(values []string) trustedProxySet {
	set := trustedProxySet{
		addrs:    make([]netip.Addr, 0, len(values)),
		prefixes: make([]netip.Prefix, 0, len(values)),
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.Contains(value, "/") {
			prefix, err := netip.ParsePrefix(value)
			if err == nil {
				set.prefixes = append(set.prefixes, prefix.Masked())
			}
			continue
		}
		if addr, ok := ParseIP(value); ok {
			set.addrs = append(set.addrs, addr)
		}
	}
	return set
}

func (s trustedProxySet) contains(ip netip.Addr) bool {
	for _, addr := range s.addrs {
		if addr == ip {
			return true
		}
	}
	for _, prefix := range s.prefixes {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

func forwardedIPValues(r *http.Request) []string {
	if r == nil {
		return nil
	}
	if values := forwardedParamValues(r.Header.Values(headerForwarded), "for"); len(values) > 0 {
		return values
	}
	if values := csvValues(r.Header.Values(headerXForwardedFor)); len(values) > 0 {
		return values
	}
	return csvValues(r.Header.Values(headerXRealIP))
}

func forwardedParamValues(headers []string, name string) []string {
	values := make([]string, 0, len(headers))
	for _, header := range headers {
		for _, item := range strings.Split(header, ",") {
			if value := forwardedParam(item, name); value != "" {
				values = append(values, value)
			}
		}
	}
	return values
}

func firstForwardedParam(headers []string, name string) string {
	values := forwardedParamValues(headers, name)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func forwardedParam(item string, name string) string {
	for _, part := range strings.Split(item, ";") {
		key, value, ok := strings.Cut(part, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), name) {
			continue
		}
		return strings.TrimSpace(strings.Trim(value, `"`))
	}
	return ""
}

func csvValues(headers []string) []string {
	values := make([]string, 0, len(headers))
	for _, header := range headers {
		for _, value := range strings.Split(header, ",") {
			if value = strings.TrimSpace(value); value != "" {
				values = append(values, value)
			}
		}
	}
	return values
}

func firstCSV(header string) string {
	values := csvValues([]string{header})
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
