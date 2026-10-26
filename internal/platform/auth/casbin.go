package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// AccessMode 描述 HTTP 路由的访问控制模式。
type AccessMode string

const (
	// AccessModePublic 表示公开路由，不需要登录即可访问。
	AccessModePublic AccessMode = "public"
	// AccessModeAuthenticated 表示认证路由，只需要登录态即可访问。
	AccessModeAuthenticated AccessMode = "authenticated"
	// AccessModePermission 表示权限路由，需要登录态并通过 RBAC 授权。
	AccessModePermission AccessMode = "permission"
)

// RouteSecurity 描述某个 HTTP 路由的认证与授权要求。
type RouteSecurity struct {
	AccessMode AccessMode
	Permission string
}

// RouteSecurityLookup 定义路由安全元数据查询能力，方便中间件与具体注册实现解耦。
type RouteSecurityLookup interface {
	Lookup(method string, path string) (RouteSecurity, bool)
}

// PolicyRule 是数据库权限关系映射到 Casbin 后的稳定策略结构。
type PolicyRule struct {
	RoleCode       string
	PermissionCode string
}

// PolicyLoader 从业务数据库加载全部有效角色权限关系。
type PolicyLoader interface {
	LoadPolicyRules(ctx context.Context) ([]PolicyRule, error)
}

// PolicyLoaderFunc 让函数可直接作为 PolicyLoader 使用。
type PolicyLoaderFunc func(context.Context) ([]PolicyRule, error)

// LoadPolicyRules 调用底层策略加载函数。
func (f PolicyLoaderFunc) LoadPolicyRules(ctx context.Context) ([]PolicyRule, error) {
	return f(ctx)
}

// IdentityResolver 在请求时从缓存或数据库解析用户当前有效身份。
type IdentityResolver interface {
	ResolvePrincipal(ctx context.Context, userID idgen.ID) (Principal, bool, error)
}

// IdentityResolverFunc 让函数可直接作为 IdentityResolver 使用。
type IdentityResolverFunc func(context.Context, idgen.ID) (Principal, bool, error)

// ResolvePrincipal 调用底层身份解析函数。
func (f IdentityResolverFunc) ResolvePrincipal(ctx context.Context, userID idgen.ID) (Principal, bool, error) {
	return f(ctx, userID)
}

// DatabasePolicyAdapter 把业务数据库中的角色权限关系只读映射为 Casbin 策略。
// 策略写入必须通过业务事务完成，禁止通过 Casbin API 双写数据库。
type DatabasePolicyAdapter struct {
	loader PolicyLoader
}

// NewDatabasePolicyAdapter 创建数据库策略 adapter。
func NewDatabasePolicyAdapter(loader PolicyLoader) (*DatabasePolicyAdapter, error) {
	if loader == nil {
		return nil, errors.New("casbin policy loader 不能为空")
	}
	return &DatabasePolicyAdapter{loader: loader}, nil
}

// LoadPolicy 加载 p(role_code, permission_code) 策略。
func (a *DatabasePolicyAdapter) LoadPolicy(m model.Model) error {
	rules, err := a.loader.LoadPolicyRules(context.Background())
	if err != nil {
		return fmt.Errorf("load casbin policy from database: %w", err)
	}
	for _, rule := range rules {
		roleCode := strings.TrimSpace(rule.RoleCode)
		permissionCode := strings.TrimSpace(rule.PermissionCode)
		if roleCode == "" || permissionCode == "" {
			return errors.New("casbin database policy contains empty role or permission code")
		}
		if strings.ContainsAny(roleCode, ",\r\n") || strings.ContainsAny(permissionCode, ",\r\n") {
			return errors.New("casbin database policy contains invalid delimiter")
		}
		persist.LoadPolicyLine(fmt.Sprintf("p, %s, %s", roleCode, permissionCode), m)
	}
	return nil
}

// SavePolicy 明确拒绝从 Casbin 反向写入业务数据库。
func (*DatabasePolicyAdapter) SavePolicy(model.Model) error {
	return errors.New("casbin policy is read-only; update role permissions through the RBAC service")
}

// AddPolicy 明确拒绝从 Casbin 反向写入业务数据库。
func (*DatabasePolicyAdapter) AddPolicy(string, string, []string) error {
	return errors.New("casbin policy is read-only; update role permissions through the RBAC service")
}

// RemovePolicy 明确拒绝从 Casbin 反向写入业务数据库。
func (*DatabasePolicyAdapter) RemovePolicy(string, string, []string) error {
	return errors.New("casbin policy is read-only; update role permissions through the RBAC service")
}

// RemoveFilteredPolicy 明确拒绝从 Casbin 反向写入业务数据库。
func (*DatabasePolicyAdapter) RemoveFilteredPolicy(string, string, int, ...string) error {
	return errors.New("casbin policy is read-only; update role permissions through the RBAC service")
}

// NewEnforcer 使用文件模型和数据库策略加载器创建并发安全的 Casbin Enforcer。
func NewEnforcer(modelPath string, loader PolicyLoader) (*casbin.SyncedEnforcer, error) {
	m, err := model.NewModelFromFile(modelPath)
	if err != nil {
		return nil, err
	}
	adapter, err := NewDatabasePolicyAdapter(loader)
	if err != nil {
		return nil, err
	}
	return casbin.NewSyncedEnforcer(m, adapter)
}
