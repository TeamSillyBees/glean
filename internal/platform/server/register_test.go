package server

import (
	"context"
	"testing"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/samber/do"
	"github.com/stretchr/testify/require"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/logx"
)

func TestRegisterProvidesApp(t *testing.T) {
	injector := do.New()
	manager, err := platformauth.NewJWTManager(platformauth.JWTConfig{
		Issuer:          "initra",
		Secret:          "server-register-test-secret-32-bytes",
		AccessTokenTTL:  time.Minute,
		RefreshTokenTTL: time.Hour,
	})
	require.NoError(t, err)
	enforcer, err := casbin.NewSyncedEnforcer()
	require.NoError(t, err)
	do.ProvideValue(injector, logx.NewNop())
	do.ProvideValue(injector, manager)
	do.ProvideValue(injector, platformauth.IdentityResolver(platformauth.IdentityResolverFunc(func(context.Context, idgen.ID) (platformauth.Principal, bool, error) {
		return platformauth.Principal{}, false, nil
	})))
	do.ProvideValue(injector, enforcer)
	Register(injector, Options{Title: "initra", Version: "test", Env: "test"})

	app := do.MustInvoke[*App](injector)

	require.NotNil(t, app.Engine)
	require.NotNil(t, app.API)
	require.NotNil(t, app.Registry)
}
