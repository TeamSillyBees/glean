package asynqadapter

import (
	"github.com/samber/do"
	"github.com/teamsillybees/glean/internal/platform/logx"
	"github.com/teamsillybees/glean/internal/platform/task"
)

// Register 将任务队列 Publisher、Registry、Worker 和 Scheduler 注册到 DI 容器。
func Register(injector *do.Injector, cfg task.Config) {
	do.Provide(injector, func(i *do.Injector) (task.Registry, error) {
		logger := do.MustInvoke[*logx.Logger](i)
		return task.NewRegistry(
			task.RecoverMiddleware(logger),
			task.LoggingMiddleware(logger),
			task.BizKeyValidationMiddleware(),
			task.IdempotencyMiddleware(nil),
		), nil
	})
	do.Provide(injector, func(i *do.Injector) (task.Publisher, error) {
		logger := do.MustInvoke[*logx.Logger](i)
		return NewPublisher(cfg, logger)
	})
	do.Provide(injector, func(i *do.Injector) (task.Worker, error) {
		logger := do.MustInvoke[*logx.Logger](i)
		registry := do.MustInvoke[task.Registry](i)
		return NewWorker(cfg, registry, logger)
	})
	do.Provide(injector, func(i *do.Injector) (task.Scheduler, error) {
		logger := do.MustInvoke[*logx.Logger](i)
		return NewScheduler(cfg, logger)
	})
}
