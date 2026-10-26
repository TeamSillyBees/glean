package httpdemo

import (
	"context"

	"github.com/teamsillybees/glean/internal/platform/response"
)

// Handler 封装 httpdemo 示例模块的 HTTP 适配逻辑。
type Handler struct {
	service *Service
}

// NewHandler 创建 httpdemo 示例模块 HTTP Handler。
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) get(ctx context.Context, input *getHTTPBingoRequest) (*getHTTPBingoResponse, error) {
	vo, err := h.service.GetHTTPBingo(ctx, input.Message)
	if err != nil {
		return nil, err
	}
	return &getHTTPBingoResponse{
		Body: response.OK(ctx, vo),
	}, nil
}

func (h *Handler) formPage(ctx context.Context, input *getHTTPBingoFormPageRequest) (*getHTTPBingoFormPageResponse, error) {
	vo, err := h.service.GetHTTPBingoFormPage(ctx)
	if err != nil {
		return nil, err
	}
	return &getHTTPBingoFormPageResponse{
		Body: response.OK(ctx, vo),
	}, nil
}
