package user

import (
	"context"
	"strings"
	"time"

	appent "github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/sysrole"
	"github.com/teamsillybees/glean/internal/data/ent/sysuserrole"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

func (s *Service) toDomainWithClient(ctx context.Context, client *appent.Client, record *appent.SysUser) (*User, error) {
	user := userFromEnt(record)
	roleCodes, err := s.loadRoleCodes(ctx, client, user.ID)
	if err != nil {
		return nil, err
	}
	user.RoleCodes = roleCodes
	return user, nil
}

func (s *Service) resolveRoleIDs(ctx context.Context, client *appent.Client, roleCodes []string) ([]idgen.ID, error) {
	normalizedCodes := normalizeCodes(roleCodes)
	if len(normalizedCodes) == 0 {
		return []idgen.ID{}, nil
	}

	var rows []struct {
		ID   idgen.ID `json:"id"`
		Code string   `json:"code"`
	}
	err := client.SysRole.Query().
		Where(
			sysrole.CodeIn(normalizedCodes...),
			sysrole.DeletedAtIsNil(),
			sysrole.IsEnable(true),
		).
		Order(appent.Asc(sysrole.FieldID)).
		ForShare().
		Select(sysrole.FieldID, sysrole.FieldCode).
		Scan(ctx, &rows)
	if err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "query roles failed")
	}

	roleIDsByCode := make(map[string]idgen.ID, len(rows))
	for _, row := range rows {
		roleIDsByCode[row.Code] = row.ID
	}
	if len(roleIDsByCode) != len(normalizedCodes) {
		missingCodes := make([]string, 0)
		for _, roleCode := range normalizedCodes {
			if _, ok := roleIDsByCode[roleCode]; !ok {
				missingCodes = append(missingCodes, roleCode)
			}
		}
		return nil, bizerrors.BadRequest(
			"role code is invalid",
			bizerrors.WithDetail("missing_role_codes", missingCodes),
		)
	}

	roleIDs := make([]idgen.ID, 0, len(normalizedCodes))
	for _, roleCode := range normalizedCodes {
		roleIDs = append(roleIDs, roleIDsByCode[roleCode])
	}
	return roleIDs, nil
}

func (s *Service) replaceUserRoles(ctx context.Context, client *appent.Client, userID idgen.ID, roleIDs []idgen.ID) error {
	if _, err := client.SysUserRole.Update().
		Where(
			sysuserrole.UserID(userID),
			sysuserrole.DeletedAtIsNil(),
		).
		SetDeletedAt(time.Now()).
		Save(ctx); err != nil {
		return mapEntWriteError(ctx, err, "replace user roles failed")
	}

	for _, roleID := range roleIDs {
		relation, err := client.SysUserRole.Query().
			Where(
				sysuserrole.UserID(userID),
				sysuserrole.RoleID(roleID),
			).
			Only(ctx)
		switch {
		case appent.IsNotFound(err):
			if _, err := client.SysUserRole.Create().
				SetUserID(userID).
				SetRoleID(roleID).
				Save(ctx); err != nil {
				return mapEntWriteError(ctx, err, "insert user roles failed")
			}
		case err != nil:
			return bizerrors.WrapDBContext(ctx, err, "query user role relation failed")
		default:
			if _, err := client.SysUserRole.UpdateOne(relation).
				ClearDeletedAt().
				Save(ctx); err != nil {
				return mapEntWriteError(ctx, err, "restore user role relation failed")
			}
		}
	}
	return nil
}

func (s *Service) loadRoleCodes(ctx context.Context, client *appent.Client, userID idgen.ID) ([]string, error) {
	var rows []struct {
		Code string `json:"code"`
	}
	err := client.SysRole.Query().
		Where(
			sysrole.DeletedAtIsNil(),
			sysrole.IsEnable(true),
			sysrole.HasUserRolesWith(
				sysuserrole.UserID(userID),
				sysuserrole.DeletedAtIsNil(),
			),
		).
		Order(appent.Asc(sysrole.FieldSortID), appent.Asc(sysrole.FieldID)).
		Select(sysrole.FieldCode).
		Scan(ctx, &rows)
	if err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "query user roles failed")
	}

	roleCodes := make([]string, 0, len(rows))
	for _, row := range rows {
		roleCodes = append(roleCodes, row.Code)
	}
	return roleCodes, nil
}

func (s *Service) loadRoleCodesByUserIDs(ctx context.Context, userIDs []idgen.ID) (map[idgen.ID][]string, error) {
	result := make(map[idgen.ID][]string, len(userIDs))
	if len(userIDs) == 0 {
		return result, nil
	}

	roles, err := s.client.SysRole.Query().
		Where(
			sysrole.DeletedAtIsNil(),
			sysrole.IsEnable(true),
			sysrole.HasUserRolesWith(
				sysuserrole.UserIDIn(userIDs...),
				sysuserrole.DeletedAtIsNil(),
			),
		).
		WithUserRoles(func(query *appent.SysUserRoleQuery) {
			query.Where(
				sysuserrole.UserIDIn(userIDs...),
				sysuserrole.DeletedAtIsNil(),
			)
		}).
		Order(appent.Asc(sysrole.FieldSortID), appent.Asc(sysrole.FieldID)).
		All(ctx)
	if err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "query users roles failed")
	}

	for _, role := range roles {
		for _, relation := range role.Edges.UserRoles {
			result[relation.UserID] = append(result[relation.UserID], role.Code)
		}
	}
	return result, nil
}

func normalizeRoleCodes(roleCodes []string) []string {
	if len(roleCodes) == 0 {
		return []string{"viewer"}
	}

	seen := make(map[string]struct{}, len(roleCodes))
	result := make([]string, 0, len(roleCodes))
	for _, roleCode := range roleCodes {
		roleCode = strings.TrimSpace(roleCode)
		if roleCode == "" {
			continue
		}
		if _, ok := seen[roleCode]; ok {
			continue
		}
		seen[roleCode] = struct{}{}
		result = append(result, roleCode)
	}
	if len(result) == 0 {
		return []string{"viewer"}
	}
	return result
}

func normalizeCodes(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
