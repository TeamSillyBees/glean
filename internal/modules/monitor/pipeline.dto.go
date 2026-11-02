package monitor

import (
	"context"
	"encoding/json"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"time"
)

// PipelineVersion 标识阶段协议版本，模型结果不代表人工科学结论。
const PipelineVersion = "paper-pipeline-v1"

// Material 保存实际供模型阅读的材料和范围；默认只有题录/摘要，不假称全文。
type Material struct {
	Scope     string          `json:"scope"`
	Text      string          `json:"text"`
	SourceURL string          `json:"sourceUrl"`
	SHA256    string          `json:"sha256"`
	Blocks    []MaterialBlock `json:"blocks,omitempty"`
	Reason    string          `json:"reason,omitempty"`
	Retryable bool            `json:"retryable"`
	RawSHA256 string          `json:"rawSha256,omitempty"`
	Truncated bool            `json:"truncated"`
	License   string          `json:"license,omitempty"`
}

// MaterialBlock 保存模型可引用的稳定材料块及逐块原文哈希。
type MaterialBlock struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	SourceURL string `json:"sourceUrl"`
	SHA256    string `json:"sha256"`
}

// PipelineClaim 保存模型声明及其原文证据，供独立复审和确定性校验。
type PipelineClaim struct {
	EvidenceBlockID string   `json:"evidenceBlockId"`
	Text            string   `json:"text"`
	Quote           string   `json:"quote"`
	Numbers         []string `json:"numbers"`
	Citations       []string `json:"citations"`
}

// PipelineResult 是严格结构化模型输出；Jev仅填写分类、评分和证据判定，不生成总结。
type PipelineResult struct {
	Topics       []TopicDecision    `json:"topics,omitempty"`
	Call         *ModelCallMetadata `json:"call,omitempty"`
	ClaimSupport []float64          `json:"claimSupport,omitempty"`
	Relevant     *bool              `json:"relevant,omitempty"`
	Category     string             `json:"category,omitempty"`
	Score        *float64           `json:"score,omitempty"`
	Confidence   *float64           `json:"confidence,omitempty"`
	Summary      string             `json:"summary,omitempty"`
	Claims       []PipelineClaim    `json:"claims,omitempty"`
	Supported    *bool              `json:"supported,omitempty"`
	Issues       []string           `json:"issues,omitempty"`
}

// PipelineStage 持久化单阶段的可恢复状态和可追溯版本。
type PipelineStage struct {
	Name          string          `json:"name"`
	Status        string          `json:"status"`
	Attempts      int             `json:"attempts"`
	InputHash     string          `json:"inputHash"`
	Provider      string          `json:"provider"`
	Model         string          `json:"model"`
	PromptVersion string          `json:"promptVersion"`
	RuleVersion   string          `json:"ruleVersion"`
	StartedAt     time.Time       `json:"startedAt"`
	FinishedAt    time.Time       `json:"finishedAt"`
	NextRetryAt   time.Time       `json:"nextRetryAt"`
	Error         string          `json:"error,omitempty"`
	Result        *PipelineResult `json:"result,omitempty"`
}

// PaperPipeline 持久化完整处理档案；discovery topics仍只是召回范围。
type PaperPipeline struct {
	TemplateVersion    string           `json:"templateVersion"`
	Key                string           `json:"key"`
	RunID              idgen.ID         `json:"runId,omitempty"`
	Templates          []SearchTemplate `json:"templates,omitempty"`
	History            []PaperPipeline  `json:"history,omitempty"`
	ProjectionError    string           `json:"projectionError,omitempty"`
	CandidateID        idgen.ID         `json:"candidateId"`
	Version            string           `json:"version"`
	Status             string           `json:"status"`
	Material           Material         `json:"material"`
	Stages             []PipelineStage  `json:"stages"`
	ScientificReviewed bool             `json:"scientificReviewed"`
}

// PipelineInput 只包含真实公开来源材料及已完成阶段输出，不传凭据或任意私有文件。
type PipelineInput struct {
	Templates []SearchTemplate          `json:"templates"`
	Record    Record                    `json:"record"`
	Material  Material                  `json:"material"`
	Previous  map[string]PipelineResult `json:"previous"`
}

// StageMetadata 记录实际模型与提示版本。
type StageMetadata struct{ Provider, Model, PromptVersion string }

// PipelineProcessor 执行单一模型阶段；程序校验由编排器独立完成。
type PipelineProcessor interface {
	Metadata(stage string) StageMetadata
	Process(context.Context, string, PipelineInput) (PipelineResult, error)
}

// PipelineError 提供允许展示的错误分类，不保留上游响应正文。
type PipelineError struct {
	Kind       string
	Retryable  bool
	StatusCode int
	RetryAfter time.Duration
}

// Error 返回不含凭据或上游正文的安全分类。
func (e *PipelineError) Error() string { return e.Kind }

func pipelineJSON(p *PaperPipeline) json.RawMessage { b, _ := json.Marshal(p); return b }

// TopicDecision 保存每个主题独立的分类和归一化优先分。
type TopicDecision struct {
	Topic      string   `json:"topic"`
	Relevant   bool     `json:"relevant"`
	Category   string   `json:"category"`
	Score      float64  `json:"score"`
	Confidence *float64 `json:"confidence"`
}
