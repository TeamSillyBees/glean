package logx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apperrors "github.com/teamsillybees/glean/internal/platform/errors"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
)

// TestLoggerErrorSplitsConsoleAndJSONLFields 验证 Error 会分别构造 console 和 jsonl 字段。
func TestLoggerErrorSplitsConsoleAndJSONLFields(t *testing.T) {
	dir := t.TempDir()
	consolePath := filepath.Join(dir, "console.log")
	jsonlPath := filepath.Join(dir, "app.jsonl")
	logger, err := NewLogger(Config{
		Level: "debug",
		Console: ConsoleConfig{
			Enabled: true,
			Level:   "debug",
			Stack:   StackShort,
			Output:  consolePath,
		},
		JSONL: JSONLConfig{
			Enabled: true,
			Level:   "debug",
			Stack:   StackFull,
			Path:    jsonlPath,
		},
		Redact: RedactConfig{Enabled: true},
	}.Normalize())
	require.NoError(t, err)
	closeLoggerOnCleanup(t, logger)

	err = apperrors.Wrap(errors.New("driver: duplicate key"), apperrors.CodeDBError, "create user failed",
		apperrors.WithCauseTrace("trace-1"),
		apperrors.WithCauseAttr("password", "secret-password"),
	)
	ctx := requestctx.WithTraceID(context.Background(), "trace-from-context")
	logger.Error(ctx, "request failed", err,
		String("method", "GET"),
		String("path", "/api/v1/users"),
		Int("status", 404),
		String("operation", "GetProfile"),
		String("password", "plain-password"),
	)
	require.NoError(t, logger.Sync())

	consoleBody := readFile(t, consolePath)
	jsonlBody := readFile(t, jsonlPath)
	require.NotContains(t, consoleBody, `{"`)
	require.Contains(t, consoleBody, "request failed")
	require.Contains(t, consoleBody, "GET /api/v1/users -> 404")
	require.Contains(t, consoleBody, "code=DB_ERROR")
	require.Contains(t, consoleBody, "trace=trace-1")
	require.Contains(t, consoleBody, "public=internal error")
	require.Contains(t, consoleBody, "ctx: operation=GetProfile")
	require.Contains(t, consoleBody, "stack:")
	require.LessOrEqual(t, len(consoleStackLines(consoleBody)), shortStackFrameLimit)
	require.NotContains(t, consoleBody, "error_context")
	require.NotContains(t, consoleBody, "secret-password")
	require.NotContains(t, consoleBody, "plain-password")
	require.Contains(t, jsonlBody, `"error_code":"DB_ERROR"`)
	require.Contains(t, jsonlBody, `"trace_id":"trace-1"`)
	require.Equal(t, 1, strings.Count(jsonlBody, `"trace_id"`))
	require.Contains(t, jsonlBody, `"error_context"`)
	require.NotContains(t, jsonlBody, "secret-password")
	require.NotContains(t, jsonlBody, "plain-password")
}

// TestLoggerInfoRedactsFields 验证普通日志也会经过统一脱敏层。
func TestLoggerInfoRedactsFields(t *testing.T) {
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "app.jsonl")
	logger, err := NewLogger(Config{
		Console: ConsoleConfig{Enabled: false},
		JSONL:   JSONLConfig{Enabled: true, Level: "debug", Path: jsonlPath},
		Redact:  RedactConfig{Enabled: true},
	}.Normalize())
	require.NoError(t, err)
	closeLoggerOnCleanup(t, logger)

	logger.Info(context.Background(), "config loaded", String("authorization", "Bearer secret-token"))
	require.NoError(t, logger.Sync())

	body := readFile(t, jsonlPath)
	require.Contains(t, body, `"authorization":"`+RedactedValue+`"`)
	require.NotContains(t, body, "secret-token")
}

