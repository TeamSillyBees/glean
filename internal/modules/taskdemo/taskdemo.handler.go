package taskdemo

import (
	"context"

	"github.com/teamsillybees/glean/internal/platform/requestctx"
	"github.com/teamsillybees/glean/internal/platform/response"
)

// Handler 封装 taskdemo 示例模块的 HTTP 适配逻辑。
type Handler struct {
	service *Service
}

// NewHandler 创建 taskdemo 示例模块 HTTP Handler。
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) publishEmail(ctx context.Context, input *publishEmailRequest) (*publishEmailResponse, error) {
	traceID := traceIDFromContext(ctx)
	vo, err := h.service.PublishEmail(ctx, input.Body, traceID)
	if err != nil {
		return nil, err
	}
	return &publishEmailResponse{
		Body: response.OK(ctx, vo),
	}, nil
}

func traceIDFromContext(ctx context.Context) string {
	traceID, _ := requestctx.TraceIDFromContext(ctx)
	return traceID
}
