package asynqadapter

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/teamsillybees/glean/internal/platform/logx"
	"github.com/teamsillybees/glean/internal/platform/task"
)

// Scheduler 是基于 Asynq PeriodicTaskManager 的周期任务调度器。
type Scheduler struct {
	manager  *asynq.PeriodicTaskManager
	provider *staticConfigProvider
	cfg      task.Config
	started  atomic.Bool
	once     sync.Once
	done     chan struct{}
}

// NewScheduler 使用 task.Config 自建 Redis 连接并创建 Scheduler。
func NewScheduler(cfg task.Config, logger *logx.Logger) (task.Scheduler, error) {
	cfg = cfg.Normalize()
	if !cfg.Enabled || !cfg.Scheduler.Enabled {
		return task.NewDisabledScheduler(), nil
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	connOpt, err := redisConnOpt(cfg.Redis)
	if err != nil {
		return nil, err
	}
	provider := &staticConfigProvider{}
	manager, err := asynq.NewPeriodicTaskManager(asynq.PeriodicTaskManagerOpts{
		RedisConnOpt:               connOpt,
		PeriodicTaskConfigProvider: provider,
		SchedulerOpts:              schedulerOpts(cfg.Scheduler, logger),
		SyncInterval:               cfg.Scheduler.SyncInterval,
	})
	if err != nil {
		return nil, err
	}
	return &Scheduler{manager: manager, provider: provider, cfg: cfg, done: make(chan struct{})}, nil
}

// NewSchedulerFromRedisClient 使用外部 Redis 连接创建 Scheduler。
func NewSchedulerFromRedisClient(client redis.UniversalClient, cfg task.Config, logger *logx.Logger) (task.Scheduler, error) {
	cfg = cfg.Normalize()
	if !cfg.Enabled || !cfg.Scheduler.Enabled {
		return task.NewDisabledScheduler(), nil
	}
	if client == nil {
		return nil, fmt.Errorf("%w: redis client 不能为空", task.ErrInvalidTask)
	}
	if err := cfg.Scheduler.Validate(); err != nil {
		return nil, err
	}
	provider := &staticConfigProvider{}
	manager, err := asynq.NewPeriodicTaskManager(asynq.PeriodicTaskManagerOpts{
		RedisUniversalClient:       client,
		PeriodicTaskConfigProvider: provider,
		SchedulerOpts:              schedulerOpts(cfg.Scheduler, logger),
		SyncInterval:               cfg.Scheduler.SyncInterval,
	})
	if err != nil {
		return nil, err
	}
	return &Scheduler{manager: manager, provider: provider, cfg: cfg, done: make(chan struct{})}, nil
}

// Register 注册静态周期任务。
func (s *Scheduler) Register(cron string, item task.Task, opts ...task.ScheduleOption) error {
	if s == nil || s.manager == nil {
		return task.ErrDisabled
	}
	if s.started.Load() {
		return fmt.Errorf("%w: scheduler 已启动，第一版仅支持启动前静态注册", task.ErrInvalidTask)
	}
	if cron == "" {
		return fmt.Errorf("%w: cron 不能为空", task.ErrInvalidTask)
	}
	publishOpts := make([]task.PublishOption, 0, len(opts))
	publishOpts = append(publishOpts, opts...)
	resolved, err := task.ResolvePublishOptions(s.cfg.Publisher, item, publishOpts...)
	if err != nil {
		return err
	}
	item.Meta.BizKey = resolved.BizKey
	payload, err := task.MarshalPayload(item.Payload)
	if err != nil {
		return err
	}
	headers := task.HeadersFromMeta(item.Meta, resolved.Headers)
	s.provider.add(&asynq.PeriodicTaskConfig{
		Cronspec: cron,
		Task:     asynq.NewTaskWithHeaders(item.Type, payload, headers),
		Opts:     asynqOptions(resolved),
	})
	return nil
}

// Start 启动周期任务调度器，并在 ctx 取消时关闭。
func (s *Scheduler) Start(ctx context.Context) error {
	if s == nil || s.manager == nil {
		return task.ErrDisabled
	}
	if err := s.manager.Start(); err != nil {
		return err
	}
	s.started.Store(true)
	go func() {
		<-ctx.Done()
		_ = s.Shutdown(context.Background())
	}()
	return nil
}

// Shutdown 优雅关闭周期任务调度器。
func (s *Scheduler) Shutdown(ctx context.Context) error {
	if s == nil || s.manager == nil {
		return nil
	}
	s.once.Do(func() {
		go func() {
			s.manager.Shutdown()
			close(s.done)
		}()
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return nil
	}
}

type staticConfigProvider struct {
	mu      sync.RWMutex
	configs []*asynq.PeriodicTaskConfig
}

func (p *staticConfigProvider) add(config *asynq.PeriodicTaskConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.configs = append(p.configs, config)
}

// GetConfigs 返回当前周期任务配置的并发安全副本，供 Asynq 调度器读取。
func (p *staticConfigProvider) GetConfigs() ([]*asynq.PeriodicTaskConfig, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]*asynq.PeriodicTaskConfig, len(p.configs))
	copy(result, p.configs)
	return result, nil
}

func schedulerOpts(cfg task.SchedulerConfig, logger *logx.Logger) *asynq.SchedulerOpts {
	location, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		location = time.UTC
	}
	return &asynq.SchedulerOpts{
		Logger:   newAsynqLogger(logger),
		Location: location,
	}
}