// TestLoggerJSONLCallerSkipsLogxWrappers 验证 JSONL caller 指向业务调用点而不是 logx 包装层。
func TestLoggerJSONLCallerSkipsLogxWrappers(t *testing.T) {
	jsonlPath := filepath.Join(t.TempDir(), "app.jsonl")
	logger, err := NewLogger(Config{
		Console: ConsoleConfig{Enabled: false},
		JSONL:   JSONLConfig{Enabled: true, Level: "debug", Path: jsonlPath},
		Redact:  RedactConfig{Enabled: true},
	}.Normalize())
	require.NoError(t, err)
	closeLoggerOnCleanup(t, logger)

	logger.Info(context.Background(), "caller probe")
	logger.Error(context.Background(), "error caller probe", errors.New("boom"))
	require.NoError(t, logger.Sync())

	lines := strings.Split(strings.TrimSpace(readFile(t, jsonlPath)), "\n")
	require.Len(t, lines, 2)
	for _, line := range lines {
		var payload map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &payload))
		caller, _ := payload["caller"].(string)
		require.Contains(t, caller, "logger_test.go")
		require.NotContains(t, caller, "logger.go")
	}
}

// TestNewLoggerWritesJSONLToStdout 验证 JSONL 可以写入 stdout，且不会额外输出 console 文本。
func TestNewLoggerWritesJSONLToStdout(t *testing.T) {
	output := captureStdout(t, func() {
		logger, err := NewLogger(Config{
			Console: ConsoleConfig{Enabled: true, Output: "stdout"},
			JSONL:   JSONLConfig{Enabled: true, Level: "debug", Path: "stdout"},
			Redact:  RedactConfig{Enabled: true},
		}.Normalize())
		require.NoError(t, err)
		closeLoggerOnCleanup(t, logger)

		logger.Info(context.Background(), "stdout jsonl", String("operation", "StdoutProbe"))
		require.NoError(t, logger.Sync())
	})

	lines := strings.Split(strings.TrimSpace(output), "\n")
	require.Len(t, lines, 1)
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &payload))
	require.Equal(t, "stdout jsonl", payload["msg"])
	require.Equal(t, "StdoutProbe", payload["operation"])
	require.NotContains(t, output, "ctx:")
}

// TestNewLoggerRejectsJSONLStderrPath 验证 JSONL 不允许写入 stderr。
func TestNewLoggerRejectsJSONLStderrPath(t *testing.T) {
	_, err := NewLogger(Config{
		Console: ConsoleConfig{Enabled: false},
		JSONL:   JSONLConfig{Enabled: true, Path: "stderr"},
	}.Normalize())

	require.Error(t, err)
	require.Contains(t, err.Error(), "log.jsonl.path")
}

// TestNewLoggerCreatesJSONLParentDirectory 验证 JSONL 文件父目录会被自动创建。
func TestNewLoggerCreatesJSONLParentDirectory(t *testing.T) {
	jsonlPath := filepath.Join(t.TempDir(), "missing", "logs", "app.jsonl")
	logger, err := NewLogger(Config{
		Console: ConsoleConfig{Enabled: false},
		JSONL:   JSONLConfig{Enabled: true, Level: "debug", Path: jsonlPath},
		Redact:  RedactConfig{Enabled: true},
	}.Normalize())
	require.NoError(t, err)
	closeLoggerOnCleanup(t, logger)

	logger.Info(context.Background(), "jsonl ready", String("operation", "CreateParent"))
	require.NoError(t, logger.Sync())

	require.FileExists(t, jsonlPath)
	require.Contains(t, readFile(t, jsonlPath), `"msg":"jsonl ready"`)
}

// TestNewLoggerWritesJSONLDateRotatedFile 验证启用滚动后 JSONL 会写入带日期的文件。
func TestNewLoggerWritesJSONLDateRotatedFile(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "app.jsonl")
	logger, err := NewLogger(Config{
		Console: ConsoleConfig{Enabled: false},
		JSONL: JSONLConfig{
			Enabled: true,
			Level:   "debug",
			Path:    basePath,
			Rotation: RotationConfig{
				Enabled:    true,
				DateFormat: DefaultRotationDateFormat,
			},
		},
		Redact: RedactConfig{Enabled: true},
	}.Normalize())
	require.NoError(t, err)
	closeLoggerOnCleanup(t, logger)

	logger.Info(context.Background(), "rotated by date")
	require.NoError(t, logger.Sync())

	datedPath := filepath.Join(dir, "app-"+time.Now().Format(DefaultRotationDateFormat)+".jsonl")
	require.NoFileExists(t, basePath)
	require.FileExists(t, datedPath)
	require.Contains(t, readFile(t, datedPath), `"msg":"rotated by date"`)
}

