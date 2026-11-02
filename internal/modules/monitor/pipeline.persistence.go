package monitor

import (
	"context"
	"github.com/teamsillybees/glean/internal/data/ent/sysuser"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/entx"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"time"
)

// RetryPipelineStage 仅重开一个失败阶段，不重置来源水位，不重跑已完成付费阶段。
func (s *Service) RetryPipelineStage(ctx context.Context, id idgen.ID, name string) (PaperPipeline, error) {
	principal, ok := platformauth.PrincipalFromContext(ctx)
	if !ok || !principal.IsSuperAdmin {
		return PaperPipeline{}, bizerrors.CatalogForbidden()
	}
	if s.client != nil {
		exists, err := s.client.SysUser.Query().Where(sysuser.ID(principal.UserID), sysuser.DeletedAtIsNil(), sysuser.IsEnable(true), sysuser.IsSuperAdmin(true)).Exist(ctx)
		if err != nil {
			return PaperPipeline{}, err
		}
		if !exists {
			return PaperPipeline{}, bizerrors.CatalogForbidden()
		}
	}
	ctx = entx.WithOperatorID(ctx, principal.UserID)
	state, err := s.acquire(ctx)
	if err != nil {
		return PaperPipeline{}, err
	}
	defer s.release(ctx, state)
	found := false
	for _, c := range state.Records {
		if c.ID == id {
			found = true
			break
		}
	}
	if !found {
		c, err := s.archivedPipeline(ctx, id)
		if err != nil {
			return PaperPipeline{}, err
		}
		state.Records[c.Key] = c
	}
	for _, c := range state.Records {
		if c.ID != id || c.Pipeline == nil {
			continue
		}
		if c.PipelineArchived {
			if err := s.restorePipeline(ctx, c); err != nil {
				return PaperPipeline{}, err
			}
		}
		for i := range c.Pipeline.Stages {
			stage := &c.Pipeline.Stages[i]
			if stage.Name != name {
				continue
			}
			if stage.Name == "material" && stage.Status == "completed" && c.Pipeline.Material.Scope != "fulltext" {
				c.PipelineHistory = append(c.PipelineHistory, *c.Pipeline)
				c.Pipeline = newPaperPipeline(c)
				c.PipelineProjected = false
				if err := s.save(ctx, state); err != nil {
					return PaperPipeline{}, err
				}
				return *c.Pipeline, nil
			}
			if stage.Status != "failed" && stage.Status != "awaiting_configuration" && stage.Status != "retry_wait" {
				return PaperPipeline{}, bizerrors.BadRequest("只能重试失败或待配置的阶段")
			}
			if stage.NextRetryAt.After(s.clock().UTC()) {
				return PaperPipeline{}, bizerrors.BadRequest("阶段退避尚未结束")
			}
			stage.Status = "pending"
			stage.Error = ""
			stage.NextRetryAt = time.Time{}
			c.Pipeline.Status = "processing"
			c.PipelineProjected = false
			if err := s.save(ctx, state); err != nil {
				return PaperPipeline{}, err
			}
			return *c.Pipeline, nil
		}
		return PaperPipeline{}, bizerrors.BadRequest("阶段不存在")
	}
	return PaperPipeline{}, bizerrors.BadRequest("候选处理档案不存在")
}
