package auth

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type mutablePolicyLoader struct {
	mu    sync.RWMutex
	rules []PolicyRule
}

func (l *mutablePolicyLoader) LoadPolicyRules(context.Context) ([]PolicyRule, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return append([]PolicyRule(nil), l.rules...), nil
}

func (l *mutablePolicyLoader) set(rules ...PolicyRule) {
	l.mu.Lock()
	l.rules = append([]PolicyRule(nil), rules...)
	l.mu.Unlock()
}

// TestDatabasePolicyReloadAppliesGrantAndRevoke 验证数据库关系新增和撤销会在 LoadPolicy 后立即生效。
func TestDatabasePolicyReloadAppliesGrantAndRevoke(t *testing.T) {
	loader := &mutablePolicyLoader{}
	enforcer, err := NewEnforcer(writePermissionModel(t), loader)
	require.NoError(t, err)

	allowed, err := enforcer.Enforce("auditor", "system:audit:read")
	require.NoError(t, err)
	require.False(t, allowed)

	loader.set(PolicyRule{RoleCode: "auditor", PermissionCode: "system:audit:read"})
	require.NoError(t, enforcer.LoadPolicy())
	allowed, err = enforcer.Enforce("auditor", "system:audit:read")
	require.NoError(t, err)
	require.True(t, allowed)

	loader.set()
	require.NoError(t, enforcer.LoadPolicy())
	allowed, err = enforcer.Enforce("auditor", "system:audit:read")
	require.NoError(t, err)
	require.False(t, allowed)
}

func writePermissionModel(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rbac_model.conf")
	content := `
[request_definition]
r = sub, perm

[policy_definition]
p = sub, perm

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = r.sub == p.sub && r.perm == p.perm
`
	require.NoError(t, os.WriteFile(path, []byte(strings.TrimSpace(content)), 0o600))
	return path
}
