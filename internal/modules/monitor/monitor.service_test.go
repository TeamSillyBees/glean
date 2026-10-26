package monitor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

var scenarioTime = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

func offline(t *testing.T, sources []Source, reviewer Reviewer, publisher Publisher) *Service {
	t.Helper()
	ids, err := idgen.NewGenerator(7)
	require.NoError(t, err)
	cfg := DefaultConfig()
	cfg.MaxPagesPerCall = 100
	service, err := NewOfflineService(ids, cfg, sources, reviewer, publisher)
	require.NoError(t, err)
	return service
}

// TestBaselineIncrementalKeepsVersions 验证离线全链路、新论文、版本与正式出版事件互不覆盖。
func TestBaselineIncrementalKeepsVersions(t *testing.T) {
	service := offline(t, DemoSources(scenarioTime, false), nil, nil)
	baseline, err := service.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	require.Len(t, baseline.Records, 4)
	require.Equal(t, "completed", baseline.Runs[0].Status)
	require.Equal(t, scenarioTime.Add(-30*24*time.Hour), baseline.Runs[0].Sources["crossref"].Window.Start)
	service.sources = DemoSources(scenarioTime, true)
	incremental, err := service.RunOnce(context.Background(), scenarioTime.Add(72*time.Hour))
	require.NoError(t, err)
	require.Len(t, incremental.Records, 12)
	require.Len(t, incremental.Runs, 2)
	identities := map[string]int{}
	formalEvent := false
	for _, candidate := range incremental.Records {
		identities[candidate.Identity]++
		require.NotNil(t, candidate.Review)
		require.True(t, candidate.Review.IsMock)
		require.False(t, candidate.Review.ScientificReviewed)
		require.Equal(t, "pending", candidate.Review.InclusionStatus)
		require.True(t, candidate.Published)
		if candidate.Record.Source == "biorxiv" && candidate.Record.Version == "2" {
			require.Len(t, candidate.Record.PublicationEvents, 2)
			formalEvent = true
		}
	}
	require.True(t, formalEvent)
	require.Equal(t, 2, identities["doi:10.0000/glean-mock-biorxiv"])
	duplicate, err := service.RunOnce(context.Background(), scenarioTime.Add(72*time.Hour))
	require.NoError(t, err)
	require.Len(t, duplicate.Runs, 2)
	require.Len(t, duplicate.Records, 12)
	for _, checkpoint := range incremental.Sources {
		require.Equal(t, scenarioTime.Add(72*time.Hour), checkpoint.AcquiredThrough)
		require.Equal(t, checkpoint.AcquiredThrough, checkpoint.ReviewedThrough)
		require.Equal(t, checkpoint.AcquiredThrough, checkpoint.PublishedThrough)
	}
}

// TestCheckpointWaitsForAllPages 验证来源分页与三个模板全部完成前不推进采集水位。
func TestCheckpointWaitsForAllPages(t *testing.T) {
	service := offline(t, DemoSources(scenarioTime, false)[:1], nil, nil)
	service.config.MaxPagesPerCall = 1
	first, err := service.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	require.True(t, first.Sources["crossref"].AcquiredThrough.IsZero())
	require.Len(t, first.Records, 1)
	id := first.Runs[0].ID
	second, err := service.RunOnce(context.Background(), scenarioTime.Add(time.Hour))
	require.NoError(t, err)
	require.True(t, second.Sources["crossref"].AcquiredThrough.IsZero())
	require.Equal(t, id, second.Runs[0].ID)
	final, err := service.RunOnce(context.Background(), scenarioTime.Add(2*time.Hour))
	require.NoError(t, err)
	require.Equal(t, scenarioTime, final.Sources["crossref"].AcquiredThrough)
	require.Equal(t, scenarioTime, final.Runs[0].FixedEnd)
	require.Equal(t, "completed", final.Runs[0].Status)
}

