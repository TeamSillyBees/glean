package rbac

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/teamsillybees/glean/internal/accesscontrol"
	"github.com/teamsillybees/glean/internal/data"
	appent "github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/sysmenu"
	"github.com/teamsillybees/glean/internal/data/ent/sysrole"
	"github.com/teamsillybees/glean/internal/data/ent/sysrolemenu"
	"github.com/teamsillybees/glean/internal/data/ent/sysuser"
	"github.com/teamsillybees/glean/internal/data/ent/sysuserrole"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

var (
	roleCodePattern       = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,63}$`)
	permissionCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*(?::[a-z][a-z0-9_-]*){2,5}$`)
)

// Service 实现数据库唯一事实源下的角色、权限和分配管理。
type Service struct {
	client      *appent.Client
	invalidator accesscontrol.Invalidator
}

// NewService 创建 RBAC 服务。
func NewService(client *appent.Client, invalidator accesscontrol.Invalidator) *Service {
	return &Service{client: client, invalidator: invalidator}
}

// ListRoles 返回全部未删除角色。
func (s *Service) ListRoles(ctx context.Context) ([]RoleVO, error) {
	roles, err := s.client.SysRole.Query().Where(sysrole.DeletedAtIsNil()).
		Order(appent.Asc(sysrole.FieldSortID), appent.Asc(sysrole.FieldID)).All(ctx)
	if err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "list roles failed")
	}
	result := make([]RoleVO, 0, len(roles))
	for _, role := range roles {
		result = append(result, roleToVO(role))
	}
	return result, nil
}

// GetRole 返回角色详情。
func (s *Service) GetRole(ctx context.Context, id idgen.ID) (RoleVO, error) {
	role, err := s.findRole(ctx, s.client, id)
	if err != nil {
		return RoleVO{}, err
	}
	if role == nil {
		return RoleVO{}, bizerrors.BadRequest("role not found")
	}
	return roleToVO(role), nil
}

// CreateRole 创建非内置角色。
func (s *Service) CreateRole(ctx context.Context, body CreateRoleBody) (RoleVO, error) {
	code := strings.TrimSpace(body.Code)
	name := strings.TrimSpace(body.Name)
	if !roleCodePattern.MatchString(code) || name == "" {
		return RoleVO{}, bizerrors.BadRequest("role code or name is invalid")
	}
	enabled := true
	if body.IsEnable != nil {
		enabled = *body.IsEnable
	}
	role, err := s.client.SysRole.Create().SetCode(code).SetName(name).
		SetNillableRemark(nullableString(body.Remark)).SetIsBuiltin(false).
		SetIsEnable(enabled).SetSortID(body.SortID).Save(ctx)
	if err != nil {
		return RoleVO{}, mapWriteError(ctx, err, "create role failed")
	}
	return roleToVO(role), nil
}

// UpdateRole 更新角色；内置角色不可禁用，角色编码永远不可修改。
func (s *Service) UpdateRole(ctx context.Context, id idgen.ID, body UpdateRoleBody) (RoleVO, error) {
	var updated *appent.SysRole
	var affectedUsers []idgen.ID
	err := data.WithinTx(ctx, s.client, func(txCtx context.Context, tx *appent.Client) error {
		role, err := s.findRoleForUpdate(txCtx, tx, id)
		if err != nil {
			return err
		}
		if role == nil {
			return bizerrors.BadRequest("role not found")
		}
		if role.IsBuiltin && body.IsEnable != nil && !*body.IsEnable {
			return bizerrors.BadRequest("builtin role cannot be disabled")
		}
		update := tx.SysRole.UpdateOne(role)
		if body.Name != nil {
			name := strings.TrimSpace(*body.Name)
			if name == "" {
				return bizerrors.BadRequest("role name is required")
			}
			update.SetName(name)
		}
		if body.Remark != nil {
			update.SetNillableRemark(nullableString(*body.Remark))
		}
		if body.IsEnable != nil {
			update.SetIsEnable(*body.IsEnable)
		}
		if body.SortID != nil {
			update.SetSortID(*body.SortID)
		}
		updated, err = update.Save(txCtx)
		if err != nil {
			return mapWriteError(txCtx, err, "update role failed")
		}
		affectedUsers, err = userIDsForRole(txCtx, tx, id)
		return err
	})
	if err != nil {
		return RoleVO{}, err
	}
	if err := s.invalidator.NotifyChanged(ctx, affectedUsers, true); err != nil {
		return RoleVO{}, bizerrors.WrapInternalContext(ctx, err, "refresh authorization after role update failed")
	}
	return roleToVO(updated), nil
}

