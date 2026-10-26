package catalog

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/server"
)

// Module 负责目录端点与 fail-closed 安全策略注册。
type Module struct{ handler *Handler }

// NewModule 创建目录模块。
func NewModule(handler *Handler) *Module { return &Module{handler: handler} }

// Register 为每条 API 显式登记认证要求。
func (m *Module) Register(api huma.API, registry *server.RouteRegistry) {
	var operations []huma.Operation
	register := func(method, path, id, summary string) huma.Operation {
		operations = append(operations, huma.Operation{Method: method, Path: path})
		return huma.Operation{OperationID: id, Method: method, Path: path, Summary: summary, Tags: []string{"Glean 论文目录"}, MaxBodyBytes: 40 * 1024}
	}
	huma.Register(api, register(http.MethodGet, "/api/v1/catalog/topics", "catalog-topics", "研究主题"), m.handler.topics)
	huma.Register(api, register(http.MethodGet, "/api/v1/catalog/papers", "catalog-papers", "检索历史论文"), m.handler.papers)
	huma.Register(api, register(http.MethodGet, "/api/v1/catalog/papers/{id}", "catalog-paper", "论文来源详情"), m.handler.paper)
	huma.Register(api, register(http.MethodGet, "/api/v1/catalog/facets", "catalog-facets", "论文筛选维度"), m.handler.facets)
	huma.Register(api, register(http.MethodGet, "/api/v1/catalog/venues", "catalog-venues", "期刊会议资格证据"), m.handler.venues)
	huma.Register(api, register(http.MethodGet, "/api/v1/catalog/preferences", "catalog-preferences", "个人筛选偏好"), m.handler.preferences)
	huma.Register(api, register(http.MethodPut, "/api/v1/catalog/preferences", "catalog-save-preferences", "保存个人筛选偏好"), m.handler.savePreferences)
	huma.Register(api, register(http.MethodPut, "/api/v1/catalog/papers/{id}/state", "catalog-paper-state", "保存个人阅读隐藏状态"), m.handler.saveState)
	for _, op := range operations {
		registry.Register(op.Method, op.Path, platformauth.RouteSecurity{AccessMode: platformauth.AccessModeAuthenticated})
	}
}
