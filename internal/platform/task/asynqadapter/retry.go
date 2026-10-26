package asynqadapter

import (
	"math"
	"time"

	"github.com/hibiken/asynq"
	"github.com/teamsillybees/glean/internal/platform/task"
)

func retryDelayFunc(cfg task.RetryDelayConfig) asynq.RetryDelayFunc {
	cfg = normalizeRetryConfig(cfg)
	switch cfg.Strategy {
	case task.RetryStrategyFixed:
		return func(int, error, *asynq.Task) time.Duration {
			return cfg.Interval
		}
	case task.RetryStrategyLinear:
		return func(n int, _ error, _ *asynq.Task) time.Duration {
			delay := time.Duration(n+1) * cfg.Interval
			return capRetryDelay(delay, cfg.MaxInterval)
		}
	case task.RetryStrategyExponential:
		return func(n int, _ error, _ *asynq.Task) time.Duration {
			pow := math.Pow(2, float64(n))
			delay := time.Duration(pow) * cfg.Interval
			return capRetryDelay(delay, cfg.MaxInterval)
		}
	default:
		return nil
	}
}

func normalizeRetryConfig(cfg task.RetryDelayConfig) task.RetryDelayConfig {
	if cfg.Strategy == "" {
		cfg.Strategy = task.RetryStrategyOfficial
	}
	if cfg.Interval == 0 {
		cfg.Interval = time.Second
	}
	return cfg
}

func capRetryDelay(delay time.Duration, max time.Duration) time.Duration {
	if max > 0 && delay > max {
		return max
	}
	return delay
}