// TestAnchorGapCatchupAndTenthBackfill 验证固定 UTC 锚点、漏跑补偿与第十轮九十日回补。
func TestAnchorGapCatchupAndTenthBackfill(t *testing.T) {
	service := offline(t, DemoSources(scenarioTime, false)[:1], nil, nil)
	first, err := service.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	require.Equal(t, time.Duration(0), first.Schedule.NextRunUTC.Sub(first.Schedule.AnchorUTC)%(72*time.Hour))
	delayed := scenarioTime.Add(24 * 24 * time.Hour)
	second, err := service.RunOnce(context.Background(), delayed)
	require.NoError(t, err)
	require.Equal(t, scenarioTime.Add(-7*24*time.Hour), second.Runs[1].Sources["crossref"].Window.Start)
	require.Equal(t, delayed, second.Runs[1].FixedEnd)
	state, err := service.load(context.Background())
	require.NoError(t, err)
	state.Schedule.CompletedRuns = 9
	require.NoError(t, service.save(context.Background(), state))
	tenthAt := delayed.Add(72 * time.Hour)
	tenth, err := service.RunOnce(context.Background(), tenthAt)
	require.NoError(t, err)
	run := tenth.Runs[len(tenth.Runs)-1]
	require.Equal(t, 10, run.Number)
	require.Equal(t, tenthAt.Add(-90*24*time.Hour), run.Sources["crossref"].Window.Start)
}

type countSource struct {
	fixture *FixtureSource
	calls   int
	cancel  context.CancelFunc
}

// Name 返回测试来源名。
func (s *countSource) Name() string { return s.fixture.Name() }

// IsMock 声明测试来源不访问网络。
func (s *countSource) IsMock() bool { return true }

// Fetch 统计分页调用，可在请求中模拟取消。
func (s *countSource) Fetch(ctx context.Context, request FetchRequest) (Page, error) {
	s.calls++
	if s.cancel != nil {
		s.cancel()
		return Page{}, ctx.Err()
	}
	return s.fixture.Fetch(ctx, request)
}

// Test429RetryAfterIsDurableAndBounded 验证 Retry-After、无立即重试与固定运行水位。
func Test429RetryAfterIsDurableAndBounded(t *testing.T) {
	source := &countSource{fixture: DemoSources(scenarioTime, false)[0].(*FixtureSource)}
	source.fixture.Failure = &SourceError{Kind: "http", StatusCode: 429, Retryable: true, RetryAfter: 2 * time.Hour}
	service := offline(t, []Source{source}, nil, nil)
	failed, err := service.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	require.Equal(t, 1, source.calls)
	require.Equal(t, scenarioTime.Add(2*time.Hour), failed.Sources[source.Name()].NextRetryAt)
	require.True(t, failed.Sources[source.Name()].AcquiredThrough.IsZero())
	waiting, err := service.RunOnce(context.Background(), scenarioTime.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, source.calls)
	require.Equal(t, failed.Runs[0].ID, waiting.Runs[0].ID)
	source.fixture.Failure = nil
	resumed, err := service.RunOnce(context.Background(), scenarioTime.Add(2*time.Hour))
	require.NoError(t, err)
	require.Equal(t, "completed", resumed.Runs[0].Status)
	require.Equal(t, scenarioTime, resumed.Sources[source.Name()].AcquiredThrough)
}

// TestFailureBudgetDoesNotLoseAcquisitionGap 验证重试预算耗尽后新运行仍从旧采集水位补齐。
func TestFailureBudgetDoesNotLoseAcquisitionGap(t *testing.T) {
	source := &countSource{fixture: DemoSources(scenarioTime, false)[0].(*FixtureSource)}
	source.fixture.Failure = &SourceError{Kind: "http", StatusCode: 503, Retryable: true}
	service := offline(t, []Source{source}, nil, nil)
	service.config.RetryBudget = 2
	_, err := service.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	failed, err := service.RunOnce(context.Background(), scenarioTime.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, "completed_with_errors", failed.Runs[0].Status)
	require.Equal(t, 2, source.calls)
	require.True(t, failed.Sources[source.Name()].AcquiredThrough.IsZero())
	source.fixture.Failure = nil
	recovered, err := service.RunOnce(context.Background(), scenarioTime.Add(72*time.Hour))
	require.NoError(t, err)
	require.Equal(t, "completed", recovered.Runs[1].Status)
	require.Len(t, recovered.Records, 1)
}

