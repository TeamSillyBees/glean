package cache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestManagerBuildKeyUsesUnifiedFormat 验证缓存 key 保持 app:module:domain:id 的统一格式。
func TestManagerBuildKeyUsesUnifiedFormat(t *testing.T) {
	manager := NewManager(Config{
		AppName:   "initra",
		LocalTTL:  time.Minute,
		RemoteTTL: 10 * time.Minute,
	}, nil)

	require.Equal(t, "initra:user:profile:1001", manager.BuildKey("user", "profile", 1001))
}
