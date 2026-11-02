package monitor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/teamsillybees/glean/internal/platform/httpclient"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ModelUsage 保存实际 token 用量。
type ModelUsage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

// ModelCallMetadata 保存实际响应模型和请求摘要，不含凭据。
type ModelCallMetadata struct {
	Model     string      `json:"model"`
	Usage     *ModelUsage `json:"usage"`
	InputHash string      `json:"inputHash"`
}

// ModelClientConfig 是启动层注入的模型参数。
type ModelClientConfig struct {
	Model, Path string
	APIKey      string `json:"-"`
	Timeout     time.Duration
}

// JevQuestion 描述 System One 文本问题。
type JevQuestion struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

// JevAnswer 保留原始概率，指针区分零与缺失。
type JevAnswer struct {
	Type          string                     `json:"type"`
	Choice        string                     `json:"choice,omitempty"`
	Score         *float64                   `json:"score,omitempty"`
	Noul          *float64                   `json:"noul,omitempty"`
	Confidence    *float64                   `json:"confidence,omitempty"`
	Probabilities map[string]float64         `json:"probabilities,omitempty"`
	Legend        map[string]json.RawMessage `json:"legend,omitempty"`
}

// JevResult 是经过结构校验而非科学核验的响应。
type JevResult struct {
	Answers map[string]JevAnswer
	Call    ModelCallMetadata
}

// JevClient 复用平台Executor且不自动重试。
type JevClient struct {
	executor httpclient.Executor
	config   ModelClientConfig
}

// NewJevClient 构造时不联网。
func NewJevClient(e httpclient.Executor, c ModelClientConfig) *JevClient { return &JevClient{e, c} }