// DeleteRole 软删除未被用户引用的非内置角色及其权限关系。
func (s *Service) DeleteRole(ctx context.Context, id idgen.ID) error {
	err := data.WithinTx(ctx, s.client, func(txCtx context.Context, tx *appent.Client) error {
		role, err := s.findRoleForUpdate(txCtx, tx, id)
		if err != nil {
			return err
		}
		if role == nil {
			return bizerrors.BadRequest("role not found")
		}
		if role.IsBuiltin {
			return bizerrors.BadRequest("builtin role cannot be deleted")
		}
		count, err := tx.SysUserRole.Query().Where(sysuserrole.RoleID(id), sysuserrole.DeletedAtIsNil()).Count(txCtx)
		if err != nil {
			return bizerrors.WrapDBContext(txCtx, err, "count role users failed")
		}
		if count > 0 {
			return bizerrors.BadRequest("role assigned to users cannot be deleted")
		}
		now := time.Now()
		if _, err := tx.SysRoleMenu.Update().Where(sysrolemenu.RoleID(id), sysrolemenu.DeletedAtIsNil()).SetDeletedAt(now).Save(txCtx); err != nil {
			return mapWriteError(txCtx, err, "delete role permissions failed")
		}
		if _, err := tx.SysRole.Update().Where(sysrole.ID(id), sysrole.DeletedAtIsNil()).SetDeletedAt(now).Save(txCtx); err != nil {
			return mapWriteError(txCtx, err, "delete role failed")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := s.invalidator.NotifyChanged(ctx, nil, true); err != nil {
		return bizerrors.WrapInternalContext(ctx, err, "refresh authorization after role delete failed")
	}
	return nil
}

// ListPermissions 返回全部具有权限编码的未删除资源。
func (s *Service) ListPermissions(ctx context.Context) ([]PermissionVO, error) {
	menus, err := s.client.SysMenu.Query().Where(sysmenu.DeletedAtIsNil(), sysmenu.PermissionCodeNotNil()).
		Order(appent.Asc(sysmenu.FieldSortID), appent.Asc(sysmenu.FieldID)).All(ctx)
	if err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "list permissions failed")
	}
	result := make([]PermissionVO, 0, len(menus))
	for _, menu := range menus {
		result = append(result, permissionToVO(menu))
	}
	return result, nil
}

// GetPermission 返回权限资源详情。
func (s *Service) GetPermission(ctx context.Context, id idgen.ID) (PermissionVO, error) {
	menu, err := s.findPermission(ctx, s.client, id)
	if err != nil {
		return PermissionVO{}, err
	}
	if menu == nil {
		return PermissionVO{}, bizerrors.BadRequest("permission not found")
	}
	return permissionToVO(menu), nil
}

// CreatePermission 创建具有稳定编码的权限资源。
func (s *Service) CreatePermission(ctx context.Context, body CreatePermissionBody) (PermissionVO, error) {
	title := strings.TrimSpace(body.Title)
	code := strings.TrimSpace(body.PermissionCode)
	if title == "" || !permissionCodePattern.MatchString(code) {
		return PermissionVO{}, bizerrors.BadRequest("permission title or code is invalid")
	}
	visible := true
	if body.IsVisible != nil {
		visible = *body.IsVisible
	}
	menuType := int16(1)
	if body.MenuType != nil {
		menuType = *body.MenuType
	}
	if !validMenuType(menuType) {
		return PermissionVO{}, bizerrors.BadRequest("menu type is invalid")
	}
	menu, err := s.client.SysMenu.Create().SetTitle(title).SetMenuType(menuType).
		SetPermissionCode(code).SetIsVisible(visible).SetSortID(body.SortID).Save(ctx)
	if err != nil {
		return PermissionVO{}, mapWriteError(ctx, err, "create permission failed")
	}
	return permissionToVO(menu), nil
}

// UpdatePermission 更新展示信息；稳定权限编码不可修改。
func (s *Service) UpdatePermission(ctx context.Context, id idgen.ID, body UpdatePermissionBody) (PermissionVO, error) {
	menu, err := s.findPermission(ctx, s.client, id)
	if err != nil {
		return PermissionVO{}, err
	}
	if menu == nil {
		return PermissionVO{}, bizerrors.BadRequest("permission not found")
	}
	update := s.client.SysMenu.UpdateOne(menu)
	if body.Title != nil {
		title := strings.TrimSpace(*body.Title)
		if title == "" {
			return PermissionVO{}, bizerrors.BadRequest("permission title is required")
		}
		update.SetTitle(title)
	}
	if body.MenuType != nil {
		if !validMenuType(*body.MenuType) {
			return PermissionVO{}, bizerrors.BadRequest("menu type is invalid")
		}
		update.SetMenuType(*body.MenuType)
	}
	if body.IsVisible != nil {
		update.SetIsVisible(*body.IsVisible)
	}
	if body.SortID != nil {
		update.SetSortID(*body.SortID)
	}
	menu, err = update.Save(ctx)
	if err != nil {
		return PermissionVO{}, mapWriteError(ctx, err, "update permission failed")
	}
	return permissionToVO(menu), nil
}

// DeletePermission 软删除权限资源和全部角色授权关系。
func (s *Service) DeletePermission(ctx context.Context, id idgen.ID) error {
	err := data.WithinTx(ctx, s.client, func(txCtx context.Context, tx *appent.Client) error {
		menu, err := s.findPermissionForUpdate(txCtx, tx, id)
		if err != nil {
			return err
		}
		if menu == nil {
			return bizerrors.BadRequest("permission not found")
		}
		now := time.Now()
		if _, err := tx.SysRoleMenu.Update().Where(sysrolemenu.MenuID(id), sysrolemenu.DeletedAtIsNil()).SetDeletedAt(now).Save(txCtx); err != nil {
			return mapWriteError(txCtx, err, "delete permission relations failed")
		}
		if _, err := tx.SysMenu.Update().Where(sysmenu.ID(id), sysmenu.DeletedAtIsNil()).SetDeletedAt(now).Save(txCtx); err != nil {
			return mapWriteError(txCtx, err, "delete permission failed")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := s.invalidator.NotifyChanged(ctx, nil, true); err != nil {
		return bizerrors.WrapInternalContext(ctx, err, "refresh authorization after permission delete failed")
	}
	return nil
}

// GetUserRoles 返回用户当前有效角色。
func (s *Service) GetUserRoles(ctx context.Context, userID idgen.ID) ([]RoleVO, error) {
	if exists, err := s.client.SysUser.Query().Where(sysuser.ID(userID), sysuser.DeletedAtIsNil()).Exist(ctx); err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "query user failed")
	} else if !exists {
		return nil, bizerrors.UserNotFound(userID)
	}
	roles, err := s.client.SysRole.Query().Where(
		sysrole.DeletedAtIsNil(),
		sysrole.HasUserRolesWith(sysuserrole.UserID(userID), sysuserrole.DeletedAtIsNil()),
	).Order(appent.Asc(sysrole.FieldSortID), appent.Asc(sysrole.FieldID)).All(ctx)
	if err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "list user roles failed")
	}
	result := make([]RoleVO, 0, len(roles))
	for _, role := range roles {
		result = append(result, roleToVO(role))
	}
	return result, nil
}

