package startup

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/redisx"
	"github.com/teamsillybees/glean/internal/platform/task"
)

// TestRenderIncludesRuntimeSummary 验证启动提示展示关键运行时信息。
func TestRenderIncludesRuntimeSummary(t *testing.T) {
	banner := Render(Info{
		AppName:     "initra",
		Env:         "local",
		Version:     "0.1.0",
		InstanceID:  "local-1",
		Addr:        ":8080",
		Database:    "postgres://127.0.0.1:5432/initra user=initra pool=10/20",
		Redis:       "enabled mode=standalone addr=127.0.0.1:6379 db=0",
		Task:        "enabled backend=asynq",
		DocsURL:     "http://localhost:8080/docs",
		Health:      "http://localhost:8080/health",
		Storage:     "enabled local",
		HTTPClient:  "enabled services=1",
		ShutdownTTL: "20s",
	})

	require.Contains(t, banner, "initra")
	require.Contains(t, banner, "Environment")
	require.Contains(t, banner, "local / instance local-1")
	require.Contains(t, banner, "http://localhost:8080")
	require.Contains(t, banner, "Port:")
	require.Contains(t, banner, "8080")
	require.Contains(t, banner, "postgres://127.0.0.1:5432/initra user=initra pool=10/20")
	require.Contains(t, banner, "Redis")
	require.Contains(t, banner, "Task")
}

// TestSummariesDoNotLeakSecrets 验证内置摘要不会泄露密码。
func TestSummariesDoNotLeakSecrets(t *testing.T) {
	db := SQLDatabaseSummary(SQLDatabase{
		Driver:       "postgres",
		Host:         "127.0.0.1",
		Port:         5432,
		User:         "initra",
		DBName:       "initra",
		MaxIdleConns: 10,
		MaxOpenConns: 20,
	})
	redis := RedisSummary(redisx.Config{
		Enabled:  true,
		Mode:     redisx.ModeStandalone,
		Addr:     "127.0.0.1:6379",
		Password: "redis-secret",
		DB:       0,
	})

	require.Equal(t, "postgres://127.0.0.1:5432/initra user=initra pool=10/20", db)
	require.Equal(t, "enabled mode=standalone addr=127.0.0.1:6379 db=0", redis)
	require.NotContains(t, redis, "redis-secret")
}

// TestTaskSummary 验证任务队列摘要使用归一化后的默认值。
func TestTaskSummary(t *testing.T) {
	got := TaskSummary(task.Config{
		Enabled: true,
		Backend: task.BackendAsynq,
	})

	require.Contains(t, got, "backend=asynq")
	require.Contains(t, got, "publisher_queue=default")
}

// TestLocalURLNormalizesCommonListenAddresses 验证常见监听地址会被转换为易访问的本机 URL。
func TestLocalURLNormalizesCommonListenAddresses(t *testing.T) {
	tests := map[string]string{
		":8080":          "http://localhost:8080",
		"0.0.0.0:8080":   "http://localhost:8080",
		"127.0.0.1:8080": "http://127.0.0.1:8080",
		"8080":           "http://localhost:8080",
	}

	for addr, want := range tests {
		t.Run(strings.ReplaceAll(addr, ":", "_"), func(t *testing.T) {
			require.Equal(t, want, LocalURL(addr))
		})
	}
}
