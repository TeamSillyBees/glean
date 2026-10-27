package monitor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/response"
)

// QuarantineItemVO 提供隔离记录的有界安全元数据，不代表论文已通过科学核验。
type QuarantineItemVO struct {
	ID              idgen.ID   `json:"id"`
	Source          string     `json:"source"`
	SourceID        string     `json:"sourceId"`
	Title           string     `json:"title"`
	DOI             string     `json:"doi"`
	ArxivID         string     `json:"arxivId"`
	PMID            string     `json:"pmid"`
	Topics          []string   `json:"topics"`
	Reason          string     `json:"reason"`
	QuarantinedAt   time.Time  `json:"quarantinedAt"`
	AcquiredAt      time.Time  `json:"acquiredAt"`
	SourceUpdatedAt time.Time  `json:"sourceUpdatedAt"`
	RunIDs          []idgen.ID `json:"runIds"`
}

// QuarantineListVO 返回稳定排序的隔离记录分页。
type QuarantineListVO struct {
	Items    []QuarantineItemVO `json:"items"`
	Total    int                `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"pageSize"`
}

// QuarantineEvidenceVO 仅提供原始证据摘要及过滤后的元数据预览，原文保留于服务端。
type QuarantineEvidenceVO struct {
	Format    string `json:"format"`
	Bytes     int    `json:"bytes"`
	SHA256    string `json:"sha256"`
	Preview   string `json:"preview"`
	Redacted  bool   `json:"redacted"`
	Truncated bool   `json:"truncated"`
}

// QuarantineDetailVO 不暴露原始响应、请求头、凭据或任意来源地址。
type QuarantineDetailVO struct {
	QuarantineItemVO
	SourceURL string               `json:"sourceUrl"`
	Evidence  QuarantineEvidenceVO `json:"evidence"`
}
type quarantineRequest struct {
	Page     int    `query:"page" default:"1" minimum:"1" maximum:"1000000"`
	PageSize int    `query:"pageSize" default:"20" minimum:"1" maximum:"100"`
	Source   string `query:"source" maxLength:"32"`
}
type quarantineResponse struct {
	Body response.SuccessVO[QuarantineListVO]
}
type quarantineDetailRequest struct {
	ID idgen.ID `path:"id"`
}
type quarantineDetailResponse struct {
	Body response.SuccessVO[QuarantineDetailVO]
}

func knownQuarantineSource(s string) bool {
	switch s {
	case "crossref", "pubmed", "arxiv", "biorxiv", "medrxiv", "europepmc":
		return true
	}
	return false
}