type flakyReviewer struct {
	failures int
	calls    int
}

// Review 为审核阶段注入有限故障。
func (r *flakyReviewer) Review(ctx context.Context, record Record) (Review, error) {
	r.calls++
	if r.failures > 0 {
		r.failures--
		return Review{}, errors.New("temporary review failure")
	}
	return MockReviewer{}.Review(ctx, record)
}

type flakyPublisher struct {
	failures int
	calls    int
	keys     map[string]bool
}

// Publish 为目录投影阶段注入有限故障并按键幂等记录。
func (p *flakyPublisher) Publish(ctx context.Context, _ idgen.ID, records []Publication) error {
	p.calls++
	if p.failures > 0 {
		p.failures--
		return errors.New("temporary publish failure")
	}
	if p.keys == nil {
		p.keys = map[string]bool{}
	}
	for _, record := range records {
		p.keys[record.Key] = true
	}
	return ctx.Err()
}

// TestReviewRetryDoesNotRefetch 验证审核失败单独退避，恢复后不重复来源采集。
func TestReviewRetryDoesNotRefetch(t *testing.T) {
	source := &countSource{fixture: DemoSources(scenarioTime, false)[0].(*FixtureSource)}
	reviewer := &flakyReviewer{failures: 1}
	service := offline(t, []Source{source}, reviewer, nil)
	failed, err := service.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	require.Equal(t, scenarioTime, failed.Sources[source.Name()].AcquiredThrough)
	require.True(t, failed.Sources[source.Name()].ReviewedThrough.IsZero())
	calls := source.calls
	recovered, err := service.RunOnce(context.Background(), scenarioTime.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, "completed", recovered.Runs[0].Status)
	require.Equal(t, calls, source.calls)
	require.Equal(t, 2, reviewer.calls)
}

// TestPublicationRetryIsSeparateAndIdempotent 验证目录投影失败不回退采集或审核水位。
func TestPublicationRetryIsSeparateAndIdempotent(t *testing.T) {
	reviewer := &flakyReviewer{}
	publisher := &flakyPublisher{failures: 1}
	service := offline(t, DemoSources(scenarioTime, false)[:1], reviewer, publisher)
	failed, err := service.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	require.Equal(t, scenarioTime, failed.Sources["crossref"].ReviewedThrough)
	require.True(t, failed.Sources["crossref"].PublishedThrough.IsZero())
	recovered, err := service.RunOnce(context.Background(), scenarioTime.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, "completed", recovered.Runs[0].Status)
	require.Equal(t, 1, reviewer.calls)
	require.Equal(t, 2, publisher.calls)
	require.Len(t, publisher.keys, 1)
}

// TestExhaustedReviewBacklogSurvivesOverlap 验证旧待审核记录不因下一轮窗口已移动而丢失。
func TestExhaustedReviewBacklogSurvivesOverlap(t *testing.T) {
	reviewer := &flakyReviewer{failures: 1}
	service := offline(t, DemoSources(scenarioTime, false)[:1], reviewer, nil)
	service.config.RetryBudget = 1
	failed, err := service.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	require.Equal(t, "completed_with_errors", failed.Runs[0].Status)
	// 记录早于 acquiredThrough-7d，下一轮不会重新发现，必须从持久化审核积压恢复。
	recovered, err := service.RunOnce(context.Background(), scenarioTime.Add(30*24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, "completed", recovered.Runs[1].Status)
	require.Equal(t, 2, reviewer.calls)
	for _, candidate := range recovered.Records {
		require.True(t, candidate.Published)
		require.NotNil(t, candidate.Review)
	}
}

// TestCanceledRunResumesSameIdentity 验证取消只保存阶段状态，不创建第二个运行或越过水位。
func TestCanceledRunResumesSameIdentity(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	source := &countSource{fixture: DemoSources(scenarioTime, false)[0].(*FixtureSource), cancel: cancel}
	service := offline(t, []Source{source}, nil, nil)
	canceled, err := service.RunOnce(ctx, scenarioTime)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, "canceled", canceled.Runs[0].Status)
	require.True(t, canceled.Sources[source.Name()].AcquiredThrough.IsZero())
	source.cancel = nil
	recovered, err := service.RunOnce(context.Background(), scenarioTime.Add(time.Minute))
	require.NoError(t, err)
	require.Len(t, recovered.Runs, 1)
	require.Equal(t, canceled.Runs[0].ID, recovered.Runs[0].ID)
	require.Equal(t, "completed", recovered.Runs[0].Status)
}

