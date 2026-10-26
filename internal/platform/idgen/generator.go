package idgen

import (
	"sync"

	"github.com/bwmarrin/snowflake"
)

// Generator 对 snowflake 节点做了一层薄封装，避免业务模块直接依赖第三方细节。
type Generator struct {
	node *snowflake.Node
}

var defaultState = struct {
	sync.RWMutex
	generator *Generator
}{}

// NewGenerator 初始化指定节点编号的雪花算法生成器。
func NewGenerator(node int64) (*Generator, error) {
	n, err := snowflake.NewNode(node)
	if err != nil {
		return nil, err
	}
	return &Generator{node: n}, nil
}

// NextID 返回一个全局唯一的业务 ID。
func (g *Generator) NextID() ID {
	return New(g.node.Generate().Int64())
}

// NextInt64 返回一个全局唯一的 int64 主键。
//
// 该方法仅用于对接第三方库、手写 SQL 或其他底层基础设施场景；
// 业务代码应优先使用 NextID 和 ID 类型。
func (g *Generator) NextInt64() int64 {
	return g.NextID().Int64()
}

// ConfigureDefault 使用指定节点编号配置包级默认 ID 生成器。
func ConfigureDefault(node int64) (*Generator, error) {
	generator, err := NewGenerator(node)
	if err != nil {
		return nil, err
	}
	SetDefaultGenerator(generator)
	return generator, nil
}

// SetDefaultGenerator 设置 Ent schema 字段默认使用的 ID 生成器。
func SetDefaultGenerator(generator *Generator) {
	defaultState.Lock()
	defer defaultState.Unlock()
	defaultState.generator = generator
}

// DefaultGenerator 返回当前包级默认 ID 生成器。
func DefaultGenerator() *Generator {
	defaultState.RLock()
	defer defaultState.RUnlock()
	return defaultState.generator
}

// NextID 返回包级默认生成器生成的业务 ID。
func NextID() ID {
	generator := DefaultGenerator()
	if generator == nil {
		panic("idgen default generator is not configured")
	}
	return generator.NextID()
}
