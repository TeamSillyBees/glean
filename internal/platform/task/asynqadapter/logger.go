package asynqadapter

import (
	"context"
	"fmt"

	"github.com/teamsillybees/glean/internal/platform/logx"
)

// asynqLogger 将 Asynq 内部日志接口适配到 logx.Logger。
type asynqLogger struct {
	logger *logx.Logger
}

// newAsynqLogger 创建适配 Asynq Logger 接口的 logx 包装器。
func newAsynqLogger(logger *logx.Logger) asynqLogger {
	if logger == nil {
		logger = logx.NewNop()
	}
	return asynqLogger{logger: logger}
}

// Debug 输出 Asynq debug 日志。
func (l asynqLogger) Debug(args ...interface{}) {
	l.logger.Debug(context.Background(), fmt.Sprint(args...))
}

// Info 输出 Asynq info 日志。
func (l asynqLogger) Info(args ...interface{}) {
	l.logger.Info(context.Background(), fmt.Sprint(args...))
}

// Warn 输出 Asynq warn 日志。
func (l asynqLogger) Warn(args ...interface{}) {
	l.logger.Warn(context.Background(), fmt.Sprint(args...))
}

// Error 输出 Asynq error 日志。
func (l asynqLogger) Error(args ...interface{}) {
	l.logger.Error(context.Background(), fmt.Sprint(args...), fmt.Errorf("%s", fmt.Sprint(args...)))
}

// Fatal 输出 Asynq fatal 日志，交由进程外层生命周期管理决定是否退出。
func (l asynqLogger) Fatal(args ...interface{}) {
	l.logger.Error(context.Background(), fmt.Sprint(args...), fmt.Errorf("%s", fmt.Sprint(args...)))
}
