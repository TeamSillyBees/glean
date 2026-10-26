// Package accesscontrol 实现标准项目的数据库权限策略、请求身份缓存和多实例同步。
package accesscontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/samber/do"
	appent "github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/sysmenu"
	"github.com/teamsillybees/glean/internal/data/ent/sysrole"
	"github.com/teamsillybees/glean/internal/data/ent/sysrolemenu"
	"github.com/teamsillybees/glean/internal/data/ent/sysuser"
	"github.com/teamsillybees/glean/internal/data/ent/sysuserrole"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/logx"
	"github.com/teamsillybees/glean/internal/platform/redisx"
)

const identityKeyPrefixName = "authz-identity"

// Options 描述权限访问层的运行时配置。
type Options struct {
	AppName      string
	Env          string
	InstanceID   string
	CacheTTL     time.Duration
	RedisEnabled bool
}

// Invalidator 是用户和 RBAC 模块提交事务后使用的缓存/策略失效契约。
type Invalidator interface {
	NotifyChanged(ctx context.Context, userIDs []idgen.ID, reloadPolicy bool) error
}

type changeEvent struct {
	Source       string   `json:"source"`
	UserIDs      []string `json:"userIds,omitempty"`
	ReloadPolicy bool     `json:"reloadPolicy"`
}

type cachedIdentity struct {
	Found     bool                   `json:"found"`
	Principal platformauth.Principal `json:"principal"`
}

// Control 是 Ent、Redis 与 Casbin 之间唯一的权限访问适配器。
type Control struct {
	client     *appent.Client
	redis      redisx.UniversalClient
	logger     *logx.Logger
	keys       *redisx.KeyBuilder
	instanceID string
	cacheTTL   time.Duration
	channel    string

	mu       sync.RWMutex
	enforcer *casbin.SyncedEnforcer
	pubsub   *redisx.PubSub
}

// New 创建权限访问适配器。
func New(client *appent.Client, redisClient redisx.UniversalClient, logger *logx.Logger, opts Options) (*Control, error) {
	if client == nil {
		return nil, fmt.Errorf("access control Ent client 不能为空")
	}
	if opts.CacheTTL <= 0 {
		return nil, fmt.Errorf("access control cache ttl 必须大于 0")
	}
	keys := redisx.NewKeyBuilder(redisx.KeyConfig{App: opts.AppName, Env: opts.Env})
	if err := keys.RegisterPrefix(identityKeyPrefixName, "authz", "user"); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = logx.NewNop()
	}
	return &Control{
		client:     client,
		redis:      redisClient,
		logger:     logger,
		keys:       keys,
		instanceID: strings.TrimSpace(opts.InstanceID),
		cacheTTL:   opts.CacheTTL,
		channel:    fmt.Sprintf("%s:%s:authz:changed", strings.TrimSpace(opts.AppName), strings.TrimSpace(opts.Env)),
	}, nil
}

// BindEnforcer 绑定当前实例的并发安全 Enforcer，供事务提交后立即重载。
func (c *Control) BindEnforcer(enforcer *casbin.SyncedEnforcer) {
	c.mu.Lock()
	c.enforcer = enforcer
	c.mu.Unlock()
}

// LoadPolicyRules 从 sys_role、sys_menu、sys_role_menu 加载全部有效策略。
func (c *Control) LoadPolicyRules(ctx context.Context) ([]platformauth.PolicyRule, error) {
	roles, err := c.client.SysRole.Query().
		Where(sysrole.DeletedAtIsNil(), sysrole.IsEnable(true)).
		WithRoleMenus(func(query *appent.SysRoleMenuQuery) {
			query.Where(sysrolemenu.DeletedAtIsNil()).
				WithMenu(func(menuQuery *appent.SysMenuQuery) {
					menuQuery.Where(sysmenu.DeletedAtIsNil(), sysmenu.PermissionCodeNotNil())
				})
		}).
		Order(appent.Asc(sysrole.FieldCode)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query role permission policies: %w", err)
	}

	seen := make(map[string]struct{})
	rules := make([]platformauth.PolicyRule, 0)
	for _, role := range roles {
		for _, relation := range role.Edges.RoleMenus {
			menu := relation.Edges.Menu
			if menu == nil || menu.PermissionCode == nil {
				continue
			}
			permissionCode := strings.TrimSpace(*menu.PermissionCode)
			if permissionCode == "" {
				continue
			}
			key := role.Code + "\x00" + permissionCode
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			rules = append(rules, platformauth.PolicyRule{RoleCode: role.Code, PermissionCode: permissionCode})
		}
	}
	return rules, nil
}

