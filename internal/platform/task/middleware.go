package task

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/teamsillybees/glean/internal/platform/logx"
)

// Middleware 包装任务处理器。
type Middleware func(Handler) Handler

// Chain 按声明顺序组合任务处理中间件。
func Chain(middleware ...Middleware) Middleware {
	return func(final Handler) Handler {
		if final == nil {
			final = HandlerFunc(func(context.Context, Task) error { return nil })
		}
		for i := len(middleware) - 1; i >= 0; i-- {
			if middleware[i] != nil {
				final = middleware[i](final)
			}
		}
		return final
	}
}

// RecoverMiddleware 捕获 handler panic 并转换为错误。
func RecoverMiddleware(logger *logx.Logger) Middleware {
	return func(next Handler) Handler {
		return HandlerFunc(func(ctx context.Context, task Task) (err error) {
			defer func() {
				if value := recover(); value != nil {
					err = fmt.Errorf("task panic: %v", value)
					if logger != nil {
						logger.Error(ctx, "task panic recovered", err,
							logx.String("task_type", task.Type),
							logx.String("biz_key", task.Meta.BizKey),
							logx.Any("panic", value),
						)
					}
				}
			}()
			return next.HandleTask(ctx, task)
		})
	}
}

// LoggingMiddleware 记录任务开始、成功、失败和耗时，不记录 payload。
func LoggingMiddleware(logger *logx.Logger) Middleware {
	return func(next Handler) Handler {
		return HandlerFunc(func(ctx context.Context, task Task) error {
			if logger == nil {
				return next.HandleTask(ctx, task)
			}
			start := time.Now()
			logger.Info(ctx, "task processing started", taskLogFields(task, 0)...)
			err := next.HandleTask(ctx, task)
			duration := time.Since(start)
			if err != nil {
				logger.Error(ctx, "task processing failed", err, taskLogFields(task, duration)...)
				return err
			}
			logger.Info(ctx, "task processing succeeded", taskLogFields(task, duration)...)
			return nil
		})
	}
}

// TaskProcessMetric 描述一次任务处理结果，供 metrics adapter 使用。
type TaskProcessMetric struct {
	TaskType   string
	Queue      string
	Module     string
	Result     string
	Duration   time.Duration
	OccurredAt time.Time
}

// MetricsRecorder 预留任务指标记录接口。
type MetricsRecorder interface {
	RecordTaskProcessed(ctx context.Context, metric TaskProcessMetric)
}

// MetricsMiddleware 预留任务处理指标记录能力。
func MetricsMiddleware(recorder MetricsRecorder) Middleware {
	return func(next Handler) Handler {
		return HandlerFunc(func(ctx context.Context, task Task) error {
			start := time.Now()
			err := next.HandleTask(ctx, task)
			if recorder != nil {
				result := "success"
				if err != nil {
					result = "failed"
				}
				recorder.RecordTaskProcessed(ctx, TaskProcessMetric{
					TaskType:   task.Type,
					Module:     task.Meta.Module,
					Result:     result,
					Duration:   time.Since(start),
					OccurredAt: time.Now(),
				})
			}
			return err
		})
	}
}

// TaskTracer 预留任务链路追踪接口。
type TaskTracer interface {
	StartTask(ctx context.Context, task Task) (context.Context, func(error))
}

// TracingMiddleware 预留任务链路追踪能力。
func TracingMiddleware(tracer TaskTracer) Middleware {
	return func(next Handler) Handler {
		return HandlerFunc(func(ctx context.Context, task Task) error {
			finish := func(error) {}
			if tracer != nil {
				ctx, finish = tracer.StartTask(ctx, task)
			}
			err := next.HandleTask(ctx, task)
			finish(err)
			return err
		})
	}
}

// BizKeyValidationMiddleware 校验声明必需 biz_key 的任务。
func BizKeyValidationMiddleware() Middleware {
	return func(next Handler) Handler {
		return HandlerFunc(func(ctx context.Context, task Task) error {
			if requiresBizKey(task.Meta) && strings.TrimSpace(task.Meta.BizKey) == "" {
				return fmt.Errorf("%w: task type %s", ErrMissingBizKey, task.Type)
			}
			return next.HandleTask(ctx, task)
		})
	}
}

// IdempotencyChecker 预留业务幂等检查接口。
type IdempotencyChecker interface {
	CheckTask(ctx context.Context, task Task) error
}

// IdempotencyMiddleware 预留幂等检查能力；第一版由业务侧实现具体幂等。
func IdempotencyMiddleware(checker IdempotencyChecker) Middleware {
	return func(next Handler) Handler {
		return HandlerFunc(func(ctx context.Context, task Task) error {
			if checker != nil {
				if err := checker.CheckTask(ctx, task); err != nil {
					return err
				}
			}
			return next.HandleTask(ctx, task)
		})
	}
}

func taskLogFields(task Task, duration time.Duration) []logx.Field {
	fields := []logx.Field{
		logx.String("task_type", task.Type),
		logx.String("task_name", task.Meta.Name),
		logx.String("biz_key", task.Meta.BizKey),
		logx.String("module", task.Meta.Module),
		logx.String("owner", task.Meta.Owner),
		logx.String("cost_level", string(task.Meta.CostLevel)),
		logx.String("trace_id", task.Meta.TraceID),
		logx.String("tenant_id", task.Meta.TenantID),
		logx.String("correlation_id", task.Meta.CorrelationID),
	}
	if duration > 0 {
		fields = append(fields, logx.Int64("duration_ms", duration.Milliseconds()))
	}
	return fields
}
