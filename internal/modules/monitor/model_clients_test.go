package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/teamsillybees/glean/internal/platform/httpclient"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// modelFakeExecutor 不联网并保留请求供阶段隔离断言。
type modelFakeExecutor struct {
	replies []string
	status  int
	bodies  []string
	calls   int
}

func (f *modelFakeExecutor) Do(ctx context.Context, method, path string, opts ...httpclient.RequestOption) (*httpclient.Response, error) {
	o, err := httpclient.ApplyRequestOptions(opts...)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(o.Body)
	f.bodies = append(f.bodies, string(b))
	f.calls++
	status := f.status
	if status == 0 {
		status = 200
	}
	body := "{}"
	if len(f.replies) > 0 {
		body = f.replies[0]
		f.replies = f.replies[1:]
	}
	return &httpclient.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{"30"}}, Body: []byte(body)}, nil
}
func modelTestConfig() ModelClientConfig {
	return ModelClientConfig{Model: "fixture-model", Path: "/fixture", APIKey: "fixture-not-real"}
}
func generationResponse(content string) string {
	b, _ := json.Marshal(map[string]any{"model": "actual-version", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}}}, "usage": map[string]int{"prompt_tokens": 12, "completion_tokens": 8}})
	return string(b)
}
func TestModelGenerativeStrictResponses(t *testing.T) {
	for _, body := range []string{`{}`, `{"summary":"x","unexpected":true}`, `{"summary":"x"} {}`, `null`, `{"summary":"x"} trailing`} {
		t.Run(body, func(t *testing.T) {
			f := &modelFakeExecutor{replies: []string{generationResponse(body)}}
			var target struct {
				Summary string `json:"summary"`
			}
			_, err := NewGenerativeClient(f, modelTestConfig()).Generate(context.Background(), "system", Material{Text: "original"}, &target)
			if body == `{}` {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil {
				t.Fatal("accepted invalid output")
			}
		})
	}
}
func TestModelJevRejectsMissingAndOutOfRange(t *testing.T) {
	q := map[string]JevQuestion{"support": {Type: "noul", Instructions: "Supported?"}}
	for _, answer := range []string{`{"type":"noul"}`, `{"type":"noul","noul":1.2}`, `{"type":"score","noul":1}`} {
		f := &modelFakeExecutor{replies: []string{`{"model":"jev-version","answers":{"support":` + answer + `},"usage":{"input_tokens":1,"output_tokens":2}}`}}
		if _, err := NewJevClient(f, modelTestConfig()).Evaluate(context.Background(), "material", q); err == nil {
			t.Fatal("accepted invalid answer", answer)
		}
	}
}

// TestModelJevRoundingEnvelope 覆盖实服两位小数的独立舍入，仍拒绝超出可解释误差的分布。
func TestModelJevRoundingEnvelope(t *testing.T) {
	q := JevQuestion{Type: "score", Criteria: []string{"none", "possible", "clear", "core"}}
	score, confidence := 1.97, 0.42
	a := JevAnswer{Type: "score", Score: &score, Confidence: &confidence, Probabilities: map[string]float64{"0": 0.02, "1": 0.26, "2": 0.44, "3": 0.28}}
	if !validJevAnswer(q, a) {
		t.Fatal("independently rounded public response must be accepted")
	}
	score = 1.70
	if validJevAnswer(q, a) {
		t.Fatal("inconsistent score must still be rejected")
	}
	score = 1.97
	a.Probabilities["0"] = 0.1
	if validJevAnswer(q, a) {
		t.Fatal("invalid total probability must still be rejected")
	}
}
func TestModelHTTPErrorSafeAndNoRetry(t *testing.T) {
	for _, status := range []int{400, 401, 413, 422, 429, 529} {
		f := &modelFakeExecutor{status: status, replies: []string{`secret upstream response`}}
		_, _, err := modelPOST(context.Background(), f, modelTestConfig(), map[string]string{"state": "original"})
		var p *PipelineError
		if !errors.As(err, &p) || strings.Contains(err.Error(), "secret") || f.calls != 1 {
			t.Fatal("unsafe error or hidden retry", err)
		}
		if p.Retryable != (status == 429 || status == 529) {
			t.Fatal("wrong retry class", status, p)
		}
		if p.RetryAfter.Seconds() != 30 {
			t.Fatal("retry after lost")
		}
	}
}
func TestModelPipelineIndependentReviewReadsOriginal(t *testing.T) {
	f := &modelFakeExecutor{replies: []string{generationResponse(`{"summary":"中文摘要","claims":[{"text":"Result 4","quote":"Result 4","numbers":["4"],"citations":[]}],"issues":[]}`), generationResponse(`{"supported":true,"issues":[]}`)}}
	p := &ModelPipelineProcessor{config: ModelPipelineConfig{Enabled: true}, generative: NewGenerativeClient(f, modelTestConfig())}
	in := PipelineInput{Material: Material{Text: "ORIGINAL Result 4", Scope: "abstract"}, Previous: map[string]PipelineResult{}}
	draft, err := p.Process(context.Background(), "summary", in)
	if err != nil {
		t.Fatal(err)
	}
	in.Previous["summary"] = draft
	review, err := p.Process(context.Background(), "independent_review", in)
	if err != nil {
		t.Fatal(err)
	}
	if f.calls != 2 || !strings.Contains(f.bodies[1], "ORIGINAL") || review.Call.Model != "actual-version" || review.Call.Usage.InputTokens != 12 {
		t.Fatal("review did not independently receive original or preserve metadata")
	}
}
func TestModelPipelineUnconfiguredNeverCalls(t *testing.T) {
	p, _ := NewModelPipelineProcessor(nil, ModelPipelineConfig{})
	_, err := p.Process(context.Background(), "summary", PipelineInput{})
	var e *PipelineError
	if !errors.As(err, &e) || e.Kind != "awaiting_configuration" {
		t.Fatal(err)
	}
}
func TestModelEndpointRejectsCredentialURL(t *testing.T) {
	for _, endpoint := range []string{"https://secret@example.com/v1", "http://example.com/api", "https://example.com/api?key=secret"} {
		if (ModelPipelineConfig{Jev: ModelEndpointConfig{Endpoint: endpoint}}).Validate() == nil {
			t.Fatal("unsafe endpoint")
		}
	}
}

func TestModelDuplicateJSONKeysRejected(t *testing.T) {
	for _, raw := range []string{`{"a":1,"a":2}`, `{"a":{"b":1,"b":2}}`, `{} {}`, `{"a":1`} {
		if uniqueModelJSON([]byte(raw)) {
			t.Fatal("ambiguous JSON accepted", raw)
		}
	}
	if !uniqueModelJSON([]byte(`{"a":[1,{"b":true}]}`)) {
		t.Fatal("valid JSON rejected")
	}
}
func TestModelPipelineEvidenceEveryClaim(t *testing.T) {
	f := &modelFakeExecutor{replies: []string{`{"model":"jev-version","answers":{"claim_0":{"type":"noul","noul":0.95},"claim_1":{"type":"noul","noul":0.5}},"usage":{"input_tokens":30,"output_tokens":4}}`}}
	p := &ModelPipelineProcessor{config: ModelPipelineConfig{Enabled: true}, jev: NewJevClient(f, modelTestConfig())}
	r, err := p.Process(context.Background(), "evidence_check", PipelineInput{Material: Material{Text: "original"}, Previous: map[string]PipelineResult{"summary": {Claims: []PipelineClaim{{Text: "first", Quote: "one"}, {Text: "second", Quote: "two"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Supported == nil || *r.Supported || len(r.ClaimSupport) != 2 || len(r.Issues) != 1 || !strings.Contains(f.bodies[0], "original") {
		t.Fatal("claim evidence was not independently checked")
	}
}

func TestModelFactoryLocalHTTPAndRedirectBoundary(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer fixture-not-real" {
			t.Error("missing injected auth")
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/stolen", http.StatusTemporaryRedirect)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(generationResponse(`{"summary":"ok"}`)))
	}))
	defer server.Close()
	t.Setenv("GLEAN_TEST_MODEL_KEY", "fixture-not-real")
	for _, path := range []string{"/chat", "/redirect"} {
		cfg := ModelPipelineConfig{Enabled: true, Generative: ModelEndpointConfig{Endpoint: server.URL + path, Model: "fixture", APIKeyEnv: "GLEAN_TEST_MODEL_KEY"}}
		f, err := httpclient.NewFactory(httpclient.Config{Enabled: true, Services: cfg.ModelHTTPServices()}, nil, cfg.ModelHTTPFactoryOptions()...)
		if err != nil {
			t.Fatal(err)
		}
		p, err := NewModelPipelineProcessor(f, cfg)
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			Summary string `json:"summary"`
		}
		before := calls
		_, err = p.generative.Generate(context.Background(), "JSON", "fixture", &out)
		if path == "/chat" && (err != nil || out.Summary != "ok") {
			t.Fatal(err)
		}
		if path == "/redirect" && (err == nil || calls-before != 1) {
			t.Fatal("redirect escaped boundary", err, calls-before)
		}
		f.ClearAll()
	}
}
func TestModelGenerativeMissingUsageIsUnknown(t *testing.T) {
	f := &modelFakeExecutor{replies: []string{`{"choices":[{"finish_reason":"stop","message":{"content":"{\"summary\":\"ok\"}"}}]}`}}
	var out struct {
		Summary string `json:"summary"`
	}
	m, err := NewGenerativeClient(f, modelTestConfig()).Generate(context.Background(), "JSON", "fixture", &out)
	if err != nil || m.Usage != nil || m.Model != "unreported" {
		t.Fatal("missing metadata must stay unknown", m, err)
	}
}

func TestModelPipelineClassifiesTopicsSeparately(t *testing.T) {
	answers := map[string]any{}
	for i, category := range []string{"relevant", "out_of_scope"} {
		probabilities := map[string]float64{"relevant": 0, "out_of_scope": 0, "uncertain": 0}
		probabilities[category] = 1
		answers["topic_"+strconv.Itoa(i)+"_category"] = map[string]any{"type": "choice", "choice": category, "confidence": 1, "probabilities": probabilities}
		answers["topic_"+strconv.Itoa(i)+"_priority"] = map[string]any{"type": "score", "score": 3, "confidence": 1, "probabilities": map[string]float64{"0": 0, "1": 0, "2": 0, "3": 1}}
	}
	raw, _ := json.Marshal(map[string]any{"model": "jev-version", "answers": answers, "usage": map[string]int{"input_tokens": 1, "output_tokens": 2}})
	f := &modelFakeExecutor{replies: []string{string(raw)}}
	p := &ModelPipelineProcessor{config: ModelPipelineConfig{Enabled: true}, jev: NewJevClient(f, modelTestConfig())}
	r, err := p.Process(context.Background(), "classification", PipelineInput{Record: Record{Topics: []string{"one", "two"}}, Templates: []SearchTemplate{{Topic: "one"}, {Topic: "one", Core: true}, {Topic: "two"}, {Topic: "ignored"}}, Material: Material{Text: "source"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Topics) != 2 || !r.Topics[0].Relevant || r.Topics[1].Relevant || r.Topics[0].Score != 1 {
		t.Fatal("topic decisions or score normalization incorrect", r)
	}
}
func TestModelLegacyRuntimePathFailsClosed(t *testing.T) {
	r, err := (pipelineOnlyReviewer{}).Review(context.Background(), Record{})
	if err == nil || r.IsMock || r.Summary != "" {
		t.Fatal("legacy path produced successful mock", r, err)
	}
}

func TestModelEnableAndMaterialGates(t *testing.T) {
	f := &modelFakeExecutor{}
	p := &ModelPipelineProcessor{jev: NewJevClient(f, modelTestConfig()), generative: NewGenerativeClient(f, modelTestConfig())}
	if p.Ready("material") {
		t.Fatal("disabled configuration became ready")
	}
	_, err := p.Process(context.Background(), "summary", PipelineInput{})
	if err == nil || f.calls != 0 {
		t.Fatal("disabled processor called provider")
	}
	p.config.Enabled = true
	if !p.Ready("material") {
		t.Fatal("complete enabled configuration not ready")
	}
	p.generative.config.APIKey = ""
	if p.Ready("material") {
		t.Fatal("missing generative key allowed material HTTP")
	}
}
func TestModelAuthenticationFailureDoesNotResumeAsMissingConfig(t *testing.T) {
	for _, status := range []int{401, 403} {
		f := &modelFakeExecutor{status: status}
		_, _, err := modelPOST(context.Background(), f, modelTestConfig(), map[string]string{})
		var p *PipelineError
		if !errors.As(err, &p) || p.Kind != "authentication_failed" || p.Retryable {
			t.Fatal("authentication failure could resume indefinitely", err)
		}
	}
}
func TestModelStageTimeoutAndExplicitEnv(t *testing.T) {
	p := &ModelPipelineProcessor{config: ModelPipelineConfig{Jev: ModelEndpointConfig{Timeout: 90 * time.Second}}}
	if p.StageTimeout("classification") != 90*time.Second || p.StageTimeout("summary") != 60*time.Second {
		t.Fatal("timeout configuration lost")
	}
	if (ModelPipelineConfig{Jev: ModelEndpointConfig{Timeout: 121 * time.Second}}).Validate() == nil {
		t.Fatal("unbounded stage timeout")
	}
	t.Setenv("INITRA_MONITOR_MODELS_ENABLED", "true")
	t.Setenv("INITRA_MONITOR_MODELS_AUTO_DISPLAY", "true")
	cfg := ModelPipelineConfigFromEnv()
	if !cfg.Enabled || !cfg.AutoDisplay {
		t.Fatal("explicit CLI switches lost")
	}
}

func TestModelEnabledWithoutKeysDoesNotFetchMaterial(t *testing.T) {
	p, err := NewModelPipelineProcessor(nil, ModelPipelineConfig{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	s := offline(t, DemoSources(scenarioTime, false)[:1], nil, &flakyPublisher{})
	material := &materialCounter{}
	s.SetPipelineProcessor(p)
	s.SetMaterialProvider(material)
	snapshot, err := s.RunOnce(context.Background(), scenarioTime)
	if err != nil {
		t.Fatal(err)
	}
	if material.calls != 0 {
		t.Fatal("enabled but missing keys fetched material")
	}
	for _, c := range snapshot.Records {
		if !c.Published || c.Pipeline.Status != "awaiting_configuration" {
			t.Fatal("raw persistence was blocked or state incorrect")
		}
	}
}
