package user

import (
	"context"
	"strings"

	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/pagination"
)

// Create 创建用户。
func (s *Service) Create(ctx context.Context, body CreateUserBody) (UserVO, error) {
	if strings.TrimSpace(body.Username) == "" {
		return UserVO{}, bizerrors.BadRequest("username is required")
	}
	if strings.TrimSpace(body.Password) == "" {
		return UserVO{}, bizerrors.BadRequest("password is required")
	}

	passwordHash, err := s.passwords.Hash(body.Password)
	if err != nil {
		return UserVO{}, bizerrors.WrapInternalContext(ctx, err, "hash password failed")
	}

	isEnable := true
	if body.IsEnable != nil {
		isEnable = *body.IsEnable
	}

	user := &User{
		Username:     strings.TrimSpace(body.Username),
		PasswordHash: passwordHash,
		Nickname:     strings.TrimSpace(body.Nickname),
		Phone:        strings.TrimSpace(body.Phone),
		Email:        strings.TrimSpace(body.Email),
		AvatarURL:    strings.TrimSpace(body.AvatarURL),
		RoleCodes:    normalizeRoleCodes(body.RoleCodes),
		IsSuperAdmin: body.IsSuperAdmin,
		IsEnable:     isEnable,
		SortID:       body.SortID,
	}

	if err := s.createEnt(ctx, user); err != nil {
		return UserVO{}, err
	}

	return userToVO(user), nil
}

// Get 获取用户详情，缓存未命中时自动回填。
func (s *Service) Get(ctx context.Context, id idgen.ID) (UserVO, error) {
	if cached, found, err := s.cache.Get(ctx, id); err != nil {
		return UserVO{}, bizerrors.WrapCacheContext(ctx, err, "load user from cache failed")
	} else if found {
		return userToVO(cached), nil
	}

	user, err := s.findByID(ctx, id)
	if err != nil {
		return UserVO{}, err
	}
	if user == nil {
		return UserVO{}, bizerrors.UserNotFound(id)
	}

	if err := s.cache.Set(ctx, user); err != nil {
		return UserVO{}, bizerrors.WrapCacheContext(ctx, err, "set user cache failed")
	}

	return userToVO(user), nil
}

// Page 返回分页用户列表。
func (s *Service) Page(ctx context.Context, query PageUsersQuery) (pagination.PageVO[UserVO], error) {
	items, total, err := s.page(ctx, query)
	if err != nil {
		return pagination.PageVO[UserVO]{}, err
	}

	vos := make([]UserVO, 0, len(items))
	for _, item := range items {
		vos = append(vos, userToVO(item))
	}

	return pagination.NewPageVO(vos, total, query.PageQuery), nil
}

// Update 更新用户并清理缓存。
func (s *Service) Update(ctx context.Context, id idgen.ID, body UpdateUserBody) (UserVO, error) {
	user, err := s.findByID(ctx, id)
	if err != nil {
		return UserVO{}, err
	}
	if user == nil {
		return UserVO{}, bizerrors.UserNotFound(id)
	}

	if body.Nickname != nil {
		user.Nickname = strings.TrimSpace(*body.Nickname)
	}
	if body.Phone != nil {
		user.Phone = strings.TrimSpace(*body.Phone)
	}
	if body.Email != nil {
		user.Email = strings.TrimSpace(*body.Email)
	}
	if body.AvatarURL != nil {
		user.AvatarURL = strings.TrimSpace(*body.AvatarURL)
	}
	if body.RoleCodes != nil {
		user.RoleCodes = normalizeRoleCodes(*body.RoleCodes)
	}
	if body.IsSuperAdmin != nil {
		user.IsSuperAdmin = *body.IsSuperAdmin
	}
	if body.IsEnable != nil {
		user.IsEnable = *body.IsEnable
	}
	if body.SortID != nil {
		user.SortID = *body.SortID
	}

	if err := s.updateEnt(ctx, user); err != nil {
		return UserVO{}, err
	}
	if err := s.cache.Delete(ctx, user.ID); err != nil {
		return UserVO{}, bizerrors.WrapCacheContext(ctx, err, "delete user cache failed")
	}
	if s.authz != nil {
		if err := s.authz.NotifyChanged(ctx, []idgen.ID{user.ID}, false); err != nil {
			return UserVO{}, bizerrors.WrapInternalContext(ctx, err, "invalidate user authorization failed")
		}
	}

	return userToVO(user), nil
}

// Delete 删除用户并清理缓存。
func (s *Service) Delete(ctx context.Context, id idgen.ID) error {
	if err := s.deleteEnt(ctx, id); err != nil {
		return err
	}
	if err := s.cache.Delete(ctx, id); err != nil {
		return bizerrors.WrapCacheContext(ctx, err, "delete user cache failed")
	}
	if s.authz != nil {
		if err := s.authz.NotifyChanged(ctx, []idgen.ID{id}, false); err != nil {
			return bizerrors.WrapInternalContext(ctx, err, "invalidate user authorization failed")
		}
	}
	return nil
}
