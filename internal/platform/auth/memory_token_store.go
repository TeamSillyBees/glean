package auth

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MemoryTokenStore 是进程内 token 状态存储，适合本地开发、测试或单实例服务。
type MemoryTokenStore struct {
	mu              sync.Mutex
	refreshTokens   map[string]memoryRefreshToken
	accessBlacklist map[string]time.Time
	now             func() time.Time
}

type memoryRefreshToken struct {
	record    RefreshTokenRecord
	expiresAt time.Time
}

// NewMemoryTokenStore 创建进程内 token 状态存储。
func NewMemoryTokenStore() *MemoryTokenStore {
	return &MemoryTokenStore{
		refreshTokens:   map[string]memoryRefreshToken{},
		accessBlacklist: map[string]time.Time{},
		now:             time.Now,
	}
}

// StoreRefreshToken 保存 refresh token 指纹和关联 access token 信息。
func (s *MemoryTokenStore) StoreRefreshToken(_ context.Context, tokenID string, record RefreshTokenRecord, ttl time.Duration) error {
	if s == nil {
		return nil
	}
	if ttl <= 0 {
		return fmt.Errorf("refresh token ttl must be positive")
	}

	now := s.currentTime()
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ensureMapsLocked()
	s.pruneLocked(now)
	s.refreshTokens[tokenID] = memoryRefreshToken{
		record:    record,
		expiresAt: now.Add(ttl),
	}
	return nil
}

// ValidateRefreshToken 校验 refresh token 是否仍处于有效状态。
func (s *MemoryTokenStore) ValidateRefreshToken(_ context.Context, tokenID string) (RefreshTokenRecord, bool, error) {
	if s == nil {
		return RefreshTokenRecord{}, false, nil
	}

	now := s.currentTime()
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ensureMapsLocked()
	s.pruneLocked(now)
	item, ok := s.refreshTokens[tokenID]
	if !ok {
		return RefreshTokenRecord{}, false, nil
	}
	return item.record, true, nil
}

// ConsumeRefreshToken 原子读取并删除 refresh token。
func (s *MemoryTokenStore) ConsumeRefreshToken(_ context.Context, tokenID string) (RefreshTokenRecord, bool, error) {
	if s == nil {
		return RefreshTokenRecord{}, false, nil
	}

	now := s.currentTime()
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ensureMapsLocked()
	s.pruneLocked(now)
	item, ok := s.refreshTokens[tokenID]
	if !ok {
		return RefreshTokenRecord{}, false, nil
	}
	delete(s.refreshTokens, tokenID)
	return item.record, true, nil
}

// RotateRefreshToken 在同一临界区内校验旧记录、替换 refresh token 并吊销配对 access token。
func (s *MemoryTokenStore) RotateRefreshToken(
	_ context.Context,
	oldTokenID string,
	expected RefreshTokenRecord,
	newTokenID string,
	replacement RefreshTokenRecord,
	ttl time.Duration,
	blacklistTTL time.Duration,
) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("memory token store 不能为空")
	}
	if ttl <= 0 {
		return false, fmt.Errorf("refresh token ttl must be positive")
	}

	now := s.currentTime()
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ensureMapsLocked()
	s.pruneLocked(now)
	item, ok := s.refreshTokens[oldTokenID]
	if !ok || !sameRefreshTokenRecord(item.record, expected) {
		return false, nil
	}
	s.refreshTokens[newTokenID] = memoryRefreshToken{
		record:    replacement,
		expiresAt: now.Add(ttl),
	}
	delete(s.refreshTokens, oldTokenID)
	if blacklistTTL > 0 {
		s.accessBlacklist[expected.AccessTokenID] = now.Add(blacklistTTL)
	}
	return true, nil
}

// BlacklistAccessToken 将 access token jti 写入进程内黑名单。
func (s *MemoryTokenStore) BlacklistAccessToken(_ context.Context, tokenID string, ttl time.Duration) error {
	if s == nil || ttl <= 0 {
		return nil
	}

	now := s.currentTime()
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ensureMapsLocked()
	s.pruneLocked(now)
	s.accessBlacklist[tokenID] = now.Add(ttl)
	return nil
}

// IsAccessTokenBlacklisted 判断 access token 是否已被加入进程内黑名单。
func (s *MemoryTokenStore) IsAccessTokenBlacklisted(_ context.Context, tokenID string) (bool, error) {
	if s == nil {
		return false, nil
	}

	now := s.currentTime()
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ensureMapsLocked()
	s.pruneLocked(now)
	_, ok := s.accessBlacklist[tokenID]
	return ok, nil
}

func (s *MemoryTokenStore) currentTime() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
}

func (s *MemoryTokenStore) ensureMapsLocked() {
	if s.refreshTokens == nil {
		s.refreshTokens = map[string]memoryRefreshToken{}
	}
	if s.accessBlacklist == nil {
		s.accessBlacklist = map[string]time.Time{}
	}
}

func (s *MemoryTokenStore) pruneLocked(now time.Time) {
	for tokenID, item := range s.refreshTokens {
		if !item.expiresAt.After(now) {
			delete(s.refreshTokens, tokenID)
		}
	}
	for tokenID, expiresAt := range s.accessBlacklist {
		if !expiresAt.After(now) {
			delete(s.accessBlacklist, tokenID)
		}
	}
}

func sameRefreshTokenRecord(left RefreshTokenRecord, right RefreshTokenRecord) bool {
	return left.UserID == right.UserID &&
		left.SessionID == right.SessionID &&
		left.SessionVersion == right.SessionVersion &&
		left.AccessTokenID == right.AccessTokenID &&
		left.AccessExpiresAt.Equal(right.AccessExpiresAt)
}
