package monitor

import (
	"context"
	"encoding/json"

	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// PipelineArchive 从真实目录读取已持久化的处理档案，支持热缓存淘汰后的查询与显式恢复。
type PipelineArchive interface {
	LoadPipeline(context.Context, idgen.ID) (Publication, error)
	FindPipeline(context.Context, Record, string) (Publication, bool, error)
}

func (s *Service) archivedPipeline(ctx context.Context, id idgen.ID) (*Candidate, error) {
	archive, ok := s.publisher.(PipelineArchive)
	if !ok {
		return nil, bizerrors.BadRequest("论文处理归档不可用")
	}
	publication, err := archive.LoadPipeline(ctx, id)
	if err != nil {
		return nil, err
	}
	return archivedCandidate(publication, id)
}
func archivedCandidate(publication Publication, id idgen.ID) (*Candidate, error) {
	var p PaperPipeline
	if json.Unmarshal(publication.Review.Pipeline, &p) != nil || p.CandidateID != id || p.Key == "" || p.RunID <= 0 {
		return nil, bizerrors.BadRequest("论文处理归档无效")
	}
	if p.TemplateVersion != "" {
		publication.Record.TemplateVersion = p.TemplateVersion
	}
	history := p.History
	p.History = nil
	_, identity := recordIdentity(publication.Record)
	return &Candidate{ID: id, Key: p.Key, RunID: p.RunID, Templates: p.Templates, Identity: identity, Record: publication.Record, Review: &publication.Review, Pipeline: &p, PipelineHistory: history, Published: true, PipelineProjected: true}, nil
}
func (s *Service) restorePipeline(ctx context.Context, c *Candidate) error {
	loaded, err := s.archivedPipeline(ctx, c.ID)
	if err != nil {
		return err
	}
	*c = *loaded
	return nil
}
