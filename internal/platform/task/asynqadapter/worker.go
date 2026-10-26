package asynqadapter

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/teamsillybees/glean/internal/platform/logx"
	"github.com/teamsillybees/glean/internal/platform/task"
)

// Worker 是基于 Asynq Server 的任务消费端。
type Worker struct {
	server   *asynq.Server
	registry task.Registry
	logger   *logx.Logger
	once     sync.Once
	done     chan struct{}
}

// NewWorker 使用 task.Config 自建 Redis 连接并创建 Worker。
func NewWorker(cfg task.Config, registry task.Registry, logger *logx.Logger) (task.Worker, error) {
	cfg = cfg.Normalize()
	if !cfg.Enabled || !cfg.Worker.Enabled {
		return task.NewDisabledWorker(), nil
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	connOpt, err := redisConnOpt(cfg.Redis)
	if err != nil {
		return nil, err
	}
	return &Worker{
		server:   asynq.NewServer(connOpt, serverConfig(cfg, logger)),
		registry: registry,
		logger:   logger,
		done:     make(chan struct{}),
	}, nil
}

// NewWorkerFromRedisClient 使用外部 Redis 连接创建 Worker。
func NewWorkerFromRedisClient(client redis.UniversalClient, cfg task.Config, registry task.Registry, logger *logx.Logger) (task.Worker, error) {
	cfg = cfg.Normalize()
	if !cfg.Enabled || !cfg.Worker.Enabled {
		return task.NewDisabledWorker(), nil
	}
	if client == nil {
		return nil, fmt.Errorf("%w: redis client 不能为空", task.ErrInvalidTask)
	}
	if err := cfg.Worker.Validate(); err != nil {
		return nil, err
	}
	return &Worker{
		server:   asynq.NewServerFromRedisClient(client, serverConfig(cfg, logger)),
		registry: registry,
		logger:   logger,
		done:     make(chan struct{}),
	}, nil
}

// Start 启动任务消费端，并在 ctx 取消时触发优雅关闭。
func (w *Worker) Start(ctx context.Context) error {
	if w == nil || w.server == nil {
		return task.ErrDisabled
	}
	mux, err := NewServeMux(w.registry)
	if err != nil {
		return err
	}
	w.logRegisteredTasks()
	if err := w.server.Start(mux); err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		_ = w.Shutdown(context.Background())
	}()
	return nil
}

// Stop 停止拉取新任务。
func (w *Worker) Stop(context.Context) error {
	if w == nil || w.server == nil {
		return nil
	}
	w.server.Stop()
	return nil
}

// Shutdown 优雅关闭任务消费端。
func (w *Worker) Shutdown(ctx context.Context) error {
	if w == nil || w.server == nil {
		return nil
	}
	w.once.Do(func() {
		go func() {
			w.server.Shutdown()
			close(w.done)
		}()
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-w.done:
		return nil
	}
}

func serverConfig(cfg task.Config, logger *logx.Logger) asynq.Config {
	if logger == nil {
		logger = logx.NewNop()
	}
	worker := cfg.Worker
	asynqCfg := asynq.Config{
		Concurrency:              worker.Concurrency,
		Queues:                   worker.Queues,
		StrictPriority:           worker.StrictPriority,
		ShutdownTimeout:          worker.ShutdownTimeout,
		HealthCheckInterval:      worker.HealthCheckInterval,
		DelayedTaskCheckInterval: worker.DelayedTaskCheckInterval,
		TaskCheckInterval:        worker.TaskCheckInterval,
		Logger:                   newAsynqLogger(logger),
		ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, raw *asynq.Task, err error) {
			retryCount, _ := asynq.GetRetryCount(ctx)
			maxRetry, _ := asynq.GetMaxRetry(ctx)
			queue, _ := asynq.GetQueueName(ctx)
			taskID, _ := asynq.GetTaskID(ctx)
			logger.Error(ctx, "task handler failed", err,
				logx.String("task_id", taskID),
				logx.String("task_type", raw.Type()),
				logx.String("queue", queue),
				logx.Int("retry_count", retryCount),
				logx.Int("max_retry", maxRetry),
			)
		}),
	}
	if retry := retryDelayFunc(worker.Retry); retry != nil {
		asynqCfg.RetryDelayFunc = retry
	}
	return asynqCfg
}

func (w *Worker) logRegisteredTasks() {
	if w.logger == nil || w.registry == nil {
		return
	}
	types := make([]string, 0)
	for _, entry := range w.registry.Registrations() {
		types = append(types, entry.TaskType)
	}
	w.logger.Info(context.Background(), "task worker registered handlers", logx.String("task_types", strings.Join(types, ",")))
}
