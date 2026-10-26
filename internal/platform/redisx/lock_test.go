package redisx

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLockerAcquireRefreshTTLAndRelease(t *testing.T) {
	_, client := newRedisForTest(t)
	ctx := context.Background()

	locker := NewLocker(client)
	lock, err := locker.Obtain(ctx, "lock:job:1", time.Minute, nil)
	require.NoError(t, err)
	require.NotEmpty(t, lock.Token())

	_, err = locker.Obtain(ctx, "lock:job:1", time.Minute, nil)
	require.Error(t, err)

	ttl, err := lock.TTL(ctx)
	require.NoError(t, err)
	require.Greater(t, ttl, time.Duration(0))

	require.NoError(t, lock.Refresh(ctx, 2*time.Minute))
	ttl, err = lock.TTL(ctx)
	require.NoError(t, err)
	require.Greater(t, ttl, time.Minute)

	require.NoError(t, lock.Release(ctx))
	lock2, err := locker.Obtain(ctx, "lock:job:1", time.Minute, nil)
	require.NoError(t, err)
	require.NoError(t, lock2.Release(ctx))
}

func TestLockerRequiresTTL(t *testing.T) {
	_, client := newRedisForTest(t)

	locker := NewLocker(client)
	_, err := locker.Obtain(context.Background(), "lock:job:1", 0, nil)
	require.ErrorContains(t, err, "ttl")
}
