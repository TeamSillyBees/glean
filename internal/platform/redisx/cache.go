package redisx

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

// CacheOptions 描述 Redis 泛型缓存参数。
type CacheOptions struct {
	Client   redis.Cmdable
	Codec    Codec
	TTL      time.Duration
	Jitter   time.Duration
	NullTTL  time.Duration
	CacheNil bool
	Metrics  CacheMetricsRecorder
}

// LoadFunc 描述缓存未命中时的加载函数；第二个返回值表示业务值是否存在。
type LoadFunc[T any] func(context.Context) (T, bool, error)

// Cache 提供 Get/Set/Delete/GetOrLoad、空值缓存和 singleflight 防击穿能力。
type Cache[T any] struct {
	client   redis.Cmdable
	codec    Codec
	ttl      time.Duration
	jitter   time.Duration
	nullTTL  time.Duration
	cacheNil bool
	metrics  CacheMetricsRecorder
	group    singleflight.Group
}

type cacheEnvelope struct {
	Null bool   `json:"null" msgpack:"null"`
	Data []byte `json:"data,omitempty" msgpack:"data,omitempty"`
}

type cacheState int

const (
	cacheMiss cacheState = iota
	cacheHitValue
	cacheHitNil
)

type cacheLoadResult[T any] struct {
	value T
	ok    bool
}

// NewCache 创建 Redis 泛型缓存。
func NewCache[T any](opts CacheOptions) (*Cache[T], error) {
	if opts.Client == nil {
		return nil, fmt.Errorf("redis cache client 不能为空")
	}
	if opts.TTL <= 0 {
		return nil, fmt.Errorf("redis cache ttl 必须大于 0")
	}
	if opts.Jitter < 0 || opts.NullTTL < 0 {
		return nil, fmt.Errorf("redis cache ttl 配置不能为负数")
	}
	if opts.Codec == nil {
		opts.Codec = JSONCodec{}
	}
	if opts.NullTTL == 0 {
		opts.NullTTL = opts.TTL
	}
	return &Cache[T]{
		client:   opts.Client,
		codec:    opts.Codec,
		ttl:      opts.TTL,
		jitter:   opts.Jitter,
		nullTTL:  opts.NullTTL,
		cacheNil: opts.CacheNil,
		metrics:  opts.Metrics,
	}, nil
}

// Set 写入业务值。
func (c *Cache[T]) Set(ctx context.Context, key string, value T) error {
	payload, err := c.marshalValue(value)
	if err != nil {
		c.recordError()
		return err
	}
	if err := c.client.Set(ctx, key, payload, withJitter(c.ttl, c.jitter)).Err(); err != nil {
		c.recordError()
		return err
	}
	return nil
}

// Get 读取业务值；key 不存在或命中空值缓存时 found=false。
func (c *Cache[T]) Get(ctx context.Context, key string) (value T, found bool, err error) {
	state, value, err := c.get(ctx, key)
	switch state {
	case cacheHitValue:
		return value, true, err
	default:
		var zero T
		return zero, false, err
	}
}

// Delete 删除缓存 key。
func (c *Cache[T]) Delete(ctx context.Context, key string) error {
	if err := c.client.Del(ctx, key).Err(); err != nil {
		c.recordError()
		return err
	}
	return nil
}

// GetOrLoad 先读缓存，未命中时使用 singleflight 合并加载并回填缓存。
func (c *Cache[T]) GetOrLoad(ctx context.Context, key string, loader LoadFunc[T]) (T, bool, error) {
	state, value, err := c.get(ctx, key)
	if err != nil {
		var zero T
		return zero, false, err
	}
	if state == cacheHitValue {
		return value, true, nil
	}
	if state == cacheHitNil {
		var zero T
		return zero, false, nil
	}

	result, err, _ := c.group.Do(key, func() (any, error) {
		state, value, err := c.get(ctx, key)
		if err != nil {
			var zero T
			return cacheLoadResult[T]{value: zero, ok: false}, err
		}
		if state == cacheHitValue {
			return cacheLoadResult[T]{value: value, ok: true}, nil
		}
		if state == cacheHitNil {
			var zero T
			return cacheLoadResult[T]{value: zero, ok: false}, nil
		}

		loaded, ok, err := loader(ctx)
		if err != nil {
			c.recordError()
			var zero T
			return cacheLoadResult[T]{value: zero, ok: false}, err
		}
		if ok {
			if err := c.Set(ctx, key, loaded); err != nil {
				var zero T
				return cacheLoadResult[T]{value: zero, ok: false}, err
			}
		} else if c.cacheNil {
			if err := c.setNil(ctx, key); err != nil {
				var zero T
				return cacheLoadResult[T]{value: zero, ok: false}, err
			}
		}
		return cacheLoadResult[T]{value: loaded, ok: ok}, nil
	})
	if err != nil {
		var zero T
		return zero, false, err
	}
	typed := result.(cacheLoadResult[T])
	return typed.value, typed.ok, nil
}

func (c *Cache[T]) get(ctx context.Context, key string) (cacheState, T, error) {
	var zero T
	payload, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		c.recordMiss()
		return cacheMiss, zero, nil
	}
	if err != nil {
		c.recordError()
		return cacheMiss, zero, err
	}

	var envelope cacheEnvelope
	if err := c.codec.Unmarshal(payload, &envelope); err != nil {
		c.recordError()
		return cacheMiss, zero, c.cleanCorrupt(ctx, key, err)
	}
	if envelope.Null {
		c.recordHit()
		return cacheHitNil, zero, nil
	}

	var value T
	if err := c.codec.Unmarshal(envelope.Data, &value); err != nil {
		c.recordError()
		return cacheMiss, zero, c.cleanCorrupt(ctx, key, err)
	}
	c.recordHit()
	return cacheHitValue, value, nil
}

func (c *Cache[T]) marshalValue(value T) ([]byte, error) {
	data, err := c.codec.Marshal(value)
	if err != nil {
		return nil, err
	}
	return c.codec.Marshal(cacheEnvelope{Data: data})
}

func (c *Cache[T]) setNil(ctx context.Context, key string) error {
	payload, err := c.codec.Marshal(cacheEnvelope{Null: true})
	if err != nil {
		c.recordError()
		return err
	}
	if err := c.client.Set(ctx, key, payload, withJitter(c.nullTTL, c.jitter)).Err(); err != nil {
		c.recordError()
		return err
	}
	return nil
}

func (c *Cache[T]) cleanCorrupt(ctx context.Context, key string, cause error) error {
	if err := c.client.Unlink(ctx, key).Err(); err != nil {
		return fmt.Errorf("redis cache payload decode failed and cleanup failed: %w", err)
	}
	return fmt.Errorf("redis cache payload decode failed: %w", cause)
}

func (c *Cache[T]) recordHit() {
	if c.metrics != nil {
		c.metrics.RecordCacheHit()
	}
}

func (c *Cache[T]) recordMiss() {
	if c.metrics != nil {
		c.metrics.RecordCacheMiss()
	}
}

func (c *Cache[T]) recordError() {
	if c.metrics != nil {
		c.metrics.RecordCacheError()
	}
}

func withJitter(ttl time.Duration, jitter time.Duration) time.Duration {
	if jitter <= 0 {
		return ttl
	}
	max := big.NewInt(int64(jitter) + 1)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return ttl
	}
	return ttl + time.Duration(n.Int64())
}
