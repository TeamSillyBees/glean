package auth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// TestMemoryTokenStoreConsumesRefreshTokenOnce 验证内存 token store 支持 refresh token 一次性消费。
func TestMemoryTokenStoreConsumesRefreshTokenOnce(t *testing.T) {
	store := NewMemoryTokenStore()
	record := RefreshTokenRecord{
		UserID:          idgen.New(1001),
		AccessTokenID:   "access-1",
		AccessExpiresAt: time.Now().Add(time.Hour),
	}

	require.NoError(t, store.StoreRefreshToken(context.Background(), "refresh-1", record, time.Hour))

	got, valid, err := store.ConsumeRefreshToken(context.Background(), "refresh-1")
	require.NoError(t, err)
	require.True(t, valid)
	require.Equal(t, record, got)

	_, valid, err = store.ValidateRefreshToken(context.Background(), "refresh-1")
	require.NoError(t, err)
	require.False(t, valid)
}

// TestMemoryTokenStoreBlacklistsAccessToken 验证内存 token store 支持 access token 黑名单。
func TestMemoryTokenStoreBlacklistsAccessToken(t *testing.T) {
	store := NewMemoryTokenStore()

	require.NoError(t, store.BlacklistAccessToken(context.Background(), "access-1", time.Hour))

	blacklisted, err := store.IsAccessTokenBlacklisted(context.Background(), "access-1")
	require.NoError(t, err)
	require.True(t, blacklisted)
}

// TestMemoryTokenStoreRotatesRefreshTokenAtomically 验证记录不匹配时不消费旧 token，匹配时一次性完成替换和吊销。
func TestMemoryTokenStoreRotatesRefreshTokenAtomically(t *testing.T) {
	store := NewMemoryTokenStore()
	oldRecord := RefreshTokenRecord{
		UserID:          idgen.New(1001),
		AccessTokenID:   "access-1",
		AccessExpiresAt: time.Now().Add(time.Hour),
	}
	newRecord := RefreshTokenRecord{
		UserID:          idgen.New(1001),
		AccessTokenID:   "access-2",
		AccessExpiresAt: time.Now().Add(2 * time.Hour),
	}
	require.NoError(t, store.StoreRefreshToken(context.Background(), "refresh-1", oldRecord, time.Hour))

	mismatch := oldRecord
	mismatch.AccessTokenID = "other"
	rotated, err := store.RotateRefreshToken(context.Background(), "refresh-1", mismatch, "refresh-2", newRecord, 2*time.Hour, time.Hour)
	require.NoError(t, err)
	require.False(t, rotated)
	_, valid, err := store.ValidateRefreshToken(context.Background(), "refresh-1")
	require.NoError(t, err)
	require.True(t, valid)

	rotated, err = store.RotateRefreshToken(context.Background(), "refresh-1", oldRecord, "refresh-2", newRecord, 2*time.Hour, time.Hour)
	require.NoError(t, err)
	require.True(t, rotated)
	_, valid, err = store.ValidateRefreshToken(context.Background(), "refresh-1")
	require.NoError(t, err)
	require.False(t, valid)
	got, valid, err := store.ValidateRefreshToken(context.Background(), "refresh-2")
	require.NoError(t, err)
	require.True(t, valid)
	require.Equal(t, newRecord, got)
	blacklisted, err := store.IsAccessTokenBlacklisted(context.Background(), "access-1")
	require.NoError(t, err)
	require.True(t, blacklisted)
}
