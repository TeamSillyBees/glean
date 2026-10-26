package logx

import (
	"context"
	"strings"

	"github.com/teamsillybees/glean/internal/platform/requestctx"
	"go.uber.org/zap"
)

// consoleContextWhitelist 定义 console 输出允许保留的业务上下文字段。
var consoleContextWhitelist = map[string]struct{}{
	"user_id":    {},
	"tenant_id":  {},
	"order_id":   {},
	"request_id": {},
	"operation":  {},
	"provider":   {},
	"channel":    {},
	"method":     {},
	"path":       {},
	"status":     {},
	"task":       {},
	"task_type":  {},
	"task_name":  {},
	"queue":      {},
	"biz_key":    {},
}

// baseFields 从全局配置和请求上下文中提取所有日志基础字段。
func baseFields(ctx context.Context, cfg FieldsConfig) []Field {
	fields := make([]Field, 0, 8)
	appendString := func(key string, value string) {
		if strings.TrimSpace(value) != "" {
			fields = append(fields, zap.String(key, strings.TrimSpace(value)))
		}
	}
	appendString("service", cfg.Service)
	appendString("env", cfg.Env)
	appendString("version", cfg.Version)
	appendString("instance", cfg.Instance)
	if ctx != nil {
		values := requestctx.ValuesFromContext(ctx)
		appendString("trace_id", values.TraceID)
		appendString("request_id", values.RequestID)
		appendString("user_id", values.UserID)
		appendString("tenant_id", values.TenantID)
	}
	return fields
}

// consoleUserFields 只保留适合 console 输出的调用方字段。
func consoleUserFields(fields []Field, cfg RedactConfig) []Field {
	if len(fields) == 0 {
		return nil
	}
	filtered := make([]Field, 0, len(fields))
	for _, field := range fields {
		if _, ok := consoleContextWhitelist[field.Key]; ok {
			filtered = append(filtered, field)
		}
	}
	return RedactFields(filtered, cfg)
}