// ResolvePrincipal 从 Redis 缓存读取身份，未命中时回源数据库。
func (c *Control) ResolvePrincipal(ctx context.Context, userID idgen.ID) (platformauth.Principal, bool, error) {
	if userID <= 0 {
		return platformauth.Principal{}, false, nil
	}
	key := c.keys.MustBuild(identityKeyPrefixName, userID)
	if c.redis != nil {
		payload, err := c.redis.Get(ctx, key).Bytes()
		switch {
		case err == nil:
			var cached cachedIdentity
			if err := json.Unmarshal(payload, &cached); err != nil {
				_ = c.redis.Unlink(ctx, key).Err()
				return platformauth.Principal{}, false, fmt.Errorf("decode authorization identity cache: %w", err)
			}
			return cached.Principal, cached.Found, nil
		case err != redisx.ErrNil:
			return platformauth.Principal{}, false, fmt.Errorf("read authorization identity cache: %w", err)
		}
	}

	principal, found, err := c.loadPrincipal(ctx, userID)
	if err != nil {
		return platformauth.Principal{}, false, err
	}
	if c.redis != nil {
		payload, err := json.Marshal(cachedIdentity{Found: found, Principal: principal})
		if err != nil {
			return platformauth.Principal{}, false, err
		}
		if err := c.redis.Set(ctx, key, payload, c.cacheTTL).Err(); err != nil {
			return platformauth.Principal{}, false, fmt.Errorf("write authorization identity cache: %w", err)
		}
	}
	return principal, found, nil
}

func (c *Control) loadPrincipal(ctx context.Context, userID idgen.ID) (platformauth.Principal, bool, error) {
	user, err := c.client.SysUser.Query().
		Where(sysuser.ID(userID), sysuser.DeletedAtIsNil(), sysuser.IsEnable(true)).
		Only(ctx)
	if appent.IsNotFound(err) {
		return platformauth.Principal{}, false, nil
	}
	if err != nil {
		return platformauth.Principal{}, false, fmt.Errorf("query authorization identity: %w", err)
	}

	var rows []struct {
		Code string `json:"code"`
	}
	err = c.client.SysRole.Query().
		Where(
			sysrole.DeletedAtIsNil(),
			sysrole.IsEnable(true),
			sysrole.HasUserRolesWith(sysuserrole.UserID(userID), sysuserrole.DeletedAtIsNil()),
		).
		Order(appent.Asc(sysrole.FieldSortID), appent.Asc(sysrole.FieldID)).
		Select(sysrole.FieldCode).
		Scan(ctx, &rows)
	if err != nil {
		return platformauth.Principal{}, false, fmt.Errorf("query authorization roles: %w", err)
	}
	roles := make([]string, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, row.Code)
	}
	return platformauth.Principal{
		UserID:         user.ID,
		SessionVersion: user.SessionVersion,
		Roles:          roles,
		IsSuperAdmin:   user.IsSuperAdmin,
	}, true, nil
}

