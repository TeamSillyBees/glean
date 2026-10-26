package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// TestRealPageProjectsBeforeCheckpointCompletion 验证真实页立即进入真实发布接口，只有 LLM 是模拟，来源水位必须等待全部分页。
func TestRealPageProjectsBeforeCheckpointCompletion(t *testing.T) {
	source := &pacedSource{countSource: countSource{fixture: DemoSources(scenarioTime, false)[0].(*FixtureSource)}}
	publisher := &flakyPublisher{}
	ids, err := idgen.NewGenerator(43)
	require.NoError(t, err)
	cfg := RuntimeConfig()
	cfg.CompactPublished = false
	service, err := NewOfflineService(ids, cfg, []Source{source}, MockReviewer{}, publisher)
	require.NoError(t, err)
	service.clock = func() time.Time { return scenarioTime }
	first, err := service.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	require.Equal(t, 1, first.Runs[0].Requests)
	require.Equal(t, 1, publisher.calls)
	require.Len(t, publisher.keys, 1)
	require.True(t, first.Sources[source.Name()].AcquiredThrough.IsZero())
	require.True(t, first.Sources[source.Name()].PublishedThrough.IsZero())
	for _, candidate := range first.Records {
		require.False(t, candidate.Record.IsMock)
		require.True(t, candidate.Review.IsMock)
		require.False(t, candidate.Review.ScientificReviewed)
		require.True(t, candidate.Published)
	}
	require.Equal(t, "live", first.Schedule.AcquisitionMode)
	require.Equal(t, "mock", first.Schedule.ReviewMode)
}

// TestRealFiniteBudgetNeverPretendsComplete 验证显式预算限制实际上游尝试，保留未完成游标和固定水位。
func TestRealFiniteBudgetNeverPretendsComplete(t *testing.T) {
	source := &pacedSource{countSource: countSource{fixture: DemoSources(scenarioTime, false)[0].(*FixtureSource)}}
	ids, err := idgen.NewGenerator(44)
	require.NoError(t, err)
	service, err := NewOfflineService(ids, RuntimeConfig(), []Source{source}, MockReviewer{}, &flakyPublisher{})
	require.NoError(t, err)
	service.clock = func() time.Time { return scenarioTime }
	window := Window{Start: scenarioTime.Add(-30 * 24 * time.Hour), End: scenarioTime}
	snapshot, err := service.RunFinite(context.Background(), window, 1)
	require.NoError(t, err)
	require.Equal(t, 1, source.calls)
	require.Equal(t, 1, snapshot.Runs[0].Requests)
	require.True(t, snapshot.Runs[0].FinishedAt.IsZero())
	require.True(t, snapshot.Sources[source.Name()].AcquiredThrough.IsZero())
	require.Equal(t, window, snapshot.Runs[0].Sources[source.Name()].Window)
	_, err = service.RunFinite(context.Background(), Window{Start: window.Start, End: window.End.Add(time.Hour)}, 1)
	require.Error(t, err)
	require.Equal(t, 1, source.calls)
}

// TestManualWindowCannotJumpAcquisitionGap 验证任意手工窗口不能越过未覆盖历史或回退已有采集水位。
func TestManualWindowCannotJumpAcquisitionGap(t *testing.T) {
	service := offline(t, DemoSources(scenarioTime, false)[:1], nil, nil)
	service.clock = func() time.Time { return scenarioTime.Add(20 * 24 * time.Hour) }
	_, err := service.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	window := Window{Start: scenarioTime.Add(10 * 24 * time.Hour), End: scenarioTime.Add(11 * 24 * time.Hour)}
	scanned, err := service.RunWindow(context.Background(), window)
	require.NoError(t, err)
	require.Equal(t, "completed", scanned.Runs[1].Status)
	require.Equal(t, scenarioTime, scanned.Sources["crossref"].AcquiredThrough)
	require.Equal(t, scenarioTime, scanned.Sources["crossref"].TopicAcquiredThrough["biomed-ai"])
	replay, err := service.RunWindow(context.Background(), window)
	require.NoError(t, err)
	require.Len(t, replay.Runs, 2)
}

