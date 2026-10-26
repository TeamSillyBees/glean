package data

import (
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSQLDBConfigBuildsEscapedTLSURL 验证数据库凭据通过 URL 结构安全编码并保留 TLS 模式。
func TestSQLDBConfigBuildsEscapedTLSURL(t *testing.T) {
	cfg := SQLDBConfig(DatabaseConfig{
		Host:            "2001:db8::1",
		Port:            5432,
		User:            "service@example.com",
		Password:        "p@ss word:/?#",
		DBName:          "initra/dev ?#%",
		ApplicationName: "initra-api",
		SSLMode:         "verify-full",
		SSLRootCert:     "/run/secrets/postgres-ca.pem",
		ConnectTimeout:  4500 * time.Millisecond,
		ConnMaxIdleTime: 15 * time.Minute,
		PingTimeout:     7 * time.Second,
	})

	parsed, err := url.Parse(cfg.DataSourceName)
	require.NoError(t, err)
	require.Equal(t, "[2001:db8::1]:5432", parsed.Host)
	require.Equal(t, "service@example.com", parsed.User.Username())
	password, ok := parsed.User.Password()
	require.True(t, ok)
	require.Equal(t, "p@ss word:/?#", password)
	require.Equal(t, "/initra/dev ?#%", parsed.Path)
	require.Equal(t, "/initra%2Fdev%20%3F%23%25", parsed.EscapedPath())
	require.Contains(t, cfg.DataSourceName, "/initra%2Fdev%20%3F%23%25?")
	require.Equal(t, "verify-full", parsed.Query().Get("sslmode"))
	require.Equal(t, "initra-api", parsed.Query().Get("application_name"))
	require.Equal(t, "/run/secrets/postgres-ca.pem", parsed.Query().Get("sslrootcert"))
	require.Equal(t, "5", parsed.Query().Get("connect_timeout"))
	require.Equal(t, 15*time.Minute, cfg.ConnMaxIdleTime)
	require.Equal(t, 7*time.Second, cfg.PingTimeout)
}
