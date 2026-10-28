package monitor

import (
	"context"
	"errors"
	"github.com/teamsillybees/glean/internal/platform/httpclient"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// recoverySource 为恢复测试提供两个分页，并能在持久游标后注入取消或来源失败。
type recoverySource struct {
	name     string
	requests []FetchRequest
	cancel   context.CancelFunc
	failure  error
}

func (s *recoverySource) Name() string { return s.name }
func (s *recoverySource) IsMock() bool { return true }
func (s *recoverySource) Fetch(ctx context.Context, r FetchRequest) (Page, error) {
	s.requests = append(s.requests, r)
	if s.cancel != nil {
		s.cancel()
		return Page{}, ctx.Err()
	}
	if s.failure != nil {
		return Page{}, s.failure
	}
	record := Record{SourceID: s.name + "-" + r.Cursor, Title: "Synthetic recovery fixture", Version: "1"}
	if r.Cursor == "" {
		return Page{Records: []Record{record}, NextCursor: "saved-page-2"}, nil
	}
	return Page{Records: []Record{record}, Complete: true}, nil
}

// exerciseDurableRecovery 同时运行于序列化内存与 PostgreSQL；每次恢复均重建服务，不共享运行对象。
func exerciseDurableRecovery(t *testing.T, ctx context.Context, factory func([]Source) *Service) {
	t.Helper()
	healthy := &recoverySource{name: "healthy"}
	failing := &recoverySource{name: "recovering"}
	sources := []Source{healthy, failing}
	provider := &mutableTemplates{items: []SearchTemplate{TopicSearchTemplate("custom", [][]string{{"original"}}, 1)}}
	now := scenarioTime
	build := func() *Service {
		s := factory(sources)
		s.config.RetryBudget = 1
		s.config.MaxPagesPerCall = 3
		s.templates = provider
		s.clock = func() time.Time { return now }
		return s
	}
	first, err := build().RunOnce(ctx, now)
	require.NoError(t, err)
	require.Equal(t, "pending", first.Runs[0].Status)
	require.Equal(t, "saved-page-2", first.Runs[0].Sources[failing.name].Cursor)
	require.Equal(t, now, first.Sources[healthy.name].AcquiredThrough)
	require.Equal(t, now, first.Sources[healthy.name].LastAcquiredAt)
	require.True(t, first.Sources[failing.name].AcquiredThrough.IsZero())
	require.True(t, first.Sources[failing.name].LastAcquiredAt.IsZero())
	require.Len(t, first.Records, 3)
	original := first.Runs[0]
	// 改动/停用主题及配置服务不可用不能改写已经持久化的查询快照。
	provider.items[0].TermGroups[0][0] = "changed"
	provider.err = errors.New("topic provider unavailable")
	canceledCtx, cancel := context.WithCancel(ctx)
	failing.cancel = cancel
	canceled, err := build().RunOnce(canceledCtx, now)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, "canceled", canceled.Runs[0].Status)
	require.Equal(t, original.ID, canceled.Runs[0].ID)
	failing.cancel = nil
	failing.failure = &SourceError{Kind: "http", StatusCode: 503, Retryable: true}
	now = now.Add(time.Minute)
	failed, err := build().RunOnce(ctx, now)
	require.NoError(t, err)
	require.Equal(t, "completed_with_errors", failed.Runs[0].Status)
	require.Equal(t, original.ID, failed.Runs[0].ID)
	require.True(t, failed.Sources[failing.name].AcquiredThrough.IsZero())
	require.True(t, failed.Sources[failing.name].TopicAcquiredThrough["custom"].IsZero())
	require.True(t, failed.Sources[failing.name].LastAcquiredAt.IsZero())
	require.Equal(t, scenarioTime, failed.Sources[healthy.name].LastAcquiredAt)
	require.Len(t, healthy.requests, 2, "successful source must not be fetched again")
	failing.failure = nil
	now = now.Add(time.Hour)
	restored := build()
	done, err := restored.RetryFailedFinite(ctx, original.ID, 10)
	require.NoError(t, err)
	require.Len(t, done.Runs, 2)
	require.Equal(t, "completed", done.Runs[1].Status)
	require.Equal(t, original.ID, done.Runs[1].RetryOf)
	require.Equal(t, original.FixedEnd, done.Runs[1].FixedEnd)
	require.Equal(t, original.SearchTemplates, done.Runs[1].SearchTemplates)
	require.Equal(t, original.TemplateVersion, done.Runs[1].TemplateVersion)
	require.Equal(t, scenarioTime, done.Sources[failing.name].AcquiredThrough)
	require.Equal(t, scenarioTime, done.Sources[failing.name].TopicAcquiredThrough["custom"])
	require.Equal(t, now, done.Sources[failing.name].LastAcquiredAt)
	require.Len(t, done.Records, 4)
	require.Len(t, healthy.requests, 2)
	require.Len(t, failing.requests, 4)
	for _, request := range failing.requests[1:] {
		require.Equal(t, "saved-page-2", request.Cursor)
		require.Equal(t, failing.requests[0].Window, request.Window)
		require.Equal(t, failing.requests[0].Template, request.Template)
	}
	require.Equal(t, 1, provider.calls)
	replayed, err := build().RetryFailedFinite(ctx, original.ID, 10)
	require.NoError(t, err)
	require.Len(t, replayed.Runs, 2)
	require.Len(t, replayed.Records, 4)
	require.Len(t, healthy.requests, 2)
	require.Len(t, failing.requests, 4)
	require.Equal(t, now, replayed.Sources[failing.name].LastAcquiredAt)
}

