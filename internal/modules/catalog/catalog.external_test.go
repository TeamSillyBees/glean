package catalog

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/require"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/server"
)

// TestCatalogExternalDimensionsRequireObject 防止 null 和非对象输入绕过约束或触发 nil map panic。
func TestCatalogExternalDimensionsRequireObject(t *testing.T) {
	for _, value := range []string{"null", "[]", `"text"`, "true", "42", `{"task":42}`, `{"task":["x"]}`, "{"} {
		t.Run(value, func(t *testing.T) {
			q := PaperQuery{Dimension: value, Task: "clinical"}
			require.NotPanics(t, func() {
				require.Error(t, validateQuery(&q))
			})
		})
	}
	q := PaperQuery{Dimension: `{}`, Task: "clinical"}
	require.NoError(t, validateQuery(&q))
}

// TestCatalogExternalDimensionKeysCannotBecomeSQL 防止 Ent JSON path 将非可信键名直接插入 SQL。
func TestCatalogExternalDimensionKeysCannotBecomeSQL(t *testing.T) {
	// sqljson.Path 适合固定标识符，而不是未经处理的外部键名。
	for _, key := range []string{"x' OR TRUE --", "x'); SELECT pg_sleep(9); --", "[0]"} {
		t.Run(key, func(t *testing.T) {
			encoded, err := json.Marshal(map[string]string{key: "needle"})
			require.NoError(t, err)
			q := PaperQuery{Dimension: string(encoded)}
			if err := validateQuery(&q); err != nil {
				return // 明确拒绝不支持的键名也是安全且可预期的结果。
			}
			statement, args := membershipSelector(q).Query()
			require.NotContains(t, statement, key)
			require.Contains(t, fmt.Sprint(args), key, "合法外部键必须作为参数绑定，不能解释为 JSON 数组索引")
		})
	}
}

// TestCatalogExternalFilterValuesAreBound 验证普通筛选、JSON 值和主题别名都通过 SQL 参数传入。
func TestCatalogExternalFilterValuesAreBound(t *testing.T) {
	attack := "needle' OR 1=1 --"
	encoded, err := json.Marshal(map[string]string{"custom_dimension": attack})
	require.NoError(t, err)
	q := PaperQuery{Topic: "biomed-ai", Tier: attack, Category: attack, Task: attack, Role: attack, Method: attack, Dimension: string(encoded)}
	require.NoError(t, validateQuery(&q))
	statement, args := membershipSelector(q).Query()
	require.NotContains(t, statement, attack)
	require.Contains(t, fmt.Sprint(args), attack)
	require.Contains(t, statement, "$1")
	// method 下拉来自 ai_methods，不能查询不同的 paper_types 维度。
	methodSQL, methodArgs := membershipSelector(PaperQuery{Method: "transformer"}).Query()
	require.Contains(t, methodSQL+fmt.Sprint(methodArgs), "ai_methods")
	require.NotContains(t, methodSQL+fmt.Sprint(methodArgs), "paper_types")
}

// TestCatalogExternalRoutesRequireAuthentication 固定八条路由的运行时和 OpenAPI 安全契约。
func TestCatalogExternalRoutesRequireAuthentication(t *testing.T) {
	app, err := server.NewApp(server.Options{Title: "Glean QA", Version: "test", Env: "test"}, nil, nil, nil, nil)
	require.NoError(t, err)
	// service 故意为 nil；未认证请求必须在进入业务和数据库前被阻断。
	NewModule(NewHandler(nil)).Register(app.API, app.Registry)
	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/catalog/topics"},
		{http.MethodGet, "/api/v1/catalog/papers"},
		{http.MethodGet, "/api/v1/catalog/papers/{id}"},
		{http.MethodGet, "/api/v1/catalog/facets"},
		{http.MethodGet, "/api/v1/catalog/venues"},
		{http.MethodGet, "/api/v1/catalog/preferences"},
		{http.MethodPut, "/api/v1/catalog/preferences"},
		{http.MethodPut, "/api/v1/catalog/papers/{id}/state"},
	}
	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			security, ok := app.Registry.Lookup(route.method, route.path)
			require.True(t, ok)
			require.Equal(t, platformauth.AccessModeAuthenticated, security.AccessMode)
			request := httptest.NewRequest(route.method, strings.ReplaceAll(route.path, "{id}", "123"), nil)
			response := httptest.NewRecorder()
			app.Engine.ServeHTTP(response, request)
			require.Equal(t, http.StatusUnauthorized, response.Code)
			pathItem := app.API.OpenAPI().Paths[route.path]
			require.NotNil(t, pathItem)
			var operation *huma.Operation
			if route.method == http.MethodGet {
				operation = pathItem.Get
			} else {
				operation = pathItem.Put
			}
			require.NotNil(t, operation)
			require.Equal(t, []map[string][]string{{"bearerAuth": {}}}, operation.Security)
			require.Contains(t, operation.Responses, "401")
		})
	}
}
