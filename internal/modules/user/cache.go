package user

import (
	"context"
	"errors"
	"time"

	jetcache "github.com/mgtv-tech/jetcache-go"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	platformcache "github.com/teamsillybees/glean/internal/platform/cache"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// User 是 user 模块的核心领域实体。
type User struct {
	ID           idgen.ID
	Username     string
	PasswordHash string
	Nickname     string
	Phone        string
	Email        string
	AvatarURL    string
	RoleCodes    []string
	IsSuperAdmin bool
	IsEnable     bool
	SortID       int32
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    *time.Time
	CreatedBy    idgen.ID
	UpdatedBy    idgen.ID
}

// UserCache 负责 user 模块详情缓存的读写与 Key 规范封装。
type UserCache struct {
	manager *platformcache.Manager
	cache   jetcache.Cache
}

// NewUserCache 创建用户详情缓存实现。
func NewUserCache(manager *platformcache.Manager) *UserCache {
	return &UserCache{
		manager: manager,
		cache:   manager.New("user-profile"),
	}
}

// Get 读取用户详情缓存。
func (c *UserCache) Get(ctx context.Context, id idgen.ID) (*User, bool, error) {
	var user User
	err := c.cache.Get(ctx, c.key(id), &user)
	if err == nil {
		return &user, true, nil
	}
	if errors.Is(err, jetcache.ErrCacheMiss) {
		return nil, false, nil
	}
	return nil, false, bizerrors.WrapCacheContext(ctx, err, "get user cache failed")
}

// Set 写入用户详情缓存。
func (c *UserCache) Set(ctx context.Context, user *User) error {
	return c.cache.Set(ctx, c.key(user.ID), jetcache.Value(user))
}

// Delete 删除用户详情缓存。
func (c *UserCache) Delete(ctx context.Context, id idgen.ID) error {
	if err := c.cache.Delete(ctx, c.key(id)); err != nil {
		return bizerrors.WrapCacheContext(ctx, err, "delete user cache failed")
	}
	return nil
}

func (c *UserCache) key(id idgen.ID) string {
	return c.manager.BuildKey("user", "profile", id)
}