// TestNewLoggerSplitsJSONLBySize 验证启用大小滚动后同一天内会追加序号文件。
func TestNewLoggerSplitsJSONLBySize(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "app.jsonl")
	logger, err := NewLogger(Config{
		Console: ConsoleConfig{Enabled: false},
		JSONL: JSONLConfig{
			Enabled: true,
			Level:   "debug",
			Path:    basePath,
			Rotation: RotationConfig{
				Enabled:    true,
				DateFormat: DefaultRotationDateFormat,
				MaxSizeMB:  1,
			},
		},
		Redact: RedactConfig{Enabled: true},
	}.Normalize())
	require.NoError(t, err)
	closeLoggerOnCleanup(t, logger)

	payload := strings.Repeat("x", 700*1024)
	logger.Info(context.Background(), "large one", String("payload", payload))
	logger.Info(context.Background(), "large two", String("payload", payload))
	require.NoError(t, logger.Sync())

	date := time.Now().Format(DefaultRotationDateFormat)
	firstPath := filepath.Join(dir, "app-"+date+".jsonl")
	secondPath := filepath.Join(dir, "app-"+date+".1.jsonl")
	require.FileExists(t, firstPath)
	require.FileExists(t, secondPath)
	require.Contains(t, readFile(t, firstPath), `"msg":"large one"`)
	require.Contains(t, readFile(t, secondPath), `"msg":"large two"`)
}

func TestLoggerSyncDoesNotCloseSharedSink(t *testing.T) {
	jsonlPath := filepath.Join(t.TempDir(), "app.jsonl")
	logger, err := NewLogger(Config{
		Console: ConsoleConfig{Enabled: false},
		JSONL:   JSONLConfig{Enabled: true, Level: "debug", Path: jsonlPath},
		Redact:  RedactConfig{Enabled: true},
	}.Normalize())
	require.NoError(t, err)
	closeLoggerOnCleanup(t, logger)

	derived := logger.With(String("component", "derived"))
	derived.Info(context.Background(), "before sync")
	require.NoError(t, derived.Sync())
	logger.Info(context.Background(), "after sync")
	require.NoError(t, logger.Sync())

	body := readFile(t, jsonlPath)
	require.Contains(t, body, `"msg":"before sync"`)
	require.Contains(t, body, `"msg":"after sync"`)
}

func TestLoggerCloseAndShutdownAreIdempotent(t *testing.T) {
	jsonlPath := filepath.Join(t.TempDir(), "app.jsonl")
	logger, err := NewLogger(Config{
		Console: ConsoleConfig{Enabled: false},
		JSONL:   JSONLConfig{Enabled: true, Level: "debug", Path: jsonlPath},
		Redact:  RedactConfig{Enabled: true},
	}.Normalize())
	require.NoError(t, err)

	logger.Info(context.Background(), "before close")
	require.NoError(t, logger.Close())
	require.NoError(t, logger.Close())
	require.NoError(t, logger.Shutdown())
	require.Contains(t, readFile(t, jsonlPath), `"msg":"before close"`)
}

// readFile 读取测试日志文件内容。
func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(content)
}

func closeLoggerOnCleanup(t *testing.T, logger *Logger) {
	t.Helper()
	t.Cleanup(func() {
		require.NoError(t, logger.Close())
	})
}

// captureStdout 捕获函数执行期间写入标准输出的内容。
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = writer
	defer func() {
		os.Stdout = oldStdout
	}()

	fn()
	require.NoError(t, writer.Close())

	var output bytes.Buffer
	_, err = io.Copy(&output, reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	return output.String()
}

// consoleStackLines 提取 console 输出中的短栈行。
func consoleStackLines(body string) []string {
	lines := strings.Split(body, "\n")
	stackIndex := -1
	for index, line := range lines {
		if strings.TrimSpace(line) == "stack:" {
			stackIndex = index
			break
		}
	}
	if stackIndex < 0 {
		return nil
	}
	result := make([]string, 0)
	for _, line := range lines[stackIndex+1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, "  ") {
			break
		}
		result = append(result, strings.TrimSpace(line))
	}
	return result
}
