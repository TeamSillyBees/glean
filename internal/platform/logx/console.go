package logx

import (
	"bytes"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	// consoleANSIReset 重置 console 颜色，避免颜色泄漏到后续输出。
	consoleANSIReset = "\x1b[0m"
	// consoleANSIDefault 是 console 普通文本颜色，只恢复终端默认前景色。
	consoleANSIDefault = "\x1b[39m"
	// consoleANSIDebug 是 DEBUG 标识符颜色。
	consoleANSIDebug = "\x1b[36m"
	// consoleANSIInfo 是 INFO 标识符颜色。
	consoleANSIInfo = "\x1b[32m"
	// consoleANSIWarn 是 WARN 标识符颜色。
	consoleANSIWarn = "\x1b[33m"
	// consoleANSIError 是 ERROR 标识符颜色。
	consoleANSIError = "\x1b[31m"
)

// consoleLogger 负责把日志渲染为面向终端阅读的多行文本。
type consoleLogger struct {
	sink        zapcore.WriteSyncer
	defaultSink zapcore.WriteSyncer
	level       zapcore.Level
	color       bool
	name        string
	fields      []Field
	redact      RedactConfig
}

// newConsoleLogger 创建面向人眼阅读的 console logger。
func newConsoleLogger(cfg Config) (*consoleLogger, func(), error) {
	level, err := parseLevel(cfg.Console.Level)
	if err != nil {
		return nil, nil, err
	}
	sink, closeSink, err := zap.Open(cfg.Console.Output)
	if err != nil {
		return nil, nil, err
	}
	defaultSink, closeDefaultSink, err := newConsoleDefaultSink(cfg.Console.Output, sink)
	if err != nil {
		closeSink()
		return nil, nil, err
	}
	return &consoleLogger{
			sink:        sink,
			defaultSink: defaultSink,
			level:       level,
			color:       cfg.Console.Color,
			redact:      cfg.Redact,
		}, func() {
			closeDefaultSink()
			closeSink()
		}, nil
}

// newNopConsoleLogger 创建不输出内容的 console logger。
func newNopConsoleLogger() *consoleLogger {
	return &consoleLogger{level: zapcore.FatalLevel + 1}
}

// newConsoleDefaultSink 在 stderr 模式下为 INFO/DEBUG 提供 stdout 输出目标。
func newConsoleDefaultSink(output string, fallback zapcore.WriteSyncer) (zapcore.WriteSyncer, func(), error) {
	if strings.ToLower(strings.TrimSpace(output)) != "stderr" {
		return fallback, func() {}, nil
	}
	sink, closeSink, err := zap.Open("stdout")
	if err != nil {
		return nil, nil, err
	}
	return sink, closeSink, nil
}

// With 返回附加固定字段的新 console logger。
func (l *consoleLogger) With(fields ...Field) *consoleLogger {
	if l == nil {
		return nil
	}
	next := *l
	next.fields = append(append([]Field(nil), l.fields...), RedactFields(fields, l.redact)...)
	return &next
}

// Named 返回带名称的新 console logger。
func (l *consoleLogger) Named(name string) *consoleLogger {
	if l == nil {
		return nil
	}
	next := *l
	name = strings.TrimSpace(name)
	if next.name == "" {
		next.name = name
	} else if name != "" {
		next.name += "." + name
	}
	return &next
}

// Sync 刷新 console 输出。
func (l *consoleLogger) Sync() error {
	if l == nil || l.sink == nil {
		return nil
	}
	err := syncWriteSyncer(l.sink)
	if l.defaultSink != nil && l.defaultSink != l.sink {
		err = combineSyncErrors(err, syncWriteSyncer(l.defaultSink))
	}
	return err
}

// syncWriteSyncer 刷新单个 console 输出目标。
func syncWriteSyncer(sink zapcore.WriteSyncer) error {
	if sink == nil {
		return nil
	}
	if err := sink.Sync(); err != nil && !isIgnorableSyncError(err) {
		return err
	}
	return nil
}

