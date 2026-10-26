package httpdemo

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/server"
)

// Module 负责 httpdemo 示例模块路由注册。
type Module struct {
	handler *Handler
}

// NewModule 创建 httpdemo 示例模块实例。
func NewModule(handler *Handler) *Module {
	return &Module{handler: handler}
}

// Register 将 httpdemo 示例模块的 Huma operation 和安全策略注册到应用。
func (m *Module) Register(api huma.API, registry *server.RouteRegistry) {
	huma.Register(api, huma.Operation{
		OperationID: "httpdemo-httpbingo-get",
		Method:      http.MethodGet,
		Path:        "/api/v1/http-client/httpbingo/get",
		Summary:     "HTTP Client GET 示例",
		Description: "调用 https://httpbingo.org/get 获取示例数据。",
		Tags:        []string{"HTTP Client 示例"},
	}, m.handler.get)
	registry.Register(http.MethodGet, "/api/v1/http-client/httpbingo/get", platformauth.RouteSecurity{AccessMode: platformauth.AccessModePermission, Permission: "system:httpdemo:read"})

	huma.Register(api, huma.Operation{
		OperationID: "httpdemo-httpbingo-form-page",
		Method:      http.MethodGet,
		Path:        "/api/v1/http-client/httpbingo/forms/post",
		Summary:     "HTTP Client 表单页示例",
		Description: "调用 https://httpbingo.org/forms/post 并返回 HTML 内容。",
		Tags:        []string{"HTTP Client 示例"},
	}, m.handler.formPage)
	registry.Register(http.MethodGet, "/api/v1/http-client/httpbingo/forms/post", platformauth.RouteSecurity{AccessMode: platformauth.AccessModePermission, Permission: "system:httpdemo:read"})
}
