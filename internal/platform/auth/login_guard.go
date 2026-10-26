package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/teamsillybees/glean/internal/platform/redisx"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
)

const (
	loginAccountRatePrefixName = "auth_login_account_rate"
	loginIPRatePrefixName      = "auth_login_ip_rate"
	loginFailurePrefixName     = "auth_login_failure"
	loginLockPrefixName        = "auth_login_lock"
	checkLoginScriptName       = "auth_check_login_attempt"
	recordFailureScriptName    = "auth_record_login_failure"
)

// LoginProtectionConfig 描述登录尝试限流与连续失败锁定策略。
type LoginProtectionConfig struct {
	Enabled          bool            `mapstructure:"enabled"`
	AccountRateLimit LoginRateConfig `mapstructure:"account_rate_limit"`
	IPRateLimit      LoginRateConfig `mapstructure:"ip_rate_limit"`
	Lockout          LoginLockConfig `mapstructure:"lockout"`
}

// LoginRateConfig 描述固定时间窗口内允许的最大登录尝试次数。
type LoginRateConfig struct {
	MaxAttempts int64         `mapstructure:"max_attempts"`
	Window      time.Duration `mapstructure:"window"`
}

// LoginLockConfig 描述账号连续失败计数与临时锁定策略。
type LoginLockConfig struct {
	MaxFailures   int64         `mapstructure:"max_failures"`
	FailureWindow time.Duration `mapstructure:"failure_window"`
	LockDuration  time.Duration `mapstructure:"lock_duration"`
}

// Validate 校验启用后的登录防护参数。
func (c LoginProtectionConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	switch {
	case c.AccountRateLimit.MaxAttempts <= 0:
		return errors.New("auth.login_protection.account_rate_limit.max_attempts 必须大于 0")
	case c.AccountRateLimit.Window <= 0:
		return errors.New("auth.login_protection.account_rate_limit.window 必须大于 0")
	case c.IPRateLimit.MaxAttempts <= 0:
		return errors.New("auth.login_protection.ip_rate_limit.max_attempts 必须大于 0")
	case c.IPRateLimit.Window <= 0:
		return errors.New("auth.login_protection.ip_rate_limit.window 必须大于 0")
	case c.Lockout.MaxFailures <= 0:
		return errors.New("auth.login_protection.lockout.max_failures 必须大于 0")
	case c.Lockout.FailureWindow <= 0:
		return errors.New("auth.login_protection.lockout.failure_window 必须大于 0")
	case c.Lockout.LockDuration <= 0:
		return errors.New("auth.login_protection.lockout.lock_duration 必须大于 0")
	default:
		return nil
	}
}

// LoginGuard 是登录用例依赖的最小防暴力破解契约。
type LoginGuard interface {
	Check(ctx context.Context, account string, sourceIP string) error
	RecordFailure(ctx context.Context, account string) error
	Reset(ctx context.Context, account string) error
}

var (
	// ErrLoginRateLimited 表示账号或来源 IP 已超过登录尝试速率窗口。
	ErrLoginRateLimited = errors.New("login rate limited")
	// ErrLoginLocked 表示账号因连续登录失败处于临时锁定状态。
	ErrLoginLocked = errors.New("login locked")
	// ErrLoginProtectionStoreFailure 表示登录防护共享状态不可安全读写。
	ErrLoginProtectionStoreFailure = errors.New("login protection store failure")
)

type noopLoginGuard struct{}

// Check 在登录防护关闭时直接放行。
func (noopLoginGuard) Check(context.Context, string, string) error { return nil }

// RecordFailure 在登录防护关闭时不记录失败。
func (noopLoginGuard) RecordFailure(context.Context, string) error { return nil }

// Reset 在登录防护关闭时无需清理状态。
func (noopLoginGuard) Reset(context.Context, string) error { return nil }

// RedisLoginGuard 使用 Redis Lua 脚本在多实例间原子维护登录限流与锁定状态。
type RedisLoginGuard struct {
	client  redisx.CommandClient
	keys    *redisx.KeyBuilder
	scripts *redisx.ScriptRegistry
	config  LoginProtectionConfig
}

// NewRedisLoginGuard 创建 Redis 登录防护器。
func NewRedisLoginGuard(appName string, env string, client redisx.CommandClient, config LoginProtectionConfig) (LoginGuard, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if !config.Enabled {
		return noopLoginGuard{}, nil
	}
	if isNilRedisCommandClient(client) {
		return nil, errors.New("login guard Redis client 不能为空")
	}
	keys := redisx.NewKeyBuilder(redisx.KeyConfig{App: appName, Env: env})
	for _, prefix := range []struct {
		name string
		biz  string
	}{
		{name: loginAccountRatePrefixName, biz: "account-rate"},
		{name: loginIPRatePrefixName, biz: "ip-rate"},
		{name: loginFailurePrefixName, biz: "failure"},
		{name: loginLockPrefixName, biz: "lock"},
	} {
		if err := keys.RegisterPrefix(prefix.name, "auth", prefix.biz); err != nil {
			return nil, err
		}
	}
	return &RedisLoginGuard{
		client:  client,
		keys:    keys,
		scripts: newLoginGuardScriptRegistry(),
		config:  config,
	}, nil
}

