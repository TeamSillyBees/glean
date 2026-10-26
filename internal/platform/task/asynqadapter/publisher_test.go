package asynqadapter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/logx"
	"github.com/teamsillybees/glean/internal/platform/redisx"
	"github.com/teamsillybees/glean/internal/platform/task"
)

func TestPublisherPublish(t *testing.T) {
	redisServer := miniredis.RunT(t)
	publisher, err := NewPublisher(testConfig(redisServer.Addr()), logx.NewNop())
	require.NoError(t, err)
	defer publisher.Close()

	result, err := publisher.Publish(context.Background(), task.Task{
		Type:    "demo:send_email",
		Payload: map[string]string{"userId": "1001"},
		Meta: task.TaskMeta{
			Name:       "发送示例邮件",
			Module:     "demo",
			Owner:      "platform",
			SideEffect: true,
			BizKey:     "demo:1001:send_email",
		},
	}, task.WithTaskID("task-1001"))

	require.NoError(t, err)
	require.Equal(t, "task-1001", result.TaskID)
	require.Equal(t, "demo:send_email", result.Type)
	require.Equal(t, task.QueueDefault, result.Queue)
	require.Equal(t, "demo:1001:send_email", result.BizKey)
	require.NotEmpty(t, result.State)
}

func TestPublisherMapsDuplicateTask(t *testing.T) {
	redisServer := miniredis.RunT(t)
	publisher, err := NewPublisher(testConfig(redisServer.Addr()), logx.NewNop())
	require.NoError(t, err)
	defer publisher.Close()

	item := task.Task{
		Type:    "demo:send_email",
		Payload: map[string]string{"userId": "1001"},
	}
	_, err = publisher.Publish(context.Background(), item, task.WithTaskID("task-duplicate"))
	require.NoError(t, err)

	_, err = publisher.Publish(context.Background(), item, task.WithTaskID("task-duplicate"))

	require.Error(t, err)
	require.True(t, errors.Is(err, task.ErrDuplicateTask))
}

// TestPublisherCheckReadiness 验证任务发布器能反映 Redis 后端状态。
func TestPublisherCheckReadiness(t *testing.T) {
	redisServer := miniredis.RunT(t)
	resolved, err := NewPublisher(testConfig(redisServer.Addr()), logx.NewNop())
	require.NoError(t, err)
	publisher := resolved.(*Publisher)
	defer publisher.Close()

	require.NoError(t, publisher.CheckReadiness(context.Background()))
	redisServer.Close()
	require.Error(t, publisher.CheckReadiness(context.Background()))
}

func TestWorkerConsumesPublishedTask(t *testing.T) {
	redisServer := miniredis.RunT(t)
	cfg := testConfig(redisServer.Addr())
	handled := make(chan task.Task, 1)
	registry := task.NewRegistry(task.BizKeyValidationMiddleware())
	err := registry.Register("demo:send_email",
		task.HandlerFunc(func(ctx context.Context, item task.Task) error {
			handled <- item
			return nil
		}),
		task.WithRegisterBizKeyRequired(true),
	)
	require.NoError(t, err)
	worker, err := NewWorker(cfg, registry, logx.NewNop())
	require.NoError(t, err)
	publisher, err := NewPublisher(cfg, logx.NewNop())
	require.NoError(t, err)
	defer publisher.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, worker.Start(ctx))
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer shutdownCancel()
		require.NoError(t, worker.Shutdown(shutdownCtx))
	}()

	_, err = publisher.Publish(context.Background(), task.Task{
		Type:    "demo:send_email",
		Payload: map[string]string{"userId": "1001"},
		Meta: task.TaskMeta{
			BizKey: "demo:1001:send_email",
		},
	})
	require.NoError(t, err)

	select {
	case item := <-handled:
		require.Equal(t, "demo:send_email", item.Type)
		require.Equal(t, "demo:1001:send_email", item.Meta.BizKey)
	case <-time.After(3 * time.Second):
		t.Fatal("task was not handled")
	}
}

func testConfig(addr string) task.Config {
	return task.Config{
		Enabled: true,
		Backend: task.BackendAsynq,
		Redis: redisx.Config{
			Enabled: true,
			Mode:    redisx.ModeStandalone,
			Addr:    addr,
			DB:      0,
		},
		Publisher: task.PublisherConfig{
			DefaultQueue:     task.QueueDefault,
			DefaultMaxRetry:  1,
			DefaultTimeout:   time.Minute,
			DefaultRetention: time.Hour,
			EnforceBizKey:    true,
		},
		Worker: task.WorkerConfig{
			Enabled:         true,
			Concurrency:     1,
			ShutdownTimeout: time.Second,
			Queues:          map[string]int{task.QueueDefault: 1},
		},
		Scheduler: task.SchedulerConfig{
			Enabled:      true,
			SyncInterval: time.Minute,
			Timezone:     "UTC",
		},
	}
}
