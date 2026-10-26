package logx

import "go.uber.org/zap"

// NewNop 创建不输出日志的 logx Logger。
func NewNop() *Logger {
	return &Logger{
		console:   newNopConsoleLogger(),
		jsonl:     zap.NewNop(),
		cfg:       Config{Redact: RedactConfig{Enabled: true}}.Normalize(),
		lifecycle: &loggerLifecycle{},
	}
}
