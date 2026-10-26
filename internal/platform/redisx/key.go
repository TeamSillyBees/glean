package redisx

import (
	"fmt"
	"strings"
	"sync"
)

// KeyConfig 描述 Redis key 命名空间。
type KeyConfig struct {
	App string
	Env string
}

type keyPrefix struct {
	module string
	biz    string
}

// KeyBuilder 统一构造 {app}:{env}:{module}:{biz}:{id} 格式的 Redis key。
type KeyBuilder struct {
	app      string
	env      string
	mu       sync.RWMutex
	prefixes map[string]keyPrefix
}

// NewKeyBuilder 创建 Redis key 构造器。
func NewKeyBuilder(cfg KeyConfig) *KeyBuilder {
	return &KeyBuilder{
		app:      strings.TrimSpace(cfg.App),
		env:      strings.TrimSpace(cfg.Env),
		prefixes: make(map[string]keyPrefix),
	}
}

// RegisterPrefix 注册业务可使用的 Redis key 前缀。
func (b *KeyBuilder) RegisterPrefix(name string, module string, biz string) error {
	name = strings.TrimSpace(name)
	module = strings.TrimSpace(module)
	biz = strings.TrimSpace(biz)
	if name == "" || module == "" || biz == "" {
		return fmt.Errorf("redis key prefix name/module/biz 不能为空")
	}
	if hasKeySeparator(name, module, biz) {
		return fmt.Errorf("redis key prefix 片段不能包含 ':'")
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.prefixes[name] = keyPrefix{module: module, biz: biz}
	return nil
}

// Build 根据已注册前缀构造完整 Redis key。
func (b *KeyBuilder) Build(name string, id any) (string, error) {
	prefix, err := b.Prefix(name)
	if err != nil {
		return "", err
	}
	identifier := strings.TrimSpace(fmt.Sprint(id))
	if identifier == "" || identifier == "<nil>" {
		return "", fmt.Errorf("redis key id 不能为空")
	}
	if strings.Contains(identifier, ":") {
		return "", fmt.Errorf("redis key id 不能包含 ':'")
	}
	return prefix + identifier, nil
}

// MustBuild 根据已注册前缀构造完整 Redis key；失败时 panic，适合初始化期使用。
func (b *KeyBuilder) MustBuild(name string, id any) string {
	key, err := b.Build(name, id)
	if err != nil {
		panic(err)
	}
	return key
}

// Prefix 返回已注册前缀的扫描前缀，末尾包含 ':'。
func (b *KeyBuilder) Prefix(name string) (string, error) {
	b.mu.RLock()
	prefix, ok := b.prefixes[strings.TrimSpace(name)]
	b.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("redis key prefix %q 未注册", name)
	}
	if b.app == "" || b.env == "" {
		return "", fmt.Errorf("redis key app/env 不能为空")
	}
	return fmt.Sprintf("%s:%s:%s:%s:", b.app, b.env, prefix.module, prefix.biz), nil
}

// AllowedPrefixes 返回所有已注册扫描前缀。
func (b *KeyBuilder) AllowedPrefixes() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	result := make([]string, 0, len(b.prefixes))
	if b.app == "" || b.env == "" {
		return result
	}
	for _, prefix := range b.prefixes {
		result = append(result, fmt.Sprintf("%s:%s:%s:%s:", b.app, b.env, prefix.module, prefix.biz))
	}
	return result
}

// IsAllowedPrefix 判断 prefix 是否来自注册表。
func (b *KeyBuilder) IsAllowedPrefix(prefix string) bool {
	for _, allowed := range b.AllowedPrefixes() {
		if prefix == allowed {
			return true
		}
	}
	return false
}

func hasKeySeparator(parts ...string) bool {
	for _, part := range parts {
		if strings.Contains(part, ":") {
			return true
		}
	}
	return false
}
