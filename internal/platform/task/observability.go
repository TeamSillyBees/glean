package task

import (
	"encoding/json"
)

const (
	// HeaderTaskName 是任务名称头。
	HeaderTaskName = "initra-task-name"
	// HeaderTaskDescription 是任务描述头。
	HeaderTaskDescription = "initra-task-description"
	// HeaderTaskModule 是任务模块头。
	HeaderTaskModule = "initra-task-module"
	// HeaderTaskOwner 是任务责任方头。
	HeaderTaskOwner = "initra-task-owner"
	// HeaderTaskScenario 是任务场景头。
	HeaderTaskScenario = "initra-task-scenario"
	// HeaderTaskBizKey 是任务业务幂等键头。
	HeaderTaskBizKey = "initra-task-biz-key"
	// HeaderTaskBizKeyRequired 是任务 biz_key 必填声明头。
	HeaderTaskBizKeyRequired = "initra-task-biz-key-required"
	// HeaderTaskSideEffect 是任务外部副作用声明头。
	HeaderTaskSideEffect = "initra-task-side-effect"
	// HeaderTaskCostLevel 是任务成本等级头。
	HeaderTaskCostLevel = "initra-task-cost-level"
	// HeaderTaskIdempotent 是任务业务幂等声明头。
	HeaderTaskIdempotent = "initra-task-idempotent"
	// HeaderTaskTraceID 是任务 trace id 头。
	HeaderTaskTraceID = "initra-task-trace-id"
	// HeaderTaskTenantID 是任务租户 id 头。
	HeaderTaskTenantID = "initra-task-tenant-id"
	// HeaderTaskCorrelationID 是任务关联 id 头。
	HeaderTaskCorrelationID = "initra-task-correlation-id"
	// HeaderTaskTags 是任务标签 JSON 头。
	HeaderTaskTags = "initra-task-tags"
)

// HeadersFromMeta 将任务元数据写入 header。
func HeadersFromMeta(meta TaskMeta, headers map[string]string) map[string]string {
	result := map[string]string{}
	for key, value := range headers {
		result[key] = value
	}
	putHeader(result, HeaderTaskName, meta.Name)
	putHeader(result, HeaderTaskDescription, meta.Description)
	putHeader(result, HeaderTaskModule, meta.Module)
	putHeader(result, HeaderTaskOwner, meta.Owner)
	putHeader(result, HeaderTaskScenario, meta.Scenario)
	putHeader(result, HeaderTaskBizKey, meta.BizKey)
	putHeader(result, HeaderTaskBizKeyRequired, boolHeader(meta.BizKeyRequired))
	putHeader(result, HeaderTaskSideEffect, boolHeader(meta.SideEffect))
	putHeader(result, HeaderTaskCostLevel, string(meta.CostLevel))
	putHeader(result, HeaderTaskIdempotent, boolHeader(meta.Idempotent))
	putHeader(result, HeaderTaskTraceID, meta.TraceID)
	putHeader(result, HeaderTaskTenantID, meta.TenantID)
	putHeader(result, HeaderTaskCorrelationID, meta.CorrelationID)
	if len(meta.Tags) > 0 {
		if data, err := json.Marshal(meta.Tags); err == nil {
			result[HeaderTaskTags] = string(data)
		}
	}
	return result
}

// MetaFromHeaders 从 header 还原任务元数据。
func MetaFromHeaders(headers map[string]string) TaskMeta {
	meta := TaskMeta{
		Name:           headers[HeaderTaskName],
		Description:    headers[HeaderTaskDescription],
		Module:         headers[HeaderTaskModule],
		Owner:          headers[HeaderTaskOwner],
		Scenario:       headers[HeaderTaskScenario],
		BizKey:         headers[HeaderTaskBizKey],
		BizKeyRequired: headers[HeaderTaskBizKeyRequired] == "true",
		SideEffect:     headers[HeaderTaskSideEffect] == "true",
		CostLevel:      CostLevel(headers[HeaderTaskCostLevel]),
		Idempotent:     headers[HeaderTaskIdempotent] == "true",
		TraceID:        headers[HeaderTaskTraceID],
		TenantID:       headers[HeaderTaskTenantID],
		CorrelationID:  headers[HeaderTaskCorrelationID],
	}
	if raw := headers[HeaderTaskTags]; raw != "" {
		_ = json.Unmarshal([]byte(raw), &meta.Tags)
	}
	return meta
}

func putHeader(headers map[string]string, key string, value string) {
	if value != "" {
		headers[key] = value
	}
}

func boolHeader(value bool) string {
	if value {
		return "true"
	}
	return ""
}
