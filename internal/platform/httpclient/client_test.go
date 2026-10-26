package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/logx"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
)

type userVO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func TestClientGetJSONWithHeadersQueryAndPathParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/users/42", r.URL.Path)
		require.Equal(t, "1", r.URL.Query().Get("page"))
		require.Equal(t, "trace-1", r.Header.Get("X-Trace-ID"))
		require.Equal(t, "Bearer token-value", r.Header.Get("Authorization"))
		writeJSON(t, w, userVO{ID: "42", Name: "alice"})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, ServiceConfig{
		Auth: AuthConfig{Type: AuthTypeBearer, Token: "token-value"},
	})

	got, err := GetJSON[userVO](context.Background(), client, "/users/{id}",
		WithPathParams(map[string]string{"id": "42"}),
		WithQuery("page", "1"),
		WithHeader("X-Trace-ID", "trace-1"),
	)

	require.NoError(t, err)
	require.Equal(t, userVO{ID: "42", Name: "alice"}, got)
}

func TestClientPostJSONBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, jsonContentType, r.Header.Get("Content-Type"))
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "alice", body["name"])
		writeJSON(t, w, userVO{ID: "1", Name: "alice"})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, ServiceConfig{})

	got, err := PostJSON[userVO](context.Background(), client, "/users", map[string]string{"name": "alice"})

	require.NoError(t, err)
	require.Equal(t, "1", got.ID)
	require.Equal(t, "alice", got.Name)
}

func TestDoBytes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/page", r.URL.Path)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>ok</html>"))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, ServiceConfig{})

	body, resp, err := DoBytes(context.Background(), client, http.MethodGet, "/page")

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "text/html", resp.Header.Get("Content-Type"))
	require.Equal(t, []byte("<html>ok</html>"), body)
}

func TestClientMethods(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, ServiceConfig{})

	_, err := client.Put(context.Background(), "/resource", map[string]string{"name": "alice"})
	require.NoError(t, err)
	_, err = client.Patch(context.Background(), "/resource", map[string]string{"name": "bob"})
	require.NoError(t, err)
	_, err = client.Delete(context.Background(), "/resource")
	require.NoError(t, err)

	require.Equal(t, []string{http.MethodPut, http.MethodPatch, http.MethodDelete}, methods)
}

func TestClientUsesGlobalProxy(t *testing.T) {
	var proxyHits int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&proxyHits, 1)
		require.Equal(t, "http://remote.test/ping", r.URL.String())
		writeJSON(t, w, map[string]string{"ok": "true"})
	}))
	defer proxy.Close()

	factory, err := NewFactory(Config{
		Enabled: true,
		Proxy:   proxy.URL,
		Services: map[string]ServiceConfig{
			"svc": {BaseURL: "http://remote.test"},
		},
	}, logx.NewNop())
	require.NoError(t, err)
	client, err := factory.Get("svc")
	require.NoError(t, err)

	_, err = client.Get(context.Background(), "/ping")

	require.NoError(t, err)
	require.Equal(t, int32(1), atomic.LoadInt32(&proxyHits))
}

func TestClientServiceProxyOverridesGlobalProxy(t *testing.T) {
	var globalProxyHits int32
	globalProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&globalProxyHits, 1)
		http.Error(w, "global proxy should not be used", http.StatusBadGateway)
	}))
	defer globalProxy.Close()

	var serviceProxyHits int32
	serviceProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&serviceProxyHits, 1)
		require.Equal(t, "http://remote.test/ping", r.URL.String())
		writeJSON(t, w, map[string]string{"ok": "true"})
	}))
	defer serviceProxy.Close()

	factory, err := NewFactory(Config{
		Enabled: true,
		Proxy:   globalProxy.URL,
		Services: map[string]ServiceConfig{
			"svc": {
				BaseURL: "http://remote.test",
				Proxy:   serviceProxy.URL,
			},
		},
	}, logx.NewNop())
	require.NoError(t, err)
	client, err := factory.Get("svc")
	require.NoError(t, err)

	_, err = client.Get(context.Background(), "/ping")

	require.NoError(t, err)
	require.Equal(t, int32(0), atomic.LoadInt32(&globalProxyHits))
	require.Equal(t, int32(1), atomic.LoadInt32(&serviceProxyHits))
}

