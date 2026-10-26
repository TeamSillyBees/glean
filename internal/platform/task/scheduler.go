package task

import "context"

// Scheduler 注册并运行周期性任务。
type Scheduler interface {
	Register(cron string, task Task, opts ...ScheduleOption) error
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

// ScheduleOption 修改周期任务发布选项。
type ScheduleOption = PublishOption