// ReplaceUserRoles 原子替换用户角色并失效该用户的请求身份缓存。
func (s *Service) ReplaceUserRoles(ctx context.Context, userID idgen.ID, codes []string) ([]RoleVO, error) {
	normalized := normalizeStrings(codes)
	err := data.WithinTx(ctx, s.client, func(txCtx context.Context, tx *appent.Client) error {
		_, err := tx.SysUser.Query().Where(sysuser.ID(userID), sysuser.DeletedAtIsNil()).ForShare().Only(txCtx)
		if appent.IsNotFound(err) {
			return bizerrors.UserNotFound(userID)
		}
		if err != nil {
			return bizerrors.WrapDBContext(txCtx, err, "query user failed")
		}
		roles, err := rolesByCodes(txCtx, tx, normalized)
		if err != nil {
			return err
		}
		roleIDs := make([]idgen.ID, 0, len(roles))
		for _, role := range roles {
			roleIDs = append(roleIDs, role.ID)
		}
		return replaceUserRoleRelations(txCtx, tx, userID, roleIDs)
	})
	if err != nil {
		return nil, err
	}
	if err := s.invalidator.NotifyChanged(ctx, []idgen.ID{userID}, false); err != nil {
		return nil, bizerrors.WrapInternalContext(ctx, err, "invalidate user authorization failed")
	}
	return s.GetUserRoles(ctx, userID)
}

