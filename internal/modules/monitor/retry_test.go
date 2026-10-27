package monitor

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

func failedRetryService(t *testing.T, failure error) (*Service, *countSource, Snapshot) {
	t.Helper()
	source := &countSource{fixture: DemoSources(scenarioTime, false)[0].(*FixtureSource)}
	source.fixture.Failure = failure
	service := offline(t, []Source{source}, nil, nil)
	service.config.RetryBudget = 1
	service.clock = func() time.Time { return scenarioTime }
	first, err := service.RunFinite(context.Background(), Window{Start: scenarioTime.Add(-24 * time.Hour), End: scenarioTime}, 10)
	require.NoError(t, err)
	require.Equal(t, "completed_with_errors", first.Runs[0].Status)
	return service, source, first
}

// TestRetryFailedRunKeepsWindowAndEvidence 验证显式恢复固定原窗口、保留旧失败与来源游标，同一命令不重复创建预算。
func TestRetryFailedRunKeepsWindowAndEvidence(t *testing.T) {
	s, source, first := failedRetryService(t, &SourceError{Kind: "http", StatusCode: 503, Retryable: true})
	old := first.Runs[0]
	require.True(t, old.Sources[source.Name()].RetryableFailure)
	_, err := s.RetryFailedFinite(context.Background(), old.ID, 10)
	require.ErrorContains(t, err, "cooldown")
	require.Equal(t, 1, source.calls)
	source.fixture.Failure = nil
	s.clock = func() time.Time { return scenarioTime.Add(time.Hour) }
	restored, err := s.RetryFailedFinite(context.Background(), old.ID, 10)
	require.NoError(t, err)
	require.Len(t, restored.Runs, 2)
	child := restored.Runs[1]
	require.Equal(t, "completed", child.Status)
	require.Equal(t, old.ID, child.RetryOf)
	require.Equal(t, child.ID, restored.Runs[0].RetriedBy)
	require.Equal(t, old.FixedEnd, child.FixedEnd)
	require.Equal(t, old.RequestKey, child.RequestKey)
	require.Equal(t, old.TemplateVersion, child.TemplateVersion)
	require.Equal(t, old.Sources[source.Name()].Window, child.Sources[source.Name()].Window)
	require.Equal(t, "completed_with_errors", restored.Runs[0].Status)
	oldSources, err := json.Marshal(old.Sources)
	require.NoError(t, err)
	restoredSources, err := json.Marshal(restored.Runs[0].Sources)
	require.NoError(t, err)
	require.JSONEq(t, string(oldSources), string(restoredSources))
	require.Equal(t, old.FinishedAt, restored.Runs[0].FinishedAt)
	require.Equal(t, scenarioTime, restored.Sources[source.Name()].AcquiredThrough)
	calls := source.calls
	again, err := s.RetryFailedFinite(context.Background(), old.ID, 10)
	require.NoError(t, err)
	require.Len(t, again.Runs, 2)
	require.Equal(t, calls, source.calls)
}

// TestRetryPermanentAndLegacyFailuresFailClosed 不凭错误字符串推测旧快照可重试性，不重新请求永久错误。
func TestRetryPermanentAndLegacyFailuresFailClosed(t *testing.T) {
	for _, failure := range []*SourceError{{Kind: "malformed"}, {Kind: "forbidden", StatusCode: 403}, {Kind: "http", StatusCode: 429, Retryable: true, RetryAfter: 24 * time.Hour}} {
		t.Run(failure.Kind, func(t *testing.T) {
			s, source, first := failedRetryService(t, failure)
			s.clock = func() time.Time { return scenarioTime.Add(time.Hour) }
			_, err := s.RetryFailedFinite(context.Background(), first.Runs[0].ID, 10)
			require.Error(t, err)
			require.Equal(t, 1, source.calls)
		})
	}
	s, source, first := failedRetryService(t, &SourceError{Kind: "http", Retryable: true})
	state, err := s.load(context.Background())
	require.NoError(t, err)
	state.Runs[0].Sources[source.Name()].RetryableFailure = false // 旧版本没有持久化分类。
	require.NoError(t, s.save(context.Background(), state))
	s.clock = func() time.Time { return scenarioTime.Add(time.Hour) }
	_, err = s.RetryFailedFinite(context.Background(), first.Runs[0].ID, 10)
	require.ErrorContains(t, err, "no classified")
	require.Equal(t, 1, source.calls)
}

