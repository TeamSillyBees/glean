package monitor

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/httpclient"
)

type gateExecutor struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
	status  int
	retry   string
}

func (e *gateExecutor) Do(ctx context.Context, _ string, _ string, _ ...httpclient.RequestOption) (*httpclient.Response, error) {
	e.calls.Add(1)
	if e.started != nil {
		select {
		case e.started <- struct{}{}:
		default:
		}
	}
	if e.release != nil {
		select {
		case <-e.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	status := e.status
	if status == 0 {
		status = 200
	}
	return &httpclient.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{e.retry}}}, nil
}

// TestSourceGateSerializesEntireRequest 验证慢请求期间第二连接无法越过三秒预约并发，取消等待不调用 HTTP。
func TestSourceGateSerializesEntireRequest(t *testing.T) {
	base := &gateExecutor{started: make(chan struct{}, 1), release: make(chan struct{})}
	gate := &requestGate{}
	one := &pacedExecutor{base: base, gate: gate}
	two := &pacedExecutor{base: base, gate: gate}
	done := make(chan error, 1)
	go func() { _, err := one.Do(context.Background(), http.MethodGet, "/one"); done <- err }()
	<-base.started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := two.Do(ctx, http.MethodGet, "/two")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, int32(1), base.calls.Load())
	close(base.release)
	require.NoError(t, <-done)
}

// TestSharedProviderRetryAfterNeverMakesSecondRequest 验证 bioRxiv/medRxiv 共享上游退避而不是分别重试。
func TestSharedProviderRetryAfterNeverMakesSecondRequest(t *testing.T) {
	for _, status := range []int{429, 503, 403} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			base := &gateExecutor{status: status, retry: "60"}
			gate := &requestGate{}
			first := &pacedExecutor{base: base, gate: gate}
			second := &pacedExecutor{base: base, gate: gate}
			_, err := first.Do(context.Background(), http.MethodGet, "/first")
			require.NoError(t, err)
			_, err = second.Do(context.Background(), http.MethodGet, "/second")
			var sourceErr *SourceError
			require.True(t, errors.As(err, &sourceErr))
			require.Equal(t, status, sourceErr.StatusCode)
			require.Greater(t, sourceErr.RetryAfter, 59*time.Second)
			require.Equal(t, int32(1), base.calls.Load())
		})
	}
}

// TestOfficialSourceHookRejectsAllRedirectsAndOriginChanges 验证同源重定向也不能绕过单次请求计数和限速。
func TestOfficialSourceHookRejectsAllRedirectsAndOriginChanges(t *testing.T) {
	hook := officialSourceHook("api.crossref.org")
	for _, url := range []string{"http://api.crossref.org/works", "https://127.0.0.1/works", "https://api.crossref.org.evil.test/works", "https://user:secret@api.crossref.org/works", "https://api.crossref.org:444/works"} {
		request, err := http.NewRequest(http.MethodGet, url, nil)
		require.NoError(t, err)
		require.Error(t, hook(request))
	}
	request, err := http.NewRequest(http.MethodGet, "https://api.crossref.org/works", nil)
	require.NoError(t, err)
	require.NoError(t, hook(request))
	request.Response = &http.Response{StatusCode: 302}
	require.Error(t, hook(request))
	request.Response = nil
	request.Method = http.MethodPost
	require.Error(t, hook(request))
}
