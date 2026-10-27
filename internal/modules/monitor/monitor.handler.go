package monitor

import (
	"context"
	"sort"

	"github.com/teamsillybees/glean/internal/platform/response"
)

// Handler 只暴露只读监控视图；前端不能启动采集或修改科学分类。
type Handler struct{ service *Service }

// NewHandler 创建监控只读 HTTP 适配器。
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) sources(ctx context.Context, _ *sourcesRequest) (*sourcesResponse, error) {
	snapshot, err := h.service.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]Checkpoint, 0, len(snapshot.Sources))
	for _, checkpoint := range snapshot.Sources {
		items = append(items, *checkpoint)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Source < items[j].Source })
	diagnostics, err := h.service.Doctor(ctx)
	if err != nil {
		return nil, err
	}
	counts := StateCounts{RetainedRuns: len(snapshot.Runs)}
	for _, candidate := range snapshot.Records {
		if candidate.Quarantine != nil {
			counts.QuarantinedCandidates++
		} else if candidate.Published {
			counts.PublishedCache++
		} else {
			counts.PendingCandidates++
		}
	}
	return &sourcesResponse{Body: response.OK(ctx, SourcesVO{Items: items, Schedule: snapshot.Schedule, Diagnostics: diagnostics, Counts: counts})}, nil
}
func (h *Handler) runs(ctx context.Context, _ *runsRequest) (*runsResponse, error) {
	snapshot, err := h.service.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	items := append([]Run{}, snapshot.Runs...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].Number != items[j].Number {
			return items[i].Number > items[j].Number
		}
		if !items[i].StartedAt.Equal(items[j].StartedAt) {
			return items[i].StartedAt.After(items[j].StartedAt)
		}
		return items[i].ID > items[j].ID
	})
	return &runsResponse{Body: response.OK(ctx, RunsVO{Items: items})}, nil
}
func (h *Handler) templates(ctx context.Context, _ *templatesRequest) (*templatesResponse, error) {
	items, version, err := h.service.currentTemplates(ctx)
	if err != nil {
		return nil, err
	}
	return &templatesResponse{Body: response.OK(ctx, TemplatesVO{Items: items, Version: version})}, nil
}
