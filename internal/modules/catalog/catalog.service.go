package catalog

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
)

// Service 提供 PostgreSQL/Ent 驱动的目录查询与幂等基线导入。
type Service struct{ client *ent.Client }

// NewService 使用启动层提供的 Ent 客户端构造目录服务。
func NewService(client *ent.Client) *Service { return &Service{client: client} }

var topics = []TopicVO{
	{Slug: "biomed-ai", Name: "生物医学 × AI", Description: "生命科学、医学与人工智能交叉研究", Rule: "JIF > 10；CCF A 主会正式全文"},
	{Slug: "llm-agent", Name: "LLM × Agent", Description: "大语言模型与智能体研究", Rule: "JIF ≥ 4；CCF A/B/C 主会正式全文"},
	{Slug: "biomed-worldmodel", Name: "生物医学 × 世界模型", Description: "生物医学环境建模与可交互预测", Rule: "JIF ≥ 4；CCF A/B/C 主会正式全文"},
}

func validTopic(slug string) bool {
	for _, t := range topics {
		if t.Slug == slug {
			return true
		}
	}
	return false
}
func userID(ctx context.Context) (idgen.ID, error) {
	value, ok := requestctx.UserIDFromContext(ctx)
	if !ok {
		return 0, bizerrors.Unauthorized("login required")
	}
	id, err := idgen.Parse(value)
	if err != nil {
		return 0, bizerrors.Unauthorized("invalid identity")
	}
	return id, nil
}
func validateQuery(q *PaperQuery) error {
	if q.Topic != "" && !validTopic(q.Topic) {
		return bizerrors.BadRequest("unknown topic")
	}
	if q.Page == 0 {
		q.Page = 1
	}
	if q.PageSize == 0 {
		q.PageSize = 20
	}
	if q.Hidden == "" {
		q.Hidden = "exclude"
	}
	if q.Sort == "" {
		q.Sort = "newest"
	}
	if q.Page < 1 || q.Page > 1000000 || q.PageSize < 1 || q.PageSize > 100 {
		return bizerrors.BadRequest("invalid pagination")
	}
	if q.Hidden != "exclude" && q.Hidden != "include" && q.Hidden != "only" {
		return bizerrors.BadRequest("invalid hidden filter")
	}
	if q.Sort != "newest" && q.Sort != "oldest" && q.Sort != "title" {
		return bizerrors.BadRequest("invalid sort")
	}
	if len(q.Q) > 1200 || len(q.Dimension) > 2000 {
		return bizerrors.BadRequest("query too long")
	}
	_, err := queryDimensions(*q)
	return err
}
func queryDimensions(q PaperQuery) (map[string]string, error) {
	out := map[string]string{}
	if q.Dimension != "" {
		if err := json.Unmarshal([]byte(q.Dimension), &out); err != nil {
			return nil, bizerrors.BadRequest("dimension must be a JSON object of string values")
		}
	}
	if out == nil {
		return nil, bizerrors.BadRequest("dimension must be a JSON object")
	}
	if len(out) > 30 {
		return nil, bizerrors.BadRequest("too many dimensions")
	}
	for k, v := range out {
		if len(k) > 100 || len(v) > 300 || k == "" {
			return nil, bizerrors.BadRequest("invalid dimension")
		}
	}
	return out, nil
}

var doiPattern = regexp.MustCompile(`^10\.[0-9]{4,9}/\S+$`)
var arxivPattern = regexp.MustCompile(`^(?:[0-9]{4}\.[0-9]{4,5}|[a-z][a-z.-]+/[0-9]{7})(?:v[0-9]+)?$`)
var arxivVersion = regexp.MustCompile(`v[0-9]+$`)

func normalizeDOI(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, prefix := range []string{"https://doi.org/", "http://doi.org/", "https://dx.doi.org/", "http://dx.doi.org/", "doi:"} {
		value = strings.TrimPrefix(value, prefix)
	}
	value = strings.TrimSpace(value)
	if len(value) > 1024 || !doiPattern.MatchString(value) {
		return ""
	}
	return value
}
func normalizeArxiv(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, prefix := range []string{"https://arxiv.org/abs/", "http://arxiv.org/abs/", "https://arxiv.org/pdf/", "http://arxiv.org/pdf/", "arxiv:"} {
		value = strings.TrimPrefix(value, prefix)
	}
	value = strings.TrimSuffix(value, ".pdf")
	if !arxivPattern.MatchString(value) {
		return ""
	}
	return arxivVersion.ReplaceAllString(value, "")
}
func normalizeImport(r *ImportRecord) error {
	if !validTopic(r.Topic) || strings.TrimSpace(r.OriginalID) == "" || strings.TrimSpace(r.Title) == "" {
		return bizerrors.BadRequest("topic, originalId and title are required")
	}
	if len(r.OriginalID) > 1024 {
		return bizerrors.BadRequest("originalId too long")
	}
	r.DOI = normalizeDOI(r.DOI)
	r.ArxivID = normalizeArxiv(r.ArxivID)
	if r.Authors == nil {
		r.Authors = []string{}
	}
	if r.Categories == nil {
		r.Categories = []string{}
	}
	if r.Dimensions == nil {
		r.Dimensions = map[string][]string{}
	}
	if r.Raw == nil {
		r.Raw = map[string]any{}
	}
	if r.Evidence == nil {
		r.Evidence = []SourceEvidence{}
	}
	if r.Source == "" {
		r.Source = "historical-baseline"
	}
	if r.ReadStatus == "" {
		r.ReadStatus = "unread"
	}
	// 正式日期没有来源说明时拒绝，未知日期只能保存在 raw 中。
	if (r.FormalOnlineDate != nil || r.IssueDate != nil) && strings.TrimSpace(r.DateProvenance) == "" {
		return bizerrors.BadRequest("formal dates require dateProvenance")
	}
	return nil
}
func identityKey(r ImportRecord) string {
	if r.DOI != "" {
		return "doi:" + r.DOI
	}
	if r.ArxivID != "" {
		return "arxiv:" + r.ArxivID
	}
	return "legacy:" + r.Topic + ":" + r.OriginalID
}
