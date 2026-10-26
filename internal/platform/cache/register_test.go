package cache

import (
	"testing"
	"time"

	"github.com/samber/do"
	"github.com/stretchr/testify/require"
)

func TestRegisterProvidesManager(t *testing.T) {
	injector := do.New()
	Register(injector, Config{
		AppName:   "initra",
		LocalTTL:  time.Minute,
		RemoteTTL: 10 * time.Minute,
	})

	manager := do.MustInvoke[*Manager](injector)

	require.NotNil(t, manager)
	require.Equal(t, "initra:user:profile:1001", manager.BuildKey("user", "profile", 1001))
}
