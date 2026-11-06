package monitor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
)

// MaterialProvider 仅获取合法公开材料，无法取得正文时明确回退摘要。
type MaterialProvider interface {
	Material(context.Context, Record) (Material, error)
}

// SetPipelineProcessor 显式装配真实阶段处理器；不执行任何外部请求。
func (s *Service) SetPipelineProcessor(p PipelineProcessor) {
	s.pipeline = p
	if model, ok := p.(*ModelPipelineProcessor); ok {
		b := &modelBudget{service: s, limits: model.config.Budget.defaults()}
		model.budget = b
		if model.jev != nil {
			model.jev.config.budget = b
		}
		if model.generative != nil {
			model.generative.config.budget = b
		}
	}
}

// SetMaterialProvider 装配有界公开材料获取器。
func (s *Service) SetMaterialProvider(p MaterialProvider) { s.materialProvider = p }

func hashJSON(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func materialFromRecord(r Record) Material { return abstractMaterial(r) }
func newPaperPipeline(c *Candidate) *PaperPipeline {
	p := &PaperPipeline{Version: PipelineVersion, TemplateVersion: c.Record.TemplateVersion, Key: c.Key, RunID: c.RunID, Templates: c.Templates, CandidateID: c.ID, Status: "processing", Material: materialFromRecord(c.Record)}
	for _, name := range []string{"material", "classification", "summary", "independent_review", "program_validation", "evidence_check"} {
		p.Stages = append(p.Stages, PipelineStage{Name: name, Status: "pending", RuleVersion: "evidence-exact-v1"})
	}
	return p
}

// processPipelines 与采集checkpoint解耦；单条模型失败不会污染来源失败预算或水位。
func (s *Service) processPipelines(ctx context.Context, state *monitorState) error {
	if s.pipeline == nil {
		return nil
	}
	keys := make([]string, 0, len(state.Records))
	for key := range state.Records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	budget := min(8, s.config.MaxReviewPerCall)
	for _, key := range keys {
		c := state.Records[key]
		if c.Quarantine != nil || !c.Published || (c.Compacted && c.Pipeline == nil) {
			continue
		}
		if c.Pipeline == nil {
			continue
		} // 不自动迁移历史mock候选或触发历史批量付费处理。

		if c.PipelineArchived {
			if pipelineTerminal(c.Pipeline) {
				continue
			}
			ready := false
			for _, stage := range c.Pipeline.Stages {
				if stage.Status == "completed" {
					continue
				}
				ready = stage.Status == "pending" || stage.Status == "running" || (stage.Status == "retry_wait" && !stage.NextRetryAt.After(s.clock().UTC())) || (stage.Status == "awaiting_configuration" && s.pipelineReady(stage.Name))
				break
			}
			if !ready {
				continue
			}
			if err := s.restorePipeline(ctx, c); err != nil {
				return err
			}
		}
		p := c.Pipeline
		previous := map[string]PipelineResult{}
		for i := range p.Stages {
			stage := &p.Stages[i]
			if stage.Status == "completed" && stage.Result != nil {
				previous[stage.Name] = *stage.Result
				continue
			}
			if stage.Status == "running" {
				stage.Status = "failed"
				stage.Error = "interrupted_execution"
				p.Status = "failed"
				c.PipelineProjected = false
				if err := s.save(ctx, state); err != nil {
					return err
				}
				break
			}
			if stage.Status == "awaiting_configuration" && s.pipelineReady(stage.Name) {
				stage.Status = "pending"
			}
			if budget <= 0 || stage.Status == "failed" || stage.Status == "awaiting_configuration" || stage.NextRetryAt.After(s.clock().UTC()) {
				break
			}
			timeout := s.pipelineTimeout(stage.Name)
			if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < timeout+3*time.Second {
				break
			}
			budget--
			record := c.Record
			record.Raw = nil
			record.Abstract = "" // 模型材料唯一来源为有界Material，不能借题录字段绕过阅读范围。
			record.Authors = nil
			record.PublicationEvents = nil
			if len([]rune(record.Title)) > 2000 {
				record.Title = string([]rune(record.Title)[:2000])
			}
			input := PipelineInput{Templates: c.Templates, Record: record, Material: p.Material, Previous: previous}
			metadata := s.pipeline.Metadata(stage.Name)
			stage.InputHash = hashJSON(input)
			stage.Provider = metadata.Provider
			stage.Model = metadata.Model
			stage.PromptVersion = metadata.PromptVersion
			stage.Status = "running"
			stage.Attempts++
			stage.StartedAt = s.clock().UTC()
			stage.Error = ""
			p.Status = "processing"
			c.PipelineProjected = false
			if err := s.save(ctx, state); err != nil {
				return err
			}
			callCtx, cancel := context.WithTimeout(ctx, timeout)
			var result PipelineResult
			var err error
			if stage.Name == "material" {
				stage.Provider = "public_source"
				stage.Model = "none"
				stage.PromptVersion = "material-v1"
				if _, hasReady := s.pipeline.(interface{ Ready(string) bool }); hasReady && !s.pipelineReady("material") {
					err = &PipelineError{Kind: "awaiting_configuration"}
				} else if s.materialProvider != nil {
					var m Material
					m, err = s.materialProvider.Material(callCtx, c.Record)
					if err == nil && m.Text != "" {
						p.Material = m
					}
				}
			} else if stage.Name == "program_validation" {
				result = validatePipeline(input)
				stage.Provider = "program"
				stage.Model = "deterministic"
				stage.PromptVersion = "evidence-exact-v1"
			} else if stage.Name == "summary" && p.Material.Scope == "metadata_only" {
				err = &PipelineError{Kind: "insufficient_material"}
			} else {
				result, err = s.pipeline.Process(callCtx, stage.Name, input)
			}
			cancel()
			if err == nil {
				err = validateStageResult(stage.Name, result)
				if err == nil && stage.Name == "classification" {
					err = validateTopicResults(c.Record.Topics, result)
				}
			}
			stage.FinishedAt = s.clock().UTC()
			if err != nil {
				stage.Status = "failed"
				stage.Error = "model_processing_failed"
				var pe *PipelineError
				if errors.As(err, &pe) {
					if strings.HasPrefix(pe.Kind, "model_") {
						stage.Error = pe.Kind
					}
					if pe.Kind == "authentication_failed" || pe.Kind == "invalid_response" {
						stage.Error = pe.Kind
					}
					if pe.Kind == "insufficient_material" {
						stage.Error = "insufficient_material"
					}
					if pe.Kind == "awaiting_configuration" {
						stage.Status = "awaiting_configuration"
						stage.Attempts--
						stage.Error = "awaiting_configuration"
					} else if pe.Retryable && stage.Attempts < s.config.RetryBudget {
						stage.Status = "retry_wait"
						stage.Error = "temporary_model_failure"
						delay := max(s.config.BaseBackoff*time.Duration(stage.Attempts), pe.RetryAfter)
						stage.NextRetryAt = s.clock().UTC().Add(delay)
					}
				}
				p.Status = stage.Status
			} else {
				stage.Status = "completed"
				stage.Result = &result
				if result.Call != nil && result.Call.Model != "" {
					stage.Model = result.Call.Model
				}
				stage.NextRetryAt = time.Time{}
				previous[stage.Name] = result
			}
			if err := s.save(ctx, state); err != nil {
				return err
			}
			if stage.Status != "completed" {
				break
			}
		}
		complete := true
		supported := true
		for _, stage := range p.Stages {
			complete = complete && stage.Status == "completed"
			if stage.Result != nil && stage.Result.Supported != nil {
				supported = supported && *stage.Result.Supported
			}
			if stage.Result != nil && len(stage.Result.Issues) > 0 {
				supported = false
			}
		}
		if complete {
			p.Status = "needs_human_review"
			if s.config.PipelineAutoDisplay && supported {
				p.Status = "ready_for_display"
			}
		}
		p.ScientificReviewed = false
		if !c.PipelineProjected {
			review := Review{Provider: "pipeline", PromptVersion: PipelineVersion, ReviewedAt: s.clock().UTC(), InclusionStatus: "pending", Summary: "自动处理未完成；未经人工科学核验。", Pipeline: pipelineJSON(pipelineWithHistory(c))}
			if result, ok := previous["summary"]; ok {
				review.Summary = result.Summary
			}
			c.Review = &review
			// 投影失败只保留未投影标志，下次幂等重试，不修改采集水位。
			projectCtx, cancel := context.WithTimeout(ctx, s.config.RequestTimeout)
			err := s.publisher.Publish(projectCtx, c.RunID, []Publication{{Key: c.Key, Record: c.Record, Review: review}})
			cancel()
			if err == nil {
				c.PipelineProjected = true
				p.ProjectionError = ""
			} else {
				p.ProjectionError = "处理结果目录投影失败，等待幂等重试"
			}
			if err := s.save(ctx, state); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}

	// 模型水位仅由已完成的阶段决定，不能把原始目录投影当成模型阅读完成。
	for ri := range state.Runs {
		run := &state.Runs[ri]
		for name, progress := range run.Sources {
			if !progress.Acquired || progress.Quarantined > 0 {
				continue
			}
			complete := true
			for _, key := range progress.CandidateKeys {
				c := state.Records[key]
				if c == nil || c.Pipeline == nil || (c.Pipeline.Status != "needs_human_review" && c.Pipeline.Status != "ready_for_display") {
					complete = false
					break
				}
			}
			if complete {
				for _, candidate := range state.Records {
					if candidate.Record.Source == name && candidate.Pipeline != nil && candidate.Pipeline.Status != "needs_human_review" && candidate.Pipeline.Status != "ready_for_display" {
						complete = false
						break
					}
				}
			}
			if complete {
				progress.Reviewed = true
				if progress.AdvanceCheckpoint || run.RequestKey == "" {
					state.Sources[name].ReviewedThrough = later(state.Sources[name].ReviewedThrough, run.FixedEnd)
				}
			}
		}
	}
	s.compact(state)
	return s.save(ctx, state)
}
func validateStageResult(stage string, r PipelineResult) error {
	valid := true
	switch stage {
	case "classification":
		valid = len(r.Topics) > 0
	case "summary":
		valid = strings.TrimSpace(r.Summary) != "" && len(r.Claims) > 0
	case "independent_review", "evidence_check":
		valid = r.Supported != nil
	}
	if !valid {
		return &PipelineError{Kind: "invalid_response"}
	}
	return nil
}

var pipelineNumbers = regexp.MustCompile(`[-+]?\d+(?:,\d{3})*(?:\.\d+)?(?:[eE][-+]?\d+)?%?`)

func validatePipeline(input PipelineInput) PipelineResult {
	result := PipelineResult{}
	summary := input.Previous["summary"]
	material := input.Material.Text
	blocks := map[string]string{}
	for _, b := range input.Material.Blocks {
		blocks[b.ID] = b.Text
	}
	issues := []string{}
	for _, claim := range summary.Claims {
		block, exists := blocks[claim.EvidenceBlockID]
		if !exists || strings.TrimSpace(claim.Quote) == "" || !strings.Contains(block, claim.Quote) {
			issues = append(issues, "声明引用的原文片段不存在于实际阅读材料")
		}
		for _, n := range append(claim.Numbers, pipelineNumbers.FindAllString(claim.Text, -1)...) {
			if !containsNumber(claim.Quote, n) {
				issues = append(issues, "声明数值未在引用片段中找到")
			}
		}
		for _, citation := range claim.Citations {
			if !strings.Contains(material, citation) {
				issues = append(issues, "引用标识未在阅读材料中找到")
			}
		}
	}
	for _, n := range pipelineNumbers.FindAllString(summary.Summary, -1) {
		if !containsNumber(material, n) {
			issues = append(issues, "总结数值未在阅读材料中找到")
		}
	}
	supported := len(issues) == 0
	result.Supported = &supported
	result.Issues = uniqueStrings(issues)
	return result
}

func validateTopicResults(topics []string, r PipelineResult) error {
	seen := map[string]bool{}
	for _, decision := range r.Topics {
		if seen[decision.Topic] || decision.Category == "" || decision.Score < 0 || decision.Score > 1 {
			return &PipelineError{Kind: "invalid_response"}
		}
		seen[decision.Topic] = true
	}
	for _, topic := range topics {
		if !seen[topic] {
			return &PipelineError{Kind: "invalid_response"}
		}
	}
	return nil
}

func (s *Service) pipelineReady(stage string) bool {
	p, ok := s.pipeline.(interface{ Ready(string) bool })
	if stage == "material" {
		return ok && p.Ready("classification") && p.Ready("summary") && p.Ready("independent_review") && p.Ready("evidence_check")
	}
	return ok && p.Ready(stage)
}

func containsNumber(text, value string) bool {
	for _, n := range pipelineNumbers.FindAllString(text, -1) {
		if n == value {
			return true
		}
	}
	return false
}

func (s *Service) pipelineTimeout(stage string) time.Duration {
	if stage == "material" {
		return 30 * time.Second
	}
	if stage == "program_validation" {
		return time.Second
	}
	if p, ok := s.pipeline.(interface{ StageTimeout(string) time.Duration }); ok {
		if d := p.StageTimeout(stage); d > 0 {
			return min(d, s.config.LeaseDuration/2-time.Second)
		}
	}
	return s.config.RequestTimeout
}

func pipelineTerminal(p *PaperPipeline) bool {
	return p != nil && (p.Status == "needs_human_review" || p.Status == "ready_for_display")
}
func pipelineWithHistory(c *Candidate) *PaperPipeline {
	p := *c.Pipeline
	p.History = append([]PaperPipeline(nil), c.PipelineHistory...)
	return &p
}
