package redisx

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/redis/go-redis/v9"
)

// ScriptArgument 描述 Lua 脚本 key 或参数的含义。
type ScriptArgument struct {
	Name        string
	Description string
}

// ScriptDefinition 描述一个允许业务调用的静态 Lua 脚本。
type ScriptDefinition struct {
	Name   string
	Source string
	Keys   []ScriptArgument
	Args   []ScriptArgument
}

type registeredScript struct {
	definition ScriptDefinition
	script     *redis.Script
}

// ScriptRegistry 保存允许执行的 Lua 脚本，避免业务动态拼接 Lua。
type ScriptRegistry struct {
	mu      sync.RWMutex
	scripts map[string]registeredScript
}

// NewScriptRegistry 创建 Lua 脚本注册表。
func NewScriptRegistry() *ScriptRegistry {
	return &ScriptRegistry{scripts: make(map[string]registeredScript)}
}

// Register 注册一个静态 Lua 脚本。
func (r *ScriptRegistry) Register(def ScriptDefinition) error {
	if err := def.validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.scripts[def.Name]; exists {
		return fmt.Errorf("redis lua script %q 已注册", def.Name)
	}
	r.scripts[def.Name] = registeredScript{
		definition: def,
		script:     redis.NewScript(def.Source),
	}
	return nil
}

// Run 使用 EVALSHA 优先执行已注册脚本，NOSCRIPT 时由 go-redis 自动回退到 EVAL。
func (r *ScriptRegistry) Run(ctx context.Context, client redis.Scripter, name string, keys []string, args ...any) *redis.Cmd {
	r.mu.RLock()
	registered, ok := r.scripts[name]
	r.mu.RUnlock()
	if !ok {
		return redis.NewCmdResult(nil, fmt.Errorf("redis lua script %q 未注册", name))
	}
	return registered.script.Run(ctx, client, keys, args...)
}

// LoadAll 预加载注册表中的全部 Lua 脚本。
func (r *ScriptRegistry) LoadAll(ctx context.Context, client redis.Scripter) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for name, registered := range r.scripts {
		if err := registered.script.Load(ctx, client).Err(); err != nil {
			return fmt.Errorf("load redis lua script %q failed: %w", name, err)
		}
	}
	return nil
}

func (d ScriptDefinition) validate() error {
	if strings.TrimSpace(d.Name) == "" {
		return fmt.Errorf("redis lua script name 不能为空")
	}
	if strings.TrimSpace(d.Source) == "" {
		return fmt.Errorf("redis lua script source 不能为空")
	}
	if err := validateScriptArguments("key", d.Keys); err != nil {
		return err
	}
	if err := validateScriptArguments("arg", d.Args); err != nil {
		return err
	}
	return nil
}

func validateScriptArguments(kind string, args []ScriptArgument) error {
	for _, arg := range args {
		if strings.TrimSpace(arg.Name) == "" || strings.TrimSpace(arg.Description) == "" {
			return fmt.Errorf("redis lua script %s 必须声明名称和含义", kind)
		}
	}
	return nil
}
