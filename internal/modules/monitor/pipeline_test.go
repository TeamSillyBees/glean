package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
)

// fixturePipeline 是本地结构化fixture，绝不调用外部模型。
type fixturePipeline struct {
	calls   []string
	missing bool
	fail    string
	ready   bool
}

func (p *fixturePipeline) Metadata(stage string) StageMetadata {
	return StageMetadata{Provider: "fixture", Model: "fixture-v1", PromptVersion: stage + "-v1"}
}
func (p *fixturePipeline) Ready(string) bool { return !p.missing || p.ready }
func (p *fixturePipeline) Process(_ context.Context, stage string, in PipelineInput) (PipelineResult, error) {
	p.calls = append(p.calls, stage)
	if p.missing && !p.ready {
		return PipelineResult{}, &PipelineError{Kind: "awaiting_configuration"}
	}
	if p.fail == stage {
		return PipelineResult{}, &PipelineError{Kind: "transport", Retryable: true}
	}
	yes := true
	switch stage {
	case "classification":
		result := PipelineResult{}
		for _, topic := range in.Record.Topics {
			result.Topics = append(result.Topics, TopicDecision{Topic: topic, Relevant: true, Category: "candidate", Score: .8})
		}
		return result, nil
	case "summary":
		return PipelineResult{Summary: "来源材料中的研究描述。", Claims: []PipelineClaim{{Text: "来源研究", Quote: in.Material.Blocks[0].Text, EvidenceBlockID: in.Material.Blocks[0].ID}}}, nil
	default:
		return PipelineResult{Supported: &yes}, nil
	}
}
func TestPipelineRawPublishedBeforeMissingConfiguration(t *testing.T) {
	p := &fixturePipeline{missing: true}
	s := offline(t, DemoSources(scenarioTime, false)[:1], nil, &flakyPublisher{})
	s.SetPipelineProcessor(p)
	s.clock = func() time.Time { return scenarioTime }
	snapshot, err := s.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	require.Equal(t, "completed", snapshot.Runs[0].Status)
	for _, c := range snapshot.Records {
		require.True(t, c.Published)
		require.NotNil(t, c.Pipeline)
		require.Equal(t, "awaiting_configuration", c.Pipeline.Status)
		require.Equal(t, 0, c.Pipeline.Stages[0].Attempts)
		require.False(t, c.Review.IsMock)
		require.False(t, c.Review.ScientificReviewed)
	}
	require.Empty(t, snapshot.Sources["crossref"].LastError)
	require.Equal(t, scenarioTime, snapshot.Sources["crossref"].AcquiredThrough)
	require.True(t, snapshot.Sources["crossref"].ReviewedThrough.IsZero())
	p.ready = true
	snapshot, err = s.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	for _, c := range snapshot.Records {
		require.Equal(t, "needs_human_review", c.Pipeline.Status)
		require.False(t, c.Pipeline.ScientificReviewed)
		require.True(t, c.PipelineProjected)
		require.Equal(t, 6, len(c.Pipeline.Stages))
	}
}
func TestPipelineStageResumeDoesNotRepeatSuccessfulCalls(t *testing.T) {
	p := &fixturePipeline{fail: "summary"}
	s := offline(t, DemoSources(scenarioTime, false)[:1], nil, &flakyPublisher{})
	s.SetPipelineProcessor(p)
	now := scenarioTime
	s.clock = func() time.Time { return now }
	first, err := s.RunOnce(context.Background(), now)
	require.NoError(t, err)
	for _, c := range first.Records {
		require.Equal(t, "retry_wait", c.Pipeline.Status)
		require.Equal(t, "completed", c.Pipeline.Stages[1].Status)
	}
	before := len(p.calls)
	_, err = s.RunOnce(context.Background(), now)
	require.NoError(t, err)
	require.Len(t, p.calls, before)
	p.fail = ""
	now = now.Add(time.Hour)
	final, err := s.RunOnce(context.Background(), now)
	require.NoError(t, err)
	for _, c := range final.Records {
		require.Equal(t, "needs_human_review", c.Pipeline.Status)
		require.Equal(t, 1, c.Pipeline.Stages[1].Attempts)
		require.Equal(t, 2, c.Pipeline.Stages[2].Attempts)
		require.NotEmpty(t, c.Pipeline.Stages[2].InputHash)
	}
}
func TestProgramValidationRejectsInventedNumbersBlocksAndCitations(t *testing.T) {
	material := Material{Text: "Trial involved 12 people.", Blocks: []MaterialBlock{{ID: "abstract", Text: "Trial involved 12 people."}}}
	input := PipelineInput{Material: material, Previous: map[string]PipelineResult{"summary": {Summary: "Trial involved 99 people.", Claims: []PipelineClaim{{Text: "99 people", Quote: "Trial involved 12 people.", EvidenceBlockID: "missing", Citations: []string{"PMID:missing"}}}}}}
	r := validatePipeline(input)
	require.False(t, *r.Supported)
	require.GreaterOrEqual(t, len(r.Issues), 4)
}
func TestPipelineDoesNotUpgradeLegacyCandidates(t *testing.T) {
	s := offline(t, DemoSources(scenarioTime, false)[:1], nil, &flakyPublisher{})
	_, err := s.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	p := &fixturePipeline{}
	s.SetPipelineProcessor(p)
	_, err = s.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	require.Empty(t, p.calls)
}
func TestPipelineMaterialRetryArchivesAndRequiresManager(t *testing.T) {
	s := offline(t, DemoSources(scenarioTime, false)[:1], nil, &flakyPublisher{})
	s.SetPipelineProcessor(&fixturePipeline{})
	snapshot, err := s.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	for _, c := range snapshot.Records {
		_, err = s.RetryPipelineStage(context.Background(), c.ID, "material")
		require.Error(t, err)
		ctx := platformauth.WithPrincipal(context.Background(), platformauth.Principal{UserID: 7, IsSuperAdmin: true})
		p, err := s.RetryPipelineStage(ctx, c.ID, "material")
		require.NoError(t, err)
		require.Equal(t, "pending", p.Stages[0].Status)
		state, err := s.load(ctx)
		require.NoError(t, err)
		require.Len(t, state.Records[c.Key].PipelineHistory, 1)
		b, err := json.Marshal(p)
		require.NoError(t, err)
		require.NotContains(t, string(b), "ScientificReviewed\":true")
		break
	}
}

