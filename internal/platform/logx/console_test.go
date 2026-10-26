package logx

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
)

// TestConsoleEntryErrorUsesRedBlock 验证 ERROR 日志整块使用红色。
func TestConsoleEntryErrorUsesRedBlock(t *testing.T) {
	entry := consoleEntry{
		Time:   time.Date(2026, 5, 30, 16, 42, 1, 123000000, time.UTC),
		Level:  zapcore.ErrorLevel,
		Caller: "web/error.go:42",
		Msg:    "request failed",
		Color:  true,
	}

	body := entry.String()

	require.Contains(t, body, consoleANSIError+"2026-05-30 16:42:01.123")
	require.Contains(t, body, consoleANSIError+"ERROR"+consoleANSIError)
	require.Contains(t, body, consoleANSIError+"  web/error.go:42  request failed"+consoleANSIReset)
	require.NotContains(t, body, "\x1b[97m")
}

// TestConsoleEntryWarnUsesYellowBlock 验证 WARN 日志整块使用黄色。
func TestConsoleEntryWarnUsesYellowBlock(t *testing.T) {
	entry := consoleEntry{
		Time:   time.Date(2026, 5, 30, 16, 42, 1, 123000000, time.UTC),
		Level:  zapcore.WarnLevel,
		Caller: "web/user.go:12",
		Msg:    "request slow",
		Color:  true,
	}

	body := entry.String()

	require.Contains(t, body, consoleANSIWarn+"2026-05-30 16:42:01.123")
	require.Contains(t, body, consoleANSIWarn+"WARN "+consoleANSIWarn)
	require.Contains(t, body, consoleANSIWarn+"  web/user.go:12  request slow"+consoleANSIReset)
	require.NotContains(t, body, consoleANSIError)
	require.NotContains(t, body, "\x1b[97m")
}

// TestConsoleEntryInfoUsesDefaultBody 验证 INFO 正文使用终端默认颜色且不会使用红色。
func TestConsoleEntryInfoUsesDefaultBody(t *testing.T) {
	entry := consoleEntry{
		Time:   time.Date(2026, 5, 30, 16, 42, 1, 123000000, time.UTC),
		Level:  zapcore.InfoLevel,
		Caller: "web/user.go:12",
		Msg:    "request finished",
		Color:  true,
	}

	body := entry.String()

	require.Contains(t, body, consoleANSIInfo+"INFO ")
	require.NotContains(t, body, consoleANSIError)
	require.NotContains(t, body, consoleANSIWarn)
	require.Contains(t, body, consoleANSIDefault+"2026-05-30 16:42:01.123")
	require.Contains(t, body, consoleANSIDefault+"  web/user.go:12  request finished"+consoleANSIReset)
	require.NotContains(t, body, "\x1b[97m")
}

// TestConsoleEntryErrorDoesNotLeakIntoFollowingInfo 验证 ERROR 颜色不会泄漏到后一条 INFO 日志。
func TestConsoleEntryErrorDoesNotLeakIntoFollowingInfo(t *testing.T) {
	errorEntry := consoleEntry{
		Time:   time.Date(2026, 5, 30, 16, 42, 1, 123000000, time.UTC),
		Level:  zapcore.ErrorLevel,
		Caller: "web/error.go:42",
		Msg:    "request failed",
		Color:  true,
	}
	infoEntry := consoleEntry{
		Time:   time.Date(2026, 5, 30, 16, 42, 2, 123000000, time.UTC),
		Level:  zapcore.InfoLevel,
		Caller: "web/user.go:12",
		Msg:    "request finished",
		Color:  true,
	}

	body := errorEntry.String() + infoEntry.String()

	require.Contains(t, body, consoleANSIError+"  web/error.go:42  request failed"+consoleANSIReset+"\n"+consoleANSIDefault+"2026-05-30 16:42:02.123")
	require.Contains(t, body, consoleANSIInfo+"INFO "+consoleANSIDefault+"  web/user.go:12  request finished")
	require.NotContains(t, body, consoleANSIInfo+"INFO "+consoleANSIError)
}

// TestConsoleLoggerRoutesInfoToDefaultSink 验证 stderr 模式下 INFO 不写入错误输出目标。
func TestConsoleLoggerRoutesInfoToDefaultSink(t *testing.T) {
	errorSink := &consoleBufferSink{}
	defaultSink := &consoleBufferSink{}
	logger := &consoleLogger{
		sink:        errorSink,
		defaultSink: defaultSink,
		level:       zapcore.DebugLevel,
		color:       true,
	}

	logger.write(zapcore.ErrorLevel, "request failed")
	logger.write(zapcore.InfoLevel, "request completed")

	require.Contains(t, errorSink.String(), "request failed")
	require.NotContains(t, errorSink.String(), "request completed")
	require.Contains(t, defaultSink.String(), "request completed")
	require.NotContains(t, defaultSink.String(), "request failed")
}

// consoleBufferSink 是测试用内存 console 输出目标。
type consoleBufferSink struct {
	builder strings.Builder
}

// Write 记录写入内容。
func (s *consoleBufferSink) Write(payload []byte) (int, error) {
	return s.builder.Write(payload)
}

// Sync 满足 zapcore.WriteSyncer 接口。
func (s *consoleBufferSink) Sync() error {
	return nil
}

// String 返回已写入内容。
func (s *consoleBufferSink) String() string {
	return s.builder.String()
}
