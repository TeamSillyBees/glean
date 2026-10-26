package redisx

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/teamsillybees/glean/internal/platform/logx"
)

// CacheMetricsRecorder 记录缓存命中率和错误计数。
type CacheMetricsRecorder interface {
	RecordCacheHit()
	RecordCacheMiss()
	RecordCacheError()
}

// CacheStats 是轻量本地缓存指标收集器，可按需桥接到 OpenTelemetry。
type CacheStats struct {
	hits   atomic.Int64
	misses atomic.Int64
	errors atomic.Int64
}

// CacheStatsSnapshot 表示缓存指标快照。
type CacheStatsSnapshot struct {
	Hits   int64
	Misses int64
	Errors int64
}

// NewCacheStats 创建本地缓存指标收集器。
func NewCacheStats() *CacheStats {
	return &CacheStats{}
}

// RecordCacheHit 记录缓存命中。
func (s *CacheStats) RecordCacheHit() {
	s.hits.Add(1)
}

// RecordCacheMiss 记录缓存未命中。
func (s *CacheStats) RecordCacheMiss() {
	s.misses.Add(1)
}

// RecordCacheError 记录缓存错误。
func (s *CacheStats) RecordCacheError() {
	s.errors.Add(1)
}

// Snapshot 返回当前缓存指标快照。
func (s *CacheStats) Snapshot() CacheStatsSnapshot {
	return CacheStatsSnapshot{
		Hits:   s.hits.Load(),
		Misses: s.misses.Load(),
		Errors: s.errors.Load(),
	}
}

// CommandStats 记录 Redis 命令错误率和慢命令数量。
type CommandStats struct {
	total  atomic.Int64
	errors atomic.Int64
	slow   atomic.Int64
}

// CommandStatsSnapshot 表示 Redis 命令指标快照。
type CommandStatsSnapshot struct {
	Total  int64
	Errors int64
	Slow   int64
}

// Snapshot 返回 Redis 命令指标快照。
func (s *CommandStats) Snapshot() CommandStatsSnapshot {
	return CommandStatsSnapshot{
		Total:  s.total.Load(),
		Errors: s.errors.Load(),
		Slow:   s.slow.Load(),
	}
}

// RedisLogHook 是 go-redis hook，提供慢命令日志、错误日志和命令统计。
type RedisLogHook struct {
	logger        *logx.Logger
	slowThreshold time.Duration
	logSlow       bool
	logError      bool
	stats         *CommandStats
}

// NewRedisLogHook 创建 go-redis hook，仅记录命令名、耗时和错误，不记录 key/value/密码/token。
func NewRedisLogHook(logger *logx.Logger, cfg LogConfig) *RedisLogHook {
	if cfg.SlowThreshold == 0 {
		cfg.SlowThreshold = 200 * time.Millisecond
	}
	return &RedisLogHook{
		logger:        logger,
		slowThreshold: cfg.SlowThreshold,
		logSlow:       cfg.SlowEnabled,
		logError:      cfg.ErrorEnabled,
		stats:         &CommandStats{},
	}
}

// Snapshot 返回当前命令统计快照。
func (h *RedisLogHook) Snapshot() CommandStatsSnapshot {
	if h == nil || h.stats == nil {
		return CommandStatsSnapshot{}
	}
	return h.stats.Snapshot()
}

// DialHook 保留底层拨号行为，不采集连接参数或凭证。
func (h *RedisLogHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

// ProcessHook 包装单命令执行，记录耗时、错误和统计信息。
func (h *RedisLogHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmd)
		h.record(ctx, cmd.Name(), time.Since(start), err)
		return err
	}
}

// ProcessPipelineHook 包装流水线执行，按批次记录命令名、耗时、错误和统计信息。
func (h *RedisLogHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmds)
		names := make([]string, 0, len(cmds))
		for _, cmd := range cmds {
			names = append(names, cmd.Name())
		}
		h.record(ctx, strings.Join(names, ","), time.Since(start), err)
		return err
	}
}

func (h *RedisLogHook) record(ctx context.Context, name string, cost time.Duration, err error) {
	if h.stats != nil {
		h.stats.total.Add(1)
	}
	if err != nil && err != redis.Nil {
		if h.stats != nil {
			h.stats.errors.Add(1)
		}
		if h.logger != nil && h.logError {
			h.logger.Error(ctx, "redis command failed", err,
				logx.String("command", strings.ToUpper(name)),
				logx.Duration("cost", cost),
			)
		}
	}
	if cost >= h.slowThreshold {
		if h.stats != nil {
			h.stats.slow.Add(1)
		}
		if h.logger != nil && h.logSlow {
			h.logger.Warn(ctx, "redis slow command",
				logx.String("command", strings.ToUpper(name)),
				logx.Duration("cost", cost),
				logx.Duration("threshold", h.slowThreshold),
			)
		}
	}
}

// PoolStatsProvider 描述可导出连接池统计的 Redis 客户端。
type PoolStatsProvider interface {
	PoolStats() *redis.PoolStats
}

// ReadPoolStats 读取 go-redis 连接池指标。
func ReadPoolStats(client PoolStatsProvider) *redis.PoolStats {
	if client == nil {
		return nil
	}
	return client.PoolStats()
}
