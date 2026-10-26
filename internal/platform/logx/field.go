package logx

import (
	"time"

	"go.uber.org/zap"
)

// Field 是 logx 对外暴露的结构化日志字段类型。
type Field = zap.Field

// String 创建字符串日志字段。
func String(key string, value string) Field {
	return zap.String(key, value)
}

// Strings 创建字符串切片日志字段。
func Strings(key string, values []string) Field {
	return zap.Reflect(key, append([]string(nil), values...))
}

// Int 创建 int 日志字段。
func Int(key string, value int) Field {
	return zap.Int(key, value)
}

// Int64 创建 int64 日志字段。
func Int64(key string, value int64) Field {
	return zap.Int64(key, value)
}

// Duration 创建 duration 日志字段。
func Duration(key string, value time.Duration) Field {
	return zap.Duration(key, value)
}

// Any 创建任意类型日志字段。
func Any(key string, value any) Field {
	return zap.Reflect(key, value)
}
