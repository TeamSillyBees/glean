package boot

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/samber/do"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/logx"
	"github.com/teamsillybees/glean/internal/platform/task"
)

// fakeWorker 记录 Application 对任务消费端生命周期的调用。
type fakeWorker struct {
	startErr       error
	shutdownErr    error
	startCalls     int
	stopCalls      int
	shutdownCalls  int
	shutdownCalled chan struct{}
}

func (f *fakeWorker) Start(context.Context) error {
	f.startCalls++
	return f.startErr
}

func (f *fakeWorker) Stop(context.Context) error {
	f.stopCalls++
	return nil
}

func (f *fakeWorker) Shutdown(context.Context) error {
	f.shutdownCalls++
	if f.shutdownCalled != nil {
		close(f.shutdownCalled)
	}
	return f.shutdownErr
}

// fakeScheduler 记录 Application 对周期任务调度器生命周期的调用。
type fakeScheduler struct {
	startErr      error
	shutdownErr   error
	startCalls    int
	shutdownCalls int
}

func (f *fakeScheduler) Register(string, task.Task, ...task.ScheduleOption) error {
	return nil
}

func (f *fakeScheduler) Start(context.Context) error {
	f.startCalls++
	return f.startErr
}

func (f *fakeScheduler) Shutdown(context.Context) error {
	f.shutdownCalls++
	return f.shutdownErr
}

// TestApplicationRunManagesTaskRunners 验证 Run 启动任务组件并在 HTTP 启动失败时统一关闭。
func TestApplicationRunManagesTaskRunners(t *testing.T) {
	worker := &fakeWorker{}
	scheduler := &fakeScheduler{}
	app := newLifecycleTestApplication(worker, scheduler)

	err := app.Run(context.Background())

	require.Error(t, err)
	require.Equal(t, 1, worker.startCalls)
	require.Equal(t, 1, worker.stopCalls)
	require.Equal(t, 1, worker.shutdownCalls)
	require.Equal(t, 1, scheduler.startCalls)
	require.Equal(t, 1, scheduler.shutdownCalls)
}

// TestApplicationRunPropagatesSchedulerStartError 验证启动错误向上返回并回滚已启动的 Worker。
func TestApplicationRunPropagatesSchedulerStartError(t *testing.T) {
	worker := &fakeWorker{}
	scheduler := &fakeScheduler{startErr: errors.New("scheduler unavailable")}
	app := newLifecycleTestApplication(worker, scheduler)

	err := app.Run(context.Background())

	require.ErrorContains(t, err, "start task scheduler")
	require.Equal(t, 1, worker.startCalls)
	require.Equal(t, 1, worker.stopCalls)
	require.Equal(t, 1, worker.shutdownCalls)
	require.Equal(t, 1, scheduler.startCalls)
	require.Zero(t, scheduler.shutdownCalls)
}

// TestApplicationShutdownPropagatesRunnerError 验证任务组件关闭失败不会被生命周期吞掉。
func TestApplicationShutdownPropagatesRunnerError(t *testing.T) {
	worker := &fakeWorker{shutdownErr: errors.New("worker shutdown failed")}
	scheduler := &fakeScheduler{shutdownErr: errors.New("scheduler shutdown failed")}
	app := newLifecycleTestApplication(worker, scheduler)
	require.NoError(t, app.startTaskRunners(context.Background()))

	err := app.Shutdown(context.Background())

	require.ErrorContains(t, err, "shutdown task scheduler")
	require.ErrorContains(t, err, "shutdown task worker")
	require.Equal(t, 1, worker.shutdownCalls)
	require.Equal(t, 1, scheduler.shutdownCalls)
}

// TestApplicationShutdownDoesNotBlockWorkerBehindHTTP 验证活跃 HTTP 请求不会串行阻塞 Worker 排空。
func TestApplicationShutdownDoesNotBlockWorkerBehindHTTP(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	handlerStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	released := false
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(handlerStarted)
		<-releaseHandler
		w.WriteHeader(http.StatusNoContent)
	})}
	defer func() { _ = server.Close() }()
	defer func() {
		if !released {
			close(releaseHandler)
		}
	}()

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(listener)
	}()
	requestDone := make(chan error, 1)
	go func() {
		response, requestErr := http.Get("http://" + listener.Addr().String())
		if requestErr == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			requestErr = response.Body.Close()
		}
		requestDone <- requestErr
	}()

	select {
	case <-handlerStarted:
	case <-time.After(time.Second):
		t.Fatal("HTTP handler 未及时进入阻塞状态")
	}

	worker := &fakeWorker{shutdownCalled: make(chan struct{})}
	app := &Application{Server: server, Worker: worker, workerStarted: true}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- app.Shutdown(ctx)
	}()

	select {
	case <-worker.shutdownCalled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Worker Shutdown 被阻塞的 HTTP Shutdown 延迟")
	}

	close(releaseHandler)
	released = true
	require.NoError(t, <-shutdownDone)
	require.ErrorIs(t, <-serveDone, http.ErrServerClosed)
	require.NoError(t, <-requestDone)
	require.Equal(t, 1, worker.stopCalls)
	require.Equal(t, 1, worker.shutdownCalls)
}

// TestResolveTaskRunnersSkipsDisabledComponents 验证禁用配置不会解析惰性 provider 或创建连接。
func TestResolveTaskRunnersSkipsDisabledComponents(t *testing.T) {
	injector := do.New()
	workerResolved := false
	schedulerResolved := false
	do.Provide(injector, func(*do.Injector) (task.Worker, error) {
		workerResolved = true
		return &fakeWorker{}, nil
	})
	do.Provide(injector, func(*do.Injector) (task.Scheduler, error) {
		schedulerResolved = true
		return &fakeScheduler{}, nil
	})

	worker, scheduler, err := resolveTaskRunners(injector, task.Config{Enabled: true})

	require.NoError(t, err)
	require.Nil(t, worker)
	require.Nil(t, scheduler)
	require.False(t, workerResolved)
	require.False(t, schedulerResolved)
}

func newLifecycleTestApplication(worker task.Worker, scheduler task.Scheduler) *Application {
	return &Application{
		Config: &Config{Server: ServerConfig{ShutdownTimeout: time.Second}},
		Logger: logx.NewNop(),
		Server: &http.Server{
			Addr:    "invalid-address",
			Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		},
		Publisher: task.NewDisabledPublisher(),
		Worker:    worker,
		Scheduler: scheduler,
	}
}