// Test403StaysBlockedWithoutAllowedRetry 验证无 Retry-After 的拒绝不会被调度器下一轮自动再次请求。
func Test403StaysBlockedWithoutAllowedRetry(t *testing.T) {
	source := &countSource{fixture: DemoSources(scenarioTime, false)[0].(*FixtureSource)}
	source.fixture.Failure = &SourceError{Kind: "http", StatusCode: 403, Retryable: false}
	service := offline(t, []Source{source}, nil, nil)
	first, err := service.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	require.True(t, first.Sources[source.Name()].Blocked)
	require.Equal(t, 1, source.calls)
	_, err = service.RunOnce(context.Background(), scenarioTime.Add(72*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, source.calls)
}

// TestRetentionBoundsHistoryAndKeepsBacklog 验证长期目录投影后只留有界审核缓存，未投影积压和正在运行的状态绝不清除。
func TestRetentionBoundsHistoryAndKeepsBacklog(t *testing.T) {
	service := offline(t, DemoSources(scenarioTime, false)[:1], nil, nil)
	service.config.CompactPublished = true
	service.config.PublishedCacheLimit = 2
	service.config.CompletedRunLimit = 2
	state := service.emptyState()
	for i := 0; i < 8; i++ {
		key := idgen.ID(i + 1).String()
		state.Records[key] = &Candidate{ID: idgen.ID(i + 1), Key: key, Record: Record{Source: "crossref", Title: "full title", Abstract: "large abstract", Raw: json.RawMessage(`{"large":"source evidence"}`), AcquiredAt: scenarioTime.Add(time.Duration(i) * time.Hour)}, Review: &Review{IsMock: true}, Published: true}
		state.Runs = append(state.Runs, Run{ID: idgen.ID(i + 1), Number: i + 1, FinishedAt: scenarioTime, Sources: map[string]*SourceProgress{"crossref": {CandidateKeys: []string{key}}}})
	}
	state.Records["pending"] = &Candidate{Key: "pending", Record: Record{Title: "keep raw", Raw: json.RawMessage(`{"pending":true}`)}}
	state.Runs = append(state.Runs, Run{ID: 99, Sources: map[string]*SourceProgress{"crossref": {CandidateKeys: []string{"pending", "8"}}}})
	service.compact(state)
	require.Len(t, state.Records, 3)
	require.Len(t, state.Runs, 3)
	require.Equal(t, idgen.ID(99), state.Runs[2].ID)
	require.Equal(t, []string{"pending"}, state.Runs[2].Sources["crossref"].CandidateKeys)
	require.NotEmpty(t, state.Records["pending"].Record.Raw)
	for key, candidate := range state.Records {
		if key != "pending" {
			require.True(t, candidate.Compacted)
			require.Empty(t, candidate.Record.Raw)
			require.Empty(t, candidate.Record.Abstract)
		}
	}
}

// TestCompactedOverlapReusesReviewAndRehydratesNewTopic 验证缓存命中不重复审核，新增主题仍用刚采集的完整证据重新投影。
func TestCompactedOverlapReusesReviewAndRehydratesNewTopic(t *testing.T) {
	source := DemoSources(scenarioTime, false)[0].(*FixtureSource)
	source.Records[0].SourceUpdatedAt = scenarioTime.Add(-time.Hour)
	reviewer := &flakyReviewer{}
	publisher := &flakyPublisher{}
	service := offline(t, []Source{source}, reviewer, publisher)
	service.config.CompactPublished = true
	service.config.PublishedCacheLimit = 2
	first, err := service.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	for _, candidate := range first.Records {
		require.True(t, candidate.Compacted)
	}
	require.Equal(t, 1, reviewer.calls)
	_, err = service.RunOnce(context.Background(), scenarioTime.Add(72*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, reviewer.calls)
	source.Records[0].Topics = append(source.Records[0].Topics, "llm-agent")
	third, err := service.RunOnce(context.Background(), scenarioTime.Add(6*24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 2, reviewer.calls)
	require.Equal(t, 2, publisher.calls)
	require.Len(t, third.Records, 1)
}

type coreCoverageSource struct {
	requests []FetchRequest
	name     string
}

func (s *coreCoverageSource) Name() string {
	if s.name != "" {
		return s.name
	}
	return "crossref"
}
func (s *coreCoverageSource) Fetch(_ context.Context, request FetchRequest) (Page, error) {
	s.requests = append(s.requests, request)
	return Page{Complete: true}, nil
}

type coreCoverageRegistry struct{}

func (coreCoverageRegistry) CoreJournals(context.Context) ([]JournalEntry, error) {
	return []JournalEntry{{ISSNs: []string{"0028-0836"}}}, nil
}

// TestCoreBackfillSupplementsRatherThanReplacesDiscovery 验证第十次核心回补不会漏掉注册表之外的普通增量发现。
func TestCoreBackfillSupplementsRatherThanReplacesDiscovery(t *testing.T) {
	source := &coreCoverageSource{}
	ids, err := idgen.NewGenerator(45)
	require.NoError(t, err)
	service, err := NewOfflineService(ids, RuntimeConfig(), []Source{source}, MockReviewer{}, &flakyPublisher{})
	require.NoError(t, err)
	service.journals = coreCoverageRegistry{}
	state := service.emptyState()
	state.Schedule.CompletedRuns = 9
	state.Sources["crossref"].AcquiredThrough = scenarioTime.Add(-72 * time.Hour)
	require.NoError(t, service.save(context.Background(), state))
	var snapshot Snapshot
	for i := 0; i < 6; i++ {
		now := scenarioTime.Add(time.Duration(i) * 4 * time.Second)
		service.clock = func() time.Time { return now }
		snapshot, err = service.RunOnce(context.Background(), now)
		require.NoError(t, err)
	}
	require.Len(t, source.requests, 6)
	for i, request := range source.requests {
		require.Equal(t, i >= 3, request.Template.Core)
		require.Equal(t, scenarioTime, request.Window.End)
		if i < 3 {
			require.Equal(t, scenarioTime.Add(-10*24*time.Hour), request.Window.Start)
		} else {
			require.Equal(t, scenarioTime.Add(-90*24*time.Hour), request.Window.Start)
		}
	}
	require.Equal(t, "completed", snapshot.Runs[0].Status)
	require.Equal(t, scenarioTime, snapshot.Sources["crossref"].AcquiredThrough)
}

// TestRetryAfterSurvivesRetryBudgetExhaustion 验证终止本轮尝试不取消上游明确的跨周期等待期限。
func TestRetryAfterSurvivesRetryBudgetExhaustion(t *testing.T) {
	source := &countSource{fixture: DemoSources(scenarioTime, false)[0].(*FixtureSource)}
	source.fixture.Failure = &SourceError{Kind: "http", StatusCode: 429, Retryable: true, RetryAfter: 7 * 24 * time.Hour}
	service := offline(t, []Source{source}, nil, nil)
	service.config.RetryBudget = 1
	first, err := service.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	require.Equal(t, "completed_with_errors", first.Runs[0].Status)
	require.Equal(t, scenarioTime.Add(7*24*time.Hour), first.Sources["crossref"].NextRetryAt)
	_, err = service.RunOnce(context.Background(), scenarioTime.Add(72*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, source.calls, "the next scheduled run must not bypass the earlier Retry-After")
}

// TestCoreDiscoveryIsPendingProvenanceNotSourceVersion 验证核心发现说明不伪造新版本，也不由 mock 给出主题或科学纳入批准。
func TestCoreDiscoveryIsPendingProvenanceNotSourceVersion(t *testing.T) {
	record := Record{Source: "crossref", SourceID: "fixture", Title: "Fixture", Raw: json.RawMessage(`{"fixture":true}`)}
	first, _ := recordIdentity(record)
	record.CoreDiscovery = true
	second, _ := recordIdentity(record)
	require.Equal(t, first, second)
	review, err := (MockReviewer{}).Review(context.Background(), record)
	require.NoError(t, err)
	require.Contains(t, review.Summary, "主题相关性")
	require.Contains(t, review.Summary, "待真实审核")
	require.True(t, review.IsMock)
	require.False(t, review.ScientificReviewed)
	require.Equal(t, "pending", review.InclusionStatus)
}

// TestEveryNormalJournalRunIncludesCoreFeed 验证日常三天扫描包含完整期刊源，不等待第十次才发现无关键词论文。
func TestEveryNormalJournalRunIncludesCoreFeed(t *testing.T) {
	for _, name := range []string{"crossref", "pubmed", "europepmc"} {
		t.Run(name, func(t *testing.T) {
			source := &coreCoverageSource{name: name}
			ids, err := idgen.NewGenerator(46)
			require.NoError(t, err)
			service, err := NewOfflineService(ids, RuntimeConfig(), []Source{source}, MockReviewer{}, &flakyPublisher{})
			require.NoError(t, err)
			service.journals = coreCoverageRegistry{}
			state := service.emptyState()
			state.Sources[name].AcquiredThrough = scenarioTime.Add(-72 * time.Hour)
			require.NoError(t, service.save(context.Background(), state))
			var snapshot Snapshot
			for i := 0; i < 6; i++ {
				now := scenarioTime.Add(time.Duration(i) * 4 * time.Second)
				service.clock = func() time.Time { return now }
				snapshot, err = service.RunOnce(context.Background(), now)
				require.NoError(t, err)
			}
			require.Len(t, source.requests, 6)
			for i, request := range source.requests {
				require.Equal(t, i >= 3, request.Template.Core)
				require.Equal(t, scenarioTime.Add(-10*24*time.Hour), request.Window.Start)
				require.Equal(t, scenarioTime, request.Window.End)
			}
			require.Equal(t, "completed", snapshot.Runs[0].Status)
			require.Equal(t, scenarioTime, snapshot.Sources[name].AcquiredThrough)
		})
	}
}

// TestNonJournalSourcesDoNotRepeatISSNCoreQueries 验证预印本来源没有重复的 ISSN 核心阶段，第十次仍可扩大主题回补。
func TestNonJournalSourcesDoNotRepeatISSNCoreQueries(t *testing.T) {
	for _, name := range []string{"arxiv", "biorxiv", "medrxiv"} {
		for _, completed := range []int{0, 9} {
			t.Run(fmt.Sprintf("%s-run-%d", name, completed+1), func(t *testing.T) {
				source := &coreCoverageSource{name: name}
				ids, err := idgen.NewGenerator(47)
				require.NoError(t, err)
				service, err := NewOfflineService(ids, RuntimeConfig(), []Source{source}, MockReviewer{}, &flakyPublisher{})
				require.NoError(t, err)
				state := service.emptyState()
				state.Schedule.CompletedRuns = completed
				state.Sources[name].AcquiredThrough = scenarioTime.Add(-72 * time.Hour)
				require.NoError(t, service.save(context.Background(), state))
				for i := 0; i < 3; i++ {
					now := scenarioTime.Add(time.Duration(i) * 4 * time.Second)
					service.clock = func() time.Time { return now }
					_, err = service.RunOnce(context.Background(), now)
					require.NoError(t, err)
				}
				require.Len(t, source.requests, 3)
				for _, request := range source.requests {
					require.False(t, request.Template.Core)
					days := 10
					if completed == 9 {
						days = 90
					}
					require.Equal(t, scenarioTime.Add(-time.Duration(days)*24*time.Hour), request.Window.Start)
				}
			})
		}
	}
}

// TestMissingRegistryDoesNotSuppressOrdinaryDiscovery 验证登记表缺失时普通查询先完成，核心缺口明确失败且不假报来源完整覆盖。
func TestMissingRegistryDoesNotSuppressOrdinaryDiscovery(t *testing.T) {
	source := &coreCoverageSource{}
	ids, err := idgen.NewGenerator(48)
	require.NoError(t, err)
	service, err := NewOfflineService(ids, RuntimeConfig(), []Source{source}, MockReviewer{}, &flakyPublisher{})
	require.NoError(t, err)
	var snapshot Snapshot
	for i := 0; i < 4; i++ {
		now := scenarioTime.Add(time.Duration(i) * 4 * time.Second)
		service.clock = func() time.Time { return now }
		snapshot, err = service.RunOnce(context.Background(), now)
		require.NoError(t, err)
	}
	require.Len(t, source.requests, 3)
	for _, request := range source.requests {
		require.False(t, request.Template.Core)
	}
	require.Equal(t, "missing_registry", snapshot.Sources["crossref"].LastError)
	require.True(t, snapshot.Sources["crossref"].AcquiredThrough.IsZero())
	require.Equal(t, "completed_with_errors", snapshot.Runs[0].Status)
	require.True(t, snapshot.Sources["crossref"].TopicAcquiredThrough["biomed-ai"].IsZero(), "ordinary queries alone do not claim full topic/core coverage")
}

type topicRegistryFixture struct{}

func (topicRegistryFixture) CoreJournals(context.Context) ([]JournalEntry, error) {
	return []JournalEntry{
		{ISSNs: []string{"0028-0836"}, DiscoveryTopics: []string{"biomed-ai", "llm-agent", "biomed-worldmodel"}},
		{ISSNs: []string{"0950-7051"}, DiscoveryTopics: []string{"llm-agent"}, QualificationVerified: false},
	}, nil
}

// TestCoreRegistryUsesExplicitTopicDiscoveryRouting 验证未知指标年份的明确历史发现目标仍可用，且不会流入未经声明的主题。
func TestCoreRegistryUsesExplicitTopicDiscoveryRouting(t *testing.T) {
	source := &coreCoverageSource{}
	ids, err := idgen.NewGenerator(49)
	require.NoError(t, err)
	service, err := NewOfflineService(ids, RuntimeConfig(), []Source{source}, MockReviewer{}, &flakyPublisher{})
	require.NoError(t, err)
	service.journals = topicRegistryFixture{}
	for i := 0; i < 6; i++ {
		now := scenarioTime.Add(time.Duration(i) * 4 * time.Second)
		service.clock = func() time.Time { return now }
		_, err = service.RunOnce(context.Background(), now)
		require.NoError(t, err)
	}
	require.Len(t, source.requests, 6)
	for _, request := range source.requests[3:] {
		require.Contains(t, request.RegistryISSNs, "0028-0836")
		if request.Template.Topic == "llm-agent" {
			require.Contains(t, request.RegistryISSNs, "0950-7051")
		} else {
			require.NotContains(t, request.RegistryISSNs, "0950-7051")
		}
	}
}

type keywordFreeCoreSource struct{}

func (keywordFreeCoreSource) Name() string { return "crossref" }
func (keywordFreeCoreSource) Fetch(_ context.Context, request FetchRequest) (Page, error) {
	page := Page{Complete: true}
	if request.Template.Core {
		page.Records = []Record{{SourceID: "10.0000/core-keyword-free", DOI: "10.0000/core-keyword-free", Title: "Homeostasis mechanisms in tissue", SourceUpdatedAt: scenarioTime.Add(-time.Hour), Raw: json.RawMessage(`{"fixture":"keyword-free-core"}`)}}
	}
	return page, nil
}

// TestNormalCoreRetainsKeywordFreePaperAsPending 验证普通三天周期直接保留无检索关键词的核心文章，但绝不声明主题相关或科学纳入已确认。
func TestNormalCoreRetainsKeywordFreePaperAsPending(t *testing.T) {
	ids, err := idgen.NewGenerator(50)
	require.NoError(t, err)
	publisher := &flakyPublisher{}
	service, err := NewOfflineService(ids, RuntimeConfig(), []Source{keywordFreeCoreSource{}}, MockReviewer{}, publisher)
	require.NoError(t, err)
	service.journals = coreCoverageRegistry{}
	var snapshot Snapshot
	for i := 0; i < 6; i++ {
		now := scenarioTime.Add(time.Duration(i) * 4 * time.Second)
		service.clock = func() time.Time { return now }
		snapshot, err = service.RunOnce(context.Background(), now)
		require.NoError(t, err)
	}
	require.Equal(t, 1, snapshot.Runs[0].Number)
	require.Equal(t, "completed", snapshot.Runs[0].Status)
	require.Len(t, snapshot.Records, 1)
	require.Len(t, publisher.keys, 1)
	for _, candidate := range snapshot.Records {
		require.True(t, candidate.Record.CoreDiscovery)
		require.False(t, candidate.Record.IsMock)
		require.True(t, candidate.Review.IsMock)
		require.False(t, candidate.Review.ScientificReviewed)
		require.Equal(t, "pending", candidate.Review.InclusionStatus)
		require.Contains(t, candidate.Review.Summary, "主题相关性")
		require.True(t, candidate.Published)
	}
}

// TestExplicitWindowDoesNotConsumePendingWideCore 验证有限手工窗口不会把未执行的九十日核心回补误标完成。
func TestExplicitWindowDoesNotConsumePendingWideCore(t *testing.T) {
	source := &coreCoverageSource{}
	ids, err := idgen.NewGenerator(51)
	require.NoError(t, err)
	service, err := NewOfflineService(ids, RuntimeConfig(), []Source{source}, MockReviewer{}, &flakyPublisher{})
	require.NoError(t, err)
	service.journals = coreCoverageRegistry{}
	state := service.emptyState()
	state.Schedule.CompletedRuns = 9
	state.Sources["crossref"].AcquiredThrough = scenarioTime.Add(-72 * time.Hour)
	require.NoError(t, service.save(context.Background(), state))
	window := Window{Start: scenarioTime.Add(-2 * 24 * time.Hour), End: scenarioTime}
	var snapshot Snapshot
	for i := 0; i < 6; i++ {
		now := scenarioTime.Add(time.Duration(i) * 4 * time.Second)
		service.clock = func() time.Time { return now }
		snapshot, err = service.RunWindow(context.Background(), window)
		require.NoError(t, err)
	}
	require.Equal(t, "completed", snapshot.Runs[0].Status)
	require.True(t, snapshot.Sources["crossref"].CoreBackfillPending)
	require.False(t, snapshot.Runs[0].Sources["crossref"].CoreWide)
	for _, request := range source.requests {
		require.Equal(t, window, request.Window)
	}
	next := scenarioTime.Add(72 * time.Hour)
	service.clock = func() time.Time { return next }
	snapshot, err = service.RunOnce(context.Background(), next)
	require.NoError(t, err)
	require.True(t, snapshot.Runs[1].Sources["crossref"].CoreWide)
	require.Equal(t, next.Add(-90*24*time.Hour), snapshot.Runs[1].Sources["crossref"].Window.Start)
}

// TestExplicitEmptyDiscoveryScopeIsNotBroadened 验证显式空主题范围不会因兼容旧夹具的默认规则扩成全部主题。
func TestExplicitEmptyDiscoveryScopeIsNotBroadened(t *testing.T) {
	union, byTopic := compileRegistry([]JournalEntry{{ISSNs: []string{"0028-0836"}, DiscoveryTopics: []string{}}})
	require.Empty(t, union)
	for _, issns := range byTopic {
		require.Empty(t, issns)
	}
}
