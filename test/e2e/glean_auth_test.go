package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	authmodule "github.com/teamsillybees/glean/internal/modules/auth"
	"github.com/teamsillybees/glean/internal/modules/catalog"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/logx"
	"github.com/teamsillybees/glean/internal/platform/server"
)

// TestGleanFrontendAuthContract 验证浏览器实际使用的 JSON、路径、刷新轮转及目录访问链路。
func TestGleanFrontendAuthContract(t *testing.T) {
	db, mock, client := newMockEntClient(t)
	defer db.Close()
	passwords := platformauth.NewBcryptPasswordManager(4)
	hash, err := passwords.Hash("catalog-secret")
	require.NoError(t, err)
	tokens, err := platformauth.NewJWTManager(platformauth.JWTConfig{Store: platformauth.NewMemoryTokenStore(), Issuer: "glean-test", Secret: "glean-auth-contract-test-secret-32-bytes", AccessTokenTTL: time.Hour, RefreshTokenTTL: 24 * time.Hour})
	require.NoError(t, err)
	app, err := server.NewApp(server.Options{Title: "Glean", Version: "test", Env: "test"}, logx.NewNop(), tokens, platformauth.IdentityResolverFunc(func(_ context.Context, id idgen.ID) (platformauth.Principal, bool, error) {
		return platformauth.Principal{UserID: id, SessionVersion: 1, Roles: []string{"admin"}}, true, nil
	}), nil)
	require.NoError(t, err)
	guard, err := platformauth.NewMemoryLoginGuard(platformauth.LoginProtectionConfig{})
	require.NoError(t, err)
	service, err := authmodule.NewService(client, passwords, tokens, guard, noopAuthorizationInvalidator{}, logx.NewNop())
	require.NoError(t, err)
	authmodule.NewModule(authmodule.NewHandler(service)).Register(app.API, app.Registry)
	catalog.NewModule(catalog.NewHandler(catalog.NewService(client))).Register(app.API, app.Registry)
	request := func(method, path, body, token string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Accept", "application/json")
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		app.Engine.ServeHTTP(w, r)
		return w
	}
	require.Equal(t, http.StatusUnauthorized, request(http.MethodGet, "/api/v1/catalog/topics", "", "").Code)
	identity := func() {
		mock.ExpectQuery(`SELECT .*FROM "sys_user".*`).WillReturnRows(sysUserRows().AddRow(int64(1001), nil, testNow, testNow, nil, nil, "admin", hash, "Administrator", nil, nil, nil, true, true, int64(1), 0))
		mock.ExpectQuery(`SELECT .*FROM "sys_role".*`).WillReturnRows(sqlmock.NewRows([]string{"code"}).AddRow("admin"))
	}
	identity()
	login := request(http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"catalog-secret"}`, "")
	require.Equal(t, http.StatusOK, login.Code, login.Body.String())
	var session struct {
		Code    string             `json:"code"`
		Message string             `json:"message"`
		Data    authmodule.LoginVO `json:"data"`
	}
	require.NoError(t, json.Unmarshal(login.Body.Bytes(), &session))
	require.Equal(t, "OK", session.Code)
	require.Equal(t, "success", session.Message)
	require.Equal(t, idgen.ID(1001), session.Data.User.UserID)
	require.NotEmpty(t, session.Data.AccessToken)
	require.NotEmpty(t, session.Data.RefreshToken)
	identity()
	payload, err := json.Marshal(map[string]string{"refreshToken": session.Data.RefreshToken})
	require.NoError(t, err)
	// 刷新路由公开，浏览器无需发送已过期的 access token。
	refreshed := request(http.MethodPost, "/api/v1/auth/refresh", string(payload), "")
	require.Equal(t, http.StatusOK, refreshed.Code, refreshed.Body.String())
	var rotated struct {
		Code string               `json:"code"`
		Data authmodule.RefreshVO `json:"data"`
	}
	require.NoError(t, json.Unmarshal(refreshed.Body.Bytes(), &rotated))
	require.Equal(t, "OK", rotated.Code)
	require.NotEmpty(t, rotated.Data.AccessToken)
	require.NotEqual(t, session.Data.RefreshToken, rotated.Data.RefreshToken)
	require.Equal(t, http.StatusUnauthorized, request(http.MethodGet, "/api/v1/catalog/topics", "", session.Data.AccessToken).Code)
	// 同一个 refresh token 必须只可轮转一次，不得让重放建立新会话。
	require.Equal(t, http.StatusUnauthorized, request(http.MethodPost, "/api/v1/auth/refresh", string(payload), "").Code)
	for _, topic := range []string{"biomed-ai", "llm-agent", "biomed-worldmodel"} {
		mock.ExpectQuery(`SELECT COUNT\("glean_membership"\."id"\).*`).WithArgs(topic).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	}
	listed := request(http.MethodGet, "/api/v1/catalog/topics", "", rotated.Data.AccessToken)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	var topics struct {
		Code string `json:"code"`
		Data struct {
			Items []catalog.TopicVO `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(listed.Body.Bytes(), &topics))
	require.Equal(t, "OK", topics.Code)
	require.Len(t, topics.Data.Items, 3)
	logoutBody, err := json.Marshal(map[string]string{"refreshToken": rotated.Data.RefreshToken})
	require.NoError(t, err)
	logout := request(http.MethodPost, "/api/v1/auth/logout", string(logoutBody), rotated.Data.AccessToken)
	require.Equal(t, http.StatusOK, logout.Code, logout.Body.String())
	require.Equal(t, http.StatusUnauthorized, request(http.MethodGet, "/api/v1/catalog/topics", "", rotated.Data.AccessToken).Code)
	require.Equal(t, http.StatusUnauthorized, request(http.MethodPost, "/api/v1/auth/refresh", string(logoutBody), "").Code)
	require.NoError(t, mock.ExpectationsWereMet())
}
