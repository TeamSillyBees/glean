package monitor

import (
	"context"
	"time"

	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/task"
)

// TickTaskType 是一次有预算的监控推进任务类型；注册不代表启用真实采集。
const TickTaskType = "monitor:tick"

// RegisterTasks 接入项目 task 抽象，不依赖 Asynq；调用方按 NextWakeAt 显式安排后续任务。
func (s *Service) RegisterTasks(registry task.Registry) error {
	if registry == nil {
		return bizerrors.BadRequest("monitor task registry is required")
	}
	return registry.Register(TickTaskType, task.HandlerFunc(func(ctx context.Context, _ task.Task) error {
		next, err := s.NextWakeAt(ctx)
		if err != nil {
			return err
		}
		now := s.clock().UTC()
		if now.Before(next) {
			return nil
		}
		_, err = s.RunOnce(ctx, now)
		return err
	}), task.WithRegisterModule("monitor"), task.WithRegisterOwner("glean"), task.WithRegisterDescription("有预算地推进采集、审核和目录投影"), task.WithRegisterDefaultMaxRetry(0), task.WithRegisterDefaultTimeout(2*time.Minute), task.WithRegisterBizKeyRequired(true), task.WithRegisterSideEffect(true), task.WithRegisterCostLevel(task.CostLevelLow))
}

// NextWakeAt 返回调用方应安排的最早 UTC 时间；退避期间不要求立即重试。
func (s *Service) NextWakeAt(ctx context.Context) (time.Time, error) {
	state, err := s.load(ctx)
	if err != nil {
		return time.Time{}, err
	}
	run := activeRun(state)
	if run == nil {
		return state.Schedule.NextRunUTC, nil
	}
	now := s.clock().UTC()
	var next time.Time
	for name, progress := range run.Sources {
		if progress.Published || progress.Failed {
			continue
		}
		ready := state.Sources[name].NextRetryAt
		if ready.IsZero() || ready.Before(now) {
			ready = now
		}
		if next.IsZero() || ready.Before(next) {
			next = ready
		}
	}
	if next.IsZero() {
		return now, nil
	}
	return next, nil
}

// RegisterSchedule 显式注册分钟唤醒脉冲；持久化服务决定 72 小时 UTC 锚点、分页续跑和退避，不能用漂移的 @every72h 替代。
// 此阶段仅支持模拟服务；调用方必须在启动前验证默认关闭的 scheduler_enabled 配置。
func (s *Service) RegisterSchedule(scheduler task.Scheduler) error {
	if scheduler == nil {
		return bizerrors.BadRequest("monitor task scheduler is required")
	}
	if s.config.RealFetchEnabled {
		return bizerrors.BadRequest("automatic real harvesting is not enabled in this phase")
	}
	item := task.Task{Type: TickTaskType, Payload: struct{}{}, Meta: task.TaskMeta{Name: "Glean mock monitor tick", Description: "显式启用的模拟监控唤醒；真实采集关闭", Module: "monitor", Owner: "glean", Scenario: "mock-scheduler", BizKey: "monitor:tick:" + s.config.StateKey, BizKeyRequired: true, SideEffect: true, CostLevel: task.CostLevelLow, Idempotent: true}}
	return scheduler.Register("* * * * *", item, task.WithMaxRetry(0), task.WithTimeout(2*time.Minute), task.WithUnique(45*time.Second))
}
