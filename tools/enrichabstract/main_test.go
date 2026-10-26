package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/abstractenrichment"
)

func TestVerifyDefaultNeverNeedsDatabase(t *testing.T) {
	t.Setenv("GLEAN_ENRICHMENT_DATABASE_URL", "postgres://secret:do-not-show@private.example/db")
	var out bytes.Buffer
	err := run(nil, &out)
	require.ErrorContains(t, err, "-sources is required")
	require.NotContains(t, err.Error(), "secret")
	require.Empty(t, out.String())
}
func TestApplyRequiresExplicitAuthorityBeforeIO(t *testing.T) {
	var out bytes.Buffer
	require.ErrorContains(t, run([]string{"-mode", "apply"}, &out), "explicit -node")
	require.ErrorContains(t, run([]string{"-mode", "apply", "-node", "18"}, &out), "-operator")
	require.ErrorContains(t, run([]string{"-mode", "unknown"}, &out), "verify or apply")
}
func TestSourceMapRejectsUnboundedScopeAndPrivateErrorPaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "map.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"BAI2E6C229787":"private-file"}`), 0600))
	_, err := verifySources(path)
	require.ErrorContains(t, err, "exactly the three")
	m := map[string]string{}
	for _, s := range abstractenrichment.Specs() {
		m[s.OriginalID] = filepath.Join(dir, "private-secret-cache")
	}
	body, err := json.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, body, 0600))
	_, err = verifySources(path)
	require.Error(t, err)
	require.NotContains(t, err.Error(), dir)
	require.NotContains(t, err.Error(), "private-secret-cache")
}
func TestReadBoundedRejectsOversize(t *testing.T) {
	p := filepath.Join(t.TempDir(), "response")
	require.NoError(t, os.WriteFile(p, []byte("too large"), 0600))
	_, err := readBounded(p, 3)
	require.Error(t, err)
}
func TestDatabaseRoutingCannotBypassTLS(t *testing.T) {
	for _, k := range []string{"PGHOSTADDR", "PGSERVICE", "PGSERVICEFILE"} {
		t.Setenv(k, "")
	}
	require.NoError(t, validateDSN("postgres://u:p@db.example/app?sslmode=verify-full", false))
	require.NoError(t, validateDSN("postgres://localhost/app?sslmode=disable", true))
	for _, dsn := range []string{"postgres://db.example/app?sslmode=disable", "postgres://localhost/app?sslmode=disable&host=remote.example", "postgres://localhost/app?sslmode=disable&sslmode=verify-full", "postgres://localhost/app?sslmode=disable&SSLMode=disable", "postgres://localhost/app?sslmode=disable&service=remote", "postgres://localhost/app?sslmode=disable#fragment", "postgres://localhost/app?sslmode=disable&bad%3Dkey=x"} {
		require.Error(t, validateDSN(dsn, true), dsn)
	}
	t.Setenv("PGHOSTADDR", "remote.example")
	require.Error(t, validateDSN("postgres://localhost/app?sslmode=disable", true))
}
