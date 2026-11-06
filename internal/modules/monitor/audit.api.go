package monitor

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/response"
	"github.com/teamsillybees/glean/internal/platform/server"
	"github.com/teamsillybees/glean/internal/retrieval"
)

type auditPageRequest struct {
	ID    idgen.ID `path:"id"`
	After idgen.ID `query:"after"`
	Limit int      `query:"limit" default:"100" minimum:"1" maximum:"500"`
}
type archiveRunsRequest struct {
	Before idgen.ID `query:"before"`
	Limit  int      `query:"limit" default:"50" minimum:"1" maximum:"100"`
}
type auditPage[T any] struct {
	Items     []T       `json:"items"`
	NextAfter *idgen.ID `json:"nextAfter"`
}
type auditPageResponse[T any] struct {
	Body response.SuccessVO[auditPage[T]]
}
type archiveRunsResponse struct {
	Body response.SuccessVO[archivedRunsPage]
}

type auditExportRequest struct {
	ID idgen.ID `path:"id"`
}
type modelBudgetResponse struct {
	Body response.SuccessVO[modelBudgetView]
}

type budgetIncreaseRequest struct {
	ID   idgen.ID `path:"id"`
	Body BudgetIncreaseBody
}
type budgetIncreaseResponse struct{ Body response.SuccessVO[Run] }

func (h *Handler) increaseBudget(ctx context.Context, in *budgetIncreaseRequest) (*budgetIncreaseResponse, error) {
	run, err := h.service.IncreaseProtocolBudget(ctx, in.ID, in.Body)
	if err != nil {
		return nil, err
	}
	return &budgetIncreaseResponse{Body: response.OK(ctx, run)}, nil
}

type modelBudgetView struct {
	Usage             ModelBudgetUsage `json:"usage"`
	MaxCalls          int              `json:"maxCalls"`
	MaxReservedTokens int              `json:"maxReservedTokens"`
	MaxInputBytes     int              `json:"maxInputBytes"`
	MaxOutputTokens   int              `json:"maxOutputTokens"`
	Note              string           `json:"note"`
}

func (h *Handler) budget(ctx context.Context, _ *struct{}) (*modelBudgetResponse, error) {
	u, err := h.service.modelBudgetUsage(ctx)
	if err != nil {
		return nil, err
	}
	b := ModelBudgetConfig{}.defaults()
	if p, ok := h.service.pipeline.(*ModelPipelineProcessor); ok {
		b = p.config.Budget.defaults()
	}
	return &modelBudgetResponse{Body: response.OK(ctx, modelBudgetView{u, b.MaxCalls, b.MaxReservedTokens, b.MaxInputBytes, b.MaxOutputTokens, "同一数据库的累计调用与保守预留额度；中断/失败不自动退款。不是供应商费用硬上限；Jev 未提供同等输出 token 限制。"})}, nil
}

type auditExportResponse struct {
	ContentType  string `header:"Content-Type"`
	Disposition  string `header:"Content-Disposition"`
	CacheControl string `header:"Cache-Control"`
	Body         []byte
}

