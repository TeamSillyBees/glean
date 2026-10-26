package observability_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/logx"
	"github.com/teamsillybees/glean/internal/platform/observability"
	"github.com/teamsillybees/glean/internal/platform/server"
)

// TestModuleRegistersHealthReadyAndVersionEndpoints 验证观测模块注册健康、就绪和版本接口。
func TestModuleRegistersHealthReadyAndVersionEndpoints(t *testing.T) {
	app := newObservabilityApp(t, observability.NewReadinessRegistry())

	for _, path := range []string{"/health", "/ready", "/version"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		app.Engine.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, path)
	}
}

// TestReadyReturnsServiceUnavailable 验证必要依赖失败时拒绝流量，但存活探针保持成功。
func TestReadyReturnsServiceUnavailable(t *testing.T) {
	readiness := observability.NewReadinessRegistry()
	require.NoError(t, readiness.Register("database", time.Second, observability.ReadinessCheckFunc(func(context.Context) error {
		return errors.New("database unavailable")
	})))
	app := newObservabilityApp(t, readiness)

	ready := httptest.NewRecorder()
	app.Engine.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/ready", nil))
	require.Equal(t, http.StatusServiceUnavailable, ready.Code)

	health := httptest.NewRecorder()
	app.Engine.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	require.Equal(t, http.StatusOK, health.Code)
}

// TestReadyAppliesCheckerTimeout 验证单个检查器超时不会无限阻塞就绪探针。
func TestReadyAppliesCheckerTimeout(t *testing.T) {
	readiness := observability.NewReadinessRegistry()
	require.NoError(t, readiness.Register("slow", 20*time.Millisecond, observability.ReadinessCheckFunc(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})))
	app := newObservabilityApp(t, readiness)

	startedAt := time.Now()
	recorder := httptest.NewRecorder()
	app.Engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ready", nil))

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Less(t, time.Since(startedAt), time.Second)
}

func newObservabilityApp(t *testing.T, readiness *observability.ReadinessRegistry) *server.App {
	t.Helper()
	logger := logx.NewNop()
	jwtManager, err := auth.NewJWTManager(auth.JWTConfig{
		Issuer:          "initra",
		Secret:          "observability-test-secret-32-bytes",
		AccessTokenTTL:  3600,
		RefreshTokenTTL: 7200,
	})
	require.NoError(t, err)

	enforcer, err := casbin.NewSyncedEnforcer()
	require.NoError(t, err)

	app, err := server.NewApp(server.Options{
		Title:   "initra",
		Version: "test",
		Env:     "test",
	}, logger, jwtManager, nil, enforcer)
	require.NoError(t, err)

	module := observability.NewModule(observability.BuildInfoVO{
		Version:   "test",
		Commit:    "abc123",
		BuildTime: "2026-04-21T00:00:00Z",
	}, readiness)
	module.Register(app.API, app.Registry)
	return app
}
