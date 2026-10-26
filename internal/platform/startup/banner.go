package startup

import (
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/teamsillybees/glean/internal/platform/redisx"
	"github.com/teamsillybees/glean/internal/platform/task"
)

const defaultLogo = `
   _       _ _
  (_)_ __ (_) |_ _ __ __ _
  | | '_ \| | __| '__/ _' |
  | | | | | | |_| | | (_| |
  |_|_| |_|_|\__|_|  \__,_|
`

// Info 描述启动提示需要展示的运行时摘要。
type Info struct {
	AppName     string
	Env         string
	Version     string
	InstanceID  string
	Addr        string
	Port        string
	URL         string
	Database    string
	Redis       string
	Task        string
	DocsURL     string
	Health      string
	Storage     string
	HTTPClient  string
	ShutdownTTL string
}

// SQLDatabase 描述可安全展示的 SQL 数据库连接摘要。
type SQLDatabase struct {
	Driver       string
	Host         string
	Port         int
	User         string
	DBName       string
	MaxIdleConns int
	MaxOpenConns int
}

// Print 将启动提示输出到指定 writer。
func Print(writer io.Writer, info Info) {
	if writer == nil {
		return
	}
	fmt.Fprint(writer, Render(info))
}

// Render 渲染启动提示文本。
func Render(info Info) string {
	if info.URL == "" {
		info.URL = LocalURL(info.Addr)
	}
	if info.Port == "" {
		info.Port = ListenPort(info.Addr)
	}

	var builder strings.Builder
	builder.WriteString(defaultLogo)
	builder.WriteString("\n")
	writeLine(&builder, "Application", joinNonEmpty(info.AppName, " ", info.Version))
	writeLine(&builder, "Environment", joinNonEmpty(info.Env, " / instance ", info.InstanceID))
	writeLine(&builder, "HTTP", joinNonEmpty(info.URL, " on ", info.Addr))
	writeLine(&builder, "Port", info.Port)
	writeLine(&builder, "Docs", info.DocsURL)
	writeLine(&builder, "Health", info.Health)
	writeLine(&builder, "Database", info.Database)
	writeLine(&builder, "Redis", info.Redis)
	writeLine(&builder, "Task", info.Task)
	writeLine(&builder, "Storage", info.Storage)
	writeLine(&builder, "HTTP Client", info.HTTPClient)
	writeLine(&builder, "Shutdown", info.ShutdownTTL)
	builder.WriteString("\n")
	return builder.String()
}

// SQLDatabaseSummary 返回不包含密码的 SQL 数据库连接摘要。
func SQLDatabaseSummary(cfg SQLDatabase) string {
	driver := strings.TrimSpace(cfg.Driver)
	if driver == "" {
		driver = "sql"
	}
	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		host = "unknown"
	}
	endpoint := host
	if cfg.Port > 0 {
		endpoint = net.JoinHostPort(host, strconv.Itoa(cfg.Port))
	}
	return fmt.Sprintf("%s://%s/%s user=%s pool=%d/%d",
		driver,
		endpoint,
		emptyAsUnknown(cfg.DBName),
		emptyAsUnknown(cfg.User),
		cfg.MaxIdleConns,
		cfg.MaxOpenConns,
	)
}

// RedisSummary 返回不包含密码的 Redis 连接摘要。
func RedisSummary(cfg redisx.Config) string {
	safe := cfg.SafeForLog()
	enabled, _ := safe["enabled"].(bool)
	if !enabled {
		return "disabled"
	}
	mode := fmt.Sprint(safe["mode"])
	if sentinel, ok := safe["sentinel"].(map[string]any); ok && mode == "sentinel" {
		return fmt.Sprintf("enabled mode=%s master=%s addrs=%v db=%v",
			mode,
			emptyAsUnknown(fmt.Sprint(sentinel["master_name"])),
			sentinel["addrs"],
			safe["db"],
		)
	}
	return fmt.Sprintf("enabled mode=%s addr=%s db=%v", mode, emptyAsUnknown(fmt.Sprint(safe["addr"])), safe["db"])
}

// TaskSummary 返回任务队列启动摘要。
func TaskSummary(cfg task.Config) string {
	if !cfg.Enabled {
		return "disabled"
	}
	normalized := cfg.Normalize()
	parts := []string{
		"enabled",
		"backend=" + string(normalized.Backend),
		"publisher_queue=" + normalized.Publisher.DefaultQueue,
		"worker=" + strconv.FormatBool(normalized.Worker.Enabled),
		"scheduler=" + strconv.FormatBool(normalized.Scheduler.Enabled),
	}
	return strings.Join(parts, " ")
}

// EnabledSummary 使用 enabled/disabled 形式展示开关型能力。
func EnabledSummary(enabled bool, detail string) string {
	if !enabled {
		return "disabled"
	}
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return "enabled"
	}
	return "enabled " + detail
}

// LocalURL 根据监听地址推导开发者最容易点击访问的本机 URL。
func LocalURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		trimmed := strings.TrimSpace(strings.TrimPrefix(addr, ":"))
		if trimmed == "" {
			return ""
		}
		if _, parseErr := strconv.Atoi(trimmed); parseErr == nil {
			return "http://localhost:" + trimmed
		}
		return "http://" + strings.TrimSpace(addr)
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// ListenPort 从监听地址中提取端口号。
func ListenPort(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err == nil {
		return port
	}
	trimmed := strings.TrimSpace(strings.TrimPrefix(addr, ":"))
	if _, parseErr := strconv.Atoi(trimmed); parseErr == nil {
		return trimmed
	}
	return ""
}

// writeLine 写入一行启动提示，空值会被标记为 unknown。
func writeLine(builder *strings.Builder, label string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "unknown"
	}
	fmt.Fprintf(builder, "  %-12s %s\n", label+":", value)
}

// joinNonEmpty 仅在左右两侧都有值时加入分隔符。
func joinNonEmpty(left string, separator string, right string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	switch {
	case left == "":
		return right
	case right == "":
		return left
	default:
		return left + separator + right
	}
}

// emptyAsUnknown 将空白字符串替换为 unknown。
func emptyAsUnknown(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "<nil>" {
		return "unknown"
	}
	return value
}
