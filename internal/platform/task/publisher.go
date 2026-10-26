package task

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Publisher 发布立即、延迟和指定时间任务。
type Publisher interface {
	Publish(ctx context.Context, task Task, opts ...PublishOption) (*PublishResult, error)
	PublishAt(ctx context.Context, task Task, at time.Time, opts ...PublishOption) (*PublishResult, error)
	PublishIn(ctx context.Context, task Task, delay time.Duration, opts ...PublishOption) (*PublishResult, error)
	Close() error
}

// PublishResult 描述任务发布后的队列状态。
type PublishResult struct {
	TaskID    string
	Type      string
	Queue     string
	State     string
	ProcessAt time.Time
	BizKey    string
}

// PublishOption 修改单次任务发布选项。
type PublishOption func(*PublishOptions)

// TaskOption 是嵌入 Task 定义中的发布选项。
type TaskOption = PublishOption

// PublishOptions 是任务发布选项的解析结果。
type PublishOptions struct {
	Queue        string
	MaxRetry     int
	MaxRetrySet  bool
	Timeout      time.Duration
	TimeoutSet   bool
	Deadline     time.Time
	Retention    time.Duration
	RetentionSet bool
	Unique       time.Duration
	TaskID       string
	Headers      map[string]string
	BizKey       string
}

// WithQueue 指定任务队列。
func WithQueue(queue string) PublishOption {
	return func(o *PublishOptions) {
		o.Queue = strings.TrimSpace(queue)
	}
}

// WithMaxRetry 指定最大重试次数。
func WithMaxRetry(maxRetry int) PublishOption {
	return func(o *PublishOptions) {
		o.MaxRetry = maxRetry
		o.MaxRetrySet = true
	}
}

// WithTimeout 指定任务处理超时时间。
func WithTimeout(timeout time.Duration) PublishOption {
	return func(o *PublishOptions) {
		o.Timeout = timeout
		o.TimeoutSet = true
	}
}

// WithDeadline 指定任务处理截止时间。
func WithDeadline(deadline time.Time) PublishOption {
	return func(o *PublishOptions) {
		o.Deadline = deadline
	}
}

// WithRetention 指定任务成功后的保留时间。
func WithRetention(retention time.Duration) PublishOption {
	return func(o *PublishOptions) {
		o.Retention = retention
		o.RetentionSet = true
	}
}

// WithUnique 指定短期唯一约束 TTL；它不是长期业务幂等机制。
func WithUnique(ttl time.Duration) PublishOption {
	return func(o *PublishOptions) {
		o.Unique = ttl
	}
}

// WithTaskID 指定底层任务 ID；它不是完整业务幂等机制。
func WithTaskID(taskID string) PublishOption {
	return func(o *PublishOptions) {
		o.TaskID = strings.TrimSpace(taskID)
	}
}

// WithHeader 添加任务头，用于传递 trace 等非敏感元数据。
func WithHeader(key string, value string) PublishOption {
	return func(o *PublishOptions) {
		if o.Headers == nil {
			o.Headers = map[string]string{}
		}
		key = strings.TrimSpace(key)
		if key != "" {
			o.Headers[key] = value
		}
	}
}

// WithHeaders 批量添加任务头。
func WithHeaders(headers map[string]string) PublishOption {
	return func(o *PublishOptions) {
		if len(headers) == 0 {
			return
		}
		if o.Headers == nil {
			o.Headers = map[string]string{}
		}
		for key, value := range headers {
			key = strings.TrimSpace(key)
			if key != "" {
				o.Headers[key] = value
			}
		}
	}
}

// WithBizKey 指定业务幂等键。
func WithBizKey(bizKey string) PublishOption {
	return func(o *PublishOptions) {
		o.BizKey = strings.TrimSpace(bizKey)
	}
}

// ResolvePublishOptions 合并任务内置选项、调用选项和发布默认值。
func ResolvePublishOptions(cfg PublisherConfig, task Task, opts ...PublishOption) (PublishOptions, error) {
	if err := task.Validate(); err != nil {
		return PublishOptions{}, err
	}
	cfg = cfg.withDefaults()
	resolved := PublishOptions{
		Queue:     cfg.DefaultQueue,
		MaxRetry:  cfg.DefaultMaxRetry,
		Timeout:   cfg.DefaultTimeout,
		Retention: cfg.DefaultRetention,
		Headers:   map[string]string{},
		BizKey:    strings.TrimSpace(task.Meta.BizKey),
	}
	allOptions := make([]PublishOption, 0, len(task.Options)+len(opts))
	allOptions = append(allOptions, task.Options...)
	allOptions = append(allOptions, opts...)
	for _, opt := range allOptions {
		if opt != nil {
			opt(&resolved)
		}
	}
	if !resolved.MaxRetrySet {
		resolved.MaxRetry = cfg.DefaultMaxRetry
	}
	if !resolved.TimeoutSet {
		resolved.Timeout = cfg.DefaultTimeout
	}
	if !resolved.RetentionSet {
		resolved.Retention = cfg.DefaultRetention
	}
	if !ValidateQueueName(resolved.Queue) {
		return PublishOptions{}, fmt.Errorf("%w: queue %q 非法", ErrInvalidTask, resolved.Queue)
	}
	if resolved.MaxRetry < 0 {
		return PublishOptions{}, fmt.Errorf("%w: max retry 不能小于 0", ErrInvalidTask)
	}
	if resolved.Timeout < 0 || resolved.Retention < 0 || resolved.Unique < 0 {
		return PublishOptions{}, fmt.Errorf("%w: timeout、retention 和 unique 不能为负数", ErrInvalidTask)
	}
	if strings.TrimSpace(resolved.TaskID) == "" && resolved.TaskID != "" {
		return PublishOptions{}, fmt.Errorf("%w: task id 不能为空白字符", ErrInvalidTask)
	}
	if cfg.EnforceBizKey && requiresBizKey(task.Meta) && strings.TrimSpace(resolved.BizKey) == "" {
		return PublishOptions{}, fmt.Errorf("%w: task type %s", ErrMissingBizKey, task.Type)
	}
	return resolved, nil
}

func requiresBizKey(meta TaskMeta) bool {
	return meta.BizKeyRequired || meta.SideEffect || meta.CostLevel == CostLevelHigh
}
