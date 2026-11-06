package monitor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/stretchr/testify/require"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/response"
	"github.com/teamsillybees/glean/internal/platform/server"
)

// connectionSourceFake 只支持连通性检查，误入论文采集立即失败。
type connectionSourceFake struct {
	name  string
	probe func(context.Context) (int, error)
}

func (s connectionSourceFake) Name() string { return s.name }
func (s connectionSourceFake) Fetch(context.Context, FetchRequest) (Page, error) {
	panic("connection test must not collect papers")
}
func (s connectionSourceFake) checkConnection(ctx context.Context) (int, error) { return s.probe(ctx) }

func connectionAdminContext(ctx context.Context) context.Context {
	return platformauth.WithPrincipal(ctx, platformauth.Principal{UserID: 7, IsSuperAdmin: true})
}

// TestSourceConnectionsPreservePartialResultsAndState 验证并发有界探测、混合结果和采集状态隔离。
func TestSourceConnectionsPreservePartialResultsAndState(t *testing.T) {
	started, release := make(chan struct{}, 2), make(chan struct{})
	probe := func(ctx context.Context) (int, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > sourceConnectionTimeout {
			panic("connection probe must have a bounded deadline")
		}
		started <- struct{}{}
		<-release
		return 200, nil
	}
	service := &Service{connectionTestMu: &sync.Mutex{}, config: Config{RealFetchEnabled: true}, memory: &memoryState{data: []byte("unchanged-snapshot")}, sources: []Source{
		connectionSourceFake{"pubmed", probe},
		connectionSourceFake{"arxiv", func(ctx context.Context) (int, error) { _, _ = probe(ctx); return 0, context.DeadlineExceeded }},
	}}
	type outcome struct {
		result SourceConnectionsVO
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := service.TestSourceConnections(connectionAdminContext(t.Context()))
		done <- outcome{result, err}
	}()
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("slow source must not prevent other source from starting")
		}
	}
	_, err := service.TestSourceConnections(connectionAdminContext(t.Context()))
	require.ErrorContains(t, err, "正在测试")
	close(release)
	got := <-done
	require.NoError(t, got.err)
	require.Len(t, got.result.Items, 2)
	require.Equal(t, "arxiv", got.result.Items[0].Source)
	require.Equal(t, "timeout", got.result.Items[0].Status)
	require.Equal(t, "connected", got.result.Items[1].Status)
	require.False(t, got.result.Items[0].CheckedAt.IsZero())
	require.Equal(t, []byte("unchanged-snapshot"), service.memory.data, "检测不应读写采集快照、游标或论文队列")
	require.Zero(t, service.memory.revision)
}

func TestSourceConnectionsRequireAdminAndDoNotProbeAfterCancellation(t *testing.T) {
	var calls atomic.Int32
	service := &Service{connectionTestMu: &sync.Mutex{}, config: Config{RealFetchEnabled: true}, sources: []Source{connectionSourceFake{"pubmed", func(context.Context) (int, error) { calls.Add(1); return 200, nil }}}}
	for _, ctx := range []context.Context{t.Context(), platformauth.WithPrincipal(t.Context(), platformauth.Principal{UserID: 8})} {
		_, err := service.TestSourceConnections(ctx)
		require.Error(t, err)
	}
	ctx, cancel := context.WithCancel(connectionAdminContext(t.Context()))
	cancel()
	_, err := service.TestSourceConnections(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, calls.Load(), "未授权或已取消的调用不得访问外部来源")
	service.config.RealFetchEnabled = false
	result, err := service.TestSourceConnections(connectionAdminContext(t.Context()))
	require.NoError(t, err)
	require.Equal(t, "skipped", result.Items[0].Status)
	require.Zero(t, calls.Load(), "禁用真实来源时不得伪造测试成功或偷偷联网")
}

// TestSourceConnectionHTTPAuthorization 验证真实路由的认证、权限和部分失败仍返回正常结果的契约。
func TestSourceConnectionHTTPAuthorization(t *testing.T) {
	var calls atomic.Int32
	service := &Service{connectionTestMu: &sync.Mutex{}, config: Config{RealFetchEnabled: true}, sources: []Source{connectionSourceFake{"pubmed", func(context.Context) (int, error) { calls.Add(1); return 0, context.DeadlineExceeded }}}}
	manager, err := platformauth.NewJWTManager(platformauth.JWTConfig{Issuer: "test", Secret: "source-connection-test-secret-32-bytes", AccessTokenTTL: time.Minute, RefreshTokenTTL: time.Hour})
	require.NoError(t, err)
	policy, err := model.NewModelFromString("[request_definition]\nr = sub, perm\n[policy_definition]\np = sub, perm\n[policy_effect]\ne = some(where (p.eft == allow))\n[matchers]\nm = r.sub == p.sub && r.perm == p.perm")
	require.NoError(t, err)
	enforcer, err := casbin.NewSyncedEnforcer(policy)
	require.NoError(t, err)
	resolver := platformauth.IdentityResolverFunc(func(_ context.Context, uid idgen.ID) (platformauth.Principal, bool, error) {
		return platformauth.Principal{UserID: uid, SessionVersion: 1, IsSuperAdmin: uid == 7, Roles: []string{"reader"}}, uid != 9, nil
	})
	app, err := server.NewApp(server.Options{Title: "test", Version: "test", Env: "test"}, nil, manager, resolver, enforcer)
	require.NoError(t, err)
	NewModule(NewHandler(service)).Register(app.API, app.Registry)
	for _, tt := range []struct {
		uid    idgen.ID
		status int
	}{{0, 401}, {8, 403}, {9, 401}, {7, 200}} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/monitor/sources/test", nil)
		if tt.uid > 0 {
			pair, err := manager.IssueTokenPair(t.Context(), platformauth.Principal{UserID: tt.uid, SessionVersion: 1})
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
		}
		rec := httptest.NewRecorder()
		app.Engine.ServeHTTP(rec, req)
		require.Equal(t, tt.status, rec.Code, rec.Body.String())
		if tt.status == 200 {
			var body response.SuccessVO[SourceConnectionsVO]
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			require.Equal(t, "timeout", body.Data.Items[0].Status)
			require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		}
	}
	require.Equal(t, int32(1), calls.Load())
}
