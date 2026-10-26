package boot

import (
	"context"
	"testing"
	"time"

	"github.com/samber/do"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/modules/monitor"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/task"
)

// monitorSchedulerRecorder 记录注册，不启动 Redis 或任何计时器。
type monitorSchedulerRecorder struct {
	registrations int
	starts        int
	cron          string
}

func (s *monitorSchedulerRecorder) Register(cron string, _ task.Task, _ ...task.ScheduleOption) error {
	s.registrations++
	s.cron = cron
	return nil
}
func (s *monitorSchedulerRecorder) Start(context.Context) error    { s.starts++; return nil }
func (s *monitorSchedulerRecorder) Shutdown(context.Context) error { return nil }

// TestMonitorConfigDefaultsOff 验证新增开关默认关闭，启用时必须具备完整任务基础设施。
func TestMonitorConfigDefaultsOff(t *testing.T) {
	require.Equal(t, false, configDefaults()["monitor.scheduler_enabled"])
	cfg := validConfigForValidation()
	require.False(t, cfg.Monitor.SchedulerEnabled)
	require.NoError(t, cfg.Validate())
	cfg.Monitor.SchedulerEnabled = true
	require.ErrorContains(t, cfg.Validate(), "monitor.scheduler_enabled")
	cfg.Task.Enabled = true
	require.ErrorContains(t, cfg.Validate(), "monitor.scheduler_enabled")
	cfg.Task.Worker.Enabled = true
	require.ErrorContains(t, cfg.Validate(), "monitor.scheduler_enabled")
	cfg.Task.Scheduler.Enabled = true
	cfg.Server.ShutdownTimeout = time.Minute
	require.NoError(t, cfg.Validate())
}

// TestDisabledMonitorDoesNotResolveDependencies 验证关闭调度时不解析监控、队列或调度器依赖。
func TestDisabledMonitorDoesNotResolveDependencies(t *testing.T) {
	injector := do.New()
	resolved := 0
	do.Provide(injector, func(*do.Injector) (*monitor.Service, error) { resolved++; panic("disabled monitor must not resolve") })
	do.Provide(injector, func(*do.Injector) (task.Registry, error) { resolved++; panic("disabled registry must not resolve") })
	do.Provide(injector, func(*do.Injector) (task.Scheduler, error) { resolved++; panic("disabled scheduler must not resolve") })
	require.NoError(t, registerMonitorTasks(injector, &Config{}))
	require.Zero(t, resolved)
	cfg := &Config{Monitor: MonitorConfig{SchedulerEnabled: true}}
	require.ErrorContains(t, registerMonitorTasks(injector, cfg), "requires enabled")
	require.Zero(t, resolved)
}

// TestMonitorRegistrationPrecedesWorker 验证启动层先注册处理器与模拟脉冲，再解析 Worker，且注册本身不执行任务。
func TestMonitorRegistrationPrecedesWorker(t *testing.T) {
	ids, err := idgen.NewGenerator(40)
	require.NoError(t, err)
	service, err := monitor.NewOfflineService(ids, monitor.DefaultConfig(), monitor.DemoSources(time.Now().UTC(), false), monitor.MockReviewer{}, monitor.MockPublisher{})
	require.NoError(t, err)
	registry := task.NewRegistry()
	scheduler := &monitorSchedulerRecorder{}
	injector := do.New()
	do.ProvideValue(injector, service)
	do.ProvideValue[task.Registry](injector, registry)
	do.ProvideValue[task.Scheduler](injector, scheduler)
	workerResolved := false
	do.Provide(injector, func(*do.Injector) (task.Worker, error) {
		_, found := registry.Handler(monitor.TickTaskType)
		require.True(t, found, "monitor handler must exist before worker resolution")
		workerResolved = true
		return &fakeWorker{}, nil
	})
	cfg := &Config{Monitor: MonitorConfig{SchedulerEnabled: true}, Task: task.Config{Enabled: true, Worker: task.WorkerConfig{Enabled: true}, Scheduler: task.SchedulerConfig{Enabled: true}}}
	require.NoError(t, registerMonitorTasks(injector, cfg))
	require.False(t, workerResolved)
	require.Equal(t, 1, scheduler.registrations)
	require.Equal(t, "* * * * *", scheduler.cron)
	require.Zero(t, scheduler.starts)
	_, _, err = resolveTaskRunners(injector, cfg.Task)
	require.NoError(t, err)
	require.True(t, workerResolved)
	require.Zero(t, scheduler.starts)
}
