package task

import (
	"context"
	"time"
)

type disabledPublisher struct{}

// NewDisabledPublisher 返回未启用任务队列时的 Publisher。
func NewDisabledPublisher() Publisher {
	return disabledPublisher{}
}

// Publish 在任务队列禁用时拒绝立即发布并返回 ErrDisabled。
func (disabledPublisher) Publish(context.Context, Task, ...PublishOption) (*PublishResult, error) {
	return nil, ErrDisabled
}

// PublishAt 在任务队列禁用时拒绝定时发布并返回 ErrDisabled。
func (disabledPublisher) PublishAt(context.Context, Task, time.Time, ...PublishOption) (*PublishResult, error) {
	return nil, ErrDisabled
}

// PublishIn 在任务队列禁用时拒绝延迟发布并返回 ErrDisabled。
func (disabledPublisher) PublishIn(context.Context, Task, time.Duration, ...PublishOption) (*PublishResult, error) {
	return nil, ErrDisabled
}

// Close 在禁用发布器上执行无操作关闭。
func (disabledPublisher) Close() error {
	return nil
}

type disabledWorker struct{}

// NewDisabledWorker 返回未启用消费端时的 Worker。
func NewDisabledWorker() Worker {
	return disabledWorker{}
}

// Start 在消费端禁用时拒绝启动并返回 ErrDisabled。
func (disabledWorker) Start(context.Context) error {
	return ErrDisabled
}

// Stop 在禁用消费端上执行无操作停止。
func (disabledWorker) Stop(context.Context) error {
	return nil
}

// Shutdown 在禁用消费端上执行无操作关闭。
func (disabledWorker) Shutdown(context.Context) error {
	return nil
}

type disabledScheduler struct{}

// NewDisabledScheduler 返回未启用调度器时的 Scheduler。
func NewDisabledScheduler() Scheduler {
	return disabledScheduler{}
}

// Register 在调度器禁用时拒绝注册周期任务并返回 ErrDisabled。
func (disabledScheduler) Register(string, Task, ...ScheduleOption) error {
	return ErrDisabled
}

// Start 在调度器禁用时拒绝启动并返回 ErrDisabled。
func (disabledScheduler) Start(context.Context) error {
	return ErrDisabled
}

// Shutdown 在禁用调度器上执行无操作关闭。
func (disabledScheduler) Shutdown(context.Context) error {
	return nil
}
