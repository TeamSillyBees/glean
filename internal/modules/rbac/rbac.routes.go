package rbac

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/server"
)

// Module 注册角色、权限资源及关系管理接口。
type Module struct{ handler *Handler }

// NewModule 创建 RBAC 模块。
func NewModule(handler *Handler) *Module { return &Module{handler: handler} }

// Register 注册 RBAC API 及稳定权限标识。
func (m *Module) Register(api huma.API, registry *server.RouteRegistry) {
	tag := []string{"权限管理"}
	registerRoute(api, registry, huma.Operation{OperationID: "list-roles", Method: http.MethodGet, Path: "/api/v1/roles", Summary: "查询角色列表", Tags: tag}, "system:role:read", m.handler.listRoles)
	registerRoute(api, registry, huma.Operation{OperationID: "get-role", Method: http.MethodGet, Path: "/api/v1/roles/{id}", Summary: "查询角色", Tags: tag}, "system:role:read", m.handler.getRole)
	registerRoute(api, registry, huma.Operation{OperationID: "create-role", Method: http.MethodPost, Path: "/api/v1/roles", Summary: "创建角色", Tags: tag}, "system:role:write", m.handler.createRole)
	registerRoute(api, registry, huma.Operation{OperationID: "update-role", Method: http.MethodPut, Path: "/api/v1/roles/{id}", Summary: "更新角色", Tags: tag}, "system:role:write", m.handler.updateRole)
	registerRoute(api, registry, huma.Operation{OperationID: "delete-role", Method: http.MethodDelete, Path: "/api/v1/roles/{id}", Summary: "删除角色", Tags: tag}, "system:role:delete", m.handler.deleteRole)

	registerRoute(api, registry, huma.Operation{OperationID: "list-permissions", Method: http.MethodGet, Path: "/api/v1/permissions", Summary: "查询权限资源列表", Tags: tag}, "system:permission:read", m.handler.listPermissions)
	registerRoute(api, registry, huma.Operation{OperationID: "get-permission", Method: http.MethodGet, Path: "/api/v1/permissions/{id}", Summary: "查询权限资源", Tags: tag}, "system:permission:read", m.handler.getPermission)
	registerRoute(api, registry, huma.Operation{OperationID: "create-permission", Method: http.MethodPost, Path: "/api/v1/permissions", Summary: "创建权限资源", Tags: tag}, "system:permission:write", m.handler.createPermission)
	registerRoute(api, registry, huma.Operation{OperationID: "update-permission", Method: http.MethodPut, Path: "/api/v1/permissions/{id}", Summary: "更新权限资源", Tags: tag}, "system:permission:write", m.handler.updatePermission)
	registerRoute(api, registry, huma.Operation{OperationID: "delete-permission", Method: http.MethodDelete, Path: "/api/v1/permissions/{id}", Summary: "删除权限资源", Tags: tag}, "system:permission:delete", m.handler.deletePermission)

	registerRoute(api, registry, huma.Operation{OperationID: "get-user-roles", Method: http.MethodGet, Path: "/api/v1/users/{id}/roles", Summary: "查询用户角色", Tags: tag}, "system:user-role:read", m.handler.getUserRoles)
	registerRoute(api, registry, huma.Operation{OperationID: "replace-user-roles", Method: http.MethodPut, Path: "/api/v1/users/{id}/roles", Summary: "替换用户角色", Tags: tag}, "system:user-role:write", m.handler.replaceUserRoles)
	registerRoute(api, registry, huma.Operation{OperationID: "get-role-permissions", Method: http.MethodGet, Path: "/api/v1/roles/{id}/permissions", Summary: "查询角色权限", Tags: tag}, "system:role-permission:read", m.handler.getRolePermissions)
	registerRoute(api, registry, huma.Operation{OperationID: "replace-role-permissions", Method: http.MethodPut, Path: "/api/v1/roles/{id}/permissions", Summary: "替换角色权限", Tags: tag}, "system:role-permission:write", m.handler.replaceRolePermissions)
}

func registerRoute[I, O any](
	api huma.API,
	registry *server.RouteRegistry,
	operation huma.Operation,
	permission string,
	handler func(context.Context, *I) (*O, error),
) {
	huma.Register(api, operation, handler)
	registry.Register(operation.Method, operation.Path, platformauth.RouteSecurity{
		AccessMode: platformauth.AccessModePermission,
		Permission: permission,
	})
}