// Check 原子检查锁定状态，并分别累计账号和来源 IP 的登录尝试次数。
func (g *RedisLoginGuard) Check(ctx context.Context, account string, sourceIP string) error {
	accountID := loginKeyFingerprint(normalizeLoginAccount(account))
	ipID := loginKeyFingerprint(normalizeSourceIP(sourceIP))
	result, err := g.scripts.Run(
		ctx,
		g.client,
		checkLoginScriptName,
		[]string{
			g.keys.MustBuild(loginAccountRatePrefixName, accountID),
			g.keys.MustBuild(loginIPRatePrefixName, ipID),
			g.keys.MustBuild(loginLockPrefixName, accountID),
		},
		g.config.AccountRateLimit.MaxAttempts,
		durationMilliseconds(g.config.AccountRateLimit.Window),
		g.config.IPRateLimit.MaxAttempts,
		durationMilliseconds(g.config.IPRateLimit.Window),
	).Int64()
	if err != nil {
		return fmt.Errorf("%w: check login attempt: %v", ErrLoginProtectionStoreFailure, err)
	}
	switch result {
	case 1:
		return ErrLoginLocked
	case 2:
		return ErrLoginRateLimited
	default:
		return nil
	}
}

// RecordFailure 累计账号连续失败次数，达到阈值后原子切换为临时锁定状态。
func (g *RedisLoginGuard) RecordFailure(ctx context.Context, account string) error {
	accountID := loginKeyFingerprint(normalizeLoginAccount(account))
	locked, err := g.scripts.Run(
		ctx,
		g.client,
		recordFailureScriptName,
		[]string{
			g.keys.MustBuild(loginFailurePrefixName, accountID),
			g.keys.MustBuild(loginLockPrefixName, accountID),
		},
		g.config.Lockout.MaxFailures,
		durationMilliseconds(g.config.Lockout.FailureWindow),
		durationMilliseconds(g.config.Lockout.LockDuration),
	).Int64()
	if err != nil {
		return fmt.Errorf("%w: record login failure: %v", ErrLoginProtectionStoreFailure, err)
	}
	if locked == 1 {
		return ErrLoginLocked
	}
	return nil
}

// Reset 在登录成功后清除账号连续失败和锁定状态；速率窗口不会被成功请求绕过。
func (g *RedisLoginGuard) Reset(ctx context.Context, account string) error {
	accountID := loginKeyFingerprint(normalizeLoginAccount(account))
	if err := g.client.Del(
		ctx,
		g.keys.MustBuild(loginFailurePrefixName, accountID),
		g.keys.MustBuild(loginLockPrefixName, accountID),
	).Err(); err != nil {
		return fmt.Errorf("%w: reset login failures: %v", ErrLoginProtectionStoreFailure, err)
	}
	return nil
}

type memoryLoginCounter struct {
	count     int64
	expiresAt time.Time
}

// MemoryLoginGuard 是仅供 dev/local/test 使用的进程内登录防护实现。
type MemoryLoginGuard struct {
	mu       sync.Mutex
	config   LoginProtectionConfig
	now      func() time.Time
	counters map[string]memoryLoginCounter
	locks    map[string]time.Time
}

// NewMemoryLoginGuard 创建进程内登录防护器。
func NewMemoryLoginGuard(config LoginProtectionConfig) (LoginGuard, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if !config.Enabled {
		return noopLoginGuard{}, nil
	}
	return &MemoryLoginGuard{
		config:   config,
		now:      time.Now,
		counters: map[string]memoryLoginCounter{},
		locks:    map[string]time.Time{},
	}, nil
}

// Check 检查进程内锁定状态并累计账号、来源 IP 尝试次数。
func (g *MemoryLoginGuard) Check(_ context.Context, account string, sourceIP string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.currentTime()
	accountID := loginKeyFingerprint(normalizeLoginAccount(account))
	if expiresAt, ok := g.locks[accountID]; ok {
		if expiresAt.After(now) {
			return ErrLoginLocked
		}
		delete(g.locks, accountID)
	}
	accountExceeded := g.incrementLocked("account:"+accountID, g.config.AccountRateLimit, now)
	ipExceeded := g.incrementLocked("ip:"+loginKeyFingerprint(normalizeSourceIP(sourceIP)), g.config.IPRateLimit, now)
	if accountExceeded || ipExceeded {
		return ErrLoginRateLimited
	}
	return nil
}

