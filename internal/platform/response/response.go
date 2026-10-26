package response

import (
	"context"

	"github.com/teamsillybees/glean/internal/platform/requestctx"
)

// SuccessVO 是统一成功响应 JSON DTO。
type SuccessVO[T any] struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
	TraceID string `json:"traceId,omitempty"`
}

// OK 创建标准成功响应。
func OK[T any](ctx context.Context, data T) SuccessVO[T] {
	traceID, _ := requestctx.TraceIDFromContext(ctx)
	return SuccessVO[T]{
		Code:    "OK",
		Message: "success",
		Data:    data,
		TraceID: traceID,
	}
}
