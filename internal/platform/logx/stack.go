package logx

import "strings"

// StackMode 描述 oops stacktrace 的日志输出策略。
type StackMode string

const (
	// StackNone 表示不输出错误栈。
	StackNone StackMode = "none"
	// StackShort 表示只输出前几个关键栈帧。
	StackShort StackMode = "short"
	// StackFull 表示输出完整 oops stacktrace。
	StackFull StackMode = "full"
)

// shortStackFrameLimit 是 short stack 模式最多保留的非空栈帧数。
const shortStackFrameLimit = 5

// renderStack 按配置裁剪或关闭 oops stacktrace。
func renderStack(stacktrace string, mode StackMode) string {
	stacktrace = strings.TrimSpace(stacktrace)
	if stacktrace == "" || mode == StackNone {
		return ""
	}
	if mode == StackFull {
		return stacktrace
	}

	lines := strings.Split(stacktrace, "\n")
	selected := make([]string, 0, shortStackFrameLimit)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		selected = append(selected, line)
		if len(selected) >= shortStackFrameLimit {
			break
		}
	}
	return strings.Join(selected, "\n")
}
