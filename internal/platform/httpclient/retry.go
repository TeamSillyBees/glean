package httpclient

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-resty/resty/v2"
)

type retryIdempotentContextKey struct{}

func defaultRetryStatusCodes() []int {
	return []int{
		http.StatusRequestTimeout,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	}
}

func defaultRetryMethods() []string {
	return []string{
		http.MethodGet,
		http.MethodHead,
		http.MethodPut,
		http.MethodDelete,
	}
}

func retryCondition(cfg RetryConfig) resty.RetryConditionFunc {
	statusCodes := intSet(cfg.RetryStatusCodes)
	methods := methodSet(cfg.RetryMethods)
	return func(resp *resty.Response, err error) bool {
		if resp == nil || resp.Request == nil {
			return false
		}
		var hookErr *requestHookError
		if errors.As(err, &hookErr) {
			return false
		}
		if !isRetryableMethod(resp.Request.Method, resp.Request.Context(), methods) {
			return false
		}
		if err != nil {
			return resp.RawResponse == nil
		}
		statusCode := resp.StatusCode()
		if _, ok := statusCodes[statusCode]; ok {
			return true
		}
		return cfg.RetryAll5xx && statusCode >= http.StatusInternalServerError && statusCode <= 599
	}
}

func isRetryableMethod(method string, ctx context.Context, methods map[string]struct{}) bool {
	normalized := strings.ToUpper(strings.TrimSpace(method))
	if _, ok := methods[normalized]; ok {
		return true
	}
	if normalized == http.MethodPost || normalized == http.MethodPatch {
		idempotent, _ := ctx.Value(retryIdempotentContextKey{}).(bool)
		return idempotent
	}
	return false
}

func intSet(values []int) map[int]struct{} {
	result := make(map[int]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func methodSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		method := strings.ToUpper(strings.TrimSpace(value))
		if method != "" {
			result[method] = struct{}{}
		}
	}
	return result
}
