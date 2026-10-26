package rbac

import (
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/response"
)

// RoleVO 是角色管理接口返回的角色。
type RoleVO struct {
	ID        idgen.ID `json:"id"`
	Code      string   `json:"code"`
	Name      string   `json:"name"`
	Remark    string   `json:"remark"`
	IsBuiltin bool     `json:"isBuiltin"`
	IsEnable  bool     `json:"isEnable"`
	SortID    int32    `json:"sortId"`
}

// CreateRoleBody 描述创建角色输入。
type CreateRoleBody struct {
	Code     string `json:"code" example:"auditor"`
	Name     string `json:"name" example:"审计员"`
	Remark   string `json:"remark,omitempty"`
	IsEnable *bool  `json:"isEnable,omitempty"`
	SortID   int32  `json:"sortId,omitempty"`
}

// UpdateRoleBody 描述更新角色输入；角色编码创建后不可修改。
type UpdateRoleBody struct {
	Name     *string `json:"name,omitempty"`
	Remark   *string `json:"remark,omitempty"`
	IsEnable *bool   `json:"isEnable,omitempty"`
	SortID   *int32  `json:"sortId,omitempty"`
}

// PermissionVO 是 sys_menu 中可参与后端授权的稳定权限资源。
type PermissionVO struct {
	ID             idgen.ID `json:"id"`
	Title          string   `json:"title"`
	PermissionCode string   `json:"permissionCode"`
	MenuType       int16    `json:"menuType"`
	IsVisible      bool     `json:"isVisible"`
	SortID         int32    `json:"sortId"`
}

// CreatePermissionBody 描述创建权限资源输入。
type CreatePermissionBody struct {
	Title          string `json:"title"`
	PermissionCode string `json:"permissionCode" example:"system:audit:read"`
	MenuType       *int16 `json:"menuType,omitempty" example:"1"`
	IsVisible      *bool  `json:"isVisible,omitempty"`
	SortID         int32  `json:"sortId,omitempty"`
}

// UpdatePermissionBody 描述更新权限资源输入；权限编码创建后不可修改。
type UpdatePermissionBody struct {
	Title     *string `json:"title,omitempty"`
	MenuType  *int16  `json:"menuType,omitempty"`
	IsVisible *bool   `json:"isVisible,omitempty"`
	SortID    *int32  `json:"sortId,omitempty"`
}

// ReplaceUserRolesBody 使用完整角色编码集合替换用户角色。
type ReplaceUserRolesBody struct {
	RoleCodes []string `json:"roleCodes"`
}

// ReplaceRolePermissionsBody 使用完整权限编码集合替换角色权限。
type ReplaceRolePermissionsBody struct {
	PermissionCodes []string `json:"permissionCodes"`
}

type idRequest struct {
	ID idgen.ID `path:"id" example:"1771234567890123456"`
}

type getRoleResponse struct{ Body response.SuccessVO[RoleVO] }
type listRolesResponse struct{ Body response.SuccessVO[[]RoleVO] }
type createRoleRequest struct{ Body CreateRoleBody }
type createRoleResponse struct{ Body response.SuccessVO[RoleVO] }
type updateRoleRequest struct {
	ID   idgen.ID `path:"id"`
	Body UpdateRoleBody
}
type updateRoleResponse struct{ Body response.SuccessVO[RoleVO] }
type emptyResponse struct {
	Body response.SuccessVO[map[string]any]
}

type getPermissionResponse struct {
	Body response.SuccessVO[PermissionVO]
}
type listPermissionsResponse struct {
	Body response.SuccessVO[[]PermissionVO]
}
type createPermissionRequest struct{ Body CreatePermissionBody }
type createPermissionResponse struct {
	Body response.SuccessVO[PermissionVO]
}
type updatePermissionRequest struct {
	ID   idgen.ID `path:"id"`
	Body UpdatePermissionBody
}
type updatePermissionResponse struct {
	Body response.SuccessVO[PermissionVO]
}

type replaceUserRolesRequest struct {
	ID   idgen.ID `path:"id" doc:"用户 ID"`
	Body ReplaceUserRolesBody
}
type userRolesResponse struct{ Body response.SuccessVO[[]RoleVO] }

type replaceRolePermissionsRequest struct {
	ID   idgen.ID `path:"id" doc:"角色 ID"`
	Body ReplaceRolePermissionsBody
}
type rolePermissionsResponse struct {
	Body response.SuccessVO[[]PermissionVO]
}
