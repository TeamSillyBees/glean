package accesscontrol

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/data"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/logx"
	"github.com/teamsillybees/glean/internal/platform/redisx"
)

type sharedPolicyLoader struct {
	mu    sync.RWMutex
	rules []platformauth.PolicyRule
}

func (l *sharedPolicyLoader) LoadPolicyRules(context.Context) ([]platformauth.PolicyRule, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return append([]platformauth.PolicyRule(nil), l.rules...), nil
}

func (l *sharedPolicyLoader) set(rules ...platformauth.PolicyRule) {
	l.mu.Lock()
	l.rules = append([]platformauth.PolicyRule(nil), rules...)
	l.mu.Unlock()
}

// TestPolicyChangeReloadsOtherInstance 验证授权新增、撤销和 Redis 多实例通知形成完整闭环。
func TestPolicyChangeReloadsOtherInstance(t *testing.T) {
	server := miniredis.RunT(t)
	redisClient, err := redisx.NewClient(t.Context(), redisx.Config{Enabled: true, Addr: server.Addr()}, logx.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = redisClient.Close() })

	control1, db1 := newSyncTestControl(t, redisClient, "instance-1")
	control2, db2 := newSyncTestControl(t, redisClient, "instance-2")
	t.Cleanup(func() { _ = db1.Close() })
	t.Cleanup(func() { _ = db2.Close() })

	loader := &sharedPolicyLoader{}
	modelPath := writeSyncTestModel(t)
	enforcer1, err := platformauth.NewEnforcer(modelPath, loader)
	require.NoError(t, err)
	enforcer2, err := platformauth.NewEnforcer(modelPath, loader)
	require.NoError(t, err)
	control1.BindEnforcer(enforcer1)
	control2.BindEnforcer(enforcer2)
	require.NoError(t, control1.Start(t.Context()))
	require.NoError(t, control2.Start(t.Context()))
	t.Cleanup(func() { _ = control1.Close() })
	t.Cleanup(func() { _ = control2.Close() })

	identityKey := "initra:test:authz:user:1001"
	require.NoError(t, redisClient.Set(t.Context(), identityKey, `{"found":true}`, time.Minute).Err())
	loader.set(platformauth.PolicyRule{RoleCode: "auditor", PermissionCode: "system:audit:read"})
	require.NoError(t, control1.NotifyChanged(t.Context(), []idgen.ID{idgen.New(1001)}, true))
	requireEventuallyAuthorized(t, enforcer2, true)
	require.False(t, server.Exists(identityKey))

	loader.set()
	require.NoError(t, control1.NotifyChanged(t.Context(), nil, true))
	requireEventuallyAuthorized(t, enforcer2, false)
}

func newSyncTestControl(t *testing.T, redisClient redisx.UniversalClient, instanceID string) (*Control, *sql.DB) {
	t.Helper()
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	generator, err := idgen.NewGenerator(1)
	require.NoError(t, err)
	control, err := New(data.NewEntClientFromDB(db, generator), redisClient, logx.NewNop(), Options{
		AppName: "initra", Env: "test", InstanceID: instanceID, CacheTTL: time.Minute, RedisEnabled: true,
	})
	require.NoError(t, err)
	return control, db
}

func requireEventuallyAuthorized(t *testing.T, enforcer interface {
	Enforce(...any) (bool, error)
}, expected bool) {
	t.Helper()
	require.Eventually(t, func() bool {
		allowed, err := enforcer.Enforce("auditor", "system:audit:read")
		return err == nil && allowed == expected
	}, time.Second, 10*time.Millisecond)
}

func writeSyncTestModel(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rbac_model.conf")
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
	require.NoError(t, os.WriteFile(path, []byte(strings.TrimSpace(model)), 0o600))
	return path
}
