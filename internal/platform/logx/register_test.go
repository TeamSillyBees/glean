package logx

import (
	"path/filepath"
	"testing"

	"github.com/samber/do"
	"github.com/stretchr/testify/require"
)

// TestRegisterProvidesLogger 验证 DI 提供 logx Logger。
func TestRegisterProvidesLogger(t *testing.T) {
	injector := do.New()
	Register(injector, Config{
		Level:   "debug",
		Console: ConsoleConfig{Enabled: false},
		JSONL:   JSONLConfig{Enabled: true, Path: filepath.Join(t.TempDir(), "app.jsonl")},
	})

	logger := do.MustInvoke[*Logger](injector)
	t.Cleanup(func() {
		require.NoError(t, logger.Close())
	})
	require.NotNil(t, logger)
}
