package taskdemo

import (
	"context"
	"fmt"
	"strings"

	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/task"
)

const sendEmailTaskType = "demo:send_email"

type sendEmailPayload struct {
	UserID idgen.ID `json:"userId"`
	Email  string   `json:"email"`
}

// taskPublisher 定义 taskdemo 示例模块依赖的最小任务发布能力。
type taskPublisher interface {
	Publish(ctx context.Context, item task.Task, opts ...task.PublishOption) (*task.PublishResult, error)
}

// Service 是 taskdemo 示例模块的应用服务。
type Service struct {
	publisher taskPublisher
}

// NewService 构造 taskdemo 示例模块应用服务。
func NewService(publisher taskPublisher) *Service {
	return &Service{publisher: publisher}
}

// PublishEmail 发布 demo:send_email 异步任务。
func (s *Service) PublishEmail(ctx context.Context, body PublishEmailBody, traceID string) (PublishedTaskVO, error) {
	if s == nil || s.publisher == nil {
		return PublishedTaskVO{}, bizerrors.Internal("task publisher is not configured")
	}
	email := strings.TrimSpace(body.Email)
	if body.UserID <= 0 {
		return PublishedTaskVO{}, bizerrors.BadRequest("userId 不能为空")
	}
	if email == "" {
		return PublishedTaskVO{}, bizerrors.BadRequest("email 不能为空")
	}

	bizKey := fmt.Sprintf("demo:%s:send_email", body.UserID.String())
	result, err := s.publisher.Publish(ctx, task.Task{
		Type: sendEmailTaskType,
		Payload: sendEmailPayload{
			UserID: body.UserID,
			Email:  email,
		},
		Meta: task.TaskMeta{
			Name:           "发送示例邮件",
			Description:    "演示从 HTTP 请求发布异步任务",
			Module:         "demo",
			Owner:          "platform",
			Scenario:       "taskdemo",
			BizKey:         bizKey,
			BizKeyRequired: true,
			SideEffect:     true,
			CostLevel:      task.CostLevelLow,
			Idempotent:     true,
			TraceID:        strings.TrimSpace(traceID),
		},
	}, task.WithQueue(task.QueueDefault), task.WithMaxRetry(3), task.WithBizKey(bizKey))
	if err != nil {
		return PublishedTaskVO{}, bizerrors.WrapTaskContext(ctx, err, "publish demo email task failed")
	}
	return PublishedTaskVO{
		TaskID: result.TaskID,
		Type:   result.Type,
		Queue:  result.Queue,
		State:  result.State,
		BizKey: result.BizKey,
	}, nil
}
