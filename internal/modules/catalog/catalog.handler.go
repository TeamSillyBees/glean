package catalog

import (
	"context"
	"github.com/teamsillybees/glean/internal/platform/response"
)

// Handler 负责目录 HTTP 适配，不在前端执行科研规则。
type Handler struct{ service *Service }

// NewHandler 构造目录 Handler。
func NewHandler(service *Service) *Handler { return &Handler{service: service} }
func (h *Handler) topics(ctx context.Context, _ *struct{}) (*topicsResponse, error) {
	items, err := h.service.Topics(ctx)
	if err != nil {
		return nil, err
	}
	return &topicsResponse{Body: response.OK(ctx, struct {
		Items []TopicVO `json:"items"`
	}{Items: items})}, nil
}
func (h *Handler) papers(ctx context.Context, input *papersRequest) (*papersResponse, error) {
	result, err := h.service.Papers(ctx, input.PaperQuery)
	if err != nil {
		return nil, err
	}
	return &papersResponse{Body: response.OK(ctx, result)}, nil
}
func (h *Handler) paper(ctx context.Context, input *paperRequest) (*paperResponse, error) {
	result, err := h.service.Paper(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &paperResponse{Body: response.OK(ctx, result)}, nil
}
func (h *Handler) facets(ctx context.Context, input *papersRequest) (*facetsResponse, error) {
	result, err := h.service.Facets(ctx, input.PaperQuery)
	if err != nil {
		return nil, err
	}
	return &facetsResponse{Body: response.OK(ctx, result)}, nil
}
func (h *Handler) venues(ctx context.Context, input *venuesRequest) (*venuesResponse, error) {
	items, err := h.service.Venues(ctx, input.Q, input.Topic)
	if err != nil {
		return nil, err
	}
	return &venuesResponse{Body: response.OK(ctx, struct {
		Items []VenueVO `json:"items"`
	}{Items: items})}, nil
}
func (h *Handler) preferences(ctx context.Context, _ *struct{}) (*preferenceResponse, error) {
	result, err := h.service.Preferences(ctx)
	if err != nil {
		return nil, err
	}
	return &preferenceResponse{Body: response.OK(ctx, result)}, nil
}
func (h *Handler) savePreferences(ctx context.Context, input *preferenceRequest) (*preferenceResponse, error) {
	result, err := h.service.SavePreferences(ctx, input.Body)
	if err != nil {
		return nil, err
	}
	return &preferenceResponse{Body: response.OK(ctx, result)}, nil
}
func (h *Handler) saveState(ctx context.Context, input *stateRequest) (*stateResponse, error) {
	result, err := h.service.SavePaperState(ctx, input.ID, input.Body)
	if err != nil {
		return nil, err
	}
	return &stateResponse{Body: response.OK(ctx, result)}, nil
}
