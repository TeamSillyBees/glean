package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/redisx"
)

func testLoginProtectionConfig() LoginProtectionConfig {
	return LoginProtectionConfig{
		Enabled:          true,
		AccountRateLimit: LoginRateConfig{MaxAttempts: 2, Window: time.Minute},
		IPRateLimit:      LoginRateConfig{MaxAttempts: 3, Window: time.Minute},
		Lockout:          LoginLockConfig{MaxFailures: 2, FailureWindow: 5 * time.Minute, LockDuration: 15 * time.Minute},
	}
}

func newRedisLoginGuardForTest(t *testing.T, config LoginProtectionConfig) (LoginGuard, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client, err := redisx.NewClient(t.Context(), redisx.Config{
		Enabled: true,
		Mode:    redisx.ModeStandalone,
		Addr:    server.Addr(),
		Pool:    redisx.PoolConfig{Size: 2},
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	guard, err := NewRedisLoginGuard("initra", "test", client, config)
	require.NoError(t, err)
	return guard, server
}

// TestRedisLoginGuardLimitsAccountAndSourceIP 验证账号与来源 IP 速率窗口在 Redis 中独立生效。
func TestRedisLoginGuardLimitsAccountAndSourceIP(t *testing.T) {
	guard, _ := newRedisLoginGuardForTest(t, testLoginProtectionConfig())
	ctx := context.Background()

	require.NoError(t, guard.Check(ctx, "alice", "203.0.113.10"))
	require.NoError(t, guard.Check(ctx, "alice", "203.0.113.11"))
	require.ErrorIs(t, guard.Check(ctx, "alice", "203.0.113.12"), ErrLoginRateLimited)

	guard, _ = newRedisLoginGuardForTest(t, testLoginProtectionConfig())
	require.NoError(t, guard.Check(ctx, "alice", "203.0.113.20"))
	require.NoError(t, guard.Check(ctx, "bob", "203.0.113.20"))
	require.NoError(t, guard.Check(ctx, "carol", "203.0.113.20"))
	require.ErrorIs(t, guard.Check(ctx, "dave", "203.0.113.20"), ErrLoginRateLimited)
}

// TestRedisLoginGuardSharesLockAndResetsFailures 验证连续失败锁定可跨实例读取，成功后可清零。
func TestRedisLoginGuardSharesLockAndResetsFailures(t *testing.T) {
	config := testLoginProtectionConfig()
	server := miniredis.RunT(t)
	client, err := redisx.NewClient(t.Context(), redisx.Config{
		Enabled: true,
		Mode:    redisx.ModeStandalone,
		Addr:    server.Addr(),
		Pool:    redisx.PoolConfig{Size: 2},
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	first, err := NewRedisLoginGuard("initra", "test", client, config)
	require.NoError(t, err)
	second, err := NewRedisLoginGuard("initra", "test", client, config)
	require.NoError(t, err)

	require.NoError(t, first.RecordFailure(t.Context(), "Alice"))
	require.ErrorIs(t, first.RecordFailure(t.Context(), "alice"), ErrLoginLocked)
	require.ErrorIs(t, second.Check(t.Context(), " ALICE ", "203.0.113.30"), ErrLoginLocked)
	require.NoError(t, second.Reset(t.Context(), "alice"))
	require.NoError(t, first.Check(t.Context(), "alice", "203.0.113.30"))

	for _, key := range server.Keys() {
		require.NotContains(t, key, "alice")
		require.NotContains(t, key, "203.0.113.30")
	}
}

// TestMemoryLoginGuardExpiresLock 验证本地测试实现遵循相同的锁定时效。
func TestMemoryLoginGuardExpiresLock(t *testing.T) {
	guard, err := NewMemoryLoginGuard(testLoginProtectionConfig())
	require.NoError(t, err)
	memory := guard.(*MemoryLoginGuard)
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	memory.now = func() time.Time { return now }

	require.NoError(t, memory.RecordFailure(t.Context(), "alice"))
	require.ErrorIs(t, memory.RecordFailure(t.Context(), "alice"), ErrLoginLocked)
	require.ErrorIs(t, memory.Check(t.Context(), "alice", "203.0.113.40"), ErrLoginLocked)
	now = now.Add(16 * time.Minute)
	require.NoError(t, memory.Check(t.Context(), "alice", "203.0.113.40"))
}

func TestLoginProtectionConfigRejectsIncompleteEnabledPolicy(t *testing.T) {
	err := (LoginProtectionConfig{Enabled: true}).Validate()
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrLoginProtectionStoreFailure))
}
