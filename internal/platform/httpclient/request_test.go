package httpclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/logx"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
)

func TestClientSupportsQueryStructAndRepeatedValues(t *testing.T) {
	type listQuery struct {
		Page int      `url:"page"`
		Tags []string `url:"tag"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "2", r.URL.Query().Get("page"))
		require.Equal(t, []string{"go", "http", "client"}, r.URL.Query()["tag"])
		writeJSON(t, w, userVO{ID: "42", Name: "alice"})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, ServiceConfig{})
	got, err := GetJSON[userVO](context.Background(), client, "/users",
		WithQueryStruct(listQuery{Page: 2, Tags: []string{"go", "http"}}),
		WithQuery("tag", "client"),
	)

	require.NoError(t, err)
	require.Equal(t, "42", got.ID)
}

func TestClientSupportsFormMultipartAndRawBodies(t *testing.T) {
	t.Run("form", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, r.ParseForm())
			require.Equal(t, []string{"reader", "writer"}, r.PostForm["role"])
			writeJSON(t, w, userVO{ID: "1", Name: r.PostForm.Get("name")})
		}))
		defer server.Close()

		client := newTestClient(t, server.URL, ServiceConfig{})
		got, err := PostForm[userVO](context.Background(), client, "/users", url.Values{
			"name": {"alice"},
			"role": {"reader", "writer"},
		})

		require.NoError(t, err)
		require.Equal(t, "alice", got.Name)
	})

	t.Run("multipart", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, r.ParseMultipartForm(1<<20))
			require.Equal(t, []string{"a", "b"}, r.MultipartForm.Value["tag"])
			file, header, err := r.FormFile("file")
			require.NoError(t, err)
			defer file.Close()
			content, err := io.ReadAll(file)
			require.NoError(t, err)
			require.Equal(t, "hello.txt", header.Filename)
			require.Equal(t, "hello", string(content))
			writeJSON(t, w, userVO{ID: "2", Name: "uploaded"})
		}))
		defer server.Close()

		client := newTestClient(t, server.URL, ServiceConfig{})
		got, err := PostMultipart[userVO](context.Background(), client, "/files",
			WithMultipartValues(url.Values{"tag": {"a", "b"}}),
			WithFile("file", "hello.txt", strings.NewReader("hello")),
		)

		require.NoError(t, err)
		require.Equal(t, "uploaded", got.Name)
	})

	t.Run("raw", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			content, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.Equal(t, "application/octet-stream", r.Header.Get("Content-Type"))
			require.Equal(t, []byte{1, 2, 3}, content)
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		client := newTestClient(t, server.URL, ServiceConfig{})
		_, err := client.Do(context.Background(), http.MethodPut, "/objects/1",
			WithRawBody([]byte{1, 2, 3}, "application/octet-stream"),
		)

		require.NoError(t, err)
	})
}

func TestClientPropagatesContextAndRunsRequestHook(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "trace-1", r.Header.Get("X-Trace-ID"))
		require.Equal(t, "request-1", r.Header.Get("X-Request-ID"))
		require.Equal(t, "signed", r.Header.Get("X-Signature"))
		writeJSON(t, w, map[string]bool{"ok": true})
	}))
	defer server.Close()

	factory, err := NewFactory(Config{Enabled: true, Services: map[string]ServiceConfig{
		"svc": {BaseURL: server.URL},
	}}, logx.NewNop(), WithServiceHooks("svc", func(request *http.Request) error {
		request.Header.Set("X-Signature", "signed")
		return nil
	}))
	require.NoError(t, err)
	client, err := factory.Get("svc")
	require.NoError(t, err)

	ctx := requestctx.WithTraceID(context.Background(), "trace-1")
	ctx = requestctx.WithRequestID(ctx, "request-1")
	_, err = client.Get(ctx, "/ping")

	require.NoError(t, err)
}

func TestClientWrapsRequestHookError(t *testing.T) {
	hookFailure := errors.New("signing unavailable")
	var hookCalls int
	factory, err := NewFactory(Config{Enabled: true, Services: map[string]ServiceConfig{
		"svc": {BaseURL: "http://example.test", Retry: testRetryConfig()},
	}}, logx.NewNop(), WithServiceHooks("svc", func(*http.Request) error {
		hookCalls++
		return hookFailure
	}))
	require.NoError(t, err)
	client, err := factory.Get("svc")
	require.NoError(t, err)

	_, err = client.Get(context.Background(), "/ping")

	var httpErr *Error
	require.ErrorAs(t, err, &httpErr)
	require.Equal(t, "request_hook_error", httpErr.Code)
	require.ErrorIs(t, err, hookFailure)
	require.Equal(t, 1, hookCalls)
}

func TestClientStreamKeepsContextUntilClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(w, bytes.NewBufferString("stream-content"))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, ServiceConfig{})
	response, err := client.Stream(context.Background(), http.MethodGet, "/stream", WithTimeout(time.Second))
	require.NoError(t, err)

	content, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, "stream-content", string(content))
	require.NoError(t, response.Close())
}

func TestClientCustomDecoderAndAcceptedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte("cached"))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, ServiceConfig{})
	var result string
	response, err := client.Get(context.Background(), "/cache",
		WithAcceptedStatus(http.StatusConflict),
		WithResult(&result),
		WithDecoder(func(body []byte, target any) error {
			*(target.(*string)) = string(body)
			return nil
		}),
	)

	require.NoError(t, err)
	require.Equal(t, http.StatusConflict, response.StatusCode)
	require.Equal(t, "cached", result)
}

func TestRequestOptionsRejectConflictingBodyModes(t *testing.T) {
	_, err := ApplyRequestOptions(
		WithJSONBody(map[string]string{"name": "alice"}),
		WithForm(url.Values{"name": {"alice"}}),
	)

	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be combined")
}