func TestClientAuthTypes(t *testing.T) {
	tests := []struct {
		name    string
		auth    AuthConfig
		assert  func(*testing.T, *http.Request)
		wantErr bool
	}{
		{
			name: "basic",
			auth: AuthConfig{Type: AuthTypeBasic, Username: "user", Password: "pass"},
			assert: func(t *testing.T, r *http.Request) {
				username, password, ok := r.BasicAuth()
				require.True(t, ok)
				require.Equal(t, "user", username)
				require.Equal(t, "pass", password)
			},
		},
		{
			name: "api key",
			auth: AuthConfig{Type: AuthTypeAPIKey, Header: "X-API-Key", Value: "api-secret"},
			assert: func(t *testing.T, r *http.Request) {
				require.Equal(t, "api-secret", r.Header.Get("X-API-Key"))
			},
		},
		{
			name: "custom header",
			auth: AuthConfig{Type: AuthTypeCustomHeader, Header: "X-Service-Token", Value: "custom-secret"},
			assert: func(t *testing.T, r *http.Request) {
				require.Equal(t, "custom-secret", r.Header.Get("X-Service-Token"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tt.assert(t, r)
				writeJSON(t, w, map[string]string{"ok": "true"})
			}))
			defer server.Close()

			client := newTestClient(t, server.URL, ServiceConfig{Auth: tt.auth})
			_, err := client.Get(context.Background(), "/ping")
			require.NoError(t, err)
		})
	}
}

func TestClientRetryGetAndIdempotentPostOnly(t *testing.T) {
	var getAttempts int32
	getServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := atomic.AddInt32(&getAttempts, 1)
		if attempt < 3 {
			http.Error(w, "retry", http.StatusBadGateway)
			return
		}
		writeJSON(t, w, map[string]string{"ok": "true"})
	}))
	defer getServer.Close()

	getClient := newTestClient(t, getServer.URL, ServiceConfig{Retry: testRetryConfig()})
	_, err := getClient.Get(context.Background(), "/retry")
	require.NoError(t, err)
	require.Equal(t, int32(3), atomic.LoadInt32(&getAttempts))

	var postAttempts int32
	postServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&postAttempts, 1)
		http.Error(w, "no retry", http.StatusBadGateway)
	}))
	defer postServer.Close()

	postClient := newTestClient(t, postServer.URL, ServiceConfig{Retry: testRetryConfig()})
	_, err = postClient.Post(context.Background(), "/retry", map[string]string{"name": "alice"})
	require.Error(t, err)
	require.True(t, IsResponseError(err))
	require.Equal(t, int32(1), atomic.LoadInt32(&postAttempts))

	var idempotentPostAttempts int32
	idempotentPostServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := atomic.AddInt32(&idempotentPostAttempts, 1)
		if attempt < 2 {
			http.Error(w, "retry", http.StatusBadGateway)
			return
		}
		writeJSON(t, w, map[string]string{"ok": "true"})
	}))
	defer idempotentPostServer.Close()

	idempotentPostClient := newTestClient(t, idempotentPostServer.URL, ServiceConfig{Retry: testRetryConfig()})
	_, err = idempotentPostClient.Post(context.Background(), "/retry", map[string]string{"name": "alice"}, WithIdempotent(true))
	require.NoError(t, err)
	require.Equal(t, int32(2), atomic.LoadInt32(&idempotentPostAttempts))
}

func TestClientRetryRespectsConfiguredStatusCodes(t *testing.T) {
	tests := []struct {
		name          string
		retryAll5xx   bool
		wantAttempts  int32
		wantRequestOK bool
	}{
		{name: "unlisted 5xx is not retried", wantAttempts: 1},
		{name: "all 5xx opt-in retries", retryAll5xx: true, wantAttempts: 2, wantRequestOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var attempts int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if atomic.AddInt32(&attempts, 1) == 1 {
					http.Error(w, "retry candidate", http.StatusNotImplemented)
					return
				}
				writeJSON(t, w, map[string]string{"ok": "true"})
			}))
			defer server.Close()

			client := newTestClient(t, server.URL, ServiceConfig{Retry: RetryConfig{
				Enabled:          true,
				Count:            1,
				WaitTime:         time.Millisecond,
				MaxWaitTime:      time.Millisecond,
				RetryStatusCodes: []int{http.StatusBadGateway},
				RetryAll5xx:      tt.retryAll5xx,
			}})
			_, err := client.Get(context.Background(), "/retry")
			if tt.wantRequestOK {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.Equal(t, tt.wantAttempts, atomic.LoadInt32(&attempts))
		})
	}
}

func TestClientTimeoutReturnsRequestError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		writeJSON(t, w, map[string]string{"ok": "true"})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, ServiceConfig{})

	_, err := client.Get(context.Background(), "/slow", WithTimeout(5*time.Millisecond))

	var httpErr *Error
	require.Error(t, err)
	require.True(t, errors.As(err, &httpErr))
	require.Equal(t, ErrorKindRequest, httpErr.Kind)
	require.Equal(t, "timeout", httpErr.Code)
}