// materialCounter 证明未配置模型时不会偷偷获取额外全文。
type materialCounter struct{ calls int }

func (p *materialCounter) Material(context.Context, Record) (Material, error) {
	p.calls++
	return Material{}, nil
}
func TestMissingKeysDoNotFetchMaterial(t *testing.T) {
	s := offline(t, DemoSources(scenarioTime, false)[:1], nil, &flakyPublisher{})
	model := &fixturePipeline{missing: true}
	material := &materialCounter{}
	s.SetPipelineProcessor(model)
	s.SetMaterialProvider(material)
	snapshot, err := s.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	require.Zero(t, material.calls)
	require.Empty(t, model.calls)
	for _, c := range snapshot.Records {
		require.True(t, c.Published)
		require.Equal(t, "awaiting_configuration", c.Pipeline.Stages[0].Status)
	}
}

func TestUnconfiguredRealProcessorDoesNotFetchMaterial(t *testing.T) {
	// 使用真实生产processor而非fake Ready，证明空配置不会触发任何材料请求。
	processor, err := NewModelPipelineProcessor(nil, ModelPipelineConfig{})
	require.NoError(t, err)
	s := offline(t, DemoSources(scenarioTime, false)[:1], nil, &flakyPublisher{})
	material := &materialCounter{}
	s.SetPipelineProcessor(processor)
	s.SetMaterialProvider(material)
	snapshot, err := s.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	require.Zero(t, material.calls)
	for _, c := range snapshot.Records {
		require.True(t, c.Published)
		require.Equal(t, "awaiting_configuration", c.Pipeline.Stages[0].Status)
		require.Zero(t, c.Pipeline.Stages[0].Attempts)
	}
}

func TestPipelineDeadlineDoesNotStartUnaffordableCall(t *testing.T) {
	s := offline(t, DemoSources(scenarioTime, false)[:1], nil, &flakyPublisher{})
	p := &fixturePipeline{}
	s.SetPipelineProcessor(p)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	snapshot, err := s.RunOnce(ctx, scenarioTime)
	require.NoError(t, err)
	require.Empty(t, p.calls)
	for _, c := range snapshot.Records {
		require.True(t, c.Published)
		require.Equal(t, "pending", c.Pipeline.Stages[0].Status)
		require.Zero(t, c.Pipeline.Stages[0].Attempts)
	}
}
func TestPipelineCrashRunningStageDoesNotBlindlyRepeatPaidCall(t *testing.T) {
	s := offline(t, DemoSources(scenarioTime, false)[:1], nil, &flakyPublisher{})
	p := &fixturePipeline{missing: true}
	s.SetPipelineProcessor(p)
	_, err := s.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	state, err := s.load(context.Background())
	require.NoError(t, err)
	for _, c := range state.Records {
		c.Pipeline.Stages[0].Status = "running"
	}
	require.NoError(t, s.save(context.Background(), state))
	p.ready = true
	snapshot, err := s.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	require.Empty(t, p.calls)
	for _, c := range snapshot.Records {
		require.Equal(t, "failed", c.Pipeline.Status)
		require.Equal(t, "interrupted_execution", c.Pipeline.Stages[0].Error)
	}
}
func TestPipelineOldFailurePreventsNewerReviewedWatermark(t *testing.T) {
	s := offline(t, DemoSources(scenarioTime, false)[:1], nil, &flakyPublisher{})
	p := &fixturePipeline{missing: true}
	s.SetPipelineProcessor(p)
	first, err := s.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	state, err := s.load(context.Background())
	require.NoError(t, err)
	for _, c := range state.Records {
		c.Pipeline.Stages[0].Status = "failed"
		c.Pipeline.Status = "failed"
	}
	require.NoError(t, s.save(context.Background(), state))
	p.ready = true
	s.sources = DemoSources(scenarioTime, true)[:1]
	next, err := s.RunOnce(context.Background(), scenarioTime.Add(72*time.Hour))
	require.NoError(t, err)
	require.Equal(t, first.Sources["crossref"].ReviewedThrough, next.Sources["crossref"].ReviewedThrough)
	require.True(t, next.Sources["crossref"].AcquiredThrough.After(first.Sources["crossref"].AcquiredThrough))
}

