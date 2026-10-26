package user

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

type fakePasswordManager struct {
	hashCalls int
}

func (m *fakePasswordManager) Hash(password string) (string, error) {
	m.hashCalls++
	return "hashed:" + password, nil
}

func (*fakePasswordManager) Compare(string, string) error {
	return nil
}

type fakeUserCache struct {
	get      func(context.Context, idgen.ID) (*User, bool, error)
	setCalls int
}

func (c *fakeUserCache) Get(ctx context.Context, id idgen.ID) (*User, bool, error) {
	if c.get == nil {
		return nil, false, nil
	}
	return c.get(ctx, id)
}

func (c *fakeUserCache) Set(context.Context, *User) error {
	c.setCalls++
	return nil
}

func (c *fakeUserCache) Delete(context.Context, idgen.ID) error {
	return nil
}

// TestServiceCreateValidatesRequiredFields 验证无效创建参数不会进入密码或数据库流程。
func TestServiceCreateValidatesRequiredFields(t *testing.T) {
	passwords := &fakePasswordManager{}
	service := NewService(nil, &fakeUserCache{}, passwords)
	tests := []struct {
		name string
		body CreateUserBody
	}{
		{name: "username is blank", body: CreateUserBody{Username: "  ", Password: "secret"}},
		{name: "password is blank", body: CreateUserBody{Username: "alice", Password: "  "}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.Create(context.Background(), tt.body)

			require.Error(t, err)
		})
	}
	require.Zero(t, passwords.hashCalls)
}

// TestServiceGetReturnsCacheHit 验证缓存命中时不会访问数据库，并复制可变角色切片。
func TestServiceGetReturnsCacheHit(t *testing.T) {
	cached := &User{
		ID:        idgen.New(1001),
		Username:  "alice",
		RoleCodes: []string{"admin", "viewer"},
		IsEnable:  true,
	}
	cache := &fakeUserCache{get: func(context.Context, idgen.ID) (*User, bool, error) {
		return cached, true, nil
	}}
	service := NewService(nil, cache, &fakePasswordManager{})

	got, err := service.Get(context.Background(), cached.ID)

	require.NoError(t, err)
	require.Equal(t, cached.ID, got.ID)
	require.Equal(t, cached.Username, got.Username)
	require.Equal(t, cached.RoleCodes, got.RoleCodes)
	require.Zero(t, cache.setCalls)
	got.RoleCodes[0] = "changed"
	require.Equal(t, "admin", cached.RoleCodes[0])
}

// TestServiceGetWrapsCacheFailure 验证缓存故障会转换为统一缓存错误并阻止数据库回退。
func TestServiceGetWrapsCacheFailure(t *testing.T) {
	cacheErr := errors.New("cache unavailable")
	cache := &fakeUserCache{get: func(context.Context, idgen.ID) (*User, bool, error) {
		return nil, false, cacheErr
	}}
	service := NewService(nil, cache, &fakePasswordManager{})

	_, err := service.Get(context.Background(), idgen.New(1001))

	require.Error(t, err)
	require.ErrorIs(t, err, cacheErr)
	require.Zero(t, cache.setCalls)
}