// 不将任意原始内容当作脱敏后原文：仅投影白名单元数据，并保守隐藏疑似凭据文本。
var quarantineSensitive = regexp.MustCompile(`(?i)(authorization|bearer|basic\s|password|passwd|secret|token|api[_ -]?key|credential|://|[?&][^ ]+=)`)
var quarantineIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:()\-]{0,255}$`)

func quarantineText(s string, limit int) string {
	if quarantineSensitive.MatchString(s) {
		return "[redacted]"
	}
	s = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, s)
	if len(s) > limit {
		s = s[:limit]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}
func quarantineID(s string) string {
	if !quarantineIdentifier.MatchString(s) || quarantineSensitive.MatchString(s) {
		return ""
	}
	return s
}
func quarantineItem(c *Candidate, runIDs []idgen.ID) QuarantineItemVO {
	r := c.Record
	source := r.Source
	if !knownQuarantineSource(source) {
		source = "unknown"
	}
	title := quarantineText(r.Title, 512)
	if strings.TrimSpace(title) == "" {
		title = "来源记录未提供可用标题"
	}
	item := QuarantineItemVO{ID: c.ID, Source: source, SourceID: quarantineID(r.SourceID), Title: title, DOI: quarantineID(r.DOI), ArxivID: quarantineID(r.ArxivID), PMID: quarantineID(r.PMID), Topics: []string{}, Reason: safeRejectionKind(c.Quarantine.Reason), QuarantinedAt: c.Quarantine.At, AcquiredAt: r.AcquiredAt, SourceUpdatedAt: r.SourceUpdatedAt, RunIDs: []idgen.ID{}}
	for _, topic := range r.Topics {
		if len(item.Topics) >= 10 {
			break
		}
		item.Topics = append(item.Topics, quarantineText(topic, 80))
	}
	item.RunIDs = append(item.RunIDs, runIDs...)
	return item
}
func (h *Handler) quarantine(ctx context.Context, req *quarantineRequest) (*quarantineResponse, error) {
	if req.Page < 1 || req.Page > 1000000 || req.PageSize < 1 || req.PageSize > 100 || (req.Source != "" && !knownQuarantineSource(req.Source)) {
		return nil, bizerrors.BadRequest("无效的隔离记录分页或来源")
	}
	snapshot, err := h.service.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	return &quarantineResponse{Body: response.OK(ctx, quarantinePage(snapshot, req))}, nil
}

type quarantineReference struct {
	key       string
	candidate *Candidate
}

// 先分页引用，再读取页内元数据；运行关联只扫描一次，避免隔离项累计后的平方级开销。
func quarantinePage(snapshot Snapshot, req *quarantineRequest) QuarantineListVO {
	refs := []quarantineReference{}
	for key, c := range snapshot.Records {
		if c != nil && c.Quarantine != nil && (req.Source == "" || c.Record.Source == req.Source) {
			refs = append(refs, quarantineReference{key: key, candidate: c})
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		a, b := refs[i].candidate, refs[j].candidate
		if a.Quarantine.At.Equal(b.Quarantine.At) {
			return a.ID > b.ID
		}
		return a.Quarantine.At.After(b.Quarantine.At)
	})
	total := len(refs)
	start := (req.Page - 1) * req.PageSize
	if start > total {
		start = total
	}
	end := start + req.PageSize
	if end > total {
		end = total
	}
	page := refs[start:end]
	associations := quarantineRunAssociations(snapshot.Runs, page)
	items := make([]QuarantineItemVO, 0, len(page))
	for _, ref := range page {
		items = append(items, quarantineItem(ref.candidate, associations[ref.key]))
	}
	return QuarantineListVO{Items: items, Total: total, Page: req.Page, PageSize: req.PageSize}
}

func quarantineRunAssociations(runs []Run, selected []quarantineReference) map[string][]idgen.ID {
	result := make(map[string][]idgen.ID, len(selected))
	for _, ref := range selected {
		result[ref.key] = []idgen.ID{}
	}
	if len(result) == 0 {
		return result
	}
	for _, run := range runs {
		found := make(map[string]bool, len(selected))
		collect := func(keys []string) {
			for _, key := range keys {
				if _, ok := result[key]; ok {
					found[key] = true
				}
			}
		}
		for _, p := range run.Sources {
			if p != nil {
				collect(p.CandidateKeys)
			}
		}
		// 恢复运行也可能保留失败原文关联；只报告当前仍保留的运行，不推断已清理的历史。
		for _, keys := range run.RetryCandidateKeys {
			collect(keys)
		}
		for key := range found {
			result[key] = append(result[key], run.ID)
		}
	}
	for key := range result {
		sort.Slice(result[key], func(i, j int) bool { return result[key][i] > result[key][j] })
	}
	return result
}

func (h *Handler) quarantineDetail(ctx context.Context, req *quarantineDetailRequest) (*quarantineDetailResponse, error) {
	snapshot, err := h.service.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	for key, c := range snapshot.Records {
		if c == nil || c.Quarantine == nil || c.ID != req.ID {
			continue
		}
		associations := quarantineRunAssociations(snapshot.Runs, []quarantineReference{{key: key, candidate: c}})
		item := quarantineItem(c, associations[key])
		raw := []byte(c.Record.Raw)
		if len(c.Quarantine.RawEvidence) > 0 {
			raw = c.Quarantine.RawEvidence
		}
		sum := sha256.Sum256(raw)
		format := "text"
		if json.Valid(raw) {
			format = "json"
		}
		if len(raw) == 0 {
			format = "empty"
		}
		// 预览永远不是原始响应；避免未知嵌套字段、XML 和错误页泄漏密钥。
		preview, _ := json.Marshal(struct {
			Title    string `json:"title"`
			Source   string `json:"source"`
			SourceID string `json:"sourceId"`
			Reason   string `json:"reason"`
		}{item.Title, item.Source, item.SourceID, item.Reason})
		sourceURL := ""
		if item.DOI != "" && strings.HasPrefix(item.DOI, "10.") {
			sourceURL = "https://doi.org/" + url.PathEscape(item.DOI)
		}
		detail := QuarantineDetailVO{QuarantineItemVO: item, SourceURL: sourceURL, Evidence: QuarantineEvidenceVO{Format: format, Bytes: len(raw), SHA256: hex.EncodeToString(sum[:]), Preview: string(preview), Redacted: true, Truncated: len(c.Record.Title) > 512 || len(c.Record.SourceID) > 256}}
		return &quarantineDetailResponse{Body: response.OK(ctx, detail)}, nil
	}
	return nil, bizerrors.WrapNotFoundContext(ctx, errors.New("quarantine record not found"), "隔离记录不存在")
}