// write 输出一条 console 文本日志。
func (l *consoleLogger) write(level zapcore.Level, msg string, fields ...Field) {
	if l == nil || l.sink == nil || level < l.level {
		return
	}
	allFields := append(append([]Field(nil), l.fields...), fields...)
	allFields = RedactFields(allFields, l.redact)
	entry := consoleEntry{
		Time:   time.Now(),
		Level:  level,
		Caller: consoleCaller(),
		Name:   l.name,
		Msg:    msg,
		Fields: zapFieldsToMap(allFields),
		Color:  l.color,
	}
	_, _ = l.writeSink(level).Write([]byte(entry.String()))
}

// writeSink 返回当前日志级别应该使用的 console 输出目标。
func (l *consoleLogger) writeSink(level zapcore.Level) zapcore.WriteSyncer {
	if l == nil || l.defaultSink == nil {
		return l.sink
	}
	if level <= zapcore.InfoLevel {
		return l.defaultSink
	}
	return l.sink
}

// consoleEntry 是单条 console 日志的渲染模型。
type consoleEntry struct {
	Time   time.Time
	Level  zapcore.Level
	Caller string
	Name   string
	Msg    string
	Fields map[string]any
	Color  bool
}

// String 将日志条目渲染为多行终端文本。
func (e consoleEntry) String() string {
	var buf bytes.Buffer
	textColor := consoleTextColor(e.Level, e.Color)
	writeConsoleLine(&buf, textColor, "%s  %s  %s  %s",
		e.Time.Format("2006-01-02 15:04:05.000"),
		consoleLevelLabel(e.Level, e.Color),
		firstNonEmpty(e.Caller, "-"),
		e.Msg)
	if line := e.requestLine(); line != "" {
		writeConsoleLine(&buf, textColor, "  %s", line)
	}
	if line := e.indexLine(); line != "" {
		writeConsoleLine(&buf, textColor, "  %s", line)
	}
	if public := consoleStringValue(e.Fields["error_public"]); public != "" {
		writeConsoleLine(&buf, textColor, "  public=%s", public)
	}
	if errText := consoleErrorText(e.Fields); errText != "" {
		writeConsoleLine(&buf, textColor, "  error=%s", errText)
	}
	if ctxLine := e.contextLine(); ctxLine != "" {
		writeConsoleLine(&buf, textColor, "  ctx: %s", ctxLine)
	}
	if stack := consoleStringValue(e.Fields["error_stacktrace"]); stack != "" {
		writeConsoleLine(&buf, textColor, "stack:")
		for _, line := range strings.Split(stack, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				writeConsoleLine(&buf, textColor, "  %s", line)
			}
		}
	}
	return buf.String()
}

// requestLine 渲染 HTTP 请求摘要行。
func (e consoleEntry) requestLine() string {
	method := consoleStringValue(e.Fields["method"])
	path := consoleStringValue(e.Fields["path"])
	status := consoleStringValue(e.Fields["status"])
	if method == "" && path == "" && status == "" {
		return ""
	}
	if method == "" {
		method = "-"
	}
	if path == "" {
		path = "-"
	}
	if status == "" {
		status = "-"
	}
	return fmt.Sprintf("%s %s -> %s", method, path, status)
}

// indexLine 渲染错误码、错误域和 trace 等索引字段。
func (e consoleEntry) indexLine() string {
	parts := make([]string, 0, 3)
	if code := consoleStringValue(e.Fields["error_code"]); code != "" {
		parts = append(parts, "code="+code)
	}
	if domain := consoleStringValue(e.Fields["error_domain"]); domain != "" {
		parts = append(parts, "domain="+domain)
	}
	if trace := consoleStringValue(e.Fields["trace_id"]); trace != "" {
		parts = append(parts, "trace="+trace)
	}
	return strings.Join(parts, " ")
}

// contextLine 渲染适合 console 展示的业务上下文。
func (e consoleEntry) contextLine() string {
	consumed := map[string]struct{}{
		"method":           {},
		"path":             {},
		"status":           {},
		"trace_id":         {},
		"error_code":       {},
		"error_domain":     {},
		"error_public":     {},
		"error":            {},
		"error_message":    {},
		"error_stacktrace": {},
	}
	keys := preferredConsoleContextKeys(e.Fields, consumed)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := consoleStringValue(e.Fields[key])
		if value == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s", key, value))
	}
	return strings.Join(parts, " ")
}

