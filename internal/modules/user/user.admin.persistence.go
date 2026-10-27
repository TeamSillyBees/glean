package user

import (
	"context"

	appent "github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/sysuser"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// lockAdministrators 必须先于目标用户锁获取；统一顺序避免交叉禁用管理员时死锁。
// 本模块所有用户写事务均遵守此顺序，使最后一个管理员检查与角色提升互斥。
func lockAdministrators(ctx context.Context, client *appent.Client) error {
	_, err := client.SysUser.Query().Where(sysuser.DeletedAtIsNil(), sysuser.IsEnable(true), sysuser.IsSuperAdmin(true)).Order(appent.Asc(sysuser.FieldID)).ForUpdate().All(ctx)
	if err != nil {
		return bizerrors.WrapDBContext(ctx, err, "lock active super administrators failed")
	}
	return nil
}

func requireSuperAdmin(ctx context.Context, client *appent.Client) error {
	principal, ok := platformauth.PrincipalFromContext(ctx)
	if !ok || !principal.IsSuperAdmin {
		return bizerrors.UserAdminForbidden()
	}
	// 不信任请求开始时的缓存身份；管理员锁内重新检查数据库当前状态。
	exists, err := client.SysUser.Query().Where(sysuser.ID(principal.UserID), sysuser.DeletedAtIsNil(), sysuser.IsEnable(true), sysuser.IsSuperAdmin(true)).Exist(ctx)
	if err != nil {
		return bizerrors.WrapDBContext(ctx, err, "validate current administrator failed")
	}
	if !exists {
		return bizerrors.UserAdminForbidden()
	}
	return nil
}

func protectSelf(ctx context.Context, id idgen.ID, disable, demote, remove bool) error {
	principal, ok := platformauth.PrincipalFromContext(ctx)
	if ok && principal.UserID == id && (disable || demote && principal.IsSuperAdmin || remove) {
		return bizerrors.BadRequest("you cannot disable, downgrade, or delete your own account")
	}
	return nil
}
