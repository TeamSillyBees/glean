package baseline

import (
	"regexp"
	"strings"
)

var privatePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?:/mnt/data|/home/[A-Za-z0-9_.-]+|/workspace|/tmp|/Users/[A-Za-z0-9_.-]+)/(?:[^\s"'<>，。；）\]\}]+)`),
	regexp.MustCompile(`(?i)[A-Z]:\\(?:Users|workspace|temp)\\[^\s"'<>]+`),
	regexp.MustCompile(`(?i)(?:https?://)?(?:chatgpt\.com|chat\.openai\.com|[^/\s]+\.oaiusercontent\.com|[^/\s]+\.openai\.org)/(?:[^\s"'<>，。；）\]\}]+)`),
	regexp.MustCompile(`(?:sediment://)?file_[A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`\b(?:turn[0-9]+(?:search|view|fetch)[0-9]+)\b`),
}

func sanitize(v any, key string) (any, int) {
	lower := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	switch lower {
	case "password", "api_key", "apikey", "access_token", "refresh_token", "authorization", "client_secret", "secret_key":
		if v != nil && v != "" {
			return "[REDACTED:private-credential]", 1
		}
	}
	count := 0
	switch value := v.(type) {
	case map[string]any:
		result := make(map[string]any, len(value))
		for k, item := range value {
			clean, n := sanitize(item, k)
			result[k] = clean
			count += n
		}
		return result, count
	case []any:
		result := make([]any, len(value))
		for i, item := range value {
			clean, n := sanitize(item, key)
			result[i] = clean
			count += n
		}
		return result, count
	case string:
		for _, pattern := range privatePatterns {
			matches := pattern.FindAllStringIndex(value, -1)
			count += len(matches)
			if len(matches) > 0 {
				value = pattern.ReplaceAllString(value, "[REDACTED:private-provenance]")
			}
		}
		return value, count
	default:
		return v, 0
	}
}
