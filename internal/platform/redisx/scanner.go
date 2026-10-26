package redisx

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/redis/go-redis/v9"
)

// ScanOptions 描述受限前缀扫描和删除参数。
type ScanOptions struct {
	Prefix          string
	AllowedPrefixes []string
	MaxKeys         int64
	BatchSize       int64
	DryRun          bool
}

// DeleteResult 描述 SCAN + UNLINK 的结果。
type DeleteResult struct {
	Matched int64
	Deleted int64
	DryRun  bool
}

// ScanPrefix 使用 SCAN 按允许前缀扫描 key，禁止无上限扫描。
func ScanPrefix(ctx context.Context, client redis.Cmdable, opts ScanOptions) ([]string, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	var cursor uint64
	keys := make([]string, 0)
	for {
		batch, next, err := client.Scan(ctx, cursor, opts.Prefix+"*", opts.BatchSize).Result()
		if err != nil {
			return nil, err
		}
		cursor = next
		for _, key := range batch {
			if int64(len(keys)) >= opts.MaxKeys {
				return keys, nil
			}
			keys = append(keys, key)
		}
		if cursor == 0 {
			return keys, nil
		}
	}
}

// UnlinkByPrefix 使用 SCAN + UNLINK 删除指定前缀的 key；DryRun=true 时只返回匹配数。
func UnlinkByPrefix(ctx context.Context, client redis.Cmdable, opts ScanOptions) (DeleteResult, error) {
	keys, err := ScanPrefix(ctx, client, opts)
	if err != nil {
		return DeleteResult{}, err
	}
	result := DeleteResult{
		Matched: int64(len(keys)),
		DryRun:  opts.DryRun,
	}
	if opts.DryRun || len(keys) == 0 {
		return result, nil
	}
	for start := 0; start < len(keys); start += int(opts.BatchSize) {
		end := start + int(opts.BatchSize)
		if end > len(keys) {
			end = len(keys)
		}
		deleted, err := client.Unlink(ctx, keys[start:end]...).Result()
		if err != nil {
			return result, err
		}
		result.Deleted += deleted
	}
	return result, nil
}

func (o ScanOptions) validate() error {
	if o.Prefix == "" {
		return fmt.Errorf("redis scan prefix 不能为空")
	}
	if strings.ContainsAny(o.Prefix, "*?[]\\") {
		return fmt.Errorf("redis scan prefix %q 不能包含 glob 元字符", o.Prefix)
	}
	if o.MaxKeys <= 0 {
		return fmt.Errorf("redis scan maxKeys 必须大于 0")
	}
	if o.BatchSize <= 0 {
		return fmt.Errorf("redis scan batchSize 必须大于 0")
	}
	if len(o.AllowedPrefixes) == 0 {
		return fmt.Errorf("redis scan prefix allowlist 不能为空")
	}
	if !slices.Contains(o.AllowedPrefixes, o.Prefix) {
		return fmt.Errorf("redis scan prefix %q 不在 allowlist 中", o.Prefix)
	}
	return nil
}
