package rbac

import (
	"context"

	"github.com/teamsillybees/glean/internal/platform/response"
)

// Handler 封装 RBAC 管理 HTTP 适配。
type Handler struct{ service *Service }

// NewHandler 创建 RBAC Handler。
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) listRoles(ctx context.Context, _ *struct{}) (*listRolesResponse, error) {
	items, err := h.service.ListRoles(ctx)
	return &listRolesResponse{Body: response.OK(ctx, items)}, err
}
func (h *Handler) getRole(ctx context.Context, input *idRequest) (*getRoleResponse, error) {
	item, err := h.service.GetRole(ctx, input.ID)
	return &getRoleResponse{Body: response.OK(ctx, item)}, err
}
func (h *Handler) createRole(ctx context.Context, input *createRoleRequest) (*createRoleResponse, error) {
	item, err := h.service.CreateRole(ctx, input.Body)
	return &createRoleResponse{Body: response.OK(ctx, item)}, err
}
func (h *Handler) updateRole(ctx context.Context, input *updateRoleRequest) (*updateRoleResponse, error) {
	item, err := h.service.UpdateRole(ctx, input.ID, input.Body)
	return &updateRoleResponse{Body: response.OK(ctx, item)}, err
}
func (h *Handler) deleteRole(ctx context.Context, input *idRequest) (*emptyResponse, error) {
	err := h.service.DeleteRole(ctx, input.ID)
	return &emptyResponse{Body: response.OK(ctx, map[string]any{})}, err
}
func (h *Handler) listPermissions(ctx context.Context, _ *struct{}) (*listPermissionsResponse, error) {
	items, err := h.service.ListPermissions(ctx)
	return &listPermissionsResponse{Body: response.OK(ctx, items)}, err
}
func (h *Handler) getPermission(ctx context.Context, input *idRequest) (*getPermissionResponse, error) {
	item, err := h.service.GetPermission(ctx, input.ID)
	return &getPermissionResponse{Body: response.OK(ctx, item)}, err
}
func (h *Handler) createPermission(ctx context.Context, input *createPermissionRequest) (*createPermissionResponse, error) {
	item, err := h.service.CreatePermission(ctx, input.Body)
	return &createPermissionResponse{Body: response.OK(ctx, item)}, err
}
func (h *Handler) updatePermission(ctx context.Context, input *updatePermissionRequest) (*updatePermissionResponse, error) {
	item, err := h.service.UpdatePermission(ctx, input.ID, input.Body)
	return &updatePermissionResponse{Body: response.OK(ctx, item)}, err
}
func (h *Handler) deletePermission(ctx context.Context, input *idRequest) (*emptyResponse, error) {
	err := h.service.DeletePermission(ctx, input.ID)
	return &emptyResponse{Body: response.OK(ctx, map[string]any{})}, err
}
func (h *Handler) getUserRoles(ctx context.Context, input *idRequest) (*userRolesResponse, error) {
	items, err := h.service.GetUserRoles(ctx, input.ID)
	return &userRolesResponse{Body: response.OK(ctx, items)}, err
}
func (h *Handler) replaceUserRoles(ctx context.Context, input *replaceUserRolesRequest) (*userRolesResponse, error) {
	items, err := h.service.ReplaceUserRoles(ctx, input.ID, input.Body.RoleCodes)
	return &userRolesResponse{Body: response.OK(ctx, items)}, err
}
func (h *Handler) getRolePermissions(ctx context.Context, input *idRequest) (*rolePermissionsResponse, error) {
	items, err := h.service.GetRolePermissions(ctx, input.ID)
	return &rolePermissionsResponse{Body: response.OK(ctx, items)}, err
}
func (h *Handler) replaceRolePermissions(ctx context.Context, input *replaceRolePermissionsRequest) (*rolePermissionsResponse, error) {
	items, err := h.service.ReplaceRolePermissions(ctx, input.ID, input.Body.PermissionCodes)
	return &rolePermissionsResponse{Body: response.OK(ctx, items)}, err
}
