package task

import (
	"encoding/json"
	"fmt"
	"strings"
)

// CostLevel 表示任务执行成本等级。
type CostLevel string

const (
	// CostLevelLow 表示低成本任务。
	CostLevelLow CostLevel = "low"
	// CostLevelMedium 表示中等成本任务。
	CostLevelMedium CostLevel = "medium"
	// CostLevelHigh 表示高成本任务。
	CostLevelHigh CostLevel = "high"
)

// Task 是框架层任务定义，业务代码不直接依赖底层队列实现。
type Task struct {
	Type    string
	Payload any
	Meta    TaskMeta
	Options []TaskOption
}

// TaskMeta 描述任务治理、审计、观测和业务幂等元数据。
type TaskMeta struct {
	Name           string
	Description    string
	Module         string
	Owner          string
	Scenario       string
	BizKey         string
	BizKeyRequired bool
	SideEffect     bool
	CostLevel      CostLevel
	Idempotent     bool
	TraceID        string
	TenantID       string
	CorrelationID  string
	Tags           map[string]string
}

// Validate 校验任务类型和 payload 是否满足发布前约束。
func (t Task) Validate() error {
	if !ValidateTaskType(t.Type) {
		return fmt.Errorf("%w: task type 必须为 {module}:{action} 格式", ErrInvalidTask)
	}
	if _, err := MarshalPayload(t.Payload); err != nil {
		return err
	}
	return nil
}

// MarshalPayload 将任务 payload 编码为 JSON。
func MarshalPayload(payload any) ([]byte, error) {
	switch value := payload.(type) {
	case nil:
		return []byte("null"), nil
	case json.RawMessage:
		if !json.Valid(value) {
			return nil, fmt.Errorf("%w: payload 不是合法 JSON", ErrInvalidTask)
		}
		return append([]byte(nil), value...), nil
	default:
		data, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("%w: payload JSON 序列化失败: %v", ErrInvalidTask, err)
		}
		return data, nil
	}
}

// DecodePayload 将任务 payload 解码为指定类型。
func DecodePayload[T any](task Task) (T, error) {
	var zero T
	data, err := MarshalPayload(task.Payload)
	if err != nil {
		return zero, err
	}
	if err := json.Unmarshal(data, &zero); err != nil {
		return zero, fmt.Errorf("%w: payload JSON 解析失败: %v", ErrInvalidTask, err)
	}
	return zero, nil
}

// ValidateTaskType 校验任务类型是否为 {module}:{action} 格式。
func ValidateTaskType(taskType string) bool {
	parts := strings.Split(strings.TrimSpace(taskType), ":")
	if len(parts) != 2 {
		return false
	}
	return isTaskNamePart(parts[0]) && isTaskNamePart(parts[1])
}

func isTaskNamePart(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}
