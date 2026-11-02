package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/teamsillybees/glean/internal/modules/monitor"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestExplicitDemoNeverUsesDatabase 验证只有显式 demo 使用夹具，且即使存在数据库环境变量也不读取或写入它。
func TestExplicitDemoNeverUsesDatabase(t *testing.T) {
	t.Setenv("INITRA_MONITOR_DATABASE_URL", "postgres://secret:never-print@invalid.example/private?sslmode=disable")
	var out bytes.Buffer
	require.NoError(t, run(context.Background(), []string{"--demo", "--node", "90", "--scenario", "incremental"}, &out))
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
	err := run(context.Background(), []string{"--demo", "--node", "1", "--persist-mock", "--operator", "7"}, &bytes.Buffer{})
	require.ErrorContains(t, err, "verify-full")
	require.NotContains(t, err.Error(), "never-print")
	t.Setenv("INITRA_MONITOR_DATABASE_URL", "postgres://private:never-print@remote.example/db?sslmode=verify-full")
	err = run(context.Background(), []string{"--demo", "--node", "1", "--persist-mock"}, &bytes.Buffer{})
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

// TestNormalModeIsRealAndBudgeted 验证正常路径必须真实数据库和明确预算，不能静默退回模拟。
func TestNormalModeIsRealAndBudgeted(t *testing.T) {
	t.Setenv("INITRA_MONITOR_DATABASE_URL", "")
	var out bytes.Buffer
	err := run(context.Background(), []string{"--node", "11"}, &out)
	require.ErrorContains(t, err, "INITRA_MONITOR_DATABASE_URL")
	require.Empty(t, out.String())
	for _, args := range [][]string{{"--node", "11", "--max-pages", "0"}, {"--node", "11", "--max-pages", "201"}, {"--node", "11", "--sources", "fake"}, {"--node", "11", "--sources", "pubmed,pubmed"}, {"--node", "11", "--since", "bad"}, {"--node", "11", "--since", "2026-01-01T00:00:00Z", "--until", "2026-10-01T00:00:00Z"}} {
		require.Error(t, run(context.Background(), args, &out))
	}
	err = run(context.Background(), []string{"--node", "11", "--persist-mock"}, &out)
	require.ErrorContains(t, err, "--demo")
}

// TestDatabaseEndpointOverridesAreRejected 验证 lib/pq 查询参数不能覆盖已验证的本机地址、身份或 TLS 模式。
func TestDatabaseEndpointOverridesAreRejected(t *testing.T) {
	for _, query := range []string{"sslmode=disable&host=remote.example", "sslmode=verify-full&hostaddr=169.254.169.254", "sslmode=disable&dbname=remote", "sslmode=disable&user=another", "sslmode=disable&port=9999", "sslmode=verify-full&sslmode=disable", "sslmode=disable&options=-c%20x=y", "sslmode=disable&password=secret"} {
		err := validateDSN("postgres://127.0.0.1/app?"+query, true)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret")
	}
}

// TestCanaryBudgetAndStatusAreExplicit 验证单页真实验收不会把拒绝或隔离结果当作完整成功。
func TestCanaryBudgetAndStatusAreExplicit(t *testing.T) {
	for _, size := range []string{"0", "1001"} {
		err := run(context.Background(), []string{"--node", "11", "--page-size", size}, &bytes.Buffer{})
		require.ErrorContains(t, err, "--page-size")
	}
	require.Equal(t, "not-due", realRunStatus(monitor.Snapshot{}))
	snapshot := monitor.Snapshot{Runs: []monitor.Run{{Sources: map[string]*monitor.SourceProgress{"europepmc": {}}}}, Sources: map[string]*monitor.Checkpoint{"europepmc": {}}}
	require.Equal(t, "bounded-partial", realRunStatus(snapshot))
	snapshot.Runs[0].FinishedAt = time.Now()
	require.Equal(t, "completed", realRunStatus(snapshot))
	snapshot.Runs[0].Sources["europepmc"].Quarantined = 1
	require.Equal(t, "quarantined", realRunStatus(snapshot))
	snapshot.Sources["europepmc"].Blocked = true
	require.Equal(t, "blocked", realRunStatus(snapshot))
}

// TestRetryFlagRejectsWindowOverrides 验证恢复运行不允许改变原窗口或混入诊断/模拟模式。
func TestRetryFlagRejectsWindowOverrides(t *testing.T) {
	for _, args := range [][]string{
		{"--node", "11", "--retry-failed", "bad"},
		{"--node", "11", "--retry-failed", "0"},
		{"--node", "11", "--retry-failed", "7", "--demo"},
		{"--node", "11", "--retry-failed", "7", "--doctor"},
		{"--node", "11", "--retry-failed", "7", "--since", "2026-10-01T00:00:00Z"},
		{"--node", "11", "--retry-failed", "7", "--until", "2026-10-01T00:00:00Z"},
	} {
		require.ErrorContains(t, run(context.Background(), args, &bytes.Buffer{}), "--retry-failed")
	}
}

// TestRetryReportsSelectedChildNotNewestRun 验证旧恢复运行状态不被新运行成功掩盖。
func TestRetryReportsSelectedChildNotNewestRun(t *testing.T) {
	snapshot := monitor.Snapshot{Runs: []monitor.Run{
		{ID: 1, RetriedBy: 2},
		{ID: 2, RetryOf: 1, Status: "completed_with_errors", FinishedAt: time.Now(), Sources: map[string]*monitor.SourceProgress{"arxiv": {Failed: true, LastError: "http"}}},
		{ID: 3, Status: "completed", FinishedAt: time.Now(), Sources: map[string]*monitor.SourceProgress{"arxiv": {Published: true}}},
	}, Sources: map[string]*monitor.Checkpoint{"arxiv": {}}}
	view, err := retryReportSnapshot(snapshot, 1)
	require.NoError(t, err)
	require.Equal(t, "2", selectedRunID(view).String())
	require.Equal(t, "blocked", realRunStatus(view))
	require.Len(t, snapshot.Runs, 3)
	require.False(t, snapshot.Sources["arxiv"].Blocked)
	snapshot.Runs[1].Status = "completed"
	snapshot.Runs[1].Sources["arxiv"] = &monitor.SourceProgress{Published: true}
	snapshot.Sources["arxiv"].Blocked = true
	view, err = retryReportSnapshot(snapshot, 1)
	require.NoError(t, err)
	require.Equal(t, "completed", realRunStatus(view))
}

// TestRetryReportSurvivesOriginalHistoryCompaction 验证仅保留恢复运行时仍能正确报告目标。
func TestRetryReportSurvivesOriginalHistoryCompaction(t *testing.T) {
	snapshot := monitor.Snapshot{Runs: []monitor.Run{{ID: 2, RetryOf: 1, FinishedAt: time.Now(), Status: "completed", Sources: map[string]*monitor.SourceProgress{"arxiv": {Published: true}}}}}
	view, err := retryReportSnapshot(snapshot, 1)
	require.NoError(t, err)
	require.Equal(t, "2", selectedRunID(view).String())
	require.Equal(t, "completed", realRunStatus(view))
	snapshot.Runs = append(snapshot.Runs, monitor.Run{ID: 3, RetryOf: 1})
	_, err = retryReportSnapshot(snapshot, 1)
	require.ErrorContains(t, err, "ambiguous")
}

// TestLiveBudgetAllowsMaximumModelStage 验证有限CLI能启动最大模型阶段并保留保存时间。
func TestLiveBudgetAllowsMaximumModelStage(t *testing.T) {
	ctx, cancel := boundedLiveContext(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	remaining := time.Until(deadline)
	require.Greater(t, remaining, 120*time.Second+3*time.Second)
	require.LessOrEqual(t, remaining, 3*time.Minute)
	parent, parentCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer parentCancel()
	child, childCancel := boundedLiveContext(parent)
	defer childCancel()
	parentDeadline, _ := parent.Deadline()
	childDeadline, _ := child.Deadline()
	require.Equal(t, parentDeadline, childDeadline)
}
