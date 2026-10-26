package logx

import "strings"

const (
	// DefaultJSONLPath 是 JSONL 文件日志的默认写入路径。
	DefaultJSONLPath = "./var/logs/app.jsonl"
	// DefaultRotationDateFormat 是 JSONL 日期滚动文件名使用的默认日期格式。
	DefaultRotationDateFormat = "2006-01-02"
)

// Config 描述统一日志配置，支持 console 与 jsonl 两套输出策略。
type Config struct {
	Level   string        `mapstructure:"level"`
	Console ConsoleConfig `mapstructure:"console"`
	JSONL   JSONLConfig   `mapstructure:"jsonl"`
	Fields  FieldsConfig  `mapstructure:"fields"`
	Redact  RedactConfig  `mapstructure:"redact"`
}

// ConsoleConfig 描述面向人眼阅读的 console 日志输出。
type ConsoleConfig struct {
	Enabled bool      `mapstructure:"enabled"`
	Level   string    `mapstructure:"level"`
	Stack   StackMode `mapstructure:"stack"`
	Color   bool      `mapstructure:"color"`
	Output  string    `mapstructure:"output"`
}

// JSONLConfig 描述面向机器检索的 JSON Lines 日志输出。
type JSONLConfig struct {
	Enabled  bool           `mapstructure:"enabled"`
	Level    string         `mapstructure:"level"`
	Stack    StackMode      `mapstructure:"stack"`
	Path     string         `mapstructure:"path"`
	Rotation RotationConfig `mapstructure:"rotation"`
}

// RotationConfig 描述 JSONL 文件日志的日期和大小滚动策略。
type RotationConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	DateFormat string `mapstructure:"date_format"`
	MaxSizeMB  int    `mapstructure:"max_size_mb"`
}

// FieldsConfig 描述所有日志默认携带的服务字段。
type FieldsConfig struct {
	Service  string `mapstructure:"service"`
	Env      string `mapstructure:"env"`
	Version  string `mapstructure:"version"`
	Instance string `mapstructure:"instance"`
}

// RedactConfig 描述结构化日志字段脱敏配置。
type RedactConfig struct {
	Enabled bool     `mapstructure:"enabled"`
	Fields  []string `mapstructure:"fields"`
}

// Normalize 补齐 logx 默认配置。
func (c Config) Normalize() Config {
	if strings.TrimSpace(c.Level) == "" {
		c.Level = "info"
	}
	hasNewOutput := consoleConfigured(c.Console) || jsonlConfigured(c.JSONL)

	if !c.Redact.Enabled {
		c.Redact.Enabled = true
	}

	if !hasNewOutput {
		c.Console.Enabled = true
		c.JSONL.Enabled = true
	}

	if c.Console.Enabled {
		c.Console.Level = firstNonEmpty(c.Console.Level, c.Level)
		if c.Console.Stack == "" {
			c.Console.Stack = StackShort
		}
		c.Console.Output = firstNonEmpty(c.Console.Output, "stderr")
	}
	if c.JSONL.Enabled {
		c.JSONL.Level = firstNonEmpty(c.JSONL.Level, c.Level)
		if c.JSONL.Stack == "" {
			c.JSONL.Stack = StackFull
		}
		c.JSONL.Path = firstNonEmpty(c.JSONL.Path, DefaultJSONLPath)
		if c.JSONL.Rotation.Enabled {
			c.JSONL.Rotation.DateFormat = firstNonEmpty(c.JSONL.Rotation.DateFormat, DefaultRotationDateFormat)
		}
		if isJSONLStdout(c.JSONL.Path) {
			c.Console.Enabled = false
		}
	}
	return c
}

// consoleConfigured 判断 console 输出是否被用户显式配置过。
func consoleConfigured(cfg ConsoleConfig) bool {
	return cfg.Enabled ||
		strings.TrimSpace(cfg.Level) != "" ||
		cfg.Stack != "" ||
		strings.TrimSpace(cfg.Output) != ""
}

// jsonlConfigured 判断 jsonl 输出是否被用户显式配置过。
func jsonlConfigured(cfg JSONLConfig) bool {
	return cfg.Enabled ||
		strings.TrimSpace(cfg.Level) != "" ||
		cfg.Stack != "" ||
		strings.TrimSpace(cfg.Path) != ""
}

// firstNonEmpty 返回首个去除空白后仍非空的字符串。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
