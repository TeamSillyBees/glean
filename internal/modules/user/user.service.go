package user

import (
	"context"

	appent "github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// passwordManager 定义密码哈希与校验能力。
type passwordManager interface {
	Hash(password string) (string, error)
	Compare(hash string, password string) error
}

// userCache 定义用户详情缓存的最小契约。
type userCache interface {
	Get(ctx context.Context, id idgen.ID) (*User, bool, error)
	Set(ctx context.Context, user *User) error
	Delete(ctx context.Context, id idgen.ID) error
}

type authorizationInvalidator interface {
	NotifyChanged(ctx context.Context, userIDs []idgen.ID, reloadPolicy bool) error
}

// Service 是 user 模块的应用服务，直接通过 Ent Client 操作数据库。
type Service struct {
	client    *appent.Client
	cache     userCache
	passwords passwordManager
	authz     authorizationInvalidator
}

// NewService 构造 user 模块应用服务。
func NewService(
	client *appent.Client,
	cache userCache,
	passwords passwordManager,
	invalidators ...authorizationInvalidator,
) *Service {
	service := &Service{
		client:    client,
		cache:     cache,
		passwords: passwords,
	}
	if len(invalidators) > 0 {
		service.authz = invalidators[0]
	}
	return service
}
