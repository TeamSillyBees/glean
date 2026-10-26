package logx

import (
	"context"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger 是项目统一日志入口，内部按 console/jsonl 策略分别渲染字段。
type Logger struct {
	console   *consoleLogger
	jsonl     *zap.Logger
	cfg       Config
	lifecycle *loggerLifecycle
}

type loggerLifecycle struct {
	mu       sync.Mutex
	closed   bool
	closeErr error
	closers  []func()
}

// NewLogger 根据配置创建统一日志器。
func NewLogger(cfg Config) (*Logger, error) {
	cfg = cfg.Normalize()
	logger := &Logger{cfg: cfg}
	closers := make([]func(), 0, 2)
	if cfg.Console.Enabled {
		console, closeConsole, err := newConsoleLogger(cfg)
		if err != nil {
			return nil, err
		}
		logger.console = console
		closers = append(closers, closeConsole)
	}
	if cfg.JSONL.Enabled {
		jsonl, closeJSONL, err := newJSONLLogger(cfg)
		if err != nil {
			closeLoggerSinks(closers)
			return nil, err
		}
		logger.jsonl = jsonl
		closers = append(closers, closeJSONL)
	}
	logger.lifecycle = &loggerLifecycle{closers: closers}
	return logger, nil
}

// writeLevel 描述普通日志写入时使用的级别分支。
type writeLevel int

const (
	// debugLevel 表示 debug 级别普通日志。
	debugLevel writeLevel = iota
	// infoLevel 表示 info 级别普通日志。
	infoLevel
	// warnLevel 表示 warn 级别普通日志。
	warnLevel
)

// Debug 输出 debug 级别日志。
func (l *Logger) Debug(ctx context.Context, msg string, fields ...Field) {
	l.write(ctx, debugLevel, msg, RedactFields(fields, l.config().Redact)...)
}

// Info 输出 info 级别日志。
func (l *Logger) Info(ctx context.Context, msg string, fields ...Field) {
	l.write(ctx, infoLevel, msg, RedactFields(fields, l.config().Redact)...)
}

// Warn 输出 warn 级别日志。
func (l *Logger) Warn(ctx context.Context, msg string, fields ...Field) {
	l.write(ctx, warnLevel, msg, RedactFields(fields, l.config().Redact)...)
}

// Error 输出 error 级别日志，并按 console/jsonl 策略渲染错误字段。
func (l *Logger) Error(ctx context.Context, msg string, err error, fields ...Field) {
	if l == nil {
		return
	}
	cfg := l.config()
	info := ExtractError(err, StackFull, cfg.Redact)
	if info.TraceID == "" {
		for _, field := range baseFields(ctx, cfg.Fields) {
			if field.Key == "trace_id" {
				info.TraceID = field.String
				break
			}
		}
	}
	if l.console != nil {
		consoleInfo := info
		consoleInfo.Stacktrace = renderStack(info.Stacktrace, cfg.Console.Stack)
		allFields := append(baseFields(ctx, cfg.Fields), fields...)
		l.console.write(zapcore.ErrorLevel, msg, ConsoleErrorFields(consoleInfo, allFields, cfg.Console.Stack, cfg.Redact)...)
	}
	if l.jsonl != nil {
		jsonlInfo := info
		jsonlInfo.Stacktrace = renderStack(info.Stacktrace, cfg.JSONL.Stack)
		allFields := append(baseFields(ctx, cfg.Fields), fields...)
		l.jsonl.Error(msg, JSONLErrorFields(jsonlInfo, allFields, cfg.JSONL.Stack, cfg.Redact)...)
	}
}

// With 返回携带固定字段的派生日志器。
func (l *Logger) With(fields ...Field) *Logger {
	if l == nil {
		return NewNop()
	}
	return &Logger{
		console:   withConsoleFields(l.console, fields),
		jsonl:     withLoggerFields(l.jsonl, fields),
		cfg:       l.cfg,
		lifecycle: l.lifecycle,
	}
}

// Named 返回带名称的派生日志器。
func (l *Logger) Named(name string) *Logger {
	if l == nil {
		return NewNop()
	}
	return &Logger{
		console:   namedConsoleLogger(l.console, name),
		jsonl:     namedLogger(l.jsonl, name),
		cfg:       l.cfg,
		lifecycle: l.lifecycle,
	}
}

// Sync 刷新所有底层 logger，但不关闭共享输出目标。
func (l *Logger) Sync() error {
	if l == nil {
		return nil
	}
	if l.lifecycle == nil {
		return l.syncSinks()
	}
	return l.lifecycle.sync(l.syncSinks)
}

// Close 刷新并关闭共享输出目标；由同一 Logger 派生的实例共享一次关闭生命周期。
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	if l.lifecycle == nil {
		return l.syncSinks()
	}
	return l.lifecycle.close(l.syncSinks)
}

