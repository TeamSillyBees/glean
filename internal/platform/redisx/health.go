package redisx

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Ping 执行 Redis Ping 健康检查。
func Ping(ctx context.Context, client redis.Cmdable) error {
	if client == nil {
		return fmt.Errorf("redis client 不能为空")
	}
	if err := client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis ping failed: %w", err)
	}
	return nil
}

// NamedClient 描述 readiness 检查中的一个 Redis 客户端。
type NamedClient struct {
	Name   string
	Client redis.Cmdable
}

// Readiness 对多个 Redis 客户端执行 Ping 检查。
func Readiness(ctx context.Context, clients ...NamedClient) error {
	for _, item := range clients {
		if err := Ping(ctx, item.Client); err != nil {
			if item.Name == "" {
				return err
			}
			return fmt.Errorf("%s: %w", item.Name, err)
		}
	}
	return nil
}
