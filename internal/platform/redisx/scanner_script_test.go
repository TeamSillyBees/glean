package redisx

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScanPrefixRequiresAllowlistAndLimit(t *testing.T) {
	_, client := newRedisForTest(t)
	ctx := context.Background()

	require.NoError(t, client.Set(ctx, "initra:dev:user:profile:1", "1", 0).Err())
	require.NoError(t, client.Set(ctx, "initra:dev:user:profile:2", "2", 0).Err())
	require.NoError(t, client.Set(ctx, "initra:dev:user:profile:3", "3", 0).Err())

	_, err := ScanPrefix(ctx, client, ScanOptions{
		Prefix:    "initra:dev:user:profile:",
		MaxKeys:   2,
		BatchSize: 1,
	})
	require.ErrorContains(t, err, "allowlist")

	keys, err := ScanPrefix(ctx, client, ScanOptions{
		Prefix:          "initra:dev:user:profile:",
		AllowedPrefixes: []string{"initra:dev:user:profile:"},
		MaxKeys:         2,
		BatchSize:       1,
	})
	require.NoError(t, err)
	require.Len(t, keys, 2)
}

func TestScanPrefixRejectsGlobMetacharacters(t *testing.T) {
	_, client := newRedisForTest(t)

	for _, prefix := range []string{"safe:*", "safe:?", "safe:[ab]", `safe:\escape`} {
		t.Run(prefix, func(t *testing.T) {
			_, err := ScanPrefix(context.Background(), client, ScanOptions{
				Prefix:          prefix,
				AllowedPrefixes: []string{prefix},
				MaxKeys:         10,
				BatchSize:       10,
			})
			require.ErrorContains(t, err, "glob")
		})
	}
}

func TestUnlinkByPrefixSupportsDryRun(t *testing.T) {
	_, client := newRedisForTest(t)
	ctx := context.Background()

	require.NoError(t, client.Set(ctx, "initra:dev:order:detail:1", "1", 0).Err())
	require.NoError(t, client.Set(ctx, "initra:dev:order:detail:2", "2", 0).Err())

	opts := ScanOptions{
		Prefix:          "initra:dev:order:detail:",
		AllowedPrefixes: []string{"initra:dev:order:detail:"},
		MaxKeys:         10,
		BatchSize:       2,
		DryRun:          true,
	}
	result, err := UnlinkByPrefix(ctx, client, opts)
	require.NoError(t, err)
	require.True(t, result.DryRun)
	require.EqualValues(t, 2, result.Matched)
	require.EqualValues(t, 0, result.Deleted)
	require.EqualValues(t, 2, client.Exists(ctx, "initra:dev:order:detail:1", "initra:dev:order:detail:2").Val())

	opts.DryRun = false
	result, err = UnlinkByPrefix(ctx, client, opts)
	require.NoError(t, err)
	require.False(t, result.DryRun)
	require.EqualValues(t, 2, result.Matched)
	require.EqualValues(t, 2, result.Deleted)
}

func TestScriptRegistryRunsRegisteredScriptWithEvalSHA(t *testing.T) {
	_, client := newRedisForTest(t)
	ctx := context.Background()

	registry := NewScriptRegistry()
	require.NoError(t, registry.Register(ScriptDefinition{
		Name:   "incr_by",
		Source: `return redis.call("INCRBY", KEYS[1], ARGV[1])`,
		Keys: []ScriptArgument{{
			Name:        "counter_key",
			Description: "计数器 Key",
		}},
		Args: []ScriptArgument{{
			Name:        "delta",
			Description: "增量",
		}},
	}))

	got, err := registry.Run(ctx, client, "incr_by", []string{"counter"}, 2).Int64()
	require.NoError(t, err)
	require.EqualValues(t, 2, got)

	got, err = registry.Run(ctx, client, "incr_by", []string{"counter"}, 3).Int64()
	require.NoError(t, err)
	require.EqualValues(t, 5, got)
}

func TestScriptRegistryRequiresDocumentedKeysAndArgs(t *testing.T) {
	registry := NewScriptRegistry()
	err := registry.Register(ScriptDefinition{
		Name:   "bad",
		Source: `return 1`,
		Keys: []ScriptArgument{{
			Name: "key_without_description",
		}},
	})
	require.ErrorContains(t, err, "key")
}
