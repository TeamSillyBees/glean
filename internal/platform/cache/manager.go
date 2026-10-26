package cache

import (
	"fmt"
	"time"

	jetcache "github.com/mgtv-tech/jetcache-go"
	"github.com/mgtv-tech/jetcache-go/local"
	"github.com/mgtv-tech/jetcache-go/remote"
	"github.com/teamsillybees/glean/internal/platform/redisx"
)

// Config 描述缓存管理器所需的最小输入。
type Config struct {
	AppName       string
	LocalTTL      time.Duration
	RemoteTTL     time.Duration
	RemoteEnabled bool
}

// Manager 负责统一缓存实例构造和缓存 Key 规范。
type Manager struct {
	appName   string
	localTTL  time.Duration
	remoteTTL time.Duration
	remote    redisx.Cmdable
}

// NewManager 创建缓存管理器。未提供 Redis 时会自动退化为本地缓存。
func NewManager(cfg Config, remoteClient redisx.Cmdable) *Manager {
	return &Manager{
		appName:   cfg.AppName,
		localTTL:  cfg.LocalTTL,
		remoteTTL: cfg.RemoteTTL,
		remote:    remoteClient,
	}
}

// BuildKey 统一构造 `<app>:<module>:<domain>:<identifier>` 格式的缓存 Key。
func (m *Manager) BuildKey(module string, domain string, identifier any) string {
	return fmt.Sprintf("%s:%s:%s:%v", m.appName, module, domain, identifier)
}

// New 创建一个同时支持本地缓存和 Redis 远端缓存的 cache 实例。
func (m *Manager) New(name string) jetcache.Cache {
	options := []jetcache.Option{
		jetcache.WithName(name),
		jetcache.WithLocal(local.NewTinyLFU(1024, m.localTTL)),
		jetcache.WithRemoteExpiry(m.remoteTTL),
	}
	if m.remote != nil {
		options = append(options, jetcache.WithRemote(remote.NewGoRedisV9Adapter(m.remote)))
	}
	return jetcache.New(options...)
}
