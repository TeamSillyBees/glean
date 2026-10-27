package catalog

import (
	"context"

	"github.com/teamsillybees/glean/internal/platform/response"
)

func (h *Handler) access(ctx context.Context, _ *struct{}) (*accessResponse, error) {
	return &accessResponse{Body: response.OK(ctx, CatalogAccessVO{CanManage: canManage(ctx)})}, nil
}
func (h *Handler) saveReview(ctx context.Context, input *reviewRequest) (*reviewResponse, error) {
	v, err := h.service.SaveReview(ctx, input.ID, input.Slug, input.Body)
	if err != nil {
		return nil, err
	}
	return &reviewResponse{Body: response.OK(ctx, v)}, nil
}
func (h *Handler) saveCorrection(ctx context.Context, input *correctionRequest) (*correctionResponse, error) {
	v, err := h.service.SaveCorrection(ctx, input.ID, input.Body)
	if err != nil {
		return nil, err
	}
	return &correctionResponse{Body: response.OK(ctx, v)}, nil
}
func (h *Handler) createTopic(ctx context.Context, input *createTopicRequest) (*topicResponse, error) {
	v, err := h.service.SaveTopic(ctx, "", input.Body, true)
	if err != nil {
		return nil, err
	}
	return &topicResponse{Body: response.OK(ctx, v)}, nil
}
func (h *Handler) updateTopic(ctx context.Context, input *updateTopicRequest) (*topicResponse, error) {
	v, err := h.service.SaveTopic(ctx, input.Slug, input.Body, false)
	if err != nil {
		return nil, err
	}
	return &topicResponse{Body: response.OK(ctx, v)}, nil
}
func (h *Handler) history(ctx context.Context, input *paperRequest) (*historyResponse, error) {
	v, err := h.service.History(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &historyResponse{Body: response.OK(ctx, catalogHistoryVO{Items: v})}, nil
}