// TestSingleRunLeaseAndFencing 验证不同服务实例不能同时推进同一状态，旧租约不能覆盖新状态。
func TestSingleRunLeaseAndFencing(t *testing.T) {
	first := offline(t, DemoSources(scenarioTime, false)[:1], nil, nil)
	second := offline(t, DemoSources(scenarioTime, false)[:1], nil, nil)
	second.memory = first.memory
	clockNow := scenarioTime
	first.clock = func() time.Time { return clockNow }
	second.clock = first.clock
	old, err := first.acquire(context.Background())
	require.NoError(t, err)
	_, err = second.RunOnce(context.Background(), scenarioTime)
	require.ErrorContains(t, err, "already active")
	clockNow = clockNow.Add(10 * time.Minute)
	newOwner, err := second.acquire(context.Background())
	require.NoError(t, err)
	require.ErrorContains(t, first.save(context.Background(), old), "lease lost")
	second.release(newOwner)
	_, err = first.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
}

// TestRealFetchOptInAndMockBoundary 验证真实来源默认拒绝，mock 审核不能伪装真实结论。
func TestRealFetchOptInAndMockBoundary(t *testing.T) {
	ids, err := idgen.NewGenerator(8)
	require.NoError(t, err)
	_, err = NewOfflineService(ids, DefaultConfig(), []Source{NewCrossrefSource(nil)}, nil, nil)
	require.ErrorContains(t, err, "disabled")
	review, err := MockReviewer{}.Review(context.Background(), Record{})
	require.NoError(t, err)
	require.True(t, review.IsMock)
	require.False(t, review.ScientificReviewed)
	require.Equal(t, "pending", review.InclusionStatus)
	require.Error(t, MockPublisher{}.Publish(context.Background(), 1, []Publication{{Review: Review{ScientificReviewed: true, InclusionStatus: "included"}}}))
}

// TestISSNRegistryUsesChecksum 验证 ISSN 规范匹配、不按标题或无效校验位猜测。
func TestISSNRegistryUsesChecksum(t *testing.T) {
	normalized, err := NormalizeISSN("2041-1723")
	require.NoError(t, err)
	require.Equal(t, "2041-1723", normalized)
	_, err = NormalizeISSN("2041-1724")
	require.Error(t, err)
	registry := []JournalEntry{{Name: "Fixture journal", ISSNs: []string{"2041-1723"}, IsMock: true}}
	require.NotNil(t, MatchJournal([]string{"20411723"}, registry))
	require.Nil(t, MatchJournal([]string{"2041-1724"}, registry))
	templates := SearchTemplates()
	require.Len(t, templates, 3)
	require.True(t, templates[0].Policy.JIFStrict)
	require.Equal(t, float64(10), templates[0].Policy.JIFMinimum)
	require.Equal(t, []string{"A"}, templates[0].Policy.CCF)
}

