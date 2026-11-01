package monitor

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// canaryDiagnostic 仅输出固定枚举与有界数字，绝不序列化错误文本或请求内容。
func canaryDiagnostic(source string, err error) string {
	diagnostic := struct {
		Source            string `json:"source"`
		Kind              string `json:"kind"`
		Status            int    `json:"status"`
		Retryable         bool   `json:"retryable"`
		RetryAfterSeconds int64  `json:"retryAfterSeconds"`
	}{Source: "unknown", Kind: "unknown"}
	for _, name := range DefaultSourceNames() {
		if source == name {
			diagnostic.Source = name
		}
	}
	var upstream *SourceError
	if errors.As(err, &upstream) && upstream != nil {
		switch upstream.Kind {
		case "http", "transport", "invalid_response", "provider_backoff", "provider_forbidden", "disabled", "missing_registry":
			diagnostic.Kind = upstream.Kind
		}
		if upstream.StatusCode >= 100 && upstream.StatusCode <= 599 {
			diagnostic.Status = upstream.StatusCode
		}
		diagnostic.Retryable = upstream.Retryable
		if upstream.RetryAfter > 0 {
			diagnostic.RetryAfterSeconds = int64(upstream.RetryAfter / time.Second)
		}
	}
	body, _ := json.Marshal(diagnostic)
	return "GLEAN_SOURCE_DIAGNOSTIC " + string(body)
}

func TestCanaryDiagnosticNeverIncludesRawError(t *testing.T) {
	for _, status := range []int{503, 429} {
		got := canaryDiagnostic("pubmed", fmt.Errorf("secret wrapper: %w", &SourceError{Kind: "http", StatusCode: status, Retryable: true, RetryAfter: 30 * time.Second, Message: "secret body/header"}))
		if strings.Contains(got, "secret") || !strings.Contains(got, fmt.Sprintf(`"status":%d`, status)) || !strings.Contains(got, `"retryAfterSeconds":30`) {
			t.Fatal("unsafe or missing typed diagnostics")
		}
	}
	for _, kind := range []string{"invalid_response", "transport", "secret"} {
		got := canaryDiagnostic("europepmc", &SourceError{Kind: kind, StatusCode: 999, RetryAfter: -time.Second, Message: "secret"})
		if strings.Contains(got, "secret") || !strings.Contains(got, `"status":0`) || !strings.Contains(got, `"retryAfterSeconds":0`) {
			t.Fatal("unsafe diagnostics")
		}
	}
	if strings.Contains(canaryDiagnostic("secret", errors.New("secret")), "secret") {
		t.Fatal("raw error or source leaked")
	}
}
