package monitor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/httpclient"
)

// exerciseArXivContinuationFailure 经序列化存储重建服务，验证异常续页不清断点、不推进覆盖水位。
func exerciseArXivContinuationFailure(t *testing.T, ctx context.Context, factory func([]Source) *Service) {
	t.Helper()
	executor := &adapterExecutor{responses: []*httpclient.Response{
		jsonResponse(`<OAI-PMH><ListRecords><record><header status="deleted"><identifier>oai:arXiv.org:synthetic</identifier></header></record><resumptionToken>unfinished-page-2</resumptionToken></ListRecords></OAI-PMH>`),
		jsonResponse(`<OAI-PMH><error code="noRecordsMatch">private-upstream-error-body</error></OAI-PMH>`),
	}}
	now := scenarioTime
	build := func() *Service {
		s := factory([]Source{NewArXivSource(executor)})
		s.templates = &mutableTemplates{items: []SearchTemplate{TopicSearchTemplate("custom", [][]string{{"clinical"}}, 1)}}
		s.clock = func() time.Time { return now }
		return s
	}
	// 单日窗口使错误完成会实际推进水位，而不只是跳到后一天。
	window := Window{Start: dayStart(now), End: now}
	first, err := build().RunWindow(ctx, window)
	require.NoError(t, err)
	require.Len(t, first.Runs, 1)
	original := first.Runs[0]
	cursor := original.Sources["arxiv"].Cursor
	require.NotEmpty(t, cursor)
	require.False(t, original.Sources["arxiv"].Acquired)
	now = now.Add(time.Minute)
	failed, err := build().RunOnce(ctx, now)
	require.NoError(t, err)
	require.Len(t, executor.requests, 2)
	require.Equal(t, "unfinished-page-2", executor.requests[1].options.QueryParams.Get("resumptionToken"))
	require.Equal(t, "completed_with_errors", failed.Runs[0].Status)
	// 再次重建并读取已保存状态；不能仅检查上一实例返回的快照。
	persisted, err := build().Snapshot(ctx)
	require.NoError(t, err)
	require.Len(t, persisted.Runs, 1)
	run := persisted.Runs[0]
	progress := run.Sources["arxiv"]
	require.Equal(t, original.ID, run.ID)
	require.Equal(t, original.FixedEnd, run.FixedEnd)
	require.Equal(t, original.SearchTemplates, run.SearchTemplates)
	require.Equal(t, original.Sources["arxiv"].Window, progress.Window)
	require.Equal(t, cursor, progress.Cursor)
	require.Equal(t, 0, progress.TemplateIndex)
	require.Equal(t, 1, progress.Pages)
	require.True(t, progress.Failed)
	require.False(t, progress.Acquired)
	require.Equal(t, "invalid_response", progress.LastError)
	checkpoint := persisted.Sources["arxiv"]
	require.True(t, checkpoint.AcquiredThrough.IsZero())
	require.True(t, checkpoint.TopicAcquiredThrough["custom"].IsZero())
	require.True(t, checkpoint.LastAcquiredAt.IsZero())
	require.Equal(t, "invalid_response", checkpoint.LastError)
	require.Len(t, executor.requests, 2, "reloading state must not fetch upstream")
	// invalid_response 沿用非重试错误语义，不能通过显式 retry 静默跳过异常。
	_, err = build().RetryFailedFinite(ctx, original.ID, 1)
	require.Error(t, err)
	require.Len(t, executor.requests, 2, "permanent failure retry must not fetch upstream")
}

// TestArXivContinuationFailureSurvivesRestart 使用真实适配器与假 HTTP 验证重建后的保守完成判定。
func TestArXivContinuationFailureSurvivesRestart(t *testing.T) {
	memory := &memoryState{}
	exerciseArXivContinuationFailure(t, context.Background(), func(sources []Source) *Service {
		base := offline(t, DemoSources(scenarioTime, false), nil, nil)
		s, err := NewOfflineService(base.ids, RuntimeConfig(), sources, nil, &flakyPublisher{})
		require.NoError(t, err)
		s.memory = memory
		return s
	})
}