// RecordFailure 累计进程内连续失败次数，并在达到阈值时锁定账号。
func (g *MemoryLoginGuard) RecordFailure(_ context.Context, account string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.currentTime()
	accountID := loginKeyFingerprint(normalizeLoginAccount(account))
	key := "failure:" + accountID
	counter := g.counters[key]
	if !counter.expiresAt.After(now) {
		counter = memoryLoginCounter{expiresAt: now.Add(g.config.Lockout.FailureWindow)}
	}
	counter.count++
	g.counters[key] = counter
	if counter.count >= g.config.Lockout.MaxFailures {
		delete(g.counters, key)
		g.locks[accountID] = now.Add(g.config.Lockout.LockDuration)
		return ErrLoginLocked
	}
	return nil
}

// Reset 清除进程内连续失败和锁定状态。
func (g *MemoryLoginGuard) Reset(_ context.Context, account string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	accountID := loginKeyFingerprint(normalizeLoginAccount(account))
	delete(g.counters, "failure:"+accountID)
	delete(g.locks, accountID)
	return nil
}

func (g *MemoryLoginGuard) incrementLocked(key string, config LoginRateConfig, now time.Time) bool {
	counter := g.counters[key]
	if !counter.expiresAt.After(now) {
		counter = memoryLoginCounter{expiresAt: now.Add(config.Window)}
	}
	counter.count++
	g.counters[key] = counter
	return counter.count > config.MaxAttempts
}

func (g *MemoryLoginGuard) currentTime() time.Time {
	if g.now == nil {
		return time.Now()
	}
	return g.now()
}

func normalizeLoginAccount(account string) string {
	return strings.ToLower(strings.TrimSpace(account))
}

func normalizeSourceIP(sourceIP string) string {
	if normalized := requestctx.NormalizeIP(sourceIP); normalized != "" {
		return normalized
	}
	return "unknown"
}

func loginKeyFingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func newLoginGuardScriptRegistry() *redisx.ScriptRegistry {
	registry := redisx.NewScriptRegistry()
	if err := registry.Register(redisx.ScriptDefinition{
		Name:   checkLoginScriptName,
		Source: checkLoginScriptSource,
		Keys: []redisx.ScriptArgument{
			{Name: "account_rate_key", Description: "账号登录速率窗口 key"},
			{Name: "ip_rate_key", Description: "来源 IP 登录速率窗口 key"},
			{Name: "account_lock_key", Description: "账号临时锁定 key"},
		},
		Args: []redisx.ScriptArgument{
			{Name: "account_max_attempts", Description: "账号窗口最大尝试次数"},
			{Name: "account_window_ms", Description: "账号速率窗口毫秒数"},
			{Name: "ip_max_attempts", Description: "IP 窗口最大尝试次数"},
			{Name: "ip_window_ms", Description: "IP 速率窗口毫秒数"},
		},
	}); err != nil {
		panic(err)
	}
	if err := registry.Register(redisx.ScriptDefinition{
		Name:   recordFailureScriptName,
		Source: recordLoginFailureScriptSource,
		Keys: []redisx.ScriptArgument{
			{Name: "account_failure_key", Description: "账号连续失败计数 key"},
			{Name: "account_lock_key", Description: "账号临时锁定 key"},
		},
		Args: []redisx.ScriptArgument{
			{Name: "max_failures", Description: "触发锁定的连续失败阈值"},
			{Name: "failure_window_ms", Description: "连续失败计数窗口毫秒数"},
			{Name: "lock_duration_ms", Description: "账号锁定时长毫秒数"},
		},
	}); err != nil {
		panic(err)
	}
	return registry
}

const checkLoginScriptSource = `
if redis.call("EXISTS", KEYS[3]) == 1 then
    return 1
end

local account_count = redis.call("INCR", KEYS[1])
if account_count == 1 then
    redis.call("PEXPIRE", KEYS[1], ARGV[2])
end

local ip_count = redis.call("INCR", KEYS[2])
if ip_count == 1 then
    redis.call("PEXPIRE", KEYS[2], ARGV[4])
end

if account_count > tonumber(ARGV[1]) or ip_count > tonumber(ARGV[3]) then
    return 2
end
return 0
`

const recordLoginFailureScriptSource = `
if redis.call("EXISTS", KEYS[2]) == 1 then
    return 1
end

local failure_count = redis.call("INCR", KEYS[1])
if failure_count == 1 then
    redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
if failure_count >= tonumber(ARGV[1]) then
    redis.call("SET", KEYS[2], "1", "PX", ARGV[3])
    redis.call("DEL", KEYS[1])
    return 1
end
return 0
`
