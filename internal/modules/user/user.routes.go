package user

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/server"
)

// Module 负责 user 模块路由注册。
type Module struct {
	handler *Handler
}

// NewModule 创建 user 模块实例。
func NewModule(handler *Handler) *Module {
	return &Module{handler: handler}
}

// Register 将 user 模块的 Huma operation 和安全策略注册到应用。
func (m *Module) Register(api huma.API, registry *server.RouteRegistry) {
	huma.Register(api, huma.Operation{
		OperationID: "get-user",
		Method:      http.MethodGet,
		Path:        "/api/v1/users/{id}",
		Summary:     "查询用户详情",
		Description: "根据用户 ID 返回单个用户详情。",
		Tags:        []string{"用户管理"},
	}, m.handler.get)
	registry.Register(http.MethodGet, "/api/v1/users/{id}", platformauth.RouteSecurity{AccessMode: platformauth.AccessModePermission, Permission: "system:user:read"})

	huma.Register(api, huma.Operation{
		OperationID: "page-users",
		Method:      http.MethodGet,
		Path:        "/api/v1/users",
		Summary:     "分页查询用户列表",
		Description: "按分页参数返回用户列表。",
		Tags:        []string{"用户管理"},
	}, m.handler.page)
	registry.Register(http.MethodGet, "/api/v1/users", platformauth.RouteSecurity{AccessMode: platformauth.AccessModePermission, Permission: "system:user:read"})

	huma.Register(api, huma.Operation{
		OperationID: "create-user",
		Method:      http.MethodPost,
		Path:        "/api/v1/users",
		Summary:     "创建用户",
		Description: "创建一个新的系统用户。",
		Tags:        []string{"用户管理"},
	}, m.handler.create)
	registry.Register(http.MethodPost, "/api/v1/users", platformauth.RouteSecurity{AccessMode: platformauth.AccessModePermission, Permission: "system:user:write"})

	huma.Register(api, huma.Operation{
		OperationID: "update-user",
		Method:      http.MethodPut,
		Path:        "/api/v1/users/{id}",
		Summary:     "更新用户",
		Description: "更新用户显示名、邮箱、角色与状态。",
		Tags:        []string{"用户管理"},
	}, m.handler.update)
	registry.Register(http.MethodPut, "/api/v1/users/{id}", platformauth.RouteSecurity{AccessMode: platformauth.AccessModePermission, Permission: "system:user:write"})

	huma.Register(api, huma.Operation{
		OperationID: "delete-user",
		Method:      http.MethodDelete,
		Path:        "/api/v1/users/{id}",
		Summary:     "删除用户",
		Description: "软删除一个用户。",
		Tags:        []string{"用户管理"},
	}, m.handler.delete)
	registry.Register(http.MethodDelete, "/api/v1/users/{id}", platformauth.RouteSecurity{AccessMode: platformauth.AccessModePermission, Permission: "system:user:delete"})
}
