package task

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Handler 处理单个任务。
type Handler interface {
	HandleTask(ctx context.Context, task Task) error
}

// HandlerFunc 将函数适配为任务处理器。
type HandlerFunc func(ctx context.Context, task Task) error

// HandleTask 执行任务处理函数。
func (fn HandlerFunc) HandleTask(ctx context.Context, task Task) error {
	return fn(ctx, task)
}

// Registry 注册任务类型和处理器。
type Registry interface {
	Register(taskType string, handler Handler, opts ...RegisterOption) error
	Handler(taskType string) (Handler, bool)
	Registrations() []Registration
}

// Registration 描述一个已注册任务处理器。
type Registration struct {
	TaskType string
	Handler  Handler
	Options  RegisterOptions
}

// RegisterOptions 描述任务处理器注册元数据。
type RegisterOptions struct {
	Module          string
	Owner           string
	Description     string
	BizKeyRequired  bool
	SideEffect      bool
	CostLevel       CostLevel
	DefaultQueue    string
	DefaultMaxRetry int
	DefaultTimeout  time.Duration
}

// RegisterOption 修改任务处理器注册选项。
type RegisterOption func(*RegisterOptions)

// WithRegisterModule 指定任务所属模块。
func WithRegisterModule(module string) RegisterOption {
	return func(o *RegisterOptions) {
		o.Module = strings.TrimSpace(module)
	}
}

// WithRegisterOwner 指定任务责任团队或责任人。
func WithRegisterOwner(owner string) RegisterOption {
	return func(o *RegisterOptions) {
		o.Owner = strings.TrimSpace(owner)
	}
}

// WithRegisterDescription 指定任务说明。
func WithRegisterDescription(description string) RegisterOption {
	return func(o *RegisterOptions) {
		o.Description = strings.TrimSpace(description)
	}
}

// WithRegisterBizKeyRequired 声明该任务类型必须携带 biz_key。
func WithRegisterBizKeyRequired(required bool) RegisterOption {
	return func(o *RegisterOptions) {
		o.BizKeyRequired = required
	}
}

// WithRegisterSideEffect 声明该任务类型会产生外部副作用。
func WithRegisterSideEffect(sideEffect bool) RegisterOption {
	return func(o *RegisterOptions) {
		o.SideEffect = sideEffect
	}
}

// WithRegisterCostLevel 指定该任务类型成本等级。
func WithRegisterCostLevel(level CostLevel) RegisterOption {
	return func(o *RegisterOptions) {
		o.CostLevel = level
	}
}

// WithRegisterDefaultQueue 指定该任务类型建议默认队列。
func WithRegisterDefaultQueue(queue string) RegisterOption {
	return func(o *RegisterOptions) {
		o.DefaultQueue = strings.TrimSpace(queue)
	}
}

// WithRegisterDefaultMaxRetry 指定该任务类型建议最大重试次数。
func WithRegisterDefaultMaxRetry(maxRetry int) RegisterOption {
	return func(o *RegisterOptions) {
		o.DefaultMaxRetry = maxRetry
	}
}

// WithRegisterDefaultTimeout 指定该任务类型建议处理超时。
func WithRegisterDefaultTimeout(timeout time.Duration) RegisterOption {
	return func(o *RegisterOptions) {
		o.DefaultTimeout = timeout
	}
}

// DefaultRegistry 是内存任务处理器注册表。
type DefaultRegistry struct {
	mu      sync.RWMutex
	entries map[string]Registration
	mw      []Middleware
}

// NewRegistry 创建任务处理器注册表。
func NewRegistry(middleware ...Middleware) *DefaultRegistry {
	return &DefaultRegistry{
		entries: map[string]Registration{},
		mw:      append([]Middleware(nil), middleware...),
	}
}

// Register 注册任务处理器。
func (r *DefaultRegistry) Register(taskType string, handler Handler, opts ...RegisterOption) error {
	taskType = strings.TrimSpace(taskType)
	if !ValidateTaskType(taskType) {
		return fmt.Errorf("%w: task type 必须为 {module}:{action} 格式", ErrInvalidTask)
	}
	if handler == nil {
		return fmt.Errorf("%w: handler 不能为空", ErrInvalidTask)
	}
	options := RegisterOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	wrapped := HandlerFunc(func(ctx context.Context, task Task) error {
		return handler.HandleTask(ctx, task)
	})
	middleware := make([]Middleware, 0, len(r.mw)+1)
	middleware = append(middleware, registrationMetaMiddleware(options))
	middleware = append(middleware, r.mw...)
	wrappedHandler := Chain(middleware...)(wrapped)

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.entries[taskType]; ok {
		return fmt.Errorf("%w: task type %s 已注册", ErrInvalidTask, taskType)
	}
	r.entries[taskType] = Registration{
		TaskType: taskType,
		Handler:  wrappedHandler,
		Options:  options,
	}
	return nil
}

// Handler 返回指定任务类型的处理器。
func (r *DefaultRegistry) Handler(taskType string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.entries[taskType]
	return entry.Handler, ok
}

// Registrations 返回已注册处理器快照。
func (r *DefaultRegistry) Registrations() []Registration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Registration, 0, len(r.entries))
	for _, entry := range r.entries {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].TaskType < result[j].TaskType
	})
	return result
}

func mergeRegistrationMeta(options RegisterOptions, meta TaskMeta) TaskMeta {
	if meta.Module == "" {
		meta.Module = options.Module
	}
	if meta.Owner == "" {
		meta.Owner = options.Owner
	}
	if meta.Description == "" {
		meta.Description = options.Description
	}
	meta.BizKeyRequired = meta.BizKeyRequired || options.BizKeyRequired
	meta.SideEffect = meta.SideEffect || options.SideEffect
	if meta.CostLevel == "" {
		meta.CostLevel = options.CostLevel
	}
	return meta
}

func registrationMetaMiddleware(options RegisterOptions) Middleware {
	return func(next Handler) Handler {
		return HandlerFunc(func(ctx context.Context, task Task) error {
			task.Meta = mergeRegistrationMeta(options, task.Meta)
			return next.HandleTask(ctx, task)
		})
	}
}
