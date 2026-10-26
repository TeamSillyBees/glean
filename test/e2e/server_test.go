package e2e_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/data"
	appent "github.com/teamsillybees/glean/internal/data/ent"
	authmodule "github.com/teamsillybees/glean/internal/modules/auth"
	filemodule "github.com/teamsillybees/glean/internal/modules/file"
	usermodule "github.com/teamsillybees/glean/internal/modules/user"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/logx"
	"github.com/teamsillybees/glean/internal/platform/observability"
	"github.com/teamsillybees/glean/internal/platform/server"
	"github.com/teamsillybees/glean/internal/platform/storage"
	"github.com/teamsillybees/glean/internal/platform/storage/local"
)

func newMockEntClient(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *appent.Client) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	generator, err := idgen.NewGenerator(1)
	require.NoError(t, err)
	return db, mock, data.NewEntClientFromDB(db, generator)
}

func sysUserRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "deleted_at", "created_at", "updated_at", "created_by", "updated_by",
		"username", "password_hash", "nickname", "phone", "email", "avatar_url",
		"is_super_admin", "is_enable", "session_version", "sort_id",
	})
}

var testNow = time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)

// noopUserCache 用于 e2e 测试，始终模拟缓存未命中。
type noopUserCache struct{}

func (noopUserCache) Get(context.Context, idgen.ID) (*usermodule.User, bool, error) {
	return nil, false, nil
}
func (noopUserCache) Set(context.Context, *usermodule.User) error { return nil }
func (noopUserCache) Delete(context.Context, idgen.ID) error      { return nil }

type noopAuthorizationInvalidator struct{}

func (noopAuthorizationInvalidator) NotifyChanged(context.Context, []idgen.ID, bool) error {
	return nil
}

