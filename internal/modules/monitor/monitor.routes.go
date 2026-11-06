package monitor

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/server"
)

// Module 注册认证后的监控视图与权限受控的管理操作，不提供启动真实采集的公开入口。
type Module struct{ handler *Handler }

// NewModule 创建监控路由模块。
func NewModule(handler *Handler) *Module { return &Module{handler: handler} }

// Register 注册操作及 fail-closed 路由安全声明。
func (m *Module) Register(api huma.API, registry *server.RouteRegistry) {
	m.registerPipeline(api, registry)
	m.registerAudit(api, registry)
	huma.Register(api, huma.Operation{OperationID: "monitor-sources", Method: http.MethodGet, Path: "/api/v1/monitor/sources", Summary: "查看来源状态", Tags: []string{"论文监控"}}, m.handler.sources)
	huma.Register(api, huma.Operation{OperationID: "monitor-test-source-connections", Method: http.MethodPost, Path: "/api/v1/monitor/sources/test", Summary: "手动测试来源连通性", Tags: []string{"论文监控"}}, m.handler.testSourceConnections)
	huma.Register(api, huma.Operation{OperationID: "monitor-runs", Method: http.MethodGet, Path: "/api/v1/monitor/runs", Summary: "查看扫描历史", Tags: []string{"论文监控"}}, m.handler.runs)
	huma.Register(api, huma.Operation{OperationID: "monitor-templates", Method: http.MethodGet, Path: "/api/v1/monitor/templates", Summary: "查看统一检索模板", Tags: []string{"论文监控"}}, m.handler.templates)
	huma.Register(api, huma.Operation{OperationID: "monitor-quarantine", Method: http.MethodGet, Path: "/api/v1/monitor/quarantine", Summary: "查看隔离记录", Tags: []string{"论文监控"}}, m.handler.quarantine)
	huma.Register(api, huma.Operation{OperationID: "monitor-quarantine-detail", Method: http.MethodGet, Path: "/api/v1/monitor/quarantine/{id}", Summary: "查看隔离证据安全摘要", Tags: []string{"论文监控"}}, m.handler.quarantineDetail)
	registry.Register(http.MethodGet, "/api/v1/monitor/quarantine", platformauth.RouteSecurity{AccessMode: platformauth.AccessModeAuthenticated})
	registry.Register(http.MethodGet, "/api/v1/monitor/quarantine/{id}", platformauth.RouteSecurity{AccessMode: platformauth.AccessModeAuthenticated})
	registry.Register(http.MethodGet, "/api/v1/monitor/sources", platformauth.RouteSecurity{AccessMode: platformauth.AccessModeAuthenticated})
	registry.Register(http.MethodPost, "/api/v1/monitor/sources/test", platformauth.RouteSecurity{AccessMode: platformauth.AccessModePermission, Permission: "catalog:manage"})
	registry.Register(http.MethodGet, "/api/v1/monitor/runs", platformauth.RouteSecurity{AccessMode: platformauth.AccessModeAuthenticated})
	registry.Register(http.MethodGet, "/api/v1/monitor/templates", platformauth.RouteSecurity{AccessMode: platformauth.AccessModeAuthenticated})
}