func TestClientResponseAndParseErrors(t *testing.T) {
	t.Run("non 2xx", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		}))
		defer server.Close()

		client := newTestClient(t, server.URL, ServiceConfig{})

		resp, err := client.Get(context.Background(), "/missing")

		var httpErr *Error
		require.Error(t, err)
		require.True(t, errors.As(err, &httpErr))
		require.Equal(t, ErrorKindResponse, httpErr.Kind)
		require.Equal(t, http.StatusNotFound, httpErr.StatusCode)
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
		require.Same(t, resp, httpErr.Response)
		require.Contains(t, resp.String(), "not found")
	})

	t.Run("invalid json", func(t *testing.T) {
		var attempts int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&attempts, 1)
			w.Header().Set("Content-Type", jsonContentType)
			_, _ = w.Write([]byte(`{bad json`))
		}))
		defer server.Close()

		client := newTestClient(t, server.URL, ServiceConfig{Retry: testRetryConfig()})

		_, err := GetJSON[userVO](context.Background(), client, "/bad-json")

		var httpErr *Error
		require.Error(t, err)
		require.True(t, errors.As(err, &httpErr))
		require.Equal(t, ErrorKindInternal, httpErr.Kind)
		require.Equal(t, "response_parse_error", httpErr.Code)
		require.Equal(t, int32(1), atomic.LoadInt32(&attempts))
	})
}

func TestClientLogsWithoutSensitiveHeaders(t *testing.T) {
	logger, logPath := newHTTPClientTestLogger(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]string{"ok": "true"})
	}))
	defer server.Close()

	factory, err := NewFactory(Config{
		Enabled: true,
		Services: map[string]ServiceConfig{
			"svc": {BaseURL: server.URL},
		},
	}, logger)
	require.NoError(t, err)
	client, err := factory.Get("svc")
	require.NoError(t, err)

	ctx := requestctx.WithTraceID(context.Background(), "trace-1")
	_, err = client.Get(ctx, "/ping?token=secret",
		WithHeader("Authorization", "Bearer secret"),
	)

	require.NoError(t, err)
	require.NoError(t, logger.Sync())
	body := readHTTPClientLogFile(t, logPath)
	require.Contains(t, body, "http client request completed")
	require.Contains(t, body, `"url_path":"/ping"`)
	require.Contains(t, body, `"trace_id":"trace-1"`)
	require.NotContains(t, body, "Authorization")
	require.NotContains(t, body, "token")
	require.NotContains(t, body, "secret")
}

func TestClientDecodesStructuredErrorWithoutLoggingBody(t *testing.T) {
	logger, logPath := newHTTPClientTestLogger(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", jsonContentType)
		w.WriteHeader(http.StatusUnprocessableEntity)
		require.NoError(t, json.NewEncoder(w).Encode(map[string]string{"code": "invalid_name"}))
	}))
	defer server.Close()

	factory, err := NewFactory(Config{
		Enabled: true,
		Services: map[string]ServiceConfig{
			"svc": {BaseURL: server.URL},
		},
	}, logger)
	require.NoError(t, err)
	client, err := factory.Get("svc")
	require.NoError(t, err)

	var errorBody struct {
		Code string `json:"code"`
	}
	resp, err := client.Get(context.Background(), "/failure", WithErrorResult(&errorBody))

	require.Error(t, err)
	require.True(t, IsStatus(err, http.StatusUnprocessableEntity))
	require.Equal(t, "invalid_name", errorBody.Code)
	require.Same(t, &errorBody, resp.ErrorResult)
	require.NoError(t, logger.Sync())
	require.NotContains(t, readHTTPClientLogFile(t, logPath), "invalid_name")
}

func newTestClient(t *testing.T, baseURL string, override ServiceConfig) *Client {
	t.Helper()
	factory := newTestFactory(t, baseURL, override)
	client, err := factory.Get("svc")
	require.NoError(t, err)
	return client
}

func testRetryConfig() RetryConfig {
	return RetryConfig{
		Enabled:     true,
		Count:       2,
		WaitTime:    time.Millisecond,
		MaxWaitTime: time.Millisecond,
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", jsonContentType)
	require.NoError(t, json.NewEncoder(w).Encode(v))
}

func newHTTPClientTestLogger(t *testing.T) (*logx.Logger, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "httpclient.jsonl")
	logger, err := logx.NewLogger(logx.Config{
		Console: logx.ConsoleConfig{Enabled: false},
		JSONL:   logx.JSONLConfig{Enabled: true, Level: "debug", Path: path},
		Redact:  logx.RedactConfig{Enabled: true},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, logger.Close()) })
	return logger, path
}

func readHTTPClientLogFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(content)
}
