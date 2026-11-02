package monitor

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/response"
	"github.com/teamsillybees/glean/internal/platform/server"
)

type pipelineRetryRequest struct {
	ID   idgen.ID `path:"id"`
	Body struct {
		Stage string `json:"stage" maxLength:"64"`
	}
}
type pipelineResponse struct {
	Body response.SuccessVO[PaperPipeline]
}

func (h *Handler) retryPipeline(ctx context.Context, in *pipelineRetryRequest) (*pipelineResponse, error) {
	p, err := h.service.RetryPipelineStage(ctx, in.ID, in.Body.Stage)
	if err != nil {
		return nil, err
	}
	return &pipelineResponse{Body: response.OK(ctx, p)}, nil
}
func (m *Module) registerPipeline(api huma.API, registry *server.RouteRegistry) {
	readPath := "/api/v1/monitor/pipeline/{id}"
	huma.Register(api, huma.Operation{OperationID: "monitor-pipeline-detail", Method: http.MethodGet, Path: readPath, Summary: "查看论文处理档案", Tags: []string{"论文监控"}}, m.handler.getPipeline)
	registry.Register(http.MethodGet, readPath, platformauth.RouteSecurity{AccessMode: platformauth.AccessModeAuthenticated})
	path := "/api/v1/monitor/pipeline/{id}/retry"
	huma.Register(api, huma.Operation{OperationID: "monitor-pipeline-retry", Method: http.MethodPost, Path: path, Summary: "重试失败的论文处理阶段", Tags: []string{"论文监控"}}, m.handler.retryPipeline)
	registry.Register(http.MethodPost, path, platformauth.RouteSecurity{AccessMode: platformauth.AccessModePermission, Permission: "catalog:manage"})
}

type pipelineGetRequest struct {
	ID idgen.ID `path:"id"`
}

func (h *Handler) getPipeline(ctx context.Context, in *pipelineGetRequest) (*pipelineResponse, error) {
	state, err := h.service.load(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range state.Records {
		if c.ID == in.ID && c.Pipeline != nil {
			if c.PipelineArchived {
				if err := h.service.restorePipeline(ctx, c); err != nil {
					return nil, err
				}
			}
			return &pipelineResponse{Body: response.OK(ctx, *pipelineWithHistory(c))}, nil
		}
	}
	archived, err := h.service.archivedPipeline(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	return &pipelineResponse{Body: response.OK(ctx, *pipelineWithHistory(archived))}, nil
}
