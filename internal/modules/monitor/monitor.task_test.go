package monitor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/task"
)

type recordingScheduler struct {
	cron    string
	item    task.Task
	options []task.ScheduleOption
	starts  int
}

// Register 只记录注册参数，确保模块不会隐式启动调度器。
func (s *recordingScheduler) Register(cron string, item task.Task, options ...task.ScheduleOption) error {
	s.cron = cron
	s.item = item
	s.options = options
	return nil
}

// Start 记录生命周期启动调用。
func (s *recordingScheduler) Start(context.Context) error { s.starts++; return nil }

// Shutdown 满足调度器测试接口。
func (s *recordingScheduler) Shutdown(context.Context) error { return nil }

// TestMockScheduleUsesDurableAnchorAndNoQueueRetry 验证一分钟脉冲不等于一分钟扫描，真实运行仍按持久化 72 小时锚点推进。
func TestMockScheduleUsesDurableAnchorAndNoQueueRetry(t *testing.T) {
	service := offline(t, DemoSources(scenarioTime, false), nil, nil)
	now := scenarioTime
	service.clock = func() time.Time { return now }
	scheduler := &recordingScheduler{}
	require.NoError(t, service.RegisterSchedule(scheduler))
	require.Equal(t, "* * * * *", scheduler.cron)
	require.Zero(t, scheduler.starts)
	require.True(t, scheduler.item.Meta.Idempotent)
	require.True(t, scheduler.item.Meta.BizKeyRequired)
	options, err := task.ResolvePublishOptions(task.PublisherConfig{}, scheduler.item, scheduler.options...)
	require.NoError(t, err)
	require.Zero(t, options.MaxRetry)
	require.Equal(t, 3*time.Minute, options.Timeout)
	require.Equal(t, 45*time.Second, options.Unique)
	registry := task.NewRegistry()
	require.NoError(t, service.RegisterTasks(registry))
	require.Equal(t, 3*time.Minute, registry.Registrations()[0].Options.DefaultTimeout)
	handler, ok := registry.Handler(TickTaskType)
	require.True(t, ok)
	require.NoError(t, handler.HandleTask(context.Background(), scheduler.item))
	snapshot, err := service.Snapshot(context.Background())
	require.NoError(t, err)
	require.Len(t, snapshot.Runs, 1)
	before, err := service.load(context.Background())
	require.NoError(t, err)
	now = now.Add(time.Minute)
	require.NoError(t, handler.HandleTask(context.Background(), scheduler.item))
	after, err := service.load(context.Background())
	require.NoError(t, err)
	require.Equal(t, before.revision, after.revision, "not-due pulses must not churn persistent state")
	now = snapshot.Schedule.NextRunUTC
	require.NoError(t, handler.HandleTask(context.Background(), scheduler.item))
	snapshot, err = service.Snapshot(context.Background())
	require.NoError(t, err)
	require.Len(t, snapshot.Runs, 2)
	require.Equal(t, now, snapshot.Runs[1].FixedEnd)
}

// TestScheduleRequiresAuditAndEnabledDependencies 验证真实自动调度可执行，但显式禁用依赖和缺失审计操作人不被绕过。
func TestScheduleRequiresAuditAndEnabledDependencies(t *testing.T) {
	service := offline(t, DemoSources(scenarioTime, false), nil, nil)
	require.Error(t, service.RegisterSchedule(nil))
	require.Error(t, service.RegisterTasks(nil))
	require.ErrorIs(t, service.RegisterSchedule(task.NewDisabledScheduler()), task.ErrDisabled)
	service.config.RealFetchEnabled = true
	require.NoError(t, service.RegisterSchedule(&recordingScheduler{}))
	service.requireOperator = true
	require.ErrorContains(t, service.RegisterSchedule(&recordingScheduler{}), "operator")
}
