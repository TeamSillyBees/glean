package catalog

import (
	"encoding/json"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/response"
	"github.com/teamsillybees/glean/internal/retrieval"
	"time"
)

// TopicVO 表示兼容内置主题的动态研究方向及真实目录数量。
type TopicVO struct {
	SearchProtocol  *retrieval.Protocol `json:"searchProtocol,omitempty"`
	SearchEnabled   bool                `json:"searchEnabled"`
	TermGroups      [][]string          `json:"termGroups"`
	SearchRoutes    []retrieval.Route   `json:"searchRoutes,omitempty"`
	Version         int                 `json:"version"`
	Slug            string              `json:"slug"`
	Name            string              `json:"name"`
	Description     string              `json:"description"`
	Rule            string              `json:"rule"`
	MembershipCount int                 `json:"membershipCount"`
	HistoricalCount int                 `json:"historicalCount"`
	CandidateCount  int                 `json:"candidateCount"`
	CuratedCount    int                 `json:"curatedCount"`
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
	EffectiveStatus string              `json:"effectiveStatus,omitempty"`
	ManualReview    *ManualReviewVO     `json:"manualReview,omitempty"`
	ReadingNotes    map[string]string   `json:"readingNotes,omitempty"`
	InclusionStatus string              `json:"inclusionStatus"`
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

// PaperVO 返回有效书目与来源详情；个人状态和人工覆盖层独立保存。
type PaperVO struct {
	Correction            *CorrectionVO         `json:"correction,omitempty"`
	AbstractEnrichment    *AbstractEnrichmentVO `json:"abstractEnrichment,omitempty"`
	Origin                string                `json:"origin"`
	ImportedAt            *time.Time            `json:"importedAt,omitempty"`
	FirstSeenAt           *time.Time            `json:"firstSeenAt,omitempty"`
	LatestSourceUpdatedAt *time.Time            `json:"latestSourceUpdatedAt,omitempty"`
	AbstractZh            string                `json:"abstractZh,omitempty"`
	TranslationStatus     string                `json:"translationStatus,omitempty"`
	Review                *ReviewVO             `json:"review,omitempty"`
	LastObservedAt        *time.Time            `json:"lastObservedAt,omitempty"`
	ObservationCount      int                   `json:"observationCount"`
	Observations          []SourceObservationVO `json:"observations"`
	ID                    idgen.ID              `json:"id"`
	Title                 string                `json:"title"`
	TitleZh               string                `json:"titleZh"`
	Abstract              string                `json:"abstract"`
	Authors               []string              `json:"authors"`
	Year                  int                   `json:"year"`
	Venue                 string                `json:"venue"`
	PublicationType       string                `json:"publicationType"`
	DOI                   string                `json:"doi"`
	ArxivID               string                `json:"arxivId"`
	URL                   string                `json:"url"`
	FormalOnlineDate      *time.Time            `json:"formalOnlineDate"`
	IssueDate             *time.Time            `json:"issueDate"`
	DateProvenance        string                `json:"dateProvenance"`
	ReadStatus            string                `json:"readStatus"`
	Hidden                bool                  `json:"hidden"`
	IsMock                bool                  `json:"isMock"`
	Memberships           []MembershipVO        `json:"memberships"`
	Evidence              []SourceEvidence      `json:"evidence"`
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
	CollectionScope string `query:"collectionScope" enum:"all,curated,pending,excluded"`
	InclusionStatus string `query:"inclusionStatus" enum:"historical,pending-review,pending,included,excluded"`
	Source          string `query:"source" maxLength:"100"`
	Topic           string `query:"topic" maxLength:"80"`
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
	Sort            string `query:"sort" default:"newest" enum:"newest,oldest,title,observed,source-updated,model-priority"`
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
	Sources           []FacetVO            `json:"sources"`
	InclusionStatuses []FacetVO            `json:"inclusionStatuses"`
	Years             []FacetVO            `json:"years"`
	Venues            []FacetVO            `json:"venues"`
	PublicationTypes  []FacetVO            `json:"publicationTypes"`
	Categories        []FacetVO            `json:"categories"`
	Tiers             []FacetVO            `json:"tiers"`
	ReadStatuses      []FacetVO            `json:"readStatuses"`
	Tasks             []FacetVO            `json:"tasks"`
	Roles             []FacetVO            `json:"roles"`
	Directions        []FacetVO            `json:"directions"`
	Methods           []FacetVO            `json:"methods"`
	Dimensions        map[string][]FacetVO `json:"dimensions"`
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
	ISSNs               []string       `json:"issns"`
	Aliases             []string       `json:"aliases"`
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
	Topic string `query:"topic" maxLength:"80"`
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

// ReviewVO 将模型处理输出与真实来源和人工科学纳入判定严格分离。
type ReviewVO struct {
	Pipeline           json.RawMessage `json:"pipeline,omitempty"`
	Provider           string          `json:"provider"`
	Model              string          `json:"model"`
	PromptVersion      string          `json:"promptVersion"`
	ReviewedAt         time.Time       `json:"reviewedAt"`
	IsMock             bool            `json:"isMock"`
	ScientificReviewed bool            `json:"scientificReviewed"`
	InclusionStatus    string          `json:"inclusionStatus"`
	Summary            string          `json:"summary"`
}

// PublicationEventVO 保留来源明示的日期类型，不用接收日期替代在线/卷期日期。
type PublicationEventVO struct {
	Kind     string    `json:"kind"`
	Date     time.Time `json:"date"`
	Evidence string    `json:"evidence"`
}

// ObservationInput 是真实来源向目录提交的版本化证据，不等于科学纳入批准。
type ObservationInput struct {
	Discoveries       []retrieval.Hit      `json:"discoveries,omitempty"`
	Pipeline          json.RawMessage      `json:"pipeline,omitempty"`
	Kind              string               `json:"kind,omitempty"`
	AbstractEvidence  *AbstractEvidenceVO  `json:"abstractEvidence,omitempty"`
	CoreDiscovery     bool                 `json:"coreDiscovery,omitempty"`
	EventType         string               `json:"eventType"`
	SourceURL         string               `json:"sourceUrl"`
	PMID              string               `json:"pmid,omitempty"`
	RunID             idgen.ID             `json:"runId"`
	Source            string               `json:"source"`
	SourceID          string               `json:"sourceId"`
	Version           string               `json:"version"`
	Title             string               `json:"title"`
	Abstract          string               `json:"abstract"`
	URL               string               `json:"url"`
	DOI               string               `json:"doi"`
	ArxivID           string               `json:"arxivId"`
	Venue             string               `json:"venue"`
	ISSNs             []string             `json:"issns"`
	Authors           []string             `json:"authors"`
	PublicationType   string               `json:"publicationType"`
	ObservedAt        time.Time            `json:"observedAt"`
	SourceUpdatedAt   *time.Time           `json:"sourceUpdatedAt"`
	PublicationEvents []PublicationEventVO `json:"publicationEvents"`
	Review            ReviewVO             `json:"review"`
	Topics            []string             `json:"topics"`
	TemplateVersion   string               `json:"templateVersion"`
	Raw               json.RawMessage      `json:"raw"`
}

// AbstractEvidenceVO 只公开已核对来源的摘要证据，不包含本机缓存路径或采集请求头。
type AbstractEvidenceVO struct {
	Source             string    `json:"source"`
	SourceID           string    `json:"sourceId"`
	SourceURL          string    `json:"sourceUrl"`
	DOI                string    `json:"doi"`
	SourceVersion      string    `json:"sourceVersion"`
	Completeness       string    `json:"completeness"`
	VerificationStatus string    `json:"verificationStatus"`
	AbstractHash       string    `json:"abstractHash"`
	ResponseHash       string    `json:"responseHash"`
	ParserVersion      string    `json:"parserVersion"`
	SourceField        string    `json:"sourceField"`
	EnrichedAt         time.Time `json:"enrichedAt"`
}

// AbstractEnrichmentVO 为独立的可追溯摘要覆盖层，不替换历史 canonical 摘要。
type AbstractEnrichmentVO struct {
	AbstractEvidenceVO
	ObservationID idgen.ID `json:"observationId"`
	Abstract      string   `json:"abstract"`
}

// SourceObservationVO 是不可覆盖的来源版本及独立审核标记。
type SourceObservationVO struct {
	DiscoveriesHasMore bool `json:"discoveriesHasMore"`
	ObservationInput
	ID          idgen.ID `json:"id"`
	ContentHash string   `json:"contentHash"`
}

// ObservationResult 返回原子投影的幂等结果。
type ObservationResult struct {
	PaperID          idgen.ID `json:"paperId"`
	ObservationID    idgen.ID `json:"observationId"`
	Created          bool     `json:"created"`
	MembershipsAdded int      `json:"membershipsAdded"`
}

// CoreJournalVO 提供真实注册证据用于核心来源发现，不能将发现范围误作科学结论。
type CoreJournalVO struct {
	DiscoveryTopics       []string `json:"discoveryTopics"`
	QualificationVerified bool     `json:"qualificationVerified"`
	Name                  string   `json:"name"`
	ISSNs                 []string `json:"issns"`
	JIF                   *float64 `json:"jif"`
	MetricYear            int      `json:"metricYear"`
	MetricSource          string   `json:"metricSource"`
	EligibleTopics        []string `json:"eligibleTopics"`
	RegistryVersion       string   `json:"registryVersion"`
}
