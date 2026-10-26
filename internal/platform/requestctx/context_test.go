package requestctx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
)

func TestValuesRoundTripFromContext(t *testing.T) {
	ctx := requestctx.WithValues(context.Background(), requestctx.Values{
		RequestID: "req-1",
		TraceID:   "trace-1",
		UserID:    "user-1",
		Roles:     []string{"admin", "viewer"},
		TenantID:  "tenant-1",
		SessionID: "session-1",
		AppID:     "app-1",
	})
	ctx = requestctx.WithUserID(ctx, "user-2")

	requestID, ok := requestctx.RequestIDFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "req-1", requestID)
	traceID, ok := requestctx.TraceIDFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "trace-1", traceID)
	userID, ok := requestctx.UserIDFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "user-2", userID)
	roles, ok := requestctx.RolesFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, []string{"admin", "viewer"}, roles)
	tenantID, ok := requestctx.TenantIDFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "tenant-1", tenantID)
	sessionID, ok := requestctx.SessionIDFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "session-1", sessionID)
	appID, ok := requestctx.AppIDFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "app-1", appID)

	values := requestctx.ValuesFromContext(ctx)
	require.Equal(t, "req-1", values.RequestID)
	require.Equal(t, "trace-1", values.TraceID)
	require.Equal(t, "user-2", values.UserID)
	require.Equal(t, []string{"admin", "viewer"}, values.Roles)
	require.Equal(t, "tenant-1", values.TenantID)
	require.Equal(t, "session-1", values.SessionID)
	require.Equal(t, "app-1", values.AppID)
}

func TestValuesFromContextReportsMissingValues(t *testing.T) {
	_, ok := requestctx.UserIDFromContext(context.Background())
	require.False(t, ok)

	_, ok = requestctx.RolesFromContext(context.Background())
	require.False(t, ok)
}

func TestRequestMetadataExtraction(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/users/42?search=bee", nil)
	req.Host = "api.example.com"
	req.Header.Set("X-Language", "zh-CN")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Custom", "header-value")
	req.AddCookie(&http.Cookie{Name: "session", Value: "cookie-value"})
	req.SetPathValue("id", "42")
	req.PostForm = url.Values{"token": {"form-value"}}

	require.Equal(t, "bee", requestctx.QueryParam(req, "search"))
	require.Equal(t, "42", requestctx.PathParam(req, "id"))
	require.Equal(t, "header-value", requestctx.HeaderValue(req, "X-Custom"))
	require.Equal(t, "cookie-value", requestctx.CookieValue(req, "session"))
	require.Equal(t, "form-value", requestctx.FormParam(req, "token"))
	require.Equal(t, "en-US,en;q=0.9", requestctx.AcceptLanguage(req))
	require.Equal(t, "zh-CN", requestctx.Language(req, "X-Language"))
	require.Equal(t, "https://app.example.com", requestctx.Origin(req))
	require.Equal(t, "api.example.com", requestctx.Host(req))
	require.Equal(t, "http", requestctx.Scheme(req))
	require.Equal(t, "http://api.example.com", requestctx.BaseURL(req))
}

func TestForwardedMetadataRequiresTrustedProxy(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.2:12345"
	req.Host = "internal.example.com"
	req.Header.Set("Forwarded", `for=203.0.113.9;proto=https;host=api.example.com`)

	require.Equal(t, "internal.example.com", requestctx.Host(req))
	require.Equal(t, "http", requestctx.Scheme(req))
	require.Equal(t, "http://internal.example.com", requestctx.BaseURL(req))

	req = req.WithContext(requestctx.WithTrustedProxies(req.Context(), "10.0.0.0/8"))
	require.Equal(t, "api.example.com", requestctx.Host(req))
	require.Equal(t, "https", requestctx.Scheme(req))
	require.Equal(t, "https://api.example.com", requestctx.BaseURL(req))
}

func TestForwardedMetadataRejectsInvalidProtoAndHost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.2:12345"
	req.Host = "internal.example.com:8080"
	req.Header.Set("X-Forwarded-Host", "evil.example.com/path")
	req.Header.Set("X-Forwarded-Proto", "javascript")

	require.Equal(t, "internal.example.com:8080", requestctx.Host(req, "10.0.0.0/8"))
	require.Equal(t, "http", requestctx.Scheme(req, "10.0.0.0/8"))
	require.Equal(t, "http://internal.example.com:8080", requestctx.BaseURL(req, "10.0.0.0/8"))
}

func TestFormParamDoesNotFallBackToQuery(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?token=query-value", nil)

	require.Empty(t, requestctx.FormParam(req, "token"))

	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader("token=form-value"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	require.Equal(t, "form-value", requestctx.FormParam(req, "token"))
}

func TestClientIPUsesForwardedHeadersFromTrustedProxy(t *testing.T) {
	tests := []struct {
		name   string
		header string
		value  string
		want   string
	}{
		{
			name:   "x-forwarded-for",
			header: "X-Forwarded-For",
			value:  "203.0.113.9, 10.0.0.1",
			want:   "203.0.113.9",
		},
		{
			name:   "x-real-ip",
			header: "X-Real-IP",
			value:  "203.0.113.10",
			want:   "203.0.113.10",
		},
		{
			name:   "forwarded",
			header: "Forwarded",
			value:  `for="[2001:db8::1]:443";proto=https;host=api.example.com`,
			want:   "2001:db8::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = "10.0.0.2:12345"
			req.Header.Set(tt.header, tt.value)

			require.Equal(t, tt.want, requestctx.ClientIP(req, "10.0.0.0/8"))
		})
	}
}

func TestClientIPUsesTrustedProxiesFromContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.2:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	req = req.WithContext(requestctx.WithTrustedProxies(req.Context(), "10.0.0.0/8"))

	require.Equal(t, "203.0.113.9", requestctx.ClientIP(req))
}

func TestClientIPIgnoresForwardedHeadersFromUntrustedPeer(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.10:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")

	require.Equal(t, "198.51.100.10", requestctx.ClientIP(req, "10.0.0.0/8"))
}

func TestIPParsingAndMatching(t *testing.T) {
	addr, ok := requestctx.ParseIP("[2001:db8::1]:443")
	require.True(t, ok)
	require.Equal(t, "2001:db8::1", addr.String())

	require.Equal(t, "192.0.2.1", requestctx.NormalizeIP("192.0.2.1:8080"))
	require.Empty(t, requestctx.NormalizeIP("not-an-ip"))
	require.True(t, requestctx.IPInRange("192.0.2.5", "192.0.2.0/24"))
	require.True(t, requestctx.IPInRange("2001:db8::1", "2001:db8::/32"))
	require.True(t, requestctx.IPInRange("203.0.113.1", "203.0.113.1"))
	require.False(t, requestctx.IPInRange("203.0.113.2", "203.0.113.1"))
}
