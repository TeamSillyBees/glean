package redisx

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
	"github.com/teamsillybees/glean/internal/platform/logx"
)

// UniversalClient 暴露 go-redis 通用客户端类型，调用方无需直接 import go-redis。
type UniversalClient = redis.UniversalClient

// Cmdable 暴露 go-redis 命令接口类型，调用方无需直接 import go-redis。
type Cmdable = redis.Cmdable

// PubSub 暴露 go-redis 订阅句柄类型，业务代码无需直接 import go-redis。
type PubSub = redis.PubSub

// ErrNil 表示 Redis key 不存在。
var ErrNil = redis.Nil

// CommandClient 描述 redisx 需要的 Redis 命令与脚本执行能力。
type CommandClient interface {
	redis.Cmdable
	redis.Scripter
}

// NewClient 创建 Redis 客户端并执行 Ping 检查；禁用时返回 nil。
func NewClient(ctx context.Context, cfg Config, logger *logx.Logger) (redis.UniversalClient, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	var client redis.UniversalClient
	switch cfg.withDefaults().Mode {
	case ModeStandalone:
		client = redis.NewClient(cfg.redisOptions())
	case ModeSentinel:
		client = redis.NewFailoverClient(cfg.failoverOptions())
	default:
		return nil, fmt.Errorf("redis mode %q 不受支持", cfg.Mode)
	}

	if logger != nil && cfg.withDefaults().Log.Enabled {
		client.AddHook(NewRedisLogHook(logger, cfg.Log))
	}
	if cfg.Observability.TracingEnabled {
		if err := redisotel.InstrumentTracing(client); err != nil {
			_ = client.Close()
			return nil, err
		}
	}
	if cfg.Observability.MetricsEnabled {
		if err := redisotel.InstrumentMetrics(client); err != nil {
			_ = client.Close()
			return nil, err
		}
	}
	if err := Ping(ctx, client); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

// Store 提供少量常用 Redis 数据结构便捷方法；复杂场景直接使用底层 go-redis client。
type Store struct {
	client redis.Cmdable
}

// NewStore 创建基础数据结构便捷封装。
func NewStore(client redis.Cmdable) *Store {
	return &Store{client: client}
}

// Client 返回底层 go-redis 客户端。
func (s *Store) Client() redis.Cmdable {
	return s.client
}

// SetString 设置 String 值。
func (s *Store) SetString(ctx context.Context, key string, value any, ttl time.Duration) error {
	return s.client.Set(ctx, key, value, ttl).Err()
}

// GetString 读取 String 值，key 不存在时 found=false。
func (s *Store) GetString(ctx context.Context, key string) (value string, found bool, err error) {
	value, err = s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

// Exists 判断 key 是否存在。
func (s *Store) Exists(ctx context.Context, key string) (bool, error) {
	count, err := s.client.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// HSet 设置 Hash 字段。
func (s *Store) HSet(ctx context.Context, key string, values ...any) error {
	return s.client.HSet(ctx, key, values...).Err()
}

// HGetAll 读取完整 Hash。
func (s *Store) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	return s.client.HGetAll(ctx, key).Result()
}

// LPush 向 List 左侧追加元素。
func (s *Store) LPush(ctx context.Context, key string, values ...any) error {
	return s.client.LPush(ctx, key, values...).Err()
}

// LRange 读取 List 范围。
func (s *Store) LRange(ctx context.Context, key string, start int64, stop int64) ([]string, error) {
	return s.client.LRange(ctx, key, start, stop).Result()
}

// SAdd 向 Set 添加元素。
func (s *Store) SAdd(ctx context.Context, key string, members ...any) error {
	return s.client.SAdd(ctx, key, members...).Err()
}

// SMembers 读取 Set 全部元素。
func (s *Store) SMembers(ctx context.Context, key string) ([]string, error) {
	return s.client.SMembers(ctx, key).Result()
}

// ZAdd 向 ZSet 添加元素。
func (s *Store) ZAdd(ctx context.Context, key string, members ...redis.Z) error {
	return s.client.ZAdd(ctx, key, members...).Err()
}

// ZRangeWithScores 读取 ZSet 范围和分数。
func (s *Store) ZRangeWithScores(ctx context.Context, key string, start int64, stop int64) ([]redis.Z, error) {
	return s.client.ZRangeWithScores(ctx, key, start, stop).Result()
}

// SetBit 设置 Bitmap 位。
func (s *Store) SetBit(ctx context.Context, key string, offset int64, value int) error {
	return s.client.SetBit(ctx, key, offset, value).Err()
}

// GetBit 读取 Bitmap 位。
func (s *Store) GetBit(ctx context.Context, key string, offset int64) (int64, error) {
	return s.client.GetBit(ctx, key, offset).Result()
}

// PFAdd 向 HyperLogLog 添加元素。
func (s *Store) PFAdd(ctx context.Context, key string, values ...any) error {
	return s.client.PFAdd(ctx, key, values...).Err()
}

// PFCount 返回 HyperLogLog 估算基数。
func (s *Store) PFCount(ctx context.Context, keys ...string) (int64, error) {
	return s.client.PFCount(ctx, keys...).Result()
}
