package logx

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestConfigNormalizeDefaults 验证空配置会启用开发期友好的默认输出。
func TestConfigNormalizeDefaults(t *testing.T) {
	cfg := Config{}.Normalize()

	require.Equal(t, "info", cfg.Level)
	require.True(t, cfg.Console.Enabled)
	require.Equal(t, "info", cfg.Console.Level)
	require.Equal(t, StackShort, cfg.Console.Stack)
	require.Equal(t, "stderr", cfg.Console.Output)
	require.True(t, cfg.JSONL.Enabled)
	require.Equal(t, "info", cfg.JSONL.Level)
	require.Equal(t, StackFull, cfg.JSONL.Stack)
	require.Equal(t, DefaultJSONLPath, cfg.JSONL.Path)
	require.True(t, cfg.Redact.Enabled)
}

// TestConfigNormalizeExplicitJSONLPath 验证显式 jsonl 文件路径会被保留。
func TestConfigNormalizeExplicitJSONLPath(t *testing.T) {
	cfg := Config{
		Level:  "debug",
		JSONL:  JSONLConfig{Enabled: true, Path: "./var/logs/custom.jsonl"},
		Redact: RedactConfig{Fields: []string{"api_key"}},
	}.Normalize()

	require.False(t, cfg.Console.Enabled)
	require.True(t, cfg.JSONL.Enabled)
	require.Equal(t, "debug", cfg.JSONL.Level)
	require.Equal(t, "./var/logs/custom.jsonl", cfg.JSONL.Path)
	require.True(t, cfg.Redact.Enabled)
	require.Equal(t, []string{"api_key"}, cfg.Redact.Fields)
}

// TestConfigNormalizeExplicitConsoleOutput 验证显式 console 配置会保留输出策略。
func TestConfigNormalizeExplicitConsoleOutput(t *testing.T) {
	cfg := Config{
		Level:   "debug",
		Console: ConsoleConfig{Enabled: true, Output: "stdout"},
	}.Normalize()

	require.True(t, cfg.Console.Enabled)
	require.Equal(t, "debug", cfg.Console.Level)
	require.Equal(t, "stdout", cfg.Console.Output)
	require.False(t, cfg.JSONL.Enabled)
}

// TestConfigNormalizeJSONLStdoutDisablesConsole 验证 JSONL 写入 stdout 时会关闭 console 输出。
func TestConfigNormalizeJSONLStdoutDisablesConsole(t *testing.T) {
	cfg := Config{
		Console: ConsoleConfig{Enabled: true, Output: "stderr"},
		JSONL:   JSONLConfig{Enabled: true, Path: "stdout"},
	}.Normalize()

	require.True(t, cfg.JSONL.Enabled)
	require.Equal(t, "stdout", cfg.JSONL.Path)
	require.False(t, cfg.Console.Enabled)
}

// TestConfigNormalizeRotationDefaults 验证启用滚动输出时会补齐日期格式。
func TestConfigNormalizeRotationDefaults(t *testing.T) {
	cfg := Config{
		JSONL: JSONLConfig{
			Enabled: true,
			Path:    "./var/logs/app.jsonl",
			Rotation: RotationConfig{
				Enabled: true,
			},
		},
	}.Normalize()

	require.Equal(t, DefaultRotationDateFormat, cfg.JSONL.Rotation.DateFormat)
	require.Equal(t, 0, cfg.JSONL.Rotation.MaxSizeMB)
}
