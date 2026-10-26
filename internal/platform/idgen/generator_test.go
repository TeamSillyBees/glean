package idgen

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGeneratorGeneratesUniquePositiveIDs 验证雪花 ID 生成器输出正数且连续调用不重复。
func TestGeneratorGeneratesUniquePositiveIDs(t *testing.T) {
	generator, err := NewGenerator(1)
	require.NoError(t, err)

	first := generator.NextID()
	second := generator.NextID()

	require.Positive(t, first.Int64())
	require.Positive(t, second.Int64())
	require.NotEqual(t, first, second)
}

// TestNextIDRequiresExplicitDefaultGenerator 验证包级生成器不会静默使用固定节点。
func TestNextIDRequiresExplicitDefaultGenerator(t *testing.T) {
	previous := DefaultGenerator()
	SetDefaultGenerator(nil)
	t.Cleanup(func() {
		SetDefaultGenerator(previous)
	})

	require.PanicsWithValue(t, "idgen default generator is not configured", func() {
		NextID()
	})
}
