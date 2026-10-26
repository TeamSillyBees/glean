package auth

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/redisx"
)

func TestRedisTokenStoreUsesRedisxKeyBuilderAndScripts(t *testing.T) {
	server := miniredis.RunT(t)
	ctx := context.Background()
	client, err := redisx.NewClient(ctx, redisx.Config{
		Enabled: true,
		Mode:    redisx.ModeStandalone,
		Addr:    server.Addr(),
		Pool: redisx.PoolConfig{
			Size: 2,
		},
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	store, err := NewRedisTokenStoreWithEnv("initra", "dev", client)
	require.NoError(t, err)
	record := RefreshTokenRecord{
		UserID:          idgen.New(1001),
		AccessTokenID:   "access-1",
		AccessExpiresAt: time.Now().Add(time.Hour),
	}

	require.NoError(t, store.StoreRefreshToken(ctx, "refresh-1", record, time.Hour))
	_, err = server.Get("initra:dev:auth:refresh:refresh-1")
	require.NoError(t, err)

	got, ok, err := store.ValidateRefreshToken(ctx, "refresh-1")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, record.UserID, got.UserID)
	require.Equal(t, record.AccessTokenID, got.AccessTokenID)

	replacement := RefreshTokenRecord{
		UserID:          idgen.New(1001),
		AccessTokenID:   "access-2",
		AccessExpiresAt: time.Now().Add(2 * time.Hour),
	}
	mismatch := record
	mismatch.AccessTokenID = "other-access"
	rotated, err := store.RotateRefreshToken(ctx, "refresh-1", mismatch, "refresh-2", replacement, 2*time.Hour, time.Hour)
	require.NoError(t, err)
	require.False(t, rotated)
	_, ok, err = store.ValidateRefreshToken(ctx, "refresh-1")
	require.NoError(t, err)
	require.True(t, ok)

	rotated, err = store.RotateRefreshToken(ctx, "refresh-1", record, "refresh-2", replacement, 2*time.Hour, time.Hour)
	require.NoError(t, err)
	require.True(t, rotated)

	_, ok, err = store.ValidateRefreshToken(ctx, "refresh-1")
	require.NoError(t, err)
	require.False(t, ok)
	got, ok, err = store.ValidateRefreshToken(ctx, "refresh-2")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, replacement.UserID, got.UserID)
	require.Equal(t, replacement.AccessTokenID, got.AccessTokenID)
	require.True(t, replacement.AccessExpiresAt.Equal(got.AccessExpiresAt))

	rotated, err = store.RotateRefreshToken(ctx, "refresh-1", record, "refresh-3", replacement, time.Hour, time.Hour)
	require.NoError(t, err)
	require.False(t, rotated)

	blacklisted, err := store.IsAccessTokenBlacklisted(ctx, "access-1")
	require.NoError(t, err)
	require.True(t, blacklisted)
	require.True(t, server.Exists("initra:dev:auth:blacklist:access-1"))

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = store.ValidateRefreshToken(canceledCtx, "refresh-2")
	require.ErrorIs(t, err, context.Canceled)
}

func TestRedisTokenStoreRejectsNilClient(t *testing.T) {
	store, err := NewRedisTokenStore("initra", nil)
	require.Nil(t, store)
	require.ErrorContains(t, err, "client 不能为空")
	var typedNil *redis.Client
	store, err = NewRedisTokenStore("initra", typedNil)
	require.Nil(t, store)
	require.ErrorContains(t, err, "client 不能为空")

	var missing *RedisTokenStore
	require.Error(t, missing.StoreRefreshToken(context.Background(), "refresh", RefreshTokenRecord{}, time.Hour))
	_, _, err = missing.ValidateRefreshToken(context.Background(), "refresh")
	require.Error(t, err)
	_, err = missing.IsAccessTokenBlacklisted(context.Background(), "access")
	require.Error(t, err)
}