// archiveFixture 模拟目录的耐久归档而非热缓存，JSON复制验证不依赖共享指针。
type archiveFixture struct{ entries map[string]Publication }

func (a *archiveFixture) Publish(_ context.Context, _ idgen.ID, items []Publication) error {
	if a.entries == nil {
		a.entries = map[string]Publication{}
	}
	for _, item := range items {
		b, _ := json.Marshal(item)
		var copied Publication
		_ = json.Unmarshal(b, &copied)
		a.entries[item.Key] = copied
	}
	return nil
}
func (a *archiveFixture) LoadPipeline(_ context.Context, id idgen.ID) (Publication, error) {
	for _, p := range a.entries {
		var v PaperPipeline
		_ = json.Unmarshal(p.Review.Pipeline, &v)
		if v.CandidateID == id {
			return p, nil
		}
	}
	return Publication{}, errors.New("missing archive")
}
func (a *archiveFixture) FindPipeline(_ context.Context, _ Record, key string) (Publication, bool, error) {
	v, ok := a.entries[key]
	return v, ok, nil
}
func TestPipelineEvictionArchiveReadRetryAndReobserve(t *testing.T) {
	archive := &archiveFixture{}
	p := &fixturePipeline{}
	s := offline(t, DemoSources(scenarioTime, true)[:1], nil, archive)
	s.SetPipelineProcessor(p)
	s.config.CompactPublished = true
	s.config.PublishedCacheLimit = 1
	// 初轮有三个来源版本，有限预算需要继续几个片段。
	for i := 0; i < 5; i++ {
		_, err := s.RunOnce(context.Background(), scenarioTime.Add(72*time.Hour))
		require.NoError(t, err)
	}
	snapshot, err := s.Snapshot(context.Background())
	require.NoError(t, err)
	require.LessOrEqual(t, len(snapshot.Records), 1)
	require.NotEmpty(t, archive.entries)
	var evicted PaperPipeline
	for _, publication := range archive.entries {
		var candidate PaperPipeline
		require.NoError(t, json.Unmarshal(publication.Review.Pipeline, &candidate))
		if snapshot.Records[candidate.Key] == nil {
			evicted = candidate
			break
		}
	}
	require.NotZero(t, evicted.CandidateID)
	response, err := NewHandler(s).getPipeline(context.Background(), &pipelineGetRequest{ID: evicted.CandidateID})
	require.NoError(t, err)
	require.NotNil(t, response)
	calls := len(p.calls)
	_, err = s.RunOnce(context.Background(), scenarioTime.Add(144*time.Hour))
	require.NoError(t, err)
	require.Len(t, p.calls, calls, "重复来源版本应从目录归档复用，不得再次付费")
	ctx := platformauth.WithPrincipal(context.Background(), platformauth.Principal{UserID: 7, IsSuperAdmin: true})
	restored, err := s.RetryPipelineStage(ctx, evicted.CandidateID, "material")
	require.NoError(t, err)
	require.Equal(t, "pending", restored.Stages[0].Status)
	state, err := s.load(ctx)
	require.NoError(t, err)
	require.Len(t, state.Records[evicted.Key].PipelineHistory, 1)
	require.NotEmpty(t, state.Records[evicted.Key].Record.Raw)
}