// consoleCaller 返回第一个非 logx 内部调用点。
func consoleCaller() string {
	for skip := 2; skip < 16; skip++ {
		_, file, line, ok := runtime.Caller(skip)
		if !ok {
			break
		}
		if isLogxCaller(file) {
			continue
		}
		return shortCaller(file, line)
	}
	return ""
}

// isLogxCaller 判断调用点是否来自 logx 包内部。
func isLogxCaller(file string) bool {
	return strings.Contains(filepath.ToSlash(file), "/internal/platform/logx/")
}

// shortCaller 将绝对路径裁剪为 package/file.go:line 形式。
func shortCaller(file string, line int) string {
	path := filepath.ToSlash(file)
	parts := strings.Split(path, "/")
	if len(parts) >= 2 {
		path = strings.Join(parts[len(parts)-2:], "/")
	}
	return fmt.Sprintf("%s:%d", path, line)
}

// consoleLevelLabel 返回 console 展示用级别文本。
func consoleLevelLabel(level zapcore.Level, color bool) string {
	label := strings.ToUpper(level.String())
	padded := fmt.Sprintf("%-5s", label)
	if !color {
		return padded
	}
	switch level {
	case zapcore.DebugLevel:
		return consoleANSIDebug + padded + consoleTextColor(level, true)
	case zapcore.WarnLevel:
		return consoleANSIWarn + padded + consoleTextColor(level, true)
	case zapcore.ErrorLevel:
		return consoleANSIError + padded + consoleTextColor(level, true)
	default:
		return consoleANSIInfo + padded + consoleTextColor(level, true)
	}
}

// consoleTextColor 返回当前级别日志正文应该使用的颜色。
func consoleTextColor(level zapcore.Level, color bool) string {
	if !color {
		return ""
	}
	switch level {
	case zapcore.WarnLevel:
		return consoleANSIWarn
	case zapcore.ErrorLevel:
		return consoleANSIError
	default:
		return consoleANSIDefault
	}
}

// writeConsoleLine 写入一行 console 文本，并按日志级别显式设置正文颜色。
func writeConsoleLine(buf *bytes.Buffer, textColor string, format string, args ...any) {
	buf.WriteString(textColor)
	fmt.Fprintf(buf, format, args...)
	if textColor != "" {
		buf.WriteString(consoleANSIReset)
	}
	buf.WriteByte('\n')
}

// zapFieldsToMap 将 zap 字段转换为易于 console 格式化的 map。
func zapFieldsToMap(fields []Field) map[string]any {
	encoder := zapcore.NewMapObjectEncoder()
	for _, field := range fields {
		field.AddTo(encoder)
	}
	return encoder.Fields
}

// preferredConsoleContextKeys 按固定优先级返回 console 上下文字段。
func preferredConsoleContextKeys(fields map[string]any, consumed map[string]struct{}) []string {
	preferred := []string{
		"request_id",
		"user_id",
		"tenant_id",
		"order_id",
		"operation",
		"provider",
		"channel",
		"task",
		"task_type",
		"task_name",
		"queue",
		"biz_key",
	}
	seen := make(map[string]struct{}, len(preferred))
	keys := make([]string, 0, len(fields))
	for _, key := range preferred {
		seen[key] = struct{}{}
		if _, ok := consumed[key]; ok {
			continue
		}
		if _, ok := fields[key]; ok {
			keys = append(keys, key)
		}
	}
	rest := make([]string, 0)
	for key := range fields {
		if _, ok := seen[key]; ok {
			continue
		}
		if _, ok := consumed[key]; ok {
			continue
		}
		if _, ok := consoleContextWhitelist[key]; ok {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	return append(keys, rest...)
}

// consoleErrorText 返回 console 错误说明文本。
func consoleErrorText(fields map[string]any) string {
	if value := consoleStringValue(fields["error"]); value != "" {
		return value
	}
	return consoleStringValue(fields["error_message"])
}

// consoleStringValue 将字段值转换为紧凑字符串。
func consoleStringValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}
