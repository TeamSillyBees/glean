package asynqadapter

import (
	"time"

	"github.com/hibiken/asynq"
	"github.com/teamsillybees/glean/internal/platform/task"
)

func asynqOptions(opts task.PublishOptions) []asynq.Option {
	result := []asynq.Option{
		asynq.Queue(opts.Queue),
		asynq.MaxRetry(opts.MaxRetry),
	}
	if opts.TimeoutSet || opts.Timeout > 0 {
		result = append(result, asynq.Timeout(opts.Timeout))
	}
	if !opts.Deadline.IsZero() {
		result = append(result, asynq.Deadline(opts.Deadline))
	}
	if opts.RetentionSet || opts.Retention > 0 {
		result = append(result, asynq.Retention(opts.Retention))
	}
	if opts.Unique > 0 {
		result = append(result, asynq.Unique(opts.Unique))
	}
	if opts.TaskID != "" {
		result = append(result, asynq.TaskID(opts.TaskID))
	}
	return result
}

func appendProcessAt(opts []asynq.Option, at time.Time) []asynq.Option {
	return append(opts, asynq.ProcessAt(at))
}

func appendProcessIn(opts []asynq.Option, delay time.Duration) []asynq.Option {
	return append(opts, asynq.ProcessIn(delay))
}