func (h *Handler) auditRequests(ctx context.Context, in *auditPageRequest) (*auditPageResponse[RequestAudit], error) {
	if in.After < 0 {
		return nil, bizerrors.BadRequest("invalid cursor")
	}
	if _, e := h.service.auditRun(ctx, in.ID); e != nil {
		return nil, e
	}
	rows, e := h.service.auditRequests(ctx, in.ID, in.After, in.Limit+1)
	if e != nil {
		return nil, e
	}
	var next *idgen.ID
	if len(rows) > in.Limit {
		rows = rows[:in.Limit]
		v := rows[len(rows)-1].RequestID
		next = &v
	}
	return &auditPageResponse[RequestAudit]{Body: response.OK(ctx, auditPage[RequestAudit]{Items: rows, NextAfter: next})}, nil
}
func (h *Handler) auditOccurrences(ctx context.Context, in *auditPageRequest) (*auditPageResponse[OccurrenceAudit], error) {
	if in.After < 0 {
		return nil, bizerrors.BadRequest("invalid cursor")
	}
	if _, e := h.service.auditRun(ctx, in.ID); e != nil {
		return nil, e
	}
	rows, e := h.service.auditOccurrences(ctx, in.ID, in.After, in.Limit+1)
	if e != nil {
		return nil, e
	}
	var next *idgen.ID
	if len(rows) > in.Limit {
		rows = rows[:in.Limit]
		v := rows[len(rows)-1].ID
		next = &v
	}
	for i := range rows {
		rows[i].Record.Abstract = ""
		rows[i].Record.Raw = nil
	}
	return &auditPageResponse[OccurrenceAudit]{Body: response.OK(ctx, auditPage[OccurrenceAudit]{Items: rows, NextAfter: next})}, nil
}
func (h *Handler) auditCatalog(ctx context.Context, in *auditPageRequest) (*auditPageResponse[retrieval.CatalogExportRecord], error) {
	if _, e := h.service.auditRun(ctx, in.ID); e != nil {
		return nil, e
	}
	reader, ok := h.service.publisher.(CatalogAuditReader)
	if !ok {
		return nil, bizerrors.BadRequest("catalog audit unavailable")
	}
	// 目录最大页长为500，多取一条只在较小分页时使用；满页提供可继续的稳定游标。
	rows, e := reader.RetrievalExportPage(ctx, in.ID, in.After, in.Limit)
	if e != nil {
		return nil, e
	}
	var next *idgen.ID
	if len(rows) == in.Limit {
		v := rows[len(rows)-1].ID
		next = &v
	}
	return &auditPageResponse[retrieval.CatalogExportRecord]{Body: response.OK(ctx, auditPage[retrieval.CatalogExportRecord]{Items: rows, NextAfter: next})}, nil
}
func (h *Handler) exportAudit(ctx context.Context, in *auditExportRequest) (*auditExportResponse, error) {
	content, err := h.service.ExportAuditZIP(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	return &auditExportResponse{ContentType: "application/zip", Disposition: "attachment; filename=glean-run-" + in.ID.String() + ".zip", CacheControl: "private, no-store", Body: content}, nil
}
func (h *Handler) archivedRuns(ctx context.Context, in *archiveRunsRequest) (*archiveRunsResponse, error) {
	page, err := h.service.archivedRunPage(ctx, in.Before, in.Limit)
	if err != nil {
		return nil, err
	}
	return &archiveRunsResponse{Body: response.OK(ctx, *page)}, nil
}

func (m *Module) registerAudit(api huma.API, registry *server.RouteRegistry) {
	budgetPath := "/api/v1/monitor/runs/{id}/budget"
	registry.Register(http.MethodPost, budgetPath, platformauth.RouteSecurity{AccessMode: platformauth.AccessModePermission, Permission: "catalog:manage"})
	huma.Register(api, huma.Operation{OperationID: "monitor-budget-increase", Method: http.MethodPost, Path: budgetPath, Summary: "提高未完成运行的采集预算", Tags: []string{"检索审计"}}, m.handler.increaseBudget)
	register := func(path, id, summary string) huma.Operation {
		registry.Register(http.MethodGet, path, platformauth.RouteSecurity{AccessMode: platformauth.AccessModeAuthenticated})
		return huma.Operation{OperationID: id, Method: http.MethodGet, Path: path, Summary: summary, Tags: []string{"检索审计"}}
	}
	huma.Register(api, register("/api/v1/monitor/archive/runs", "monitor-archive-runs", "分页查询长期检索运行"), m.handler.archivedRuns)
	huma.Register(api, register("/api/v1/monitor/model-budget", "monitor-model-budget", "读取共享模型调用预算"), m.handler.budget)
	huma.Register(api, register("/api/v1/monitor/runs/{id}/requests", "monitor-request-audit", "分页查询实际请求和失败"), m.handler.auditRequests)
	huma.Register(api, register("/api/v1/monitor/runs/{id}/occurrences", "monitor-occurrence-audit", "分页查询全部来源出现"), m.handler.auditOccurrences)
	huma.Register(api, register("/api/v1/monitor/runs/{id}/catalog", "monitor-catalog-audit", "分页查询目录证据与当前用户状态"), m.handler.auditCatalog)
	huma.Register(api, register("/api/v1/monitor/runs/{id}/export", "monitor-audit-export", "下载检索审计 ZIP"), m.handler.exportAudit)
}
