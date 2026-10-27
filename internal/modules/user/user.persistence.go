package user

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/teamsillybees/glean/internal/data"
	appent "github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/sysuser"
	"github.com/teamsillybees/glean/internal/data/ent/sysuserrole"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

func (s *Service) createEnt(ctx context.Context, user *User, explicitRoles bool) error {
	return data.WithinTx(ctx, s.client, func(txCtx context.Context, txClient *appent.Client) error {
		if err := lockAdministrators(txCtx, txClient); err != nil {
			return err
		}
		if user.IsSuperAdmin || explicitRoles {
			if err := requireSuperAdmin(txCtx, txClient); err != nil {
				return err
			}
		}
		roleIDs, err := s.resolveRoleIDs(txCtx, txClient, user.RoleCodes)
		if err != nil {
			return err
		}

		record, err := txClient.SysUser.Create().
			SetUsername(user.Username).
			SetPasswordHash(user.PasswordHash).
			SetNillableNickname(nullableString(user.Nickname)).
			SetNillablePhone(nullableString(user.Phone)).
			SetNillableEmail(nullableString(user.Email)).
			SetNillableAvatarURL(nullableString(user.AvatarURL)).
			SetIsSuperAdmin(user.IsSuperAdmin).
			SetIsEnable(user.IsEnable).
			SetSortID(user.SortID).
			Save(txCtx)
		if err != nil {
			return mapEntWriteError(txCtx, err, "create user failed")
		}

		fillDomainFromEnt(user, record)
		user.RoleCodes = normalizeCodes(user.RoleCodes)
		if err := s.replaceUserRoles(txCtx, txClient, record.ID, roleIDs); err != nil {
			return err
		}
		return nil
	})
}

func (s *Service) findByID(ctx context.Context, id idgen.ID) (*User, error) {
	return s.findByIDWithClient(ctx, s.client, id)
}

func (s *Service) findByIDWithClient(ctx context.Context, client *appent.Client, id idgen.ID) (*User, error) {
	record, err := client.SysUser.Query().
		Where(
			sysuser.ID(id),
			sysuser.DeletedAtIsNil(),
		).
		Only(ctx)
	if appent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "query user failed")
	}
	return s.toDomainWithClient(ctx, client, record)
}

func (s *Service) page(ctx context.Context, input PageUsersQuery) ([]*User, int32, error) {
	query := s.client.SysUser.Query().Where(sysuser.DeletedAtIsNil())
	if keyword := strings.TrimSpace(input.Keyword); keyword != "" {
		query.Where(sysuser.Or(
			sysuser.UsernameContainsFold(keyword),
			sysuser.NicknameContainsFold(keyword),
			sysuser.PhoneContainsFold(keyword),
			sysuser.EmailContainsFold(keyword),
		))
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, bizerrors.WrapDBContext(ctx, err, "count users failed")
	}

	offset := int(input.Offset())
	limit := int(input.Limit())
	records, err := query.
		Order(appent.Asc(sysuser.FieldSortID), appent.Asc(sysuser.FieldID)).
		Limit(limit).
		Offset(offset).
		All(ctx)
	if err != nil {
		return nil, 0, bizerrors.WrapDBContext(ctx, err, "list users failed")
	}

	userIDs := make([]idgen.ID, 0, len(records))
	items := make([]*User, 0, len(records))
	for _, record := range records {
		item := userFromEnt(record)
		items = append(items, item)
		userIDs = append(userIDs, item.ID)
	}

	roleCodesByUserID, err := s.loadRoleCodesByUserIDs(ctx, userIDs)
	if err != nil {
		return nil, 0, err
	}
	for _, item := range items {
		item.RoleCodes = append([]string(nil), roleCodesByUserID[item.ID]...)
		if item.RoleCodes == nil {
			item.RoleCodes = []string{}
		}
	}

	return items, int32(total), nil
}

