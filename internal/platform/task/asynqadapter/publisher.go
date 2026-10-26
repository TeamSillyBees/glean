package asynqadapter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/teamsillybees/glean/internal/platform/logx"
	"github.com/teamsillybees/glean/internal/platform/task"
)

// Publisher 是基于 Asynq Client 的任务发布器。
type Publisher struct {
	client *asynq.Client
	cfg    task.Config
	logger *logx.Logger
	shared bool
}

// NewPublisher 使用 task.Config 自建 Redis 连接并创建 Publisher。
func NewPublisher(cfg task.Config, logger *logx.Logger) (task.Publisher, error) {
	cfg = cfg.Normalize()
	if !cfg.Enabled {
		return task.NewDisabledPublisher(), nil
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	connOpt, err := redisConnOpt(cfg.Redis)
	if err != nil {
		return nil, err
	}
	return &Publisher{
		client: asynq.NewClient(connOpt),
		cfg:    cfg,
		logger: logger,
	}, nil
}

// NewPublisherFromRedisClient 使用外部 Redis 连接创建 Publisher。
func NewPublisherFromRedisClient(client redis.UniversalClient, cfg task.Config, logger *logx.Logger) (task.Publisher, error) {
	cfg = cfg.Normalize()
	if !cfg.Enabled {
		return task.NewDisabledPublisher(), nil
	}
	if client == nil {
		return nil, fmt.Errorf("%w: redis client 不能为空", task.ErrInvalidTask)
	}
	if err := cfg.Publisher.Validate(); err != nil {
		return nil, err
	}
	return &Publisher{
		client: asynq.NewClientFromRedisClient(client),
		cfg:    cfg,
		logger: logger,
		shared: true,
	}, nil
}

// Publish 发布立即任务。
func (p *Publisher) Publish(ctx context.Context, item task.Task, opts ...task.PublishOption) (*task.PublishResult, error) {
	return p.publish(ctx, item, nil, opts...)
}

// PublishAt 发布指定时间任务。
func (p *Publisher) PublishAt(ctx context.Context, item task.Task, at time.Time, opts ...task.PublishOption) (*task.PublishResult, error) {
	if at.IsZero() {
		return nil, fmt.Errorf("%w: process_at 不能为空", task.ErrInvalidTask)
	}
	return p.publish(ctx, item, []asynq.Option{asynq.ProcessAt(at)}, opts...)
}

// PublishIn 发布延迟任务。
func (p *Publisher) PublishIn(ctx context.Context, item task.Task, delay time.Duration, opts ...task.PublishOption) (*task.PublishResult, error) {
	if delay < 0 {
		return nil, fmt.Errorf("%w: delay 不能为负数", task.ErrInvalidTask)
	}
	return p.publish(ctx, item, []asynq.Option{asynq.ProcessIn(delay)}, opts...)
}

// Close 关闭由 Publisher 自建的 Redis 连接。
func (p *Publisher) Close() error {
	if p == nil || p.client == nil || p.shared {
		return nil
	}
	return p.client.Close()
}

// CheckReadiness 检查任务队列 Redis 后端是否可用。
func (p *Publisher) CheckReadiness(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || p.client == nil {
		return task.ErrDisabled
	}
	if err := p.client.Ping(); err != nil {
		return fmt.Errorf("task backend ping failed: %w", err)
	}
	return nil
}

func (p *Publisher) publish(ctx context.Context, item task.Task, extra []asynq.Option, opts ...task.PublishOption) (*task.PublishResult, error) {
	if p == nil || p.client == nil {
		return nil, task.ErrDisabled
	}
	resolved, err := task.ResolvePublishOptions(p.cfg.Publisher, item, opts...)
	if err != nil {
		return nil, err
	}
	item.Meta.BizKey = resolved.BizKey
	payload, err := task.MarshalPayload(item.Payload)
	if err != nil {
		return nil, err
	}
	headers := task.HeadersFromMeta(item.Meta, resolved.Headers)
	asynqTask := asynq.NewTaskWithHeaders(item.Type, payload, headers)
	asynqOpts := append(asynqOptions(resolved), extra...)
	info, err := p.client.EnqueueContext(ctx, asynqTask, asynqOpts...)
	if err != nil {
		return nil, mapPublishError(err)
	}
	result := &task.PublishResult{
		TaskID:    info.ID,
		Type:      info.Type,
		Queue:     info.Queue,
		State:     info.State.String(),
		ProcessAt: info.NextProcessAt,
		BizKey:    resolved.BizKey,
	}
	if p.logger != nil {
		p.logger.Info(ctx, "task published",
			logx.String("task_id", result.TaskID),
			logx.String("task_type", result.Type),
			logx.String("queue", result.Queue),
			logx.String("state", result.State),
			logx.String("biz_key", result.BizKey),
		)
	}
	return result, nil
}

func mapPublishError(err error) error {
	switch {
	case errors.Is(err, asynq.ErrDuplicateTask), errors.Is(err, asynq.ErrTaskIDConflict):
		return fmt.Errorf("%w: %v", task.ErrDuplicateTask, err)
	default:
		return fmt.Errorf("%w: %v", task.ErrPublishFailed, err)
	}
}