// TestDurableRecoveryAfterCancellationAndPartialFailure 验证普通 CI 不依赖外部 API 的取消、重建、部分失败与冻结配置恢复。
func TestDurableRecoveryAfterCancellationAndPartialFailure(t *testing.T) {
	memory := &memoryState{}
	exerciseDurableRecovery(t, context.Background(), func(sources []Source) *Service {
		s := offline(t, sources, nil, nil)
		s.memory = memory
		return s
	})
}

// TestEuropePMCHTTPResumeAfter503 在普通 CI 使用假 HTTP 响应验证真实适配器游标与 Retry-After，无外部请求。
func TestEuropePMCHTTPResumeAfter503(t *testing.T) {
	ctx := context.Background()
	executor := &adapterExecutor{responses: []*httpclient.Response{
		jsonResponse(`{"hitCount":2,"nextCursorMark":"next","resultList":{"result":[{"id":"1","source":"MED","title":"Synthetic first"}]}}`),
		{StatusCode: 503, Header: http.Header{"Retry-After": []string{"300"}}},
		jsonResponse(`{"hitCount":2,"resultList":{"result":[{"id":"2","source":"MED","title":"Synthetic second"}]}}`),
	}}
	publisher := &flakyPublisher{}
	memory := &memoryState{}
	now := scenarioTime
	provider := &mutableTemplates{items: []SearchTemplate{TopicSearchTemplate("custom", [][]string{{"original"}}, 1)}}
	build := func() *Service {
		base := offline(t, DemoSources(scenarioTime, false), nil, nil)
		cfg := RuntimeConfig()
		s, err := NewOfflineService(base.ids, cfg, []Source{NewEuropePMCSource(executor)}, MockReviewer{}, publisher)
		require.NoError(t, err)
		s.memory = memory
		s.clock = func() time.Time { return now }
		s.templates = provider
		return s
	}
	first, err := build().RunOnce(ctx, now)
	require.NoError(t, err)
	require.Len(t, executor.requests, 1)
	cursor := first.Runs[0].Sources["europepmc"].Cursor
	require.NotEmpty(t, cursor)
	require.True(t, first.Sources["europepmc"].LastAcquiredAt.IsZero())
	provider.err = errors.New("topic provider unavailable")
	now = now.Add(4 * time.Second)
	failed, err := build().RunOnce(ctx, now)
	require.NoError(t, err)
	require.Equal(t, 503, failed.Sources["europepmc"].LastStatusCode)
	require.Equal(t, now.Add(5*time.Minute), failed.Sources["europepmc"].NextRetryAt)
	require.Equal(t, cursor, failed.Runs[0].Sources["europepmc"].Cursor)
	require.True(t, failed.Sources["europepmc"].AcquiredThrough.IsZero())
	now = now.Add(time.Minute)
	_, err = build().RunOnce(ctx, now)
	require.NoError(t, err)
	require.Len(t, executor.requests, 2, "rebuilding must not bypass durable Retry-After")
	now = now.Add(4 * time.Minute)
	done, err := build().RunOnce(ctx, now)
	require.NoError(t, err)
	require.Equal(t, "completed", done.Runs[0].Status)
	require.Equal(t, first.Runs[0].ID, done.Runs[0].ID)
	require.Equal(t, scenarioTime, done.Sources["europepmc"].AcquiredThrough)
	require.Equal(t, now, done.Sources["europepmc"].LastAcquiredAt)
	require.Len(t, done.Records, 2)
	require.Len(t, publisher.keys, 2)
	require.Equal(t, executor.requests[1], executor.requests[2], "retry must repeat frozen HTTP query and cursor")
	_, err = build().RunOnce(ctx, now)
	require.NoError(t, err)
	require.Len(t, executor.requests, 3)
	require.Equal(t, 1, provider.calls)
}
