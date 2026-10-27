package monitor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

type mutableTemplates struct {
	items []SearchTemplate
	err   error
	calls int
}

func (p *mutableTemplates) Templates(context.Context) ([]SearchTemplate, error) {
	p.calls++
	return p.items, p.err
}

type recordingTopicSource struct {
	requests []FetchRequest
	failure  error
}

func (s *recordingTopicSource) Name() string { return "topic-fixture" }
func (s *recordingTopicSource) IsMock() bool { return true }
func (s *recordingTopicSource) Fetch(_ context.Context, r FetchRequest) (Page, error) {
	s.requests = append(s.requests, r)
	if s.failure != nil {
		return Page{}, s.failure
	}
	record := Record{SourceID: r.Template.Topic + "-" + r.Cursor, Title: "Fixture discovery", Version: "1"}
	if r.Cursor == "" {
		return Page{Records: []Record{record}, NextCursor: "next"}, nil
	}
	return Page{Records: []Record{record}, Complete: true}, nil
}

// TestDynamicTopicSnapshotResume 验证新增主题实际参与采集，编辑后旧游标、窗口、查询与版本均冻结。
func TestDynamicTopicSnapshotResume(t *testing.T) {
	ctx := context.Background()
	source := &recordingTopicSource{}
	s := offline(t, []Source{source}, nil, nil)
	s.clock = func() time.Time { return scenarioTime }
	p := &mutableTemplates{items: []SearchTemplate{TopicSearchTemplate("new-topic", [][]string{{"alpha", "beta"}, {"clinical"}}, 1)}}
	s.templates = p
	window := Window{Start: scenarioTime.Add(-time.Hour), End: scenarioTime}
	first, err := s.RunFinite(ctx, window, 1)
	require.NoError(t, err)
	old := first.Runs[0]
	require.Equal(t, "next", old.Sources[source.Name()].Cursor)
	require.Equal(t, `("alpha" OR "beta") AND ("clinical")`, source.requests[0].Template.Query)
	p.items[0].TermGroups[0][0] = "changed"
	p.items = append(p.items, TopicSearchTemplate("other-topic", [][]string{{"other"}}, 1))
	p.err = errors.New("provider unavailable during resume")
	done, err := s.RunFinite(ctx, window, 10)
	require.NoError(t, err)
	require.Equal(t, "completed", done.Runs[0].Status)
	require.Equal(t, 1, p.calls)
	require.Equal(t, old.TemplateVersion, done.Runs[0].TemplateVersion)
	require.Equal(t, old.RequestKey, done.Runs[0].RequestKey)
	require.Len(t, source.requests, 2)
	require.Equal(t, "next", source.requests[1].Cursor)
	require.Equal(t, source.requests[0].Template, source.requests[1].Template)
	require.Equal(t, window, source.requests[1].Window)
	require.Len(t, done.Records, 2)
	for _, v := range done.Records {
		require.Equal(t, []string{"new-topic"}, v.Record.Topics)
		require.Equal(t, "pending", v.Review.InclusionStatus)
		require.False(t, v.Review.ScientificReviewed)
	}
	_, err = s.RunFinite(ctx, window, 10)
	require.NoError(t, err)
	require.Len(t, source.requests, 2)
	p.err = nil
	s.clock = func() time.Time { return scenarioTime.Add(72 * time.Hour) }
	next, err := s.RunOnce(ctx, s.clock())
	require.NoError(t, err)
	require.Len(t, next.Runs, 2)
	require.NotEqual(t, old.TemplateVersion, next.Runs[1].TemplateVersion)
	require.Contains(t, source.requests[2].Template.Query, "changed")
	require.Equal(t, "other-topic", source.requests[4].Template.Topic)
}

