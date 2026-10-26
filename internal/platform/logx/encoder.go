package logx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// newJSONLLogger 创建面向机器检索的 JSON Lines zap logger。
func newJSONLLogger(cfg Config) (*zap.Logger, func(), error) {
	if err := validateJSONLPath(cfg.JSONL.Path); err != nil {
		return nil, nil, err
	}
	level, err := parseLevel(cfg.JSONL.Level)
	if err != nil {
		return nil, nil, err
	}
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "ts"
	encoderConfig.LevelKey = "level"
	encoderConfig.NameKey = "logger"
	encoderConfig.CallerKey = "caller"
	encoderConfig.MessageKey = "msg"
	encoderConfig.StacktraceKey = ""
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	sink, closeSink, err := newJSONLWriteSyncer(cfg.JSONL)
	if err != nil {
		return nil, nil, err
	}
	core := zapcore.NewCore(zapcore.NewJSONEncoder(encoderConfig), sink, level)
	return zap.New(core, loggerOptions(cfg.Redact)...), closeSink, nil
}

// validateJSONLPath 确保 JSONL 写入有效目标。
func validateJSONLPath(path string) error {
	normalized := strings.ToLower(strings.TrimSpace(path))
	switch normalized {
	case "":
		return fmt.Errorf("log.jsonl.path 不能为空")
	case "stderr":
		return fmt.Errorf("log.jsonl.path 不支持写入 %q", path)
	default:
		return nil
	}
}

// newJSONLWriteSyncer 创建 JSONL 输出目标。
func newJSONLWriteSyncer(cfg JSONLConfig) (zapcore.WriteSyncer, func(), error) {
	if isJSONLStdout(cfg.Path) {
		return zapcore.Lock(stdoutSyncer{file: os.Stdout}), func() {}, nil
	}
	if cfg.Rotation.Enabled {
		writer, err := newJSONLRotationWriter(cfg.Path, cfg.Rotation)
		if err != nil {
			return nil, nil, err
		}
		return zapcore.AddSync(writer), func() { _ = writer.Close() }, nil
	}
	if err := ensureJSONLParent(cfg.Path); err != nil {
		return nil, nil, err
	}
	return zap.Open(cfg.Path)
}

// stdoutSyncer 写入 stdout，并将 Sync 处理为 no-op，避免 Windows pipe 场景阻塞。
type stdoutSyncer struct {
	file *os.File
}

// Write 写入 stdout。
func (s stdoutSyncer) Write(payload []byte) (int, error) {
	return s.file.Write(payload)
}

// Sync 忽略 stdout 刷新。
func (s stdoutSyncer) Sync() error {
	return nil
}

// isJSONLStdout 判断 JSONL 是否写入 stdout。
func isJSONLStdout(path string) bool {
	return strings.EqualFold(strings.TrimSpace(path), "stdout")
}

// ensureJSONLParent 确保 JSONL 文件路径的父目录已存在。
func ensureJSONLParent(path string) error {
	dir := filepath.Dir(strings.TrimSpace(path))
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

// loggerOptions 返回所有底层 zap logger 共用的选项。
func loggerOptions(redact RedactConfig) []zap.Option {
	options := []zap.Option{zap.AddCaller(), zap.AddCallerSkip(1)}
	if redact.Enabled {
		options = append(options, zap.WrapCore(func(core zapcore.Core) zapcore.Core {
			return &redactCore{Core: core, cfg: redact}
		}))
	}
	return options
}

// syncLogger 刷新单个 zap logger，并忽略 stdout/stderr 常见的 sync 错误。
func syncLogger(logger *zap.Logger) error {
	if logger == nil {
		return nil
	}
	if err := logger.Sync(); err != nil && !isIgnorableSyncError(err) {
		return err
	}
	return nil
}

// isIgnorableSyncError 判断错误是否属于标准输出流不支持 Sync 的场景。
func isIgnorableSyncError(err error) bool {
	if err == nil {
		return true
	}
	text := err.Error()
	return text == "sync /dev/stderr: invalid argument" ||
		text == "sync /dev/stdout: invalid argument" ||
		text == "sync stdout: invalid argument" ||
		text == "sync stderr: invalid argument" ||
		text == "invalid argument"
}

// combineSyncErrors 合并两个 Sync 错误，保留第二个错误的 unwrap 链。
func combineSyncErrors(first error, second error) error {
	switch {
	case first == nil:
		return second
	case second == nil:
		return first
	default:
		return fmt.Errorf("%v; %w", first, second)
	}
}

// ParseLevel 将配置中的日志级别解析为 zapcore.Level。
func parseLevel(level string) (zapcore.Level, error) {
	parsed := zapcore.InfoLevel
	if strings.TrimSpace(level) == "" {
		return parsed, nil
	}
	if err := parsed.UnmarshalText([]byte(strings.ToLower(strings.TrimSpace(level)))); err != nil {
		return parsed, err
	}
	return parsed, nil
}