// GetRolePermissions 返回角色当前权限资源。
func (s *Service) GetRolePermissions(ctx context.Context, roleID idgen.ID) ([]PermissionVO, error) {
	if role, err := s.findRole(ctx, s.client, roleID); err != nil {
		return nil, err
	} else if role == nil {
		return nil, bizerrors.BadRequest("role not found")
	}
	menus, err := s.client.SysMenu.Query().Where(
		sysmenu.DeletedAtIsNil(), sysmenu.PermissionCodeNotNil(),
		sysmenu.HasRoleMenusWith(sysrolemenu.RoleID(roleID), sysrolemenu.DeletedAtIsNil()),
	).Order(appent.Asc(sysmenu.FieldSortID), appent.Asc(sysmenu.FieldID)).All(ctx)
	if err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "list role permissions failed")
	}
	result := make([]PermissionVO, 0, len(menus))
	for _, menu := range menus {
		result = append(result, permissionToVO(menu))
	}
	return result, nil
}

// ReplaceRolePermissions 原子替换角色权限并通知所有实例重载 Casbin 策略。
func (s *Service) ReplaceRolePermissions(ctx context.Context, roleID idgen.ID, codes []string) ([]PermissionVO, error) {
	normalized := normalizeStrings(codes)
	err := data.WithinTx(ctx, s.client, func(txCtx context.Context, tx *appent.Client) error {
		role, err := s.findRoleForShare(txCtx, tx, roleID)
		if err != nil {
			return err
		}
		if role == nil {
			return bizerrors.BadRequest("role not found")
		}
		menus, err := permissionsByCodes(txCtx, tx, normalized)
		if err != nil {
			return err
		}
		menuIDs := make([]idgen.ID, 0, len(menus))
		for _, menu := range menus {
			menuIDs = append(menuIDs, menu.ID)
		}
		return replaceRolePermissionRelations(txCtx, tx, roleID, menuIDs)
	})
	if err != nil {
		return nil, err
	}
	if err := s.invalidator.NotifyChanged(ctx, nil, true); err != nil {
		return nil, bizerrors.WrapInternalContext(ctx, err, "reload role permissions failed")
	}
	return s.GetRolePermissions(ctx, roleID)
}

func (s *Service) findRole(ctx context.Context, client *appent.Client, id idgen.ID) (*appent.SysRole, error) {
	role, err := client.SysRole.Query().Where(sysrole.ID(id), sysrole.DeletedAtIsNil()).Only(ctx)
	if appent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "query role failed")
	}
	return role, nil
}

func (s *Service) findRoleForShare(ctx context.Context, client *appent.Client, id idgen.ID) (*appent.SysRole, error) {
	role, err := client.SysRole.Query().Where(sysrole.ID(id), sysrole.DeletedAtIsNil()).ForShare().Only(ctx)
	if appent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "query role failed")
	}
	return role, nil
}

func (s *Service) findRoleForUpdate(ctx context.Context, client *appent.Client, id idgen.ID) (*appent.SysRole, error) {
	role, err := client.SysRole.Query().Where(sysrole.ID(id), sysrole.DeletedAtIsNil()).ForUpdate().Only(ctx)
	if appent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "query role failed")
	}
	return role, nil
}

func (s *Service) findPermission(ctx context.Context, client *appent.Client, id idgen.ID) (*appent.SysMenu, error) {
	menu, err := client.SysMenu.Query().Where(sysmenu.ID(id), sysmenu.DeletedAtIsNil(), sysmenu.PermissionCodeNotNil()).Only(ctx)
	if appent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "query permission failed")
	}
	return menu, nil
}

func (s *Service) findPermissionForUpdate(ctx context.Context, client *appent.Client, id idgen.ID) (*appent.SysMenu, error) {
	menu, err := client.SysMenu.Query().Where(sysmenu.ID(id), sysmenu.DeletedAtIsNil(), sysmenu.PermissionCodeNotNil()).ForUpdate().Only(ctx)
	if appent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "query permission failed")
	}
	return menu, nil
}