// TestDynamicTopicRetryKeepsSnapshot 验证失败重试不读取被改动的主题配置，也不重放已完成的分页。
func TestDynamicTopicRetryKeepsSnapshot(t *testing.T) {
	ctx := context.Background()
	source := &recordingTopicSource{}
	s := offline(t, []Source{source}, nil, nil)
	s.clock = func() time.Time { return scenarioTime }
	s.config.RetryBudget = 1
	p := &mutableTemplates{items: []SearchTemplate{TopicSearchTemplate("new-topic", [][]string{{"original"}}, 1)}}
	s.templates = p
	window := Window{Start: scenarioTime.Add(-time.Hour), End: scenarioTime}
	_, err := s.RunFinite(ctx, window, 1)
	require.NoError(t, err)
	source.failure = &SourceError{Kind: "http", StatusCode: 503, Retryable: true}
	failed, err := s.RunFinite(ctx, window, 1)
	require.NoError(t, err)
	old := failed.Runs[0]
	require.Equal(t, "completed_with_errors", old.Status)
	p.items = nil
	p.err = errors.New("no provider")
	source.failure = nil
	s.clock = func() time.Time { return scenarioTime.Add(time.Hour) }
	result, err := s.RetryFailedFinite(ctx, old.ID, 10)
	require.NoError(t, err)
	require.Len(t, result.Runs, 2)
	child := result.Runs[1]
	require.Equal(t, old.SearchTemplates, child.SearchTemplates)
	require.Equal(t, old.RequestKey, child.RequestKey)
	require.Equal(t, old.FixedEnd, child.FixedEnd)
	require.Len(t, source.requests, 3)
	require.Equal(t, "next", source.requests[2].Cursor)
	require.Equal(t, source.requests[0].Template, source.requests[2].Template)
	require.Len(t, result.Records, 2)
	require.Equal(t, 1, p.calls)
	_, err = s.RetryFailedFinite(ctx, old.ID, 10)
	require.NoError(t, err)
	require.Len(t, source.requests, 3)
}

// TestTemplateProviderFailsClosed 验证缺失配置、损坏快照和无法证明版本的旧运行不会触发采集。
func TestTemplateProviderFailsClosed(t *testing.T) {
	for _, p := range []TemplateProvider{nil, &mutableTemplates{err: errors.New("unavailable")}, &mutableTemplates{}, &mutableTemplates{items: []SearchTemplate{TopicSearchTemplate("bad slug", [][]string{{"term"}}, 1)}}, &mutableTemplates{items: []SearchTemplate{TopicSearchTemplate("empty-group", [][]string{{}}, 1)}}} {
		source := &recordingTopicSource{}
		s := offline(t, []Source{source}, nil, nil)
		s.templates = p
		_, err := s.RunOnce(context.Background(), scenarioTime)
		require.Error(t, err)
		require.Empty(t, source.requests)
	}
	_, err := runTemplates(&Run{TemplateVersion: "obsolete"})
	require.Error(t, err)
	legacy, err := runTemplates(&Run{TemplateVersion: TemplateVersion})
	require.NoError(t, err)
	require.Equal(t, SearchTemplates(), legacy)
	snapshot, version, err := freezeTemplates([]SearchTemplate{TopicSearchTemplate("new-topic", [][]string{{"term"}}, 1)})
	require.NoError(t, err)
	snapshot[0].TermGroups[0][0] = "tampered"
	_, err = runTemplates(&Run{TemplateVersion: version, SearchTemplates: snapshot})
	require.Error(t, err)
}

// TestCustomTopicDoesNotInventCoreEligibility 验证自定义检索不会自动获得核心期刊资格或空指标结论。
func TestCustomTopicDoesNotInventCoreEligibility(t *testing.T) {
	templates := append(SearchTemplates(), TopicSearchTemplate("custom", [][]string{{"new"}}, 1))
	execution := executionTemplates(templates, true)
	require.Len(t, execution, 7)
	require.False(t, execution[3].Core)
	require.Equal(t, InclusionPolicy{}, execution[3].Policy)
	_, byTopic := compileRegistry([]JournalEntry{{ISSNs: []string{"0028-0836"}, DiscoveryTopics: []string{"custom"}}}, templates)
	require.Equal(t, []string{"0028-0836"}, byTopic["custom"])
}

// TestRuntimeRequiresTemplateProvider 验证生产装配缺失目录提供者时不退回固定主题。
func TestRuntimeRequiresTemplateProvider(t *testing.T) {
	_, err := NewRuntimeService(nil, nil, nil, RuntimeOptions{}, nil, nil)
	require.ErrorContains(t, err, "topic template provider")
}

// TestCustomTopicJournalScanDoesNotRequireInventedRegistry 自定义主题的普通检索无需伪造核心注册表，成功水位正常推进。
func TestCustomTopicJournalScanDoesNotRequireInventedRegistry(t *testing.T) {
	source := &coreCoverageSource{}
	ids, err := idgen.NewGenerator(47)
	require.NoError(t, err)
	s, err := NewOfflineService(ids, RuntimeConfig(), []Source{source}, MockReviewer{}, &flakyPublisher{})
	require.NoError(t, err)
	s.templates = &mutableTemplates{items: []SearchTemplate{TopicSearchTemplate("custom", [][]string{{"clinical"}}, 1)}}
	s.clock = func() time.Time { return scenarioTime }
	result, err := s.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	require.Len(t, source.requests, 1)
	require.False(t, source.requests[0].Template.Core)
	require.Equal(t, "completed", result.Runs[0].Status)
	require.Equal(t, scenarioTime, result.Sources["crossref"].TopicAcquiredThrough["custom"])
}
