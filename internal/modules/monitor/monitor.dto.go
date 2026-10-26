package monitor

import (
	"context"
	"encoding/json"
	"time"

	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/response"
)

// Window 表示半开采集时间窗；End 在运行开始时固定，不能随翻页移动。
type Window struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// PublicationEvent 保存来源声明的出版事件，不能用采集、索引或接受日期替代。
type PublicationEvent struct {
	Kind     string    `json:"kind"`
	Date     time.Time `json:"date"`
	Evidence string    `json:"evidence"`
}

// Record 保存一条来源版本及原始证据，AcquiredAt 不表示正式发表日期。
type Record struct {
	Source            string             `json:"source"`
	SourceID          string             `json:"sourceId"`
	DOI               string             `json:"doi"`
	Title             string             `json:"title"`
	Abstract          string             `json:"abstract"`
	URL               string             `json:"url"`
	Journal           string             `json:"journal"`
	Version           string             `json:"version"`
	Topics            []string           `json:"topics"`
	ISSNs             []string           `json:"issns"`
	SourceUpdatedAt   time.Time          `json:"sourceUpdatedAt"`
	AcquiredAt        time.Time          `json:"acquiredAt"`
	PublicationEvents []PublicationEvent `json:"publicationEvents"`
	Raw               json.RawMessage    `json:"raw"`
}

// SearchTemplate 是各来源共用的版本化检索意图，来源适配器只翻译语法。
type SearchTemplate struct {
	ID         string          `json:"id"`
	Version    string          `json:"version"`
	Topic      string          `json:"topic"`
	Query      string          `json:"query"`
	Terms      []string        `json:"terms"`
	TermGroups [][]string      `json:"termGroups"`
	Core       bool            `json:"core"`
	Policy     InclusionPolicy `json:"policy"`
}

// InclusionPolicy 描述纳入门槛；检索命中不代表满足门槛。
type InclusionPolicy struct {
	JIFMinimum        float64  `json:"jifMinimum"`
	JIFStrict         bool     `json:"jifStrict"`
	CCF               []string `json:"ccf"`
	FullPaperRequired bool     `json:"fullPaperRequired"`
	MainTrackRequired bool     `json:"mainTrackRequired"`
}

// FetchRequest 指定一次有限翻页操作，不授权适配器自行重试。
type FetchRequest struct {
	Window   Window
	Cursor   string
	Template SearchTemplate
}

// Page 仅在分页证据完整时标记 Complete。
type Page struct {
	Records    []Record
	NextCursor string
	Complete   bool
}

// Source 是可注入的来源采集接口。
type Source interface {
	Name() string
	Fetch(context.Context, FetchRequest) (Page, error)
}

// Review 是明确区分模拟与真实审核的版本化输出。
type Review struct {
	Provider           string    `json:"provider"`
	Model              string    `json:"model"`
	PromptVersion      string    `json:"promptVersion"`
	ReviewedAt         time.Time `json:"reviewedAt"`
	IsMock             bool      `json:"isMock"`
	ScientificReviewed bool      `json:"scientificReviewed"`
	InclusionStatus    string    `json:"inclusionStatus"`
	Summary            string    `json:"summary"`
}

// Reviewer 允许后续替换真实模型，不需要现在配置凭据。
type Reviewer interface {
	Review(context.Context, Record) (Review, error)
}

// Publication 是向查询目录投影的幂等单元；不是正式发表判定。
type Publication struct {
	Key    string `json:"key"`
	Record Record `json:"record"`
	Review Review `json:"review"`
}

// Publisher 必须按 Key 幂等写入目录；重试可重复传入同一批记录。
type Publisher interface {
	Publish(context.Context, idgen.ID, []Publication) error
}

// Candidate 持久化来源版本、审核和目录投影状态，避免失败重试丢失证据。
type Candidate struct {
	ID        idgen.ID `json:"id"`
	Key       string   `json:"key"`
	Identity  string   `json:"identity"`
	Record    Record   `json:"record"`
	Review    *Review  `json:"review,omitempty"`
	Published bool     `json:"published"`
}

