package monitor

import (
	"time"

	"github.com/teamsillybees/glean/internal/platform/response"
)

// SourceConnectionVO 描述一次有界来源连通性测试，不代表采集完成或数据已入库。
type SourceConnectionVO struct {
	Source     string     `json:"source"`
	Status     string     `json:"status" enum:"connected,timeout,network_error,blocked,rate_limited,unavailable,invalid_response,deferred,skipped"`
	Message    string     `json:"message"`
	StatusCode int        `json:"statusCode,omitempty"`
	ElapsedMS  int64      `json:"elapsedMs"`
	CheckedAt  time.Time  `json:"checkedAt"`
	RetryAt    *time.Time `json:"retryAt,omitempty"`
}

// SourceConnectionsVO 返回当前服务器对已配置来源的即时检测结果。
type SourceConnectionsVO struct {
	Items []SourceConnectionVO `json:"items"`
}

type sourceConnectionsRequest struct{}
type sourceConnectionsResponse struct {
	CacheControl string `header:"Cache-Control"`
	Body         response.SuccessVO[SourceConnectionsVO]
}
