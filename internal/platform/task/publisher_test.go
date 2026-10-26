package task

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

func TestResolvePublishOptionsRequiresBizKeyForSideEffect(t *testing.T) {
	item := Task{
		Type: "demo:send_email",
		Payload: struct {
			UserID idgen.ID `json:"userId"`
		}{UserID: idgen.New(1001)},
		Meta: TaskMeta{
			SideEffect: true,
		},
	}

	_, err := ResolvePublishOptions(PublisherConfig{EnforceBizKey: true}, item)

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrMissingBizKey))
}

func TestResolvePublishOptionsAppliesDefaultsAndOverrides(t *testing.T) {
	item := Task{
		Type: "demo:send_email",
		Payload: struct {
			UserID idgen.ID `json:"userId"`
		}{UserID: idgen.New(1001)},
		Meta: TaskMeta{
			SideEffect: true,
			BizKey:     "demo:1001:send_email",
		},
		Options: []TaskOption{WithQueue(QueueLow), WithMaxRetry(1)},
	}

	resolved, err := ResolvePublishOptions(PublisherConfig{EnforceBizKey: true}, item,
		WithQueue(QueueCritical),
		WithTimeout(time.Minute),
		WithHeader("X-Trace-ID", "trace-1"),
	)

	require.NoError(t, err)
	require.Equal(t, QueueCritical, resolved.Queue)
	require.Equal(t, 1, resolved.MaxRetry)
	require.Equal(t, time.Minute, resolved.Timeout)
	require.Equal(t, "trace-1", resolved.Headers["X-Trace-ID"])
	require.Equal(t, "demo:1001:send_email", resolved.BizKey)
}

func TestMarshalAndDecodePayload(t *testing.T) {
	raw := json.RawMessage(`{"userId":"1001"}`)
	item := Task{Type: "demo:send_email", Payload: raw}

	payload, err := DecodePayload[struct {
		UserID idgen.ID `json:"userId"`
	}](item)

	require.NoError(t, err)
	require.Equal(t, idgen.New(1001), payload.UserID)
}