// Shutdown 是 Close 的语义化别名。
func (l *Logger) Shutdown() error {
	return l.Close()
}

func (l *Logger) syncSinks() error {
	err := syncConsoleLogger(l.console)
	return combineSyncErrors(err, syncLogger(l.jsonl))
}

func (l *loggerLifecycle) sync(syncFn func() error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	return syncFn()
}

func (l *loggerLifecycle) close(syncFn func() error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return l.closeErr
	}
	l.closeErr = syncFn()
	closeLoggerSinks(l.closers)
	l.closers = nil
	l.closed = true
	return l.closeErr
}

func closeLoggerSinks(closers []func()) {
	for index := len(closers) - 1; index >= 0; index-- {
		if closers[index] != nil {
			closers[index]()
		}
	}
}

// write 将普通日志写入已启用的 console/jsonl 底层 logger。
func (l *Logger) write(ctx context.Context, level writeLevel, msg string, fields ...Field) {
	if l == nil {
		return
	}
	allFields := append(baseFields(ctx, l.config().Fields), fields...)
	if l.console != nil {
		l.console.write(zapLevel(level), msg, allFields...)
	}
	if l.jsonl != nil {
		writeZap(l.jsonl, level, msg, allFields...)
	}
}

// config 返回带默认值的日志配置，nil receiver 使用安全默认值。
func (l *Logger) config() Config {
	if l == nil {
		return Config{Redact: RedactConfig{Enabled: true}}.Normalize()
	}
	return l.cfg.Normalize()
}

// zapLevel 将内部普通日志级别转换为 zapcore 级别。
func zapLevel(level writeLevel) zapcore.Level {
	switch level {
	case debugLevel:
		return zapcore.DebugLevel
	case warnLevel:
		return zapcore.WarnLevel
	default:
		return zapcore.InfoLevel
	}
}

// writeZap 根据内部级别枚举调用 zap 对应方法。
func writeZap(logger *zap.Logger, level writeLevel, msg string, fields ...Field) {
	if logger == nil {
		return
	}
	logger = logger.WithOptions(zap.AddCallerSkip(2))
	switch level {
	case debugLevel:
		logger.Debug(msg, fields...)
	case warnLevel:
		logger.Warn(msg, fields...)
	default:
		logger.Info(msg, fields...)
	}
}

// syncConsoleLogger 刷新非空 console logger。
func syncConsoleLogger(logger *consoleLogger) error {
	if logger == nil {
		return nil
	}
	return logger.Sync()
}

// withConsoleFields 为非空 console logger 附加固定字段。
func withConsoleFields(logger *consoleLogger, fields []Field) *consoleLogger {
	if logger == nil {
		return nil
	}
	return logger.With(fields...)
}

// namedConsoleLogger 为非空 console logger 附加 logger 名称。
func namedConsoleLogger(logger *consoleLogger, name string) *consoleLogger {
	if logger == nil {
		return nil
	}
	return logger.Named(name)
}

// withLoggerFields 为非空 zap logger 附加固定字段。
func withLoggerFields(logger *zap.Logger, fields []Field) *zap.Logger {
	if logger == nil {
		return nil
	}
	return logger.With(fields...)
}

// namedLogger 为非空 zap logger 附加 logger 名称。
func namedLogger(logger *zap.Logger, name string) *zap.Logger {
	if logger == nil {
		return nil
	}
	return logger.Named(name)
}
