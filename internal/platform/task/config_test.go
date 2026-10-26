package task

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/redisx"
)

func TestConfigValidateDisabled(t *testing.T) {
	cfg := Config{}

	require.NoError(t, cfg.Validate())
}

func TestConfigValidateEnabled(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Redis: redisx.Config{
			Enabled: true,
			Mode:    redisx.ModeStandalone,
			Addr:    "127.0.0.1:6379",
		},
		Worker: WorkerConfig{
			Enabled: true,
		},
	}

	require.NoError(t, cfg.Validate())
	normalized := cfg.Normalize()
	require.Equal(t, BackendAsynq, normalized.Backend)
	require.Equal(t, QueueDefault, normalized.Publisher.DefaultQueue)
	require.Equal(t, map[string]int{QueueCritical: 6, QueueDefault: 3, QueueLow: 1}, normalized.Worker.Queues)
}

func TestConfigRejectsInvalidQueue(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Redis: redisx.Config{
			Enabled: true,
			Mode:    redisx.ModeStandalone,
			Addr:    "127.0.0.1:6379",
		},
		Publisher: PublisherConfig{
			DefaultQueue: "bad queue",
		},
	}

	require.ErrorContains(t, cfg.Validate(), "default_queue")
}
