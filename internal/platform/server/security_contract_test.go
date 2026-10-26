package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
)

// TestRouteSecurityUpdatesOpenAPIContract 验证运行时路由安全元数据同步生成 Bearer scheme、operation security 和错误响应。
func TestRouteSecurityUpdatesOpenAPIContract(t *testing.T) {
	app, err := NewApp(Options{Title: "initra", Version: "test", Env: "test"}, nil, nil, nil, nil)
	require.NoError(t, err)

	huma.Register(app.API, huma.Operation{
		OperationID: "public-contract",
		Method:      http.MethodGet,
		Path:        "/api/v1/public-contract",
	}, func(context.Context, *struct{}) (*struct{}, error) { return &struct{}{}, nil })
	app.Registry.Register(http.MethodGet, "/api/v1/public-contract", platformauth.RouteSecurity{AccessMode: platformauth.AccessModePublic})

	huma.Register(app.API, huma.Operation{
		OperationID: "protected-contract",
		Method:      http.MethodGet,
		Path:        "/api/v1/protected-contract",
	}, func(context.Context, *struct{}) (*struct{}, error) { return &struct{}{}, nil })
	app.Registry.Register(http.MethodGet, "/api/v1/protected-contract", platformauth.RouteSecurity{
		AccessMode: platformauth.AccessModePermission,
		Permission: "system:contract:read",
	})

	openAPI := app.API.OpenAPI()
	scheme := openAPI.Components.SecuritySchemes[bearerSecuritySchemeName]
	require.NotNil(t, scheme)
	require.Equal(t, "http", scheme.Type)
	require.Equal(t, "bearer", scheme.Scheme)
	require.Equal(t, "JWT", scheme.BearerFormat)

	publicOperation := openAPI.Paths["/api/v1/public-contract"].Get
	require.NotNil(t, publicOperation.Security)
	require.Empty(t, publicOperation.Security)

	protectedOperation := openAPI.Paths["/api/v1/protected-contract"].Get
	require.Equal(t, []map[string][]string{{bearerSecuritySchemeName: {}}}, protectedOperation.Security)
	require.Equal(t, "#/components/responses/Unauthorized", protectedOperation.Responses["401"].Ref)
	require.Equal(t, "#/components/responses/Forbidden", protectedOperation.Responses["403"].Ref)
}

// TestDocsExposureIsEnvironmentAware 验证文档路由可配置关闭，且共享环境不能公开匿名文档。
func TestDocsExposureIsEnvironmentAware(t *testing.T) {
	disabled, err := NewApp(Options{Title: "initra", Version: "test", Env: "test"}, nil, nil, nil, nil)
	require.NoError(t, err)
	disabled.Engine.GET("/docs/accidental", func(c *gin.Context) { c.Status(http.StatusOK) })
	recorder := httptest.NewRecorder()
	disabled.Engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/docs/accidental", nil))
	require.Equal(t, http.StatusNotFound, recorder.Code)

	enabled, err := NewApp(Options{
		Title: "initra", Version: "test", Env: "dev", Docs: DocsConfig{Enabled: true},
	}, nil, nil, nil, nil)
	require.NoError(t, err)
	recorder = httptest.NewRecorder()
	enabled.Engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	require.Equal(t, http.StatusOK, recorder.Code)

	_, err = NewApp(Options{
		Title: "initra", Version: "test", Env: "prod", Docs: DocsConfig{Enabled: true},
	}, nil, nil, nil, nil)
	require.ErrorContains(t, err, "禁止公开")
}

func TestCORSConfigRejectsWildcardInProduction(t *testing.T) {
	err := (CORSConfig{
		Enabled:        true,
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{http.MethodGet},
		AllowedHeaders: []string{"Authorization"},
	}).Validate("prod")
	require.ErrorContains(t, err, "通配符")
}

func TestCORSConfigRejectsOriginWithPath(t *testing.T) {
	err := (CORSConfig{
		Enabled:        true,
		AllowedOrigins: []string{"https://admin.example.test/path"},
		AllowedMethods: []string{http.MethodGet},
		AllowedHeaders: []string{"Authorization"},
	}).Validate("test")
	require.ErrorContains(t, err, "不能包含")
}
