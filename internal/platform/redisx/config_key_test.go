package redisx

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConfigValidateStandaloneAndSafeForLog(t *testing.T) {
	cfg := Config{
		Enabled:  true,
		Mode:     ModeStandalone,
		Addr:     "127.0.0.1:6379",
		Username: "app",
		Password: "secret",
		DB:       2,
		Pool: PoolConfig{
			Size: 4,
		},
		Timeout: TimeoutConfig{
			Dial:  2 * time.Second,
			Read:  time.Second,
			Write: time.Second,
		},
		Retry: RetryConfig{
			MaxRetries: 2,
		},
	}

	require.NoError(t, cfg.Validate())

	printable := cfg.SafeForLog()
	require.Equal(t, "***", printable["password"])
	require.Equal(t, "app", printable["username"])
	require.NotContains(t, printable, "secret")
}

func TestConfigValidateSentinelRequiresMasterAndSentinels(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Mode:    ModeSentinel,
		Pool: PoolConfig{
			Size: 2,
		},
	}

	require.ErrorContains(t, cfg.Validate(), "redis.sentinel.master_name")

	cfg.Sentinel.MasterName = "mymaster"
	require.ErrorContains(t, cfg.Validate(), "redis.sentinel.addrs")

	cfg.Sentinel.Addrs = []string{"127.0.0.1:26379"}
	require.NoError(t, cfg.Validate())
}

func TestConfigRejectsClusterMode(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Mode:    Mode("cluster"),
		Addr:    "127.0.0.1:6379",
		Pool: PoolConfig{
			Size: 2,
		},
	}

	require.ErrorContains(t, cfg.Validate(), "cluster")
}

func TestKeyBuilderBuildsRegisteredPrefix(t *testing.T) {
	builder := NewKeyBuilder(KeyConfig{
		App: "initra",
		Env: "dev",
	})

	require.NoError(t, builder.RegisterPrefix("user_profile", "user", "profile"))
	key, err := builder.Build("user_profile", 1001)
	require.NoError(t, err)
	require.Equal(t, "initra:dev:user:profile:1001", key)

	prefix, err := builder.Prefix("user_profile")
	require.NoError(t, err)
	require.Equal(t, "initra:dev:user:profile:", prefix)
	require.True(t, builder.IsAllowedPrefix(prefix))
}

func TestKeyBuilderRejectsUnknownPrefix(t *testing.T) {
	builder := NewKeyBuilder(KeyConfig{App: "initra", Env: "prod"})

	_, err := builder.Build("session", "abc")
	require.ErrorContains(t, err, "未注册")
}