// Checkpoint 分离采集、审核与目录投影水位。
type Checkpoint struct {
	Source              string    `json:"source"`
	BootstrapFrom       time.Time `json:"bootstrapFrom"`
	AcquiredThrough     time.Time `json:"acquiredThrough"`
	ReviewedThrough     time.Time `json:"reviewedThrough"`
	PublishedThrough    time.Time `json:"publishedThrough"`
	NextRetryAt         time.Time `json:"nextRetryAt"`
	LastError           string    `json:"lastError,omitempty"`
	ConsecutiveFailures int       `json:"consecutiveFailures"`
}

// SourceProgress 保存单次运行每个来源的可恢复游标。
type SourceProgress struct {
	Window        Window   `json:"window"`
	TemplateIndex int      `json:"templateIndex"`
	Cursor        string   `json:"cursor,omitempty"`
	CandidateKeys []string `json:"candidateKeys"`
	Acquired      bool     `json:"acquired"`
	Reviewed      bool     `json:"reviewed"`
	Published     bool     `json:"published"`
	Failed        bool     `json:"failed"`
	Attempts      int      `json:"attempts"`
	Pages         int      `json:"pages"`
	LastError     string   `json:"lastError,omitempty"`
}

// Run 保存一次固定水位的运行，取消或退避后恢复同一 ID。
type Run struct {
	ID              idgen.ID                   `json:"id"`
	Number          int                        `json:"number"`
	StartedAt       time.Time                  `json:"startedAt"`
	FixedEnd        time.Time                  `json:"fixedEnd"`
	FinishedAt      time.Time                  `json:"finishedAt"`
	Status          string                     `json:"status"`
	Mode            string                     `json:"mode"`
	TemplateVersion string                     `json:"templateVersion"`
	Sources         map[string]*SourceProgress `json:"sources"`
}

// Schedule 保存 UTC 锚点，防止进程重启或运行耗时造成计划漂移。
type Schedule struct {
	AnchorUTC        time.Time `json:"anchorUtc"`
	NextRunUTC       time.Time `json:"nextRunUtc"`
	EveryHours       int       `json:"everyHours"`
	CompletedRuns    int       `json:"completedRuns"`
	RealFetchEnabled bool      `json:"realFetchEnabled"`
}

// Snapshot 是持久化监控状态及查询视图；lease 仅用于内部并发保护。
type Snapshot struct {
	FormatVersion int                    `json:"formatVersion"`
	Schedule      Schedule               `json:"schedule"`
	Runs          []Run                  `json:"runs"`
	Sources       map[string]*Checkpoint `json:"sources"`
	Records       map[string]*Candidate  `json:"records"`
	LeaseOwner    string                 `json:"-"`
	LeaseUntil    time.Time              `json:"-"`
}

// JournalEntry 以 ISSN 为稳定匹配键，指标必须带可追溯年份与来源。
type JournalEntry struct {
	Name         string   `json:"name"`
	ISSNs        []string `json:"issns"`
	JIF          *float64 `json:"jif,omitempty"`
	MetricYear   int      `json:"metricYear,omitempty"`
	MetricSource string   `json:"metricSource,omitempty"`
	IsMock       bool     `json:"isMock"`
}

// SourcesVO 返回来源进度及不会自动启用外部采集的计划。
type SourcesVO struct {
	Items    []Checkpoint `json:"items"`
	Schedule Schedule     `json:"schedule"`
}

// RunsVO 返回运行历史，包含失败与恢复信息。
type RunsVO struct {
	Items []Run `json:"items"`
}

// TemplatesVO 返回当前统一检索模板。
type TemplatesVO struct {
	Items   []SearchTemplate `json:"items"`
	Version string           `json:"version"`
}

type sourcesRequest struct{}
type sourcesResponse struct{ Body response.SuccessVO[SourcesVO] }
type runsRequest struct{}
type runsResponse struct{ Body response.SuccessVO[RunsVO] }
type templatesRequest struct{}
type templatesResponse struct {
	Body response.SuccessVO[TemplatesVO]
}
