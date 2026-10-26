package task

import (
	"context"
)

// Worker 消费已注册的异步任务。
type Worker interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Shutdown(ctx context.Context) error
}