func rolesByCodes(ctx context.Context, client *appent.Client, codes []string) ([]*appent.SysRole, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	roles, err := client.SysRole.Query().Where(sysrole.CodeIn(codes...), sysrole.DeletedAtIsNil(), sysrole.IsEnable(true)).
		Order(appent.Asc(sysrole.FieldID)).ForShare().All(ctx)
	if err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "query roles failed")
	}
	if len(roles) != len(codes) {
		return nil, bizerrors.BadRequest("role code is invalid")
	}
	return roles, nil
}

func permissionsByCodes(ctx context.Context, client *appent.Client, codes []string) ([]*appent.SysMenu, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	menus, err := client.SysMenu.Query().Where(sysmenu.PermissionCodeIn(codes...), sysmenu.DeletedAtIsNil()).
		Order(appent.Asc(sysmenu.FieldID)).ForShare().All(ctx)
	if err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "query permissions failed")
	}
	if len(menus) != len(codes) {
		return nil, bizerrors.BadRequest("permission code is invalid")
	}
	return menus, nil
}

func replaceUserRoleRelations(ctx context.Context, client *appent.Client, userID idgen.ID, roleIDs []idgen.ID) error {
	if _, err := client.SysUserRole.Update().Where(sysuserrole.UserID(userID), sysuserrole.DeletedAtIsNil()).SetDeletedAt(time.Now()).Save(ctx); err != nil {
		return mapWriteError(ctx, err, "replace user roles failed")
	}
	for _, roleID := range roleIDs {
		relation, err := client.SysUserRole.Query().Where(sysuserrole.UserID(userID), sysuserrole.RoleID(roleID)).Only(ctx)
		switch {
		case appent.IsNotFound(err):
			_, err = client.SysUserRole.Create().SetUserID(userID).SetRoleID(roleID).Save(ctx)
		case err == nil:
			_, err = client.SysUserRole.UpdateOne(relation).ClearDeletedAt().Save(ctx)
		}
		if err != nil {
			return mapWriteError(ctx, err, "write user role relation failed")
		}
	}
	return nil
}

func replaceRolePermissionRelations(ctx context.Context, client *appent.Client, roleID idgen.ID, menuIDs []idgen.ID) error {
	if _, err := client.SysRoleMenu.Update().Where(sysrolemenu.RoleID(roleID), sysrolemenu.DeletedAtIsNil()).SetDeletedAt(time.Now()).Save(ctx); err != nil {
		return mapWriteError(ctx, err, "replace role permissions failed")
	}
	for _, menuID := range menuIDs {
		relation, err := client.SysRoleMenu.Query().Where(sysrolemenu.RoleID(roleID), sysrolemenu.MenuID(menuID)).Only(ctx)
		switch {
		case appent.IsNotFound(err):
			_, err = client.SysRoleMenu.Create().SetRoleID(roleID).SetMenuID(menuID).Save(ctx)
		case err == nil:
			_, err = client.SysRoleMenu.UpdateOne(relation).ClearDeletedAt().Save(ctx)
		}
		if err != nil {
			return mapWriteError(ctx, err, "write role permission relation failed")
		}
	}
	return nil
}

func userIDsForRole(ctx context.Context, client *appent.Client, roleID idgen.ID) ([]idgen.ID, error) {
	var rows []struct {
		UserID idgen.ID `json:"user_id"`
	}
	err := client.SysUserRole.Query().Where(sysuserrole.RoleID(roleID), sysuserrole.DeletedAtIsNil()).Select(sysuserrole.FieldUserID).Scan(ctx, &rows)
	if err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "query role users failed")
	}
	ids := make([]idgen.ID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UserID)
	}
	return ids, nil
}

func roleToVO(role *appent.SysRole) RoleVO {
	return RoleVO{ID: role.ID, Code: role.Code, Name: role.Name, Remark: stringValue(role.Remark), IsBuiltin: role.IsBuiltin, IsEnable: role.IsEnable, SortID: role.SortID}
}

func permissionToVO(menu *appent.SysMenu) PermissionVO {
	return PermissionVO{ID: menu.ID, Title: menu.Title, PermissionCode: stringValue(menu.PermissionCode), MenuType: menu.MenuType, IsVisible: menu.IsVisible, SortID: menu.SortID}
}

func nullableString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func normalizeStrings(values []string) []string {
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

func validMenuType(value int16) bool {
	return value >= 0 && value <= 2
}

func mapWriteError(ctx context.Context, err error, message string) error {
	if appent.IsConstraintError(err) {
		return bizerrors.WrapBadRequestContext(ctx, err, message)
	}
	return bizerrors.WrapDBContext(ctx, err, message)
}
