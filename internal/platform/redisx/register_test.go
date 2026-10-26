package redisx

import (
	"testing"

	"github.com/samber/do"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/logx"
)

func TestRegisterProvidesDisabledRedisClient(t *testing.T) {
	injector := do.New()
	do.ProvideValue(injector, logx.NewNop())
	Register(injector, Config{Enabled: false})

	client := do.MustInvoke[UniversalClient](injector)

	require.Nil(t, client)
}
