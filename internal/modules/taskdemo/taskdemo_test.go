package taskdemo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/task"
)

func TestServicePublishEmail(t *testing.T) {
	publisher := &fakeTaskPublisher{}
	service := NewService(publisher)

	result, err := service.PublishEmail(context.Background(), PublishEmailBody{
		UserID: idgen.New(1001),
		Email:  "alice@example.com",
	}, "trace-1")

	require.NoError(t, err)
	require.Equal(t, sendEmailTaskType, publisher.item.Type)
	require.Equal(t, "demo:1001:send_email", publisher.item.Meta.BizKey)
	require.True(t, publisher.item.Meta.SideEffect)
	require.Equal(t, task.QueueDefault, publisher.queue)
	require.Equal(t, "task-1", result.TaskID)
	require.Equal(t, "demo:1001:send_email", result.BizKey)
}

func TestServicePublishEmailValidatesInput(t *testing.T) {
	service := NewService(&fakeTaskPublisher{})

	_, err := service.PublishEmail(context.Background(), PublishEmailBody{Email: "alice@example.com"}, "")

	require.Error(t, err)
}

type fakeTaskPublisher struct {
	item  task.Task
	queue string
}

func (f *fakeTaskPublisher) Publish(_ context.Context, item task.Task, opts ...task.PublishOption) (*task.PublishResult, error) {
	f.item = item
	options, err := task.ResolvePublishOptions(task.PublisherConfig{DefaultQueue: task.QueueDefault, EnforceBizKey: true}, item, opts...)
	if err != nil {
		return nil, err
	}
	f.queue = options.Queue
	return &task.PublishResult{
		TaskID: "task-1",
		Type:   item.Type,
		Queue:  options.Queue,
		State:  "pending",
		BizKey: options.BizKey,
	}, nil
}
