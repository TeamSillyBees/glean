package redisx

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewClientStandalonePingsRedis(t *testing.T) {
	server, _ := newRedisForTest(t)
	ctx := context.Background()

	client, err := NewClient(ctx, Config{
		Enabled: true,
		Mode:    ModeStandalone,
		Addr:    server.Addr(),
		Pool: PoolConfig{
			Size: 2,
		},
		Timeout: TimeoutConfig{
			Dial:  time.Second,
			Read:  time.Second,
			Write: time.Second,
		},
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	require.NoError(t, Ping(ctx, client))
}

func TestNewClientDisabledReturnsNilClient(t *testing.T) {
	client, err := NewClient(context.Background(), Config{Enabled: false}, nil)
	require.NoError(t, err)
	require.Nil(t, client)
}
