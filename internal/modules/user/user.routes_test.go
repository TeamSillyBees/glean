package user

import (
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/server"
)

type rejectingRoutePasswordManager struct{ calls int }

func (p *rejectingRoutePasswordManager) Hash(string) (string, error) {
	p.calls++
	return "", errors.New("test-only password hashing stopped before database")
}
func (*rejectingRoutePasswordManager) Compare(string, string) error { return nil }

// TestCreateUserRouteAcceptsSimpleForm 验证真实 Huma 路由允许简单表单省略非必填联系方式和角色字段。
func TestCreateUserRouteAcceptsSimpleForm(t *testing.T) {
	for _, extra := range []map[string]any{
		{},
		{"phone": "", "email": "", "avatarUrl": "", "roleCodes": []string{}, "sortId": 0},
	} {
		passwords := &rejectingRoutePasswordManager{}
		_, api := humatest.New(t)
		NewModule(NewHandler(NewService(nil, &fakeUserCache{}, passwords))).Register(api, server.NewRouteRegistry())
		body := map[string]any{"username": "route-reader", "password": "test-only-password", "nickname": "读者", "isSuperAdmin": false, "isEnable": true}
		for key, value := range extra {
			body[key] = value
		}
		res := api.Post("/api/v1/users", body)
		// Deliberately stop inside the real service after successful HTTP decoding.
		// Persistence and authentication are covered separately; no external DB is needed here.
		require.Equal(t, http.StatusInternalServerError, res.Code, res.Body.String())
		require.Equal(t, 1, passwords.calls, "simple form must reach application validation, not HTTP 422")
		for _, key := range []string{"username", "password"} {
			missing := make(map[string]any, len(body))
			for k, v := range body {
				if k != key {
					missing[k] = v
				}
			}
			res = api.Post("/api/v1/users", missing)
			require.Equal(t, http.StatusUnprocessableEntity, res.Code, res.Body.String())
			require.Equal(t, 1, passwords.calls, "required credentials must be rejected before hashing")
		}
	}
}
