package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOfflineDefaultNeverUsesDatabase 验证默认演练即使存在数据库环境变量也不读取或写入它。
func TestOfflineDefaultNeverUsesDatabase(t *testing.T) {
	t.Setenv("INITRA_MONITOR_DATABASE_URL", "postgres://secret:never-print@invalid.example/private?sslmode=disable")
	var out bytes.Buffer
	require.NoError(t, run(context.Background(), []string{"--node", "90", "--scenario", "incremental"}, &out))
	var result struct {
		CandidateVersions int               `json:"candidateVersions"`
		Persistence       string            `json:"persistence"`
		ScientificReview  bool              `json:"scientificReview"`
		Runs              []json.RawMessage `json:"runs"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Equal(t, 12, result.CandidateVersions)
	require.Len(t, result.Runs, 2)
	require.False(t, result.ScientificReview)
	require.Contains(t, result.Persistence, "in-memory")
	require.NotContains(t, out.String(), "never-print")
}

// TestPersistentModeRequiresExplicitSafeConfiguration 验证持久化开关、操作人和 TLS 在任何连接前检查。
func TestPersistentModeRequiresExplicitSafeConfiguration(t *testing.T) {
	for _, args := range [][]string{{"--scenario", "baseline"}, {"--node", "1", "--scenario", "unknown"}, {"--node", "1", "--at", "bad"}, {"--node", "1", "extra"}} {
		require.Error(t, run(context.Background(), args, &bytes.Buffer{}))
	}
	t.Setenv("INITRA_MONITOR_DATABASE_URL", "postgres://private:never-print@remote.example/db?sslmode=disable")
	err := run(context.Background(), []string{"--node", "1", "--persist-mock", "--operator", "7"}, &bytes.Buffer{})
	require.ErrorContains(t, err, "verify-full")
	require.NotContains(t, err.Error(), "never-print")
	t.Setenv("INITRA_MONITOR_DATABASE_URL", "postgres://private:never-print@remote.example/db?sslmode=verify-full")
	err = run(context.Background(), []string{"--node", "1", "--persist-mock"}, &bytes.Buffer{})
	require.ErrorContains(t, err, "--operator")
	require.NotContains(t, err.Error(), "never-print")
}

// TestMonitorDatabaseTLSValidation 验证本机不安全开发必须双重显式选择，远程始终验证证书。
func TestMonitorDatabaseTLSValidation(t *testing.T) {
	require.NoError(t, validateDSN("postgres://user:secret@db.example/app?sslmode=verify-full", false))
	require.Error(t, validateDSN("postgres://localhost/app?sslmode=disable", false))
	require.NoError(t, validateDSN("postgres://127.0.0.1/app?sslmode=disable", true))
	require.NoError(t, validateDSN("postgres://[::1]/app?sslmode=disable", true))
	require.Error(t, validateDSN("postgres://remote.example/app?sslmode=disable", true))
	require.Error(t, validateDSN("postgres://localhost.evil.example/app?sslmode=disable", true))
	for _, dsn := range []string{"", "password=secret", "http://example/db", "postgres:///db"} {
		err := validateDSN(dsn, false)
		require.Error(t, err)
		require.False(t, strings.Contains(err.Error(), "secret"))
	}
}