// TestRetryPreservesPartialCursorAndSuccessfulSources 验证恢复不重新采集成功来源、不重审已成功条目。
func TestRetryPreservesPartialCursorAndSuccessfulSources(t *testing.T) {
	s, source, first := failedRetryService(t, &SourceError{Kind: "http", Retryable: true})
	state, err := s.load(context.Background())
	require.NoError(t, err)
	p := state.Runs[0].Sources[source.Name()]
	p.TemplateIndex = 1
	p.Cursor = "saved-token"
	p.Pages = 3
	p.Observed = 9
	p.ReviewedCount = 5
	p.Projected = 4
	// 使用直接准备入口检查不能从后续成功响应推测此前游标正确。
	state.Runs[0].Sources["done"] = &SourceProgress{Acquired: true, Reviewed: true, Published: true, Handled: true, Pages: 4, Window: p.Window}
	state.Sources["done"] = &Checkpoint{Source: "done"}
	require.NoError(t, s.save(context.Background(), state))
	s.sources = append(s.sources, &FixtureSource{SourceName: "done"})
	s.clock = func() time.Time { return scenarioTime.Add(time.Hour) }
	id, snapshot, err := s.prepareRetry(context.Background(), first.Runs[0].ID)
	require.NoError(t, err)
	require.NotZero(t, id)
	child := snapshot.Runs[1]
	cp := child.Sources[source.Name()]
	require.Equal(t, "saved-token", cp.Cursor)
	require.Equal(t, 1, cp.TemplateIndex)
	require.Equal(t, 3, cp.Pages)
	require.Equal(t, 9, cp.Observed)
	require.Equal(t, 5, cp.ReviewedCount)
	require.Equal(t, 4, cp.Projected)
	require.Equal(t, state.Runs[0].Sources["done"], child.Sources["done"])
	require.False(t, cp.Failed)
	require.Zero(t, cp.Attempts)
}

// TestRetryPreparationConcurrencyAndRestart 验证租约与 CAS 只创建一个恢复运行，重建服务不丢恢复关系。
func TestRetryPreparationConcurrencyAndRestart(t *testing.T) {
	s, _, first := failedRetryService(t, &SourceError{Kind: "http", Retryable: true})
	s.clock = func() time.Time { return scenarioTime.Add(time.Hour) }
	var wg sync.WaitGroup
	ids := make(chan idgen.ID, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, _, err := s.prepareRetry(context.Background(), first.Runs[0].ID)
			if err == nil {
				ids <- id
			}
		}()
	}
	wg.Wait()
	close(ids)
	final, err := s.Snapshot(context.Background())
	require.NoError(t, err)
	require.Len(t, final.Runs, 2)
	for id := range ids {
		require.Equal(t, final.Runs[1].ID, id)
	}
	raw, err := json.Marshal(final)
	require.NoError(t, err)
	require.Contains(t, string(raw), "retryOf")
	restored := *s
	restored.memory = &memoryState{data: append([]byte(nil), s.memory.data...), revision: s.memory.revision, id: s.memory.id}
	id, _, err := restored.prepareRetry(context.Background(), first.Runs[0].ID)
	require.NoError(t, err)
	require.Equal(t, final.Runs[1].ID, id)
}

// TestRetryDoesNotResetFailedChildBudget 验证重复原命令不是无限重试入口。
func TestRetryDoesNotResetFailedChildBudget(t *testing.T) {
	s, source, first := failedRetryService(t, &SourceError{Kind: "http", Retryable: true})
	s.clock = func() time.Time { return scenarioTime.Add(time.Hour) }
	second, err := s.RetryFailedFinite(context.Background(), first.Runs[0].ID, 10)
	require.NoError(t, err)
	require.Equal(t, "completed_with_errors", second.Runs[1].Status)
	calls := source.calls
	s.clock = func() time.Time { return scenarioTime.Add(2 * time.Hour) }
	again, err := s.RetryFailedFinite(context.Background(), first.Runs[0].ID, 10)
	require.NoError(t, err)
	require.Len(t, again.Runs, 2)
	require.Equal(t, calls, source.calls)
	source.fixture.Failure = nil
	third, err := s.RetryFailedFinite(context.Background(), second.Runs[1].ID, 10)
	require.NoError(t, err)
	require.Len(t, third.Runs, 3)
	require.Equal(t, "completed", third.Runs[2].Status)
}