func TestPendingPipelineHotStateDropsDuplicateEvidence(t *testing.T) {
	archive := &archiveFixture{}
	s := offline(t, DemoSources(scenarioTime, false)[:1], nil, archive)
	p := &fixturePipeline{missing: true}
	s.SetPipelineProcessor(p)
	s.config.CompactPublished = true
	snapshot, err := s.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	for _, c := range snapshot.Records {
		require.True(t, c.PipelineArchived)
		require.Empty(t, c.Record.Raw)
		require.Empty(t, c.Pipeline.Material.Text)
		require.Empty(t, c.Pipeline.Material.Blocks)
		require.Equal(t, "awaiting_configuration", c.Pipeline.Status)
	}
	p.ready = true
	final, err := s.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	for _, c := range final.Records {
		require.Equal(t, "needs_human_review", c.Pipeline.Status)
		restored, err := s.archivedPipeline(context.Background(), c.ID)
		require.NoError(t, err)
		require.NotEmpty(t, restored.Pipeline.Material.Text)
		require.NotEmpty(t, restored.Record.Raw)
	}
}

// maxTimeoutPipeline 不睡眠，验证最大配置也可被有界任务预算准入。
type maxTimeoutPipeline struct{ fixturePipeline }

func (*maxTimeoutPipeline) StageTimeout(string) time.Duration { return 120 * time.Second }
func TestMaximumStageTimeoutFitsScheduledBudget(t *testing.T) {
	s := offline(t, DemoSources(scenarioTime, false)[:1], nil, &flakyPublisher{})
	p := &maxTimeoutPipeline{}
	s.SetPipelineProcessor(p)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	snapshot, err := s.RunOnce(ctx, scenarioTime)
	require.NoError(t, err)
	require.NotEmpty(t, p.calls)
	for _, c := range snapshot.Records {
		require.Equal(t, "needs_human_review", c.Pipeline.Status)
	}
}

// projectionFailure 只在模型结果投影时故障，原始来源入库保持成功。
type projectionFailure struct{ calls int }

func (p *projectionFailure) Publish(context.Context, idgen.ID, []Publication) error {
	p.calls++
	if p.calls == 2 {
		return errors.New("private database details must not leak")
	}
	return nil
}
func TestPipelineProjectionFailureIsVisibleAndIndependent(t *testing.T) {
	publisher := &projectionFailure{}
	p := &fixturePipeline{}
	s := offline(t, DemoSources(scenarioTime, false)[:1], nil, publisher)
	s.SetPipelineProcessor(p)
	snapshot, err := s.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	require.Equal(t, "completed", snapshot.Runs[0].Status)
	require.Empty(t, snapshot.Sources["crossref"].LastError)
	for _, c := range snapshot.Records {
		require.True(t, c.Published)
		require.False(t, c.PipelineProjected)
		require.NotEmpty(t, c.Pipeline.ProjectionError)
		require.NotContains(t, c.Pipeline.ProjectionError, "private")
	}
	calls := len(p.calls)
	next, err := s.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	require.Len(t, p.calls, calls)
	for _, c := range next.Records {
		require.True(t, c.PipelineProjected)
		require.Empty(t, c.Pipeline.ProjectionError)
	}
}
func TestModelReadyWakesDespiteSourceBackoff(t *testing.T) {
	p := &fixturePipeline{missing: true}
	s := offline(t, DemoSources(scenarioTime, false)[:1], nil, &flakyPublisher{})
	s.SetPipelineProcessor(p)
	s.clock = func() time.Time { return scenarioTime }
	_, err := s.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	state, err := s.load(context.Background())
	require.NoError(t, err)
	state.Runs[0].FinishedAt = time.Time{}
	state.Runs[0].Sources["crossref"].Handled = false
	state.Runs[0].Sources["crossref"].Published = false
	state.Sources["crossref"].NextRetryAt = scenarioTime.Add(24 * time.Hour)
	require.NoError(t, s.save(context.Background(), state))
	p.ready = true
	next, err := s.NextWakeAt(context.Background())
	require.NoError(t, err)
	require.Equal(t, scenarioTime, next)
}

func TestCompletedWindowStillResumesModelStages(t *testing.T) {
	p := &fixturePipeline{missing: true}
	s := offline(t, DemoSources(scenarioTime, false)[:1], nil, &flakyPublisher{})
	s.SetPipelineProcessor(p)
	window := Window{Start: scenarioTime.Add(-48 * time.Hour), End: scenarioTime}
	first, err := s.RunFinite(context.Background(), window, 100)
	require.NoError(t, err)
	require.Equal(t, "completed", first.Runs[0].Status)
	p.ready = true
	next, err := s.RunFinite(context.Background(), window, 100)
	require.NoError(t, err)
	require.Equal(t, first.Runs[0].Requests, next.Runs[0].Requests)
	for _, c := range next.Records {
		require.Equal(t, "needs_human_review", c.Pipeline.Status)
	}
}