// NotifyChanged 在事务提交后失效用户身份，并按需重载、广播数据库策略变更。
func (c *Control) NotifyChanged(ctx context.Context, userIDs []idgen.ID, reloadPolicy bool) error {
	uniqueIDs := uniqueUserIDs(userIDs)
	if c.redis != nil && len(uniqueIDs) > 0 {
		keys := make([]string, 0, len(uniqueIDs))
		for _, userID := range uniqueIDs {
			keys = append(keys, c.keys.MustBuild(identityKeyPrefixName, userID))
		}
		if err := c.redis.Unlink(ctx, keys...).Err(); err != nil {
			return fmt.Errorf("invalidate authorization identity cache: %w", err)
		}
	}
	if reloadPolicy {
		if err := c.reloadPolicy(); err != nil {
			return err
		}
	}
	if c.redis == nil {
		return nil
	}
	event := changeEvent{Source: c.instanceID, ReloadPolicy: reloadPolicy}
	for _, userID := range uniqueIDs {
		event.UserIDs = append(event.UserIDs, userID.String())
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if err := c.redis.Publish(ctx, c.channel, payload).Err(); err != nil {
		return fmt.Errorf("publish authorization change: %w", err)
	}
	return nil
}

// Start 订阅其他实例发布的权限策略变更。
func (c *Control) Start(ctx context.Context) error {
	if c.redis == nil {
		return nil
	}
	pubsub := c.redis.Subscribe(ctx, c.channel)
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return fmt.Errorf("subscribe authorization changes: %w", err)
	}
	c.mu.Lock()
	c.pubsub = pubsub
	c.mu.Unlock()
	go c.consumeChanges(ctx, pubsub)
	return nil
}

func (c *Control) consumeChanges(ctx context.Context, pubsub *redisx.PubSub) {
	channel := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case message, ok := <-channel:
			if !ok {
				return
			}
			var event changeEvent
			if err := json.Unmarshal([]byte(message.Payload), &event); err != nil {
				c.logger.Error(ctx, "decode authorization change failed", err)
				continue
			}
			if event.Source != "" && event.Source == c.instanceID {
				continue
			}
			if event.ReloadPolicy {
				if err := c.reloadPolicy(); err != nil {
					c.logger.Error(ctx, "reload authorization policy failed", err)
				}
			}
		}
	}
}

func (c *Control) reloadPolicy() error {
	c.mu.RLock()
	enforcer := c.enforcer
	c.mu.RUnlock()
	if enforcer == nil {
		return fmt.Errorf("casbin enforcer 尚未绑定")
	}
	if err := enforcer.LoadPolicy(); err != nil {
		return fmt.Errorf("reload casbin database policy: %w", err)
	}
	return nil
}

// Close 停止 Redis 权限变更订阅。
func (c *Control) Close() error {
	c.mu.Lock()
	pubsub := c.pubsub
	c.pubsub = nil
	c.mu.Unlock()
	if pubsub == nil {
		return nil
	}
	return pubsub.Close()
}

func uniqueUserIDs(values []idgen.ID) []idgen.ID {
	seen := make(map[idgen.ID]struct{}, len(values))
	result := make([]idgen.ID, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

// Provide 注册数据库权限访问层及其最小接口。
func Provide(injector *do.Injector, opts Options) {
	do.Provide(injector, func(i *do.Injector) (*Control, error) {
		client := do.MustInvoke[*appent.Client](i)
		logger := do.MustInvoke[*logx.Logger](i)
		var redisClient redisx.UniversalClient
		if opts.RedisEnabled {
			redisClient = do.MustInvoke[redisx.UniversalClient](i)
		}
		return New(client, redisClient, logger, opts)
	})
	do.Provide(injector, func(i *do.Injector) (platformauth.PolicyLoader, error) {
		return do.MustInvoke[*Control](i), nil
	})
	do.Provide(injector, func(i *do.Injector) (platformauth.IdentityResolver, error) {
		return do.MustInvoke[*Control](i), nil
	})
	do.Provide(injector, func(i *do.Injector) (Invalidator, error) {
		return do.MustInvoke[*Control](i), nil
	})
}