// TestRetryRejectsChangedInputsAndLease 验证不能改源集/检索版本，也不能抢占现有 worker。
func TestRetryRejectsChangedInputsAndLease(t *testing.T) {
	for _, kind := range []string{"sources", "template", "blocked", "lease", "missing_candidate"} {
		t.Run(kind, func(t *testing.T) {
			s, source, first := failedRetryService(t, &SourceError{Kind: "http", Retryable: true})
			s.clock = func() time.Time { return scenarioTime.Add(time.Hour) }
			state, err := s.load(context.Background())
			require.NoError(t, err)
			switch kind {
			case "sources":
				s.sources = append(s.sources, &FixtureSource{SourceName: "extra"})
			case "template":
				state.Runs[0].TemplateVersion = "obsolete"
			case "blocked":
				state.Sources[source.Name()].Blocked = true
			case "lease":
				state.leaseOwner = "other-worker"
				state.leaseUntil = s.clock().Add(time.Hour)
			case "missing_candidate":
				state.Runs[0].Sources[source.Name()].CandidateKeys = []string{"missing"}
			}
			require.NoError(t, s.save(context.Background(), state))
			_, err = s.RetryFailedFinite(context.Background(), first.Runs[0].ID, 10)
			require.Error(t, err)
			require.Equal(t, 1, source.calls)
		})
	}
}

// TestRetryDoesNotConsumeScheduledBackfillCadence 恢复不是新的周期扫描，不能跳过第十次宽窗口补漏。
func TestRetryDoesNotConsumeScheduledBackfillCadence(t *testing.T) {
	s, source, first := failedRetryService(t, &SourceError{Kind: "http", Retryable: true})
	state, err := s.load(context.Background())
	require.NoError(t, err)
	state.Schedule.CompletedRuns = 9
	state.Runs[0].Number = 9
	require.NoError(t, s.save(context.Background(), state))
	source.fixture.Failure = nil
	s.clock = func() time.Time { return scenarioTime.Add(time.Hour) }
	done, err := s.RetryFailedFinite(context.Background(), first.Runs[0].ID, 10)
	require.NoError(t, err)
	require.Equal(t, 9, done.Schedule.CompletedRuns)
	require.Equal(t, 9, done.Runs[1].Number)
	state, err = s.load(context.Background())
	require.NoError(t, err)
	next := s.startRun(state, scenarioTime.Add(72*time.Hour), nil, nil, nil)
	require.Equal(t, 10, next.Number)
}

// TestRetryProvenanceSurvivesPublishedCompaction 验证待处理候选键在正常缓存压缩后仍有恢复起点记录。
func TestRetryProvenanceSurvivesPublishedCompaction(t *testing.T) {
	s, source, first := failedRetryService(t, &SourceError{Kind: "http", Retryable: true})
	state, err := s.load(context.Background())
	require.NoError(t, err)
	state.Runs[0].Sources[source.Name()].CandidateKeys = []string{"evidence-key"}
	state.Records["evidence-key"] = &Candidate{ID: 123, Key: "evidence-key", Record: Record{Source: source.Name()}}
	require.NoError(t, s.save(context.Background(), state))
	s.clock = func() time.Time { return scenarioTime.Add(time.Hour) }
	_, _, err = s.prepareRetry(context.Background(), first.Runs[0].ID)
	require.NoError(t, err)
	state, err = s.load(context.Background())
	require.NoError(t, err)
	state.Records["evidence-key"].Published = true
	state.Records["evidence-key"].Review = &Review{}
	s.config.CompactPublished = true
	s.compact(state)
	require.Empty(t, state.Runs[1].Sources[source.Name()].CandidateKeys)
	require.Equal(t, []string{"evidence-key"}, state.Runs[1].RetryCandidateKeys[source.Name()])
}