// TestServer_LoginMeAndUserDetail 覆盖登录、当前用户、用户详情和健康检查的完整 HTTP 流程。
func TestServer_LoginMeAndUserDetail(t *testing.T) {
	logger := logx.NewNop()
	passwords := platformauth.NewBcryptPasswordManager(4)
	passwordHash, err := passwords.Hash("secret-123")
	require.NoError(t, err)

	modelPath := writeCasbinModel(t)
	jwtManager, err := platformauth.NewJWTManager(platformauth.JWTConfig{
		Issuer:          "initra",
		Secret:          "e2e-test-secret-at-least-32-bytes",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
	})
	require.NoError(t, err)

	enforcer, err := platformauth.NewEnforcer(modelPath, platformauth.PolicyLoaderFunc(func(context.Context) ([]platformauth.PolicyRule, error) {
		return []platformauth.PolicyRule{
			{RoleCode: "admin", PermissionCode: "system:user:read"},
			{RoleCode: "admin", PermissionCode: "system:file:read"},
			{RoleCode: "admin", PermissionCode: "system:file:write"},
			{RoleCode: "admin", PermissionCode: "system:file:delete"},
		}, nil
	}))
	require.NoError(t, err)

	app, err := server.NewApp(server.Options{
		Title:   "initra",
		Version: "test",
		Env:     "test",
	}, logger, jwtManager, platformauth.IdentityResolverFunc(func(_ context.Context, userID idgen.ID) (platformauth.Principal, bool, error) {
		return platformauth.Principal{UserID: userID, SessionVersion: 1, Roles: []string{"admin"}}, true, nil
	}), enforcer)
	require.NoError(t, err)

	observability.NewModule(observability.BuildInfoVO{
		Version:   "test",
		Commit:    "abc123",
		BuildTime: "2026-04-21T00:00:00Z",
	}, nil).Register(app.API, app.Registry)

	db, mock, entClient := newMockEntClient(t)
	defer db.Close()

	// Mock 期望按 HTTP 请求执行顺序排列：
	// 1) POST /api/v1/auth/login → FindByUsername
	mock.ExpectQuery(`SELECT .*FROM "sys_user".*`).
		WillReturnRows(sysUserRows().AddRow(
			int64(1001), nil, testNow, testNow, int64(9001), int64(9001),
			"alice", passwordHash, "Alice", nil, nil, nil, false, true, int64(1), 1,
		))
	mock.ExpectQuery(`SELECT .*FROM "sys_role".*`).
		WillReturnRows(sqlmock.NewRows([]string{"code"}).AddRow("admin"))
	// 2) GET /api/v1/auth/me → FindByID
	mock.ExpectQuery(`SELECT .*FROM "sys_user".*`).
		WillReturnRows(sysUserRows().AddRow(
			int64(1001), nil, testNow, testNow, int64(9001), int64(9001),
			"alice", passwordHash, "Alice", nil, nil, nil, false, true, int64(1), 1,
		))
	mock.ExpectQuery(`SELECT .*FROM "sys_role".*`).
		WillReturnRows(sqlmock.NewRows([]string{"code"}).AddRow("admin"))
	// 3) GET /api/v1/users/1001 → user.FindByID
	mock.ExpectQuery(`SELECT .*FROM "sys_user".*`).
		WillReturnRows(sysUserRows().AddRow(
			int64(1001), nil, testNow, testNow, int64(9001), int64(9001),
			"alice", passwordHash, "Alice", "13800000000", "alice@example.com",
			"https://example.com/avatar.png", true, true, int64(1), 1,
		))
	mock.ExpectQuery(`SELECT .*FROM "sys_role".*`).
		WillReturnRows(sqlmock.NewRows([]string{"code"}).AddRow("admin"))

	loginGuard, err := platformauth.NewMemoryLoginGuard(platformauth.LoginProtectionConfig{})
	require.NoError(t, err)
	authService, err := authmodule.NewService(entClient, passwords, jwtManager, loginGuard, noopAuthorizationInvalidator{}, logger)
	require.NoError(t, err)
	authmodule.NewModule(authmodule.NewHandler(authService)).Register(app.API, app.Registry)

	userService := usermodule.NewService(entClient, noopUserCache{}, passwords)
	usermodule.NewModule(usermodule.NewHandler(userService)).Register(app.API, app.Registry)
	openAPIJSON, err := json.Marshal(app.API.OpenAPI())
	require.NoError(t, err)
	openAPIText := string(openAPIJSON)
	require.Contains(t, openAPIText, `"type":"string"`)
	require.Contains(t, openAPIText, `"pattern":"^[1-9][0-9]{0,18}$"`)
	require.Contains(t, openAPIText, `"1771234567890123456"`)
	require.Contains(t, openAPIText, `"bearerAuth"`)
	require.Contains(t, openAPIText, `"401"`)
	require.Contains(t, openAPIText, `"403"`)

	localStorage, err := local.New(storage.Config{
		Enabled:  true,
		Provider: storage.ProviderLocal,
		Local: storage.LocalConfig{
			RootDir:           t.TempDir(),
			GenerateDatePath:  false,
			AllowedExtensions: []string{"txt"},
			MaxSize:           1024 * 1024,
		},
	})
	require.NoError(t, err)
	fileService := filemodule.NewService(localStorage)
	filemodule.NewModule(filemodule.NewHandler(fileService), 10*1024*1024).Register(app.API, app.Registry)

	loginResp := struct {
		Code string `json:"code"`
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}{}

	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"alice","password":"secret-123"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	app.Engine.ServeHTTP(loginRec, loginReq)
	require.Equal(t, http.StatusOK, loginRec.Code)
	require.NoError(t, json.Unmarshal(loginRec.Body.Bytes(), &loginResp))
	require.Equal(t, "OK", loginResp.Code)
	require.NotEmpty(t, loginResp.Data.AccessToken)

	meReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+loginResp.Data.AccessToken)
	meRec := httptest.NewRecorder()
	app.Engine.ServeHTTP(meRec, meReq)
	require.Equal(t, http.StatusOK, meRec.Code)
	meResp := struct {
		Code string `json:"code"`
		Data struct {
			UserID string `json:"userId"`
		} `json:"data"`
	}{}
	require.NoError(t, json.Unmarshal(meRec.Body.Bytes(), &meResp))
	require.Equal(t, "1001", meResp.Data.UserID)

	userReq := httptest.NewRequest(http.MethodGet, "/api/v1/users/1001", nil)
	userReq.Header.Set("Authorization", "Bearer "+loginResp.Data.AccessToken)
	userRec := httptest.NewRecorder()
	app.Engine.ServeHTTP(userRec, userReq)
	require.Equalf(t, http.StatusOK, userRec.Code, "body: %s", userRec.Body.String())
	userResp := struct {
		Code string `json:"code"`
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}{}
	require.NoError(t, json.Unmarshal(userRec.Body.Bytes(), &userResp))
	require.Equal(t, "1001", userResp.Data.ID)

	uploadBody := &bytes.Buffer{}
	uploadWriter := multipartWriter(t, uploadBody, "file", "hello.txt", "hello local file")
	uploadReq := httptest.NewRequest(http.MethodPost, "/api/v1/files/local", uploadBody)
	uploadReq.Header.Set("Authorization", "Bearer "+loginResp.Data.AccessToken)
	uploadReq.Header.Set("Content-Type", uploadWriter.FormDataContentType())
	uploadRec := httptest.NewRecorder()
	app.Engine.ServeHTTP(uploadRec, uploadReq)
	require.Equal(t, http.StatusCreated, uploadRec.Code)

	uploadResp := struct {
		Code string `json:"code"`
		Data struct {
			Key string `json:"key"`
		} `json:"data"`
	}{}
	require.NoError(t, json.Unmarshal(uploadRec.Body.Bytes(), &uploadResp))
	require.Equal(t, "OK", uploadResp.Code)
	require.NotEmpty(t, uploadResp.Data.Key)

	downloadReq := httptest.NewRequest(http.MethodGet, "/api/v1/files/local/download?key="+url.QueryEscape(uploadResp.Data.Key), nil)
	downloadReq.Header.Set("Authorization", "Bearer "+loginResp.Data.AccessToken)
	downloadRec := httptest.NewRecorder()
	app.Engine.ServeHTTP(downloadRec, downloadReq)
	require.Equalf(t, http.StatusOK, downloadRec.Code, "body: %s", downloadRec.Body.String())
	require.Equal(t, "hello local file", downloadRec.Body.String())

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/files/local?key="+url.QueryEscape(uploadResp.Data.Key), nil)
	deleteReq.Header.Set("Authorization", "Bearer "+loginResp.Data.AccessToken)
	deleteRec := httptest.NewRecorder()
	app.Engine.ServeHTTP(deleteRec, deleteReq)
	require.Equalf(t, http.StatusOK, deleteRec.Code, "body: %s", deleteRec.Body.String())

	healthReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	healthRec := httptest.NewRecorder()
	app.Engine.ServeHTTP(healthRec, healthReq)
	require.Equal(t, http.StatusOK, healthRec.Code)

	require.NoError(t, mock.ExpectationsWereMet())
}

// writeCasbinModel 为端到端测试写入临时 Casbin 模型。
func writeCasbinModel(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	modelPath := filepath.Join(dir, "rbac_model.conf")

	model := `
	[request_definition]
	r = sub, perm

	[policy_definition]
	p = sub, perm

	[policy_effect]
	e = some(where (p.eft == allow))

	[matchers]
	m = r.sub == p.sub && r.perm == p.perm
	`

	require.NoError(t, os.WriteFile(modelPath, []byte(strings.TrimSpace(model)), 0o600))
	return modelPath
}

func multipartWriter(t *testing.T, body *bytes.Buffer, fieldName string, fileName string, content string) *multipart.Writer {
	t.Helper()

	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile(fieldName, fileName)
	require.NoError(t, err)
	_, err = part.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return writer
}
