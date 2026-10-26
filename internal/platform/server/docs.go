package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// DocsExposureMiddleware 在文档关闭时强制隐藏 Huma 文档、OpenAPI 和 Schema 路径。
// 即使后续代码误注册相同前缀，也不能绕过环境化暴露策略。
func DocsExposureMiddleware(config DocsConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !config.Enabled && isDocumentationPath(c.Request.URL.Path) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.Next()
	}
}

func isDocumentationPath(path string) bool {
	return path == "/docs" || strings.HasPrefix(path, "/docs/") ||
		path == "/openapi" || strings.HasPrefix(path, "/openapi.") || strings.HasPrefix(path, "/openapi-") ||
		path == "/schemas" || strings.HasPrefix(path, "/schemas/")
}
