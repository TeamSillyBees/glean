package redisx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bsm/redislock"
)

// LockOptions 描述分布式锁获取参数。
type LockOptions struct {
	RetryStrategy redislock.RetryStrategy
	Metadata      string
	Token         string
}

// Locker 基于 github.com/bsm/redislock 封装短时间互斥锁。
//
// 该锁仅适合短时间互斥或效率优化，不作为强一致事务方案。强一致场景应配合数据库事务、
// 唯一约束、乐观锁、幂等表或 fencing token。
type Locker struct {
	client *redislock.Client
}

// NewLocker 创建分布式锁管理器。
func NewLocker(client redislock.RedisClient) *Locker {
	return &Locker{client: redislock.New(client)}
}

// Obtain 获取锁，ttl 必须显式大于 0。
func (l *Locker) Obtain(ctx context.Context, key string, ttl time.Duration, opts *LockOptions) (*Lock, error) {
	if l == nil || l.client == nil {
		return nil, fmt.Errorf("redis locker 不能为空")
	}
	if key == "" {
		return nil, fmt.Errorf("redis lock key 不能为空")
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("redis lock ttl 必须大于 0")
	}
	lock, err := l.client.Obtain(ctx, key, ttl, toRedisLockOptions(opts))
	if err != nil {
		return nil, err
	}
	return &Lock{lock: lock}, nil
}

// Lock 表示已获取的 Redis 分布式锁。
type Lock struct {
	lock *redislock.Lock
}

// Token 返回锁 token；release/refresh 由 redislock 使用 token 校验。
func (l *Lock) Token() string {
	if l == nil || l.lock == nil {
		return ""
	}
	return l.lock.Token()
}

// TTL 查询当前锁剩余时间。
func (l *Lock) TTL(ctx context.Context) (time.Duration, error) {
	if l == nil || l.lock == nil {
		return 0, redislock.ErrLockNotHeld
	}
	return l.lock.TTL(ctx)
}

// Refresh 使用 token 校验后刷新锁过期时间。
func (l *Lock) Refresh(ctx context.Context, ttl time.Duration) error {
	if l == nil || l.lock == nil {
		return redislock.ErrLockNotHeld
	}
	if ttl <= 0 {
		return fmt.Errorf("redis lock ttl 必须大于 0")
	}
	return l.lock.Refresh(ctx, ttl, nil)
}

// Release 使用 token 校验后释放锁。
func (l *Lock) Release(ctx context.Context) error {
	if l == nil || l.lock == nil {
		return redislock.ErrLockNotHeld
	}
	return l.lock.Release(ctx)
}

// IsLockNotObtained 判断错误是否表示锁未获取。
func IsLockNotObtained(err error) bool {
	return errors.Is(err, redislock.ErrNotObtained)
}

// IsLockNotHeld 判断错误是否表示锁已不属于当前持有者。
func IsLockNotHeld(err error) bool {
	return errors.Is(err, redislock.ErrLockNotHeld)
}

func toRedisLockOptions(opts *LockOptions) *redislock.Options {
	if opts == nil {
		return nil
	}
	return &redislock.Options{
		RetryStrategy: opts.RetryStrategy,
		Metadata:      opts.Metadata,
		Token:         opts.Token,
	}
}