// Evaluate 请求一次结构化决策。
func (c *JevClient) Evaluate(ctx context.Context, state any, questions map[string]JevQuestion) (JevResult, error) {
	var r JevResult
	if c == nil {
		return r, &PipelineError{Kind: "awaiting_configuration"}
	}
	raw, hash, err := modelPOST(ctx, c.executor, c.config, map[string]any{"model": c.config.Model, "state": state, "questions": questions})
	if err != nil {
		return r, err
	}
	var v struct {
		Model   string               `json:"model"`
		Answers map[string]JevAnswer `json:"answers"`
		Usage   *struct {
			Input  *int `json:"input_tokens"`
			Output *int `json:"output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(raw, &v) != nil || v.Model == "" || v.Usage == nil || v.Usage.Input == nil || v.Usage.Output == nil || *v.Usage.Input < 0 || *v.Usage.Output < 0 || len(v.Answers) != len(questions) {
		return r, invalidModelResponse()
	}
	for k, q := range questions {
		a, ok := v.Answers[k]
		if !ok || !validJevAnswer(q, a) {
			return r, invalidModelResponse()
		}
	}
	r.Answers = v.Answers
	r.Call = ModelCallMetadata{v.Model, &ModelUsage{*v.Usage.Input, *v.Usage.Output}, hash}
	return r, nil
}
func probability(v *float64) bool {
	return v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) && *v >= 0 && *v <= 1
}
func validJevAnswer(q JevQuestion, a JevAnswer) bool {
	if a.Type != q.Type {
		return false
	}
	if q.Type == "noul" {
		return probability(a.Noul)
	}
	if !probability(a.Confidence) {
		return false
	}
	keys := map[string]bool{}
	switch q.Type {
	case "choice":
		v, ok := q.Criteria.(map[string]string)
		if !ok {
			return false
		}
		for k := range v {
			keys[k] = true
		}
		if !keys[a.Choice] {
			return false
		}
	case "score":
		v, ok := q.Criteria.([]string)
		if !ok || len(v) < 2 || len(v) > 10 || a.Score == nil || math.IsNaN(*a.Score) || *a.Score < 0 || *a.Score > float64(len(v)-1) {
			return false
		}
		for i := range v {
			keys[strconv.Itoa(i)] = true
		}
	default:
		return false
	}
	if len(keys) != len(a.Probabilities) {
		return false
	}
	sum, expected := 0.0, 0.0
	for k, p := range a.Probabilities {
		if !keys[k] || !probability(&p) {
			return false
		}
		sum += p
		if q.Type == "score" {
			i, _ := strconv.Atoi(k)
			expected += float64(i) * p
		} else if p > a.Probabilities[a.Choice]+0.0001 {
			return false
		}
	}
	return math.Abs(sum-1) < 0.001 && (q.Type != "score" || math.Abs(expected-*a.Score) < 0.01)
}

// GenerativeClient 调用可配置 OpenAI 兼容服务。
type GenerativeClient struct {
	executor httpclient.Executor
	config   ModelClientConfig
}

// NewGenerativeClient 构造时不联网。
func NewGenerativeClient(e httpclient.Executor, c ModelClientConfig) *GenerativeClient {
	return &GenerativeClient{e, c}
}

// Generate 严格解析单个完整 JSON 对象，拒绝截断、拒答和未知输出字段。
func (c *GenerativeClient) Generate(ctx context.Context, system string, input any, target any) (ModelCallMetadata, error) {
	var meta ModelCallMetadata
	if c == nil {
		return meta, &PipelineError{Kind: "awaiting_configuration"}
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return meta, invalidModelResponse()
	}
	raw, hash, err := modelPOST(ctx, c.executor, c.config, map[string]any{"model": c.config.Model, "stream": false, "messages": []map[string]string{{"role": "system", "content": system + "\n只返回一个 JSON 对象，不要添加 Markdown 围栏。"}, {"role": "user", "content": string(encoded)}}})
	if err != nil {
		return meta, err
	}
	var v struct {
		Model   string `json:"model"`
		Choices []struct {
			Finish  string `json:"finish_reason"`
			Message struct {
				Content string `json:"content"`
				Refusal string `json:"refusal"`
			} `json:"message"`
		} `json:"choices"`
		Usage *struct {
			Input  *int `json:"prompt_tokens"`
			Output *int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(raw, &v) != nil || len(v.Choices) != 1 || v.Choices[0].Finish != "stop" || v.Choices[0].Message.Refusal != "" {
		return meta, invalidModelResponse()
	}
	content := strings.TrimSpace(v.Choices[0].Message.Content)
	if !strings.HasPrefix(content, "{") || !uniqueModelJSON([]byte(content)) {
		return meta, invalidModelResponse()
	}
	d := json.NewDecoder(strings.NewReader(content))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil {
		return meta, invalidModelResponse()
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return meta, invalidModelResponse()
	}
	var usage *ModelUsage
	if v.Usage != nil && v.Usage.Input != nil && v.Usage.Output != nil {
		if *v.Usage.Input < 0 || *v.Usage.Output < 0 {
			return meta, invalidModelResponse()
		}
		usage = &ModelUsage{*v.Usage.Input, *v.Usage.Output}
	}
	if v.Model == "" {
		v.Model = "unreported"
	}
	return ModelCallMetadata{v.Model, usage, hash}, nil
}
func invalidModelResponse() error { return &PipelineError{Kind: "invalid_response"} }
func modelPOST(ctx context.Context, e httpclient.Executor, c ModelClientConfig, body any) ([]byte, string, error) {
	if e == nil || c.APIKey == "" || c.Model == "" || c.Path == "" {
		return nil, "", &PipelineError{Kind: "awaiting_configuration"}
	}
	raw, err := json.Marshal(body)
	if err != nil || len(raw) > 2<<20 {
		return nil, "", &PipelineError{Kind: "invalid_request"}
	}
	hash := sha256.Sum256(raw)
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	resp, err := e.Do(ctx, http.MethodPost, c.Path, httpclient.WithJSONBody(json.RawMessage(raw)), httpclient.WithHeader("Authorization", "Bearer "+c.APIKey), httpclient.WithTimeout(timeout))
	if err != nil || resp == nil || !resp.IsSuccess() {
		retry := !errors.Is(err, context.Canceled)
		kind := "transport"
		if errors.Is(err, context.DeadlineExceeded) {
			kind = "timeout"
		}
		status := 0
		var after time.Duration
		if resp != nil {
			status = resp.StatusCode
			if status == 429 || status == 529 {
				kind = "rate_limit"
			}
			retry = status == 408 || status == 429 || status >= 500
			if status == 401 || status == 403 {
				kind = "authentication_failed"
			}
			if n, e := strconv.Atoi(resp.Header.Get("Retry-After")); e == nil && n > 0 && n <= 86400 {
				after = time.Duration(n) * time.Second
			} else if when, e := http.ParseTime(resp.Header.Get("Retry-After")); e == nil {
				delay := time.Until(when)
				if delay > 0 && delay <= 24*time.Hour {
					after = delay
				}
			}
		}
		return nil, "", &PipelineError{Kind: kind, Retryable: retry, StatusCode: status, RetryAfter: after}
	}
	if len(resp.Body) > 2<<20 || !uniqueModelJSON(resp.Body) {
		return nil, "", invalidModelResponse()
	}
	return resp.Body, hex.EncodeToString(hash[:]), nil
}

// uniqueModelJSON 拒绝重复键和尾随JSON，避免同一响应被不同解析器解释为不同声明。
func uniqueModelJSON(raw []byte) bool {
	d := json.NewDecoder(strings.NewReader(string(raw)))
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 64 {
			return errors.New("depth")
		}
		token, err := d.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return errors.New("duplicate key")
				}
				seen[name] = true
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("delimiter")
		}
		_, err = d.Token()
		return err
	}
	if walk(0) != nil {
		return false
	}
	_, err := d.Token()
	return err == io.EOF
}
