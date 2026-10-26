package catalog

import (
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/response"
	"time"
)

// TopicVO 表示固定研究方向及真实导入数量。
type TopicVO struct {
	Slug            string `json:"slug"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	Rule            string `json:"rule"`
	MembershipCount int    `json:"membershipCount"`
}

// SourceEvidence 保存来源及原始证据，不把接收日期冒充正式发表日期。
type SourceEvidence struct {
	Source   string         `json:"source"`
	URL      string         `json:"url"`
	DateType string         `json:"dateType"`
	Date     string         `json:"date"`
	Raw      map[string]any `json:"raw"`
}

// MembershipVO 保留主题内稳定标识、分类和原始记录。
type MembershipVO struct {
	Topic           string              `json:"topic"`
	OriginalID      string              `json:"originalId"`
	Tier            string              `json:"tier"`
	PrimaryCategory string              `json:"primaryCategory"`
	Categories      []string            `json:"categories"`
	Dimensions      map[string][]string `json:"dimensions"`
	ReadStatus      string              `json:"readStatus"`
	Source          string              `json:"source"`
	IsMock          bool                `json:"isMock"`
	Raw             map[string]any      `json:"raw"`
}

// PaperVO 为只读论文详情，个人状态与历史元数据分开保存。
type PaperVO struct {
	ID               idgen.ID         `json:"id"`
	Title            string           `json:"title"`
	TitleZh          string           `json:"titleZh"`
	Abstract         string           `json:"abstract"`
	Authors          []string         `json:"authors"`
	Year             int              `json:"year"`
	Venue            string           `json:"venue"`
	PublicationType  string           `json:"publicationType"`
	DOI              string           `json:"doi"`
	ArxivID          string           `json:"arxivId"`
	URL              string           `json:"url"`
	FormalOnlineDate *time.Time       `json:"formalOnlineDate"`
	IssueDate        *time.Time       `json:"issueDate"`
	DateProvenance   string           `json:"dateProvenance"`
	ReadStatus       string           `json:"readStatus"`
	Hidden           bool             `json:"hidden"`
	IsMock           bool             `json:"isMock"`
	Memberships      []MembershipVO   `json:"memberships"`
	Evidence         []SourceEvidence `json:"evidence"`
}

// ImportRecord 是本地历史基线导入 DTO，raw 保存原始来源。
type ImportRecord struct {
	Topic            string              `json:"topic"`
	OriginalID       string              `json:"originalId"`
	Title            string              `json:"title"`
	TitleZh          string              `json:"titleZh"`
	Abstract         string              `json:"abstract"`
	Authors          []string            `json:"authors"`
	Year             int                 `json:"year"`
	Venue            string              `json:"venue"`
	PublicationType  string              `json:"publicationType"`
	DOI              string              `json:"doi"`
	ArxivID          string              `json:"arxivId"`
	URL              string              `json:"url"`
	FormalOnlineDate *time.Time          `json:"formalOnlineDate"`
	IssueDate        *time.Time          `json:"issueDate"`
	DateProvenance   string              `json:"dateProvenance"`
	Tier             string              `json:"tier"`
	PrimaryCategory  string              `json:"primaryCategory"`
	Categories       []string            `json:"categories"`
	Dimensions       map[string][]string `json:"dimensions"`
	ReadStatus       string              `json:"readStatus"`
	Source           string              `json:"source"`
	IsMock           bool                `json:"isMock"`
	Raw              map[string]any      `json:"raw"`
	Evidence         []SourceEvidence    `json:"evidence"`
}

// ImportResult 表示幂等导入结果。
type ImportResult struct {
	PaperID      idgen.ID `json:"paperId"`
	MembershipID idgen.ID `json:"membershipId"`
	Created      bool     `json:"created"`
}

// PaperQuery 是共享查询条件，所有值通过 Ent 参数化。
type PaperQuery struct {
	Topic           string `query:"topic"`
	Q               string `query:"q" maxLength:"300"`
	Year            int    `query:"year" minimum:"0" maximum:"9999"`
	Venue           string `query:"venue" maxLength:"500"`
	PublicationType string `query:"publicationType" maxLength:"200"`
	Category        string `query:"category" maxLength:"200"`
	Tier            string `query:"tier" maxLength:"100"`
	ReadStatus      string `query:"readStatus" maxLength:"100"`
	Task            string `query:"task" maxLength:"200"`
	Role            string `query:"role" maxLength:"200"`
	Direction       string `query:"direction" maxLength:"200"`
	Method          string `query:"method" maxLength:"200"`
	Dimension       string `query:"dimension" maxLength:"2000" doc:"JSON object of dimension key to exact string value"`
	Hidden          string `query:"hidden" default:"exclude" enum:"exclude,include,only"`
	Page            int    `query:"page" default:"1" minimum:"1" maximum:"1000000"`
	PageSize        int    `query:"pageSize" default:"20" minimum:"1" maximum:"100"`
	Sort            string `query:"sort" default:"newest" enum:"newest,oldest,title"`
}

// PaperPageVO 为稳定排序的全局去重分页结果。
type PaperPageVO struct {
	Items    []PaperVO `json:"items"`
	Total    int       `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"pageSize"`
}

// FacetVO 是筛选值和匹配的全局论文数。
type FacetVO struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// FacetsVO 提供后端计算的全部筛选项。
type FacetsVO struct {
	Years            []FacetVO            `json:"years"`
	Venues           []FacetVO            `json:"venues"`
	PublicationTypes []FacetVO            `json:"publicationTypes"`
	Categories       []FacetVO            `json:"categories"`
	Tiers            []FacetVO            `json:"tiers"`
	ReadStatuses     []FacetVO            `json:"readStatuses"`
	Tasks            []FacetVO            `json:"tasks"`
	Roles            []FacetVO            `json:"roles"`
	Directions       []FacetVO            `json:"directions"`
	Methods          []FacetVO            `json:"methods"`
	Dimensions       map[string][]FacetVO `json:"dimensions"`
}

// PreferenceVO 保存当前用户的展示筛选偏好。
type PreferenceVO struct {
	Filters map[string]any `json:"filters"`
}

// PaperStateVO 保存个人隐藏和阅读状态，不改变来源记录。
type PaperStateVO struct {
	Hidden     bool   `json:"hidden"`
	ReadStatus string `json:"readStatus" enum:"unread,reading,read"`
}

// VenueVO 保存版本化资格证据，不由前端推测指标。
type VenueVO struct {
	ID                  idgen.ID       `json:"id"`
	Name                string         `json:"name"`
	Kind                string         `json:"kind"`
	JIF                 *float64       `json:"jif"`
	CCF                 string         `json:"ccf"`
	MetricYear          int            `json:"metricYear"`
	RegistryVersion     string         `json:"registryVersion"`
	Source              string         `json:"source"`
	Evidence            map[string]any `json:"evidence"`
	EligibleTopics      []string       `json:"eligibleTopics"`
	QualificationStatus string         `json:"qualificationStatus"`
}

type topicsResponse struct {
	Body response.SuccessVO[struct {
		Items []TopicVO `json:"items"`
	}]
}
type papersRequest struct{ PaperQuery }

type papersResponse struct {
	Body response.SuccessVO[PaperPageVO]
}
type paperRequest struct {
	ID idgen.ID `path:"id"`
}
type paperResponse struct{ Body response.SuccessVO[PaperVO] }
type facetsResponse struct{ Body response.SuccessVO[FacetsVO] }
type venuesRequest struct {
	Q     string `query:"q" maxLength:"300"`
	Topic string `query:"topic"`
}
type venuesResponse struct {
	Body response.SuccessVO[struct {
		Items []VenueVO `json:"items"`
	}]
}
type preferenceRequest struct{ Body PreferenceVO }
type preferenceResponse struct {
	Body response.SuccessVO[PreferenceVO]
}
type stateRequest struct {
	ID   idgen.ID `path:"id"`
	Body PaperStateVO
}
type stateResponse struct {
	Body response.SuccessVO[PaperStateVO]
}
