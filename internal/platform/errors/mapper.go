package apperrors

import "github.com/samber/oops"

// ErrorVO 是统一错误响应 JSON DTO。
type ErrorVO struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
	TraceID string         `json:"traceId,omitempty"`
}

// ToHTTP 将任意 error 归一化为 HTTP 状态码和统一响应体。
func ToHTTP(err error, traceID string) (int, ErrorVO) {
	if _, ok := oops.AsOops(err); !ok {
		return statusOf(CodeInternalError), ErrorVO{
			Code:    string(CodeInternalError),
			Message: "internal error",
			TraceID: traceID,
		}
	}

	return StatusOf(err), ErrorVO{
		Code:    string(CodeOf(err)),
		Message: PublicMessageOf(err),
		Details: PublicDetailsOf(err),
		TraceID: traceID,
	}
}
