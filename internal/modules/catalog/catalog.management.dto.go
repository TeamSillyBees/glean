package catalog

import (
	"time"

	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/response"
)

// ManualReviewVO 为主题内人工决定，不覆盖原始分类。
type ManualReviewVO struct {
	Status    string    `json:"status"`
	Note      string    `json:"note"`
	Labels    []string  `json:"labels"`
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updatedAt"`
	UpdatedBy *idgen.ID `json:"updatedBy"`
}

// ReviewBody 为一次直接保存的人工审核。
type ReviewBody struct {
	Status          string   `json:"status" enum:"pending,included,excluded"`
	Note            string   `json:"note" maxLength:"4000"`
	Labels          []string `json:"labels" maxItems:"30"`
	ExpectedVersion int      `json:"expectedVersion" minimum:"0"`
}

// CorrectionFields 仅允许修改展示书目，不允许修改去重标识和来源证据。
type CorrectionFields struct {
	Title    *string   `json:"title,omitempty" maxLength:"2000"`
	TitleZh  *string   `json:"titleZh,omitempty" maxLength:"2000"`
	Authors  *[]string `json:"authors,omitempty" maxItems:"100"`
	Year     *int      `json:"year,omitempty" minimum:"0" maximum:"9999"`
	Venue    *string   `json:"venue,omitempty" maxLength:"500"`
	Abstract *string   `json:"abstract,omitempty" maxLength:"30000"`
}

// CorrectionBody 保存完整覆盖集合，省略字段恢复来源值。
type CorrectionBody struct {
	Fields          CorrectionFields `json:"fields"`
	Reason          string           `json:"reason" maxLength:"2000"`
	ExpectedVersion int              `json:"expectedVersion" minimum:"0"`
}

// CorrectionVO 返回共享纠错和乐观锁版本。
type CorrectionVO struct {
	Fields    CorrectionFields `json:"fields"`
	Reason    string           `json:"reason"`
	Version   int              `json:"version"`
	UpdatedAt time.Time        `json:"updatedAt"`
	UpdatedBy *idgen.ID        `json:"updatedBy"`
}

// TopicBody 保存简单主题配置；组内 OR、组间 AND。
type TopicBody struct {
	Slug            string     `json:"slug,omitempty" maxLength:"80"`
	Name            string     `json:"name" minLength:"1" maxLength:"100"`
	Description     string     `json:"description" maxLength:"2000"`
	Rule            string     `json:"rule" maxLength:"4000"`
	SearchEnabled   bool       `json:"searchEnabled"`
	TermGroups      [][]string `json:"termGroups" maxItems:"10"`
	ExpectedVersion int        `json:"expectedVersion" minimum:"0"`
}

// SearchTopicConfig 是给采集装配层的主题快照；内置主题空词组表示默认模板。
type SearchTopicConfig struct {
	Slug          string
	SearchEnabled bool
	TermGroups    [][]string
	Version       int
}

// CatalogAccessVO 表示当前用户是否可以管理共享目录。
type CatalogAccessVO struct {
	CanManage bool `json:"canManage"`
}

// CatalogChangeVO 是不可变更的管理历史。
type CatalogChangeVO struct {
	ID        idgen.ID       `json:"id"`
	Kind      string         `json:"kind"`
	Topic     string         `json:"topic"`
	Before    map[string]any `json:"before"`
	After     map[string]any `json:"after"`
	ActorID   *idgen.ID      `json:"actorId"`
	CreatedAt time.Time      `json:"createdAt"`
}
type catalogHistoryVO struct {
	Items []CatalogChangeVO `json:"items"`
}
type reviewRequest struct {
	ID   idgen.ID `path:"id"`
	Slug string   `path:"slug"`
	Body ReviewBody
}
type correctionRequest struct {
	ID   idgen.ID `path:"id"`
	Body CorrectionBody
}
type createTopicRequest struct{ Body TopicBody }
type updateTopicRequest struct {
	Slug string `path:"slug"`
	Body TopicBody
}
type reviewResponse struct {
	Body response.SuccessVO[ManualReviewVO]
}
type correctionResponse struct {
	Body response.SuccessVO[CorrectionVO]
}
type topicResponse struct{ Body response.SuccessVO[TopicVO] }
type accessResponse struct {
	Body response.SuccessVO[CatalogAccessVO]
}
type historyResponse struct {
	Body response.SuccessVO[catalogHistoryVO]
}
