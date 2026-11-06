package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/teamsillybees/glean/internal/data"
	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/gleanmonitorstate"
	"sync"
)

// ModelBudgetConfig 限制同一数据库内全部真实模型任务的累计调用与保守预留额度；不会自动按进程重置。
type ModelBudgetConfig struct {
	MaxCalls          int `mapstructure:"max_calls"`
	MaxReservedTokens int `mapstructure:"max_reserved_tokens"`
	MaxInputBytes     int `mapstructure:"max_input_bytes"`
	MaxOutputTokens   int `mapstructure:"max_output_tokens"`
}

func (b ModelBudgetConfig) defaults() ModelBudgetConfig {
	if b.MaxCalls == 0 {
		b.MaxCalls = 100
	}
	if b.MaxReservedTokens == 0 {
		b.MaxReservedTokens = 1000000
	}
	if b.MaxInputBytes == 0 {
		b.MaxInputBytes = 128 * 1024
	}
	if b.MaxOutputTokens == 0 {
		b.MaxOutputTokens = 4096
	}
	return b
}
func (b ModelBudgetConfig) validate() error {
	b = b.defaults()
	if b.MaxCalls < 1 || b.MaxCalls > 1000000 || b.MaxReservedTokens < 1 || b.MaxReservedTokens > 1000000000 || b.MaxInputBytes < 1024 || b.MaxInputBytes > 2<<20 || b.MaxOutputTokens < 128 || b.MaxOutputTokens > 32768 {
		return fmt.Errorf("invalid cumulative model budget")
	}
	return nil
}

// ModelBudgetUsage 是不可自动退款的保守预留，未知或中断请求仍计入；不是供应商账单。
type ModelBudgetUsage struct {
	Calls          int `json:"calls"`
	ReservedTokens int `json:"reservedTokens"`
}
type modelBudget struct {
	service *Service
	limits  ModelBudgetConfig
	mu      sync.Mutex
	usage   ModelBudgetUsage
}

const modelBudgetKey = "glean-global-model-budget-v1"

func (b *modelBudget) reserve(ctx context.Context, inputBytes int) error {
	if inputBytes > b.limits.MaxInputBytes {
		return &PipelineError{Kind: "model_input_budget"}
	}
	// UTF-8 字节数加协议开销作为输入的保守上界。Jev 不提供输出 token 上限，预留值仅作调用配额，不能声称硬费用上限。
	tokens := inputBytes + 4096 + b.limits.MaxOutputTokens
	advance := func(u *ModelBudgetUsage) error {
		if u.Calls >= b.limits.MaxCalls || tokens > b.limits.MaxReservedTokens-u.ReservedTokens {
			return &PipelineError{Kind: "model_budget_exhausted"}
		}
		u.Calls++
		u.ReservedTokens += tokens
		return nil
	}
	if b.service.memory != nil {
		b.mu.Lock()
		defer b.mu.Unlock()
		return advance(&b.usage)
	}
	for attempt := 0; attempt < 3; attempt++ {
		err := data.WithinTx(ctx, b.service.client, func(ctx context.Context, c *ent.Client) error {
			row, err := c.GleanMonitorState.Query().Where(gleanmonitorstate.Key(modelBudgetKey)).ForUpdate().Only(ctx)
			if err != nil && !ent.IsNotFound(err) {
				return err
			}
			u := ModelBudgetUsage{}
			if row != nil {
				if err = json.Unmarshal(row.Snapshot, &u); err != nil {
					return err
				}
			}
			if err = advance(&u); err != nil {
				return err
			}
			raw, _ := json.Marshal(u)
			if row == nil {
				return c.GleanMonitorState.Create().SetID(b.service.ids.NextID()).SetKey(modelBudgetKey).SetSnapshot(raw).SetRevision(1).Exec(ctx)
			}
			return c.GleanMonitorState.UpdateOneID(row.ID).SetSnapshot(raw).AddRevision(1).Exec(ctx)
		})
		if !ent.IsConstraintError(err) {
			return err
		}
	}
	return &PipelineError{Kind: "model_budget_unavailable"}
}
func (s *Service) modelBudgetUsage(ctx context.Context) (ModelBudgetUsage, error) {
	if s.memory != nil {
		if p, ok := s.pipeline.(*ModelPipelineProcessor); ok && p.budget != nil {
			p.budget.mu.Lock()
			defer p.budget.mu.Unlock()
			return p.budget.usage, nil
		}
		return ModelBudgetUsage{}, nil
	}
	row, err := s.client.GleanMonitorState.Query().Where(gleanmonitorstate.Key(modelBudgetKey)).Only(ctx)
	if ent.IsNotFound(err) {
		return ModelBudgetUsage{}, nil
	}
	if err != nil {
		return ModelBudgetUsage{}, err
	}
	var u ModelBudgetUsage
	err = json.Unmarshal(row.Snapshot, &u)
	return u, err
}
