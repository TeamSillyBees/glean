package bizerrors

import apperrors "github.com/teamsillybees/glean/internal/platform/errors"

// UserAdminForbidden 表示当前身份不能修改用户的管理权限。
func UserAdminForbidden() error {
	return newError(apperrors.CodeForbidden, "user administration requires an active super administrator")
}