func (s *Service) updateEnt(ctx context.Context, user *User, body UpdateUserBody) error {
	return data.WithinTx(ctx, s.client, func(txCtx context.Context, txClient *appent.Client) error {
		if err := lockAdministrators(txCtx, txClient); err != nil {
			return err
		}
		current, err := txClient.SysUser.Query().Where(sysuser.ID(user.ID), sysuser.DeletedAtIsNil()).ForUpdate().Only(txCtx)
		if appent.IsNotFound(err) {
			return bizerrors.UserNotFound(user.ID)
		}
		if err != nil {
			return bizerrors.WrapDBContext(txCtx, err, "query current user failed")
		}
		if current.IsSuperAdmin || body.IsSuperAdmin != nil && *body.IsSuperAdmin != current.IsSuperAdmin || body.RoleCodes != nil {
			if err := requireSuperAdmin(txCtx, txClient); err != nil {
				return err
			}
		}
		*user = *userFromEnt(current)
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

		if current.IsSuperAdmin && current.IsEnable && (!user.IsSuperAdmin || !user.IsEnable) {
			if err := ensureAnotherSuperAdmin(txCtx, txClient, user.ID); err != nil {
				return err
			}
		}

		update := txClient.SysUser.UpdateOneID(user.ID).
			Where(sysuser.DeletedAtIsNil()).
			SetIsSuperAdmin(user.IsSuperAdmin).
			SetIsEnable(user.IsEnable).
			SetSortID(user.SortID)
		setOptionalStrings(update, user)
		if current.IsEnable && !user.IsEnable {
			if current.SessionVersion == math.MaxInt64 {
				return bizerrors.Internal("session version exhausted")
			}
			update.SetSessionVersion(current.SessionVersion + 1)
		}

		record, err := update.Save(txCtx)
		if appent.IsNotFound(err) {
			return bizerrors.UserNotFound(user.ID)
		}
		if err != nil {
			return mapEntWriteError(txCtx, err, "update user failed")
		}

		if body.RoleCodes != nil {
			roleIDs, err := s.resolveRoleIDs(txCtx, txClient, user.RoleCodes)
			if err != nil {
				return err
			}
			if err := s.replaceUserRoles(txCtx, txClient, user.ID, roleIDs); err != nil {
				return err
			}

		}

		refreshed, err := s.findByIDWithClient(txCtx, txClient, record.ID)
		if err != nil {
			return err
		}
		if refreshed != nil {
			*user = *refreshed
		}
		return nil
	})
}

func (s *Service) deleteEnt(ctx context.Context, id idgen.ID) error {
	if err := protectSelf(ctx, id, false, false, true); err != nil {
		return err
	}
	return data.WithinTx(ctx, s.client, func(txCtx context.Context, txClient *appent.Client) error {
		if err := lockAdministrators(txCtx, txClient); err != nil {
			return err
		}
		current, err := txClient.SysUser.Query().Where(sysuser.ID(id), sysuser.DeletedAtIsNil()).ForUpdate().Only(txCtx)
		if appent.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return bizerrors.WrapDBContext(txCtx, err, "query current user failed")
		}
		if current.IsSuperAdmin {
			if err := requireSuperAdmin(txCtx, txClient); err != nil {
				return err
			}
		}
		if current.IsSuperAdmin && current.IsEnable {
			if err := ensureAnotherSuperAdmin(txCtx, txClient, id); err != nil {
				return err
			}
		}

		if _, err := txClient.SysUser.Update().
			Where(
				sysuser.ID(id),
				sysuser.DeletedAtIsNil(),
			).
			SetDeletedAt(time.Now()).
			Save(txCtx); err != nil {
			return mapEntWriteError(txCtx, err, "delete user failed")
		}

		if _, err := txClient.SysUserRole.Update().
			Where(
				sysuserrole.UserID(id),
				sysuserrole.DeletedAtIsNil(),
			).
			SetDeletedAt(time.Now()).
			Save(txCtx); err != nil {
			return mapEntWriteError(txCtx, err, "delete user roles failed")
		}
		return nil
	})
}

// ensureAnotherSuperAdmin 防止删除、禁用或降级最后一个有效超级管理员。
func ensureAnotherSuperAdmin(ctx context.Context, client *appent.Client, excludedID idgen.ID) error {
	count, err := client.SysUser.Query().Where(
		sysuser.IDNEQ(excludedID),
		sysuser.DeletedAtIsNil(),
		sysuser.IsEnable(true),
		sysuser.IsSuperAdmin(true),
	).Count(ctx)
	if err != nil {
		return bizerrors.WrapDBContext(ctx, err, "count active super administrators failed")
	}
	if count == 0 {
		return bizerrors.BadRequest("the last active super administrator cannot be disabled, downgraded, or deleted")
	}
	return nil
}

func mapEntWriteError(ctx context.Context, err error, message string) error {
	if appent.IsConstraintError(err) {
		return bizerrors.WrapBadRequestContext(ctx, err, message)
	}
	return bizerrors.WrapDBContext(ctx, err, message)
}
