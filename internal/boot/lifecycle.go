package boot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/teamsillybees/glean/internal/platform/startup"
)

// Run 启动 HTTP 服务并在收到上游取消信号后优雅关闭。
func (a *Application) Run(ctx context.Context) error {
	if a.AccessControl != nil {
		if err := a.AccessControl.Start(ctx); err != nil {
			return errors.Join(err, a.shutdownWithTimeout())
		}
	}
	if err := a.startTaskRunners(ctx); err != nil {
		return errors.Join(err, a.shutdownWithTimeout())
	}

	errCh := make(chan error, 1)

	startup.Print(os.Stdout, newStartupInfo(a.Config, a.Server.Addr))

	go func() {
		if err := a.Server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		return a.shutdownWithTimeout()
	case err := <-errCh:
		return errors.Join(err, a.shutdownWithTimeout())
	}
}

func (a *Application) startTaskRunners(ctx context.Context) error {
	if a.Worker != nil {
		if err := a.Worker.Start(ctx); err != nil {
			return fmt.Errorf("start task worker: %w", err)
		}
		a.workerStarted = true
	}
	if a.Scheduler != nil {
		if err := a.Scheduler.Start(ctx); err != nil {
			return fmt.Errorf("start task scheduler: %w", err)
		}
		a.schedulerStarted = true
	}
	return nil
}

func (a *Application) shutdownWithTimeout() error {
	timeout := a.Config.Server.ShutdownTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return a.Shutdown(ctx)
}

// Shutdown 先停止 Worker 拉取，再并发排空服务组件，最后关闭底层资源。
func (a *Application) Shutdown(ctx context.Context) error {
	var errs []error

	if a.Worker != nil && a.workerStarted {
		if err := a.Worker.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("stop task worker: %w", err))
		}
	}

	shutdownResults := make(chan error, 3)
	shutdownCount := 0
	runShutdown := func(operation string, shutdown func() error) {
		shutdownCount++
		go func() {
			if err := shutdown(); err != nil {
				shutdownResults <- fmt.Errorf("%s: %w", operation, err)
				return
			}
			shutdownResults <- nil
		}()
	}

	if a.Server != nil {
		runShutdown("shutdown http server", func() error {
			return a.Server.Shutdown(ctx)
		})
	}
	if a.Scheduler != nil && a.schedulerStarted {
		runShutdown("shutdown task scheduler", func() error {
			return a.Scheduler.Shutdown(ctx)
		})
	}
	if a.Worker != nil && a.workerStarted {
		runShutdown("shutdown task worker", func() error {
			return a.Worker.Shutdown(ctx)
		})
	}

	for range shutdownCount {
		if err := <-shutdownResults; err != nil {
			errs = append(errs, err)
		}
	}

	if a.AccessControl != nil {
		if err := a.AccessControl.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close access control subscriber: %w", err))
		}
	}
	if a.DB != nil {
		if err := a.DB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close database: %w", err))
		}
	}
	if a.Publisher != nil {
		if err := a.Publisher.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close task publisher: %w", err))
		}
	}
	if a.Redis != nil {
		if err := a.Redis.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close redis: %w", err))
		}
	}
	if a.Logger != nil {
		if err := a.Logger.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close logger: %w", err))
		}
	}

	return errors.Join(errs...)
}
