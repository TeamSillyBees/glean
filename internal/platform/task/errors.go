package task

import "errors"

var (
	// ErrDisabled 表示任务队列能力未启用。
	ErrDisabled = errors.New("task queue is disabled")
	// ErrSkipRetry 表示当前错误不应触发重试。
	ErrSkipRetry = errors.New("task skip retry")
	// ErrRevoke 表示当前任务应被撤销且不进入重试或归档。
	ErrRevoke = errors.New("task revoked")
	// ErrInvalidTask 表示任务定义、payload 或选项非法。
	ErrInvalidTask = errors.New("invalid task")
	// ErrMissingBizKey 表示任务缺少必需的业务幂等键。
	ErrMissingBizKey = errors.New("missing task biz_key")
	// ErrDuplicateTask 表示任务因唯一约束或任务 ID 冲突被拒绝。
	ErrDuplicateTask = errors.New("duplicate task")
	// ErrPublishFailed 表示任务发布失败。
	ErrPublishFailed = errors.New("publish task failed")
)
