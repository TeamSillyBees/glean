package file

import (
	"math"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/server"
)

// Module 负责 file 示例模块路由注册。
type Module struct {
	handler      *Handler
	maxBodyBytes int64
}

// NewModule 创建 file 示例模块实例。
func NewModule(handler *Handler, maxFileSize int64) *Module {
	return &Module{handler: handler, maxBodyBytes: uploadBodyLimit(maxFileSize)}
}

// Register 将 file 示例模块的 Huma operation 和安全策略注册到应用。
func (m *Module) Register(api huma.API, registry *server.RouteRegistry) {
	huma.Register(api, huma.Operation{
		OperationID:   "upload-local-file",
		Method:        http.MethodPost,
		Path:          "/api/v1/files/local",
		Summary:       "上传本地文件",
		Description:   "上传文件到当前配置的 local 存储目录。",
		Tags:          []string{"文件示例"},
		DefaultStatus: http.StatusCreated,
		MaxBodyBytes:  m.maxBodyBytes,
	}, m.handler.upload)
	registry.Register(http.MethodPost, "/api/v1/files/local", platformauth.RouteSecurity{AccessMode: platformauth.AccessModePermission, Permission: "system:file:write"})

	huma.Register(api, huma.Operation{
		OperationID: "download-local-file",
		Method:      http.MethodGet,
		Path:        "/api/v1/files/local/download",
		Summary:     "下载本地文件",
		Description: "通过对象 key 下载 local 存储中的文件。",
		Tags:        []string{"文件示例"},
	}, m.handler.download)
	registry.Register(http.MethodGet, "/api/v1/files/local/download", platformauth.RouteSecurity{AccessMode: platformauth.AccessModePermission, Permission: "system:file:read"})

	huma.Register(api, huma.Operation{
		OperationID: "stat-local-file",
		Method:      http.MethodGet,
		Path:        "/api/v1/files/local/meta",
		Summary:     "查询本地文件元信息",
		Description: "通过对象 key 查询 local 存储中的文件元信息。",
		Tags:        []string{"文件示例"},
	}, m.handler.stat)
	registry.Register(http.MethodGet, "/api/v1/files/local/meta", platformauth.RouteSecurity{AccessMode: platformauth.AccessModePermission, Permission: "system:file:read"})

	huma.Register(api, huma.Operation{
		OperationID: "delete-local-file",
		Method:      http.MethodDelete,
		Path:        "/api/v1/files/local",
		Summary:     "删除本地文件",
		Description: "通过对象 key 删除 local 存储中的文件。",
		Tags:        []string{"文件示例"},
	}, m.handler.delete)
	registry.Register(http.MethodDelete, "/api/v1/files/local", platformauth.RouteSecurity{AccessMode: platformauth.AccessModePermission, Permission: "system:file:delete"})
}

const multipartBodyOverhead int64 = 1 << 20

func uploadBodyLimit(maxFileSize int64) int64 {
	if maxFileSize <= 0 {
		return multipartBodyOverhead
	}
	if maxFileSize > math.MaxInt64-multipartBodyOverhead {
		return math.MaxInt64
	}
	return maxFileSize + multipartBodyOverhead
}
