package httpclient

import (
	"net/url"
	"strings"
)

func defaultSensitiveHeaders() []string {
	return []string{
		"authorization",
		"cookie",
		"x-api-key",
		"token",
		"password",
		"secret",
	}
}

func isSensitiveHeader(key string) bool {
	normalized := normalizeLogKey(key)
	if normalized == "" {
		return false
	}
	for _, sensitive := range defaultSensitiveHeaders() {
		field := normalizeLogKey(sensitive)
		if normalized == field {
			return true
		}
		switch field {
		case "authorization", "cookie", "password", "secret", "xapikey":
			if strings.Contains(normalized, field) {
				return true
			}
		case "token":
			if normalized == "token" || strings.HasSuffix(normalized, "token") {
				return true
			}
		}
	}
	return false
}

func normalizeLogKey(value string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func safeLogPath(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		if idx := strings.Index(raw, "?"); idx >= 0 {
			return raw[:idx]
		}
		return raw
	}
	if parsed.Path == "" {
		return raw
	}
	return parsed.Path
}

func safeErrorURL(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		if idx := strings.Index(raw, "?"); idx >= 0 {
			return raw[:idx]
		}
		return raw
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
