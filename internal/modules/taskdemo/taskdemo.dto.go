package taskdemo

import (
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/response"
)

// PublishedTaskVO 是任务发布结果对外 JSON DTO。
type PublishedTaskVO struct {
	TaskID string `json:"taskId"`
	Type   string `json:"type"`
	Queue  string `json:"queue"`
	State  string `json:"state"`
	BizKey string `json:"bizKey"`
}

// PublishEmailBody 是发布示例邮件任务的请求体。
type PublishEmailBody struct {
	UserID idgen.ID `json:"userId" example:"1771234567890123456" doc:"业务用户 ID"`
	Email  string   `json:"email" example:"alice@example.com" doc:"示例邮件收件地址"`
}

type publishEmailRequest struct {
	Body PublishEmailBody
}

type publishEmailResponse struct {
	Body response.SuccessVO[PublishedTaskVO]
}
