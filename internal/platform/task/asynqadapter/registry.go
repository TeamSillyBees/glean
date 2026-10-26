package asynqadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hibiken/asynq"
	"github.com/teamsillybees/glean/internal/platform/task"
)

// NewServeMux 将框架 Registry 转换为 Asynq ServeMux。
func NewServeMux(registry task.Registry) (*asynq.ServeMux, error) {
	if registry == nil {
		return nil, fmt.Errorf("%w: registry 不能为空", task.ErrInvalidTask)
	}
	mux := asynq.NewServeMux()
	for _, entry := range registry.Registrations() {
		entry := entry
		mux.HandleFunc(entry.TaskType, func(ctx context.Context, raw *asynq.Task) error {
			item := fromAsynqTask(raw)
			err := entry.Handler.HandleTask(ctx, item)
			return mapHandlerError(err)
		})
	}
	return mux, nil
}

func fromAsynqTask(raw *asynq.Task) task.Task {
	headers := raw.Headers()
	if headers == nil {
		headers = map[string]string{}
	}
	return task.Task{
		Type:    raw.Type(),
		Payload: json.RawMessage(raw.Payload()),
		Meta:    task.MetaFromHeaders(headers),
	}
}

func mapHandlerError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, task.ErrSkipRetry):
		return fmt.Errorf("%w: %w", asynq.SkipRetry, err)
	case errors.Is(err, task.ErrRevoke):
		return fmt.Errorf("%w: %w", asynq.RevokeTask, err)
	default:
		return err
	}
}
