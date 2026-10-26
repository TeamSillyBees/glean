package auth

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/server"
)

// Module 负责 auth 模块路由注册。
type Module struct {
	handler *Handler
}

// NewModule 创建 auth 模块实例。
func NewModule(handler *Handler) *Module {
	return &Module{handler: handler}
}

// Register 将 auth 模块注册到应用。
func (m *Module) Register(api huma.API, registry *server.RouteRegistry) {
	huma.Register(api, huma.Operation{
		OperationID: "auth-login",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/login",
		Summary:     "用户登录",
		Description: "根据账号密码签发访问令牌和刷新令牌。",
		Tags:        []string{"认证管理"},
	}, m.handler.login)
	registry.Register(http.MethodPost, "/api/v1/auth/login", platformauth.RouteSecurity{AccessMode: platformauth.AccessModePublic})

	huma.Register(api, huma.Operation{
		OperationID: "auth-refresh",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/refresh",
		Summary:     "刷新令牌",
		Description: "使用 refresh token 换取新的访问令牌。",
		Tags:        []string{"认证管理"},
	}, m.handler.refresh)
	registry.Register(http.MethodPost, "/api/v1/auth/refresh", platformauth.RouteSecurity{AccessMode: platformauth.AccessModePublic})

	huma.Register(api, huma.Operation{
		OperationID: "auth-logout",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/logout",
		Summary:     "退出当前会话",
		Description: "原子消费当前 refresh token，并吊销与其绑定的 access token。",
		Tags:        []string{"认证管理"},
	}, m.handler.logout)
	registry.Register(http.MethodPost, "/api/v1/auth/logout", platformauth.RouteSecurity{AccessMode: platformauth.AccessModeAuthenticated})

	huma.Register(api, huma.Operation{
		OperationID: "auth-logout-all",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/logout-all",
		Summary:     "退出全部会话",
		Description: "递增用户会话版本，使该用户的全部旧 access token 和 refresh token 失效。",
		Tags:        []string{"认证管理"},
	}, m.handler.logoutAll)
	registry.Register(http.MethodPost, "/api/v1/auth/logout-all", platformauth.RouteSecurity{AccessMode: platformauth.AccessModeAuthenticated})

	huma.Register(api, huma.Operation{
		OperationID: "auth-change-password",
		Method:      http.MethodPut,
		Path:        "/api/v1/auth/password",
		Summary:     "修改当前用户密码",
		Description: "校验当前密码并更新密码；成功后使全部旧会话失效。",
		Tags:        []string{"认证管理"},
	}, m.handler.changePassword)
	registry.Register(http.MethodPut, "/api/v1/auth/password", platformauth.RouteSecurity{AccessMode: platformauth.AccessModeAuthenticated})

	huma.Register(api, huma.Operation{
		OperationID: "auth-me",
		Method:      http.MethodGet,
		Path:        "/api/v1/auth/me",
		Summary:     "获取当前登录用户",
		Description: "读取当前访问令牌对应的用户信息。",
		Tags:        []string{"认证管理"},
	}, m.handler.me)
	registry.Register(http.MethodGet, "/api/v1/auth/me", platformauth.RouteSecurity{AccessMode: platformauth.AccessModeAuthenticated})
}
