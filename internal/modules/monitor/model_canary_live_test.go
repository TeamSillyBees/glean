//go:build live

package monitor

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/httpclient"
)

// canaryModelExecutor 仅输出公开测试响应的结构与数值诊断；不记录请求头、请求正文或完整响应。
type canaryModelExecutor struct {
	httpclient.Executor
	t *testing.T
}

func (e canaryModelExecutor) Do(ctx context.Context, method, path string, options ...httpclient.RequestOption) (*httpclient.Response, error) {
	r, err := e.Executor.Do(ctx, method, path, options...)
	if r != nil && r.IsSuccess() {
		var v struct {
			Model   string               `json:"model"`
			Answers map[string]JevAnswer `json:"answers"`
			Usage   json.RawMessage      `json:"usage"`
		}
		if json.Unmarshal(r.Body, &v) == nil {
			safe, _ := json.Marshal(v)
			e.t.Logf("Jev response structure: %s", safe)
		}
	}
	return r, err
}

// TestLiveModelPipelineCanary 最多四次付费模型调用，只使用现场取得的公开摘要；密钥仅从进程环境读取。
func TestLiveModelPipelineCanary(t *testing.T) {
	if os.Getenv("GLEAN_LIVE_MODEL_CANARY") != "1" {
		t.Skip("explicit live model opt-in required")
	}
	cfg := ModelPipelineConfigFromEnv()
	if cfg.Generative.Model != "gpt-6.1-terra" {
		t.Fatal("this canary requires the explicitly selected gpt-6.1-terra model")
	}
	if os.Getenv(cfg.Jev.APIKeyEnv) == "" || os.Getenv(cfg.Generative.APIKeyEnv) == "" {
		t.Fatal("both process-only model keys are required")
	}
	cfg.Budget = ModelBudgetConfig{MaxCalls: 4, MaxReservedTokens: 120000, MaxInputBytes: 20000, MaxOutputTokens: 4096}
	sourceFactory, err := httpclient.NewFactory(httpclient.Config{Enabled: true, Services: SourceHTTPServices("")}, nil, SourceHTTPFactoryOptions()...)
	require.NoError(t, err)
	defer sourceFactory.ClearAll()
	sources, err := NewHTTPSourceSet(sourceFactory, []string{"europepmc"})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	page, err := sources[0].Fetch(ctx, FetchRequest{PageSize: 1, Window: Window{Start: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)}, Template: SearchTemplate{ID: "model-canary", Topic: "llm-agent", Query: `"large language model"`}})
	require.NoError(t, err, "official public metadata fetch failed; no model calls made")
	require.NotEmpty(t, page.Records)
	record := page.Records[0]
	require.NotEmpty(t, record.Abstract)
	record.Topics = []string{"llm-agent"}
	material := materialFromRecord(record)
	require.Equal(t, "abstract_only", material.Scope)
	factory, err := httpclient.NewFactory(httpclient.Config{Enabled: true, Services: cfg.ModelHTTPServices()}, nil, cfg.ModelHTTPFactoryOptions()...)
	require.NoError(t, err)
	defer factory.ClearAll()
	p, err := NewModelPipelineProcessor(factory, cfg)
	require.NoError(t, err)
	p.jev.executor = canaryModelExecutor{Executor: p.jev.executor, t: t}
	service := offline(t, DemoSources(scenarioTime, false), nil, nil)
	service.SetPipelineProcessor(p)
	input := PipelineInput{Record: record, Material: material, Templates: []SearchTemplate{TopicSearchTemplate("llm-agent", [][]string{{"large language model"}, {"knowledge engineering", "knowledge graph"}}, 1)}, Previous: map[string]PipelineResult{}}
	t.Logf("public source=europepmc source_id=%s reading_scope=%s material_sha256=%s; no fulltext claim", record.SourceID, material.Scope, material.SHA256)
	for _, stage := range []string{"classification", "summary", "independent_review", "evidence_check"} {
		result, err := p.Process(ctx, stage, input)
		if err != nil {
			t.Fatalf("live stage=%s failed safely: %v; no retry or model substitution", stage, err)
		}
		require.NoError(t, validateStageResult(stage, result))
		require.NotNil(t, result.Call)
		require.NotEmpty(t, result.Call.InputHash)
		require.NotNil(t, result.Call.Usage)
		input.Previous[stage] = result
		t.Logf("stage=%s actual_model=%s input_tokens=%d output_tokens=%d claims=%d; output retained only for this test", stage, result.Call.Model, result.Call.Usage.InputTokens, result.Call.Usage.OutputTokens, len(result.Claims))
	}
	checked := validatePipeline(input)
	require.NotNil(t, checked.Supported)
	t.Logf("deterministic_evidence_supported=%v issues=%d; model judgments are not scientific verification", *checked.Supported, len(checked.Issues))
	usage, err := service.modelBudgetUsage(ctx)
	require.NoError(t, err)
	require.Equal(t, 4, usage.Calls)
}