// TestFailedBootstrapKeepsOriginalLowerBound 验证首次采集长期失败后仍补齐最初三十日起点。
func TestFailedBootstrapKeepsOriginalLowerBound(t *testing.T) {
	source := DemoSources(scenarioTime, false)[0].(*FixtureSource)
	source.Failure = &SourceError{Kind: "invalid_response", Message: "bad source"}
	service := offline(t, []Source{source}, nil, nil)
	failed, err := service.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	require.Equal(t, "completed_with_errors", failed.Runs[0].Status)
	source.Failure = nil
	recovered, err := service.RunOnce(context.Background(), scenarioTime.Add(90*24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, scenarioTime.Add(-30*24*time.Hour), recovered.Runs[1].Sources[source.Name()].Window.Start)
	require.Len(t, recovered.Records, 1)
}

// TestRawEvidenceAndTopicChangesAreNotLost 验证非映射原始证据变化保留新版本，新增主题重新审核投影。
func TestRawEvidenceAndTopicChangesAreNotLost(t *testing.T) {
	source := DemoSources(scenarioTime, false)[0].(*FixtureSource)
	source.Records[0].SourceUpdatedAt = scenarioTime.Add(-time.Hour)
	reviewer := &flakyReviewer{}
	publisher := &flakyPublisher{}
	service := offline(t, []Source{source}, reviewer, publisher)
	first, err := service.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	require.Len(t, first.Records, 1)
	source.Records[0].Topics = append(source.Records[0].Topics, "llm-agent")
	second, err := service.RunOnce(context.Background(), scenarioTime.Add(72*time.Hour))
	require.NoError(t, err)
	require.Len(t, second.Records, 1)
	require.Equal(t, 2, reviewer.calls)
	require.Equal(t, 2, publisher.calls)
	source.Records[0].Raw = []byte(`{"mock":true,"new_source_link":"10.0000/new-formal-link"}`)
	third, err := service.RunOnce(context.Background(), scenarioTime.Add(6*24*time.Hour))
	require.NoError(t, err)
	require.Len(t, third.Records, 2)
	record := source.Records[0]
	a, _ := recordIdentity(record)
	record.Raw = []byte(`{ "new_source_link": "10.0000/new-formal-link", "mock": true }`)
	b, _ := recordIdentity(record)
	require.Equal(t, a, b, "JSON property order must not invent revisions")
}

// TestNextWakeHonorsBackoff 验证 task 调用方获得持久化最早重试时刻，而非立即自旋。
func TestNextWakeHonorsBackoff(t *testing.T) {
	source := DemoSources(scenarioTime, false)[0].(*FixtureSource)
	source.Failure = &SourceError{Kind: "http", StatusCode: 429, Retryable: true, RetryAfter: time.Hour}
	service := offline(t, []Source{source}, nil, nil)
	service.clock = func() time.Time { return scenarioTime }
	_, err := service.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	next, err := service.NextWakeAt(context.Background())
	require.NoError(t, err)
	require.Equal(t, scenarioTime.Add(time.Hour), next)
}

type pacedSource struct{ countSource }

// IsMock 将测试来源标记为真实适配路径以验证默认开关和持久化限流，但仍不访问网络。
func (s *pacedSource) IsMock() bool { return false }

// TestLivePathPacesEveryRequestAndIsolatesState 验证显式开启后的来源请求逐次持久化节流，并与模拟状态隔离。
func TestLivePathPacesEveryRequestAndIsolatesState(t *testing.T) {
	source := &pacedSource{countSource: countSource{fixture: DemoSources(scenarioTime, false)[0].(*FixtureSource)}}
	ids, err := idgen.NewGenerator(8)
	require.NoError(t, err)
	cfg := DefaultConfig()
	cfg.RealFetchEnabled = true
	service, err := NewOfflineService(ids, cfg, []Source{source}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "glean-monitor-live-v1", service.config.StateKey)
	first, err := service.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	require.Equal(t, 1, source.calls)
	require.Equal(t, "retry_wait", first.Runs[0].Status)
	require.Equal(t, scenarioTime.Add(3*time.Second), first.Sources[source.Name()].NextRetryAt)
	_, err = service.RunOnce(context.Background(), scenarioTime.Add(time.Second))
	require.NoError(t, err)
	require.Equal(t, 1, source.calls)
	_, err = service.RunOnce(context.Background(), scenarioTime.Add(3*time.Second))
	require.NoError(t, err)
	require.Equal(t, 2, source.calls)
	final, err := service.RunOnce(context.Background(), scenarioTime.Add(6*time.Second))
	require.NoError(t, err)
	require.Equal(t, 3, source.calls)
	require.Equal(t, "completed", final.Runs[0].Status)
	require.Equal(t, "live", final.Runs[0].Mode)
	for _, candidate := range final.Records {
		require.True(t, candidate.Review.IsMock)
		require.False(t, candidate.Review.ScientificReviewed)
	}
}
