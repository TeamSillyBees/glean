package idgen

import (
	"testing"

	"github.com/samber/do"
	"github.com/stretchr/testify/require"
)

// TestRegisterProvidesGenerator 验证 Register 同时装配 DI 生成器和包级默认生成器。
func TestRegisterProvidesGenerator(t *testing.T) {
	injector := do.New()
	Register(injector, 1)

	generator := do.MustInvoke[*Generator](injector)

	require.Positive(t, generator.NextID().Int64())
	require.Same(t, generator, DefaultGenerator())
}
