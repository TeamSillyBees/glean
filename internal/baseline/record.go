// Package baseline 提供历史目录的可审计提取、校验及逐条导入流程。
package baseline

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Record 与 catalog.ImportRecord 的 JSON 合约一致，不依赖数据库实现。
type Record struct {
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
	Evidence         []Evidence          `json:"evidence"`
}

// Evidence 保留原有来源与日期精度，不把获取等同于核读。
type Evidence struct {
	Source   string         `json:"source"`
	URL      string         `json:"url"`
	DateType string         `json:"dateType"`
	Date     string         `json:"date"`
	Raw      map[string]any `json:"raw"`
}

var dimensionKeys = []string{"category", "collection", "paper_types", "technical_tasks", "llm_role", "relation_direction", "medical_flag", "medical_evidence_type", "topic_status", "publication_eligibility", "track", "ccf", "current_review_status", "evidence_level", "scope", "biomedical_domains", "ai_methods", "ai_leadership", "world_model_role", "world_model_type", "model_type", "modeling_objects", "modeling_scales", "dynamics_type", "evidence_status", "primary_category", "secondary_categories", "roles", "scale", "tasks", "paper_type", "scope_decision", "eligibility_status", "inclusion_status", "venue_gate_decision"}
var doiPattern = regexp.MustCompile(`(?i)^10\.[0-9]{4,9}/\S+$`)
var arxivPattern = regexp.MustCompile(`(?i)^(?:[a-z-]+(?:\.[A-Z]{2})?/\d{7}|\d{4}\.\d{4,5})(?:v\d+)?$`)

// NormalizeDOI 只接受显式 DOI 标识，不按标题建立身份。
func NormalizeDOI(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	for _, prefix := range []string{"https://doi.org/", "http://doi.org/", "https://dx.doi.org/", "http://dx.doi.org/", "doi:"} {
		s = strings.TrimPrefix(s, prefix)
	}
	s = strings.TrimSpace(s)
	if !doiPattern.MatchString(s) {
		return ""
	}
	return s
}

// NormalizeArxiv 只接受显式 arXiv 标识并剥离同一论文版本号。
func NormalizeArxiv(s string) string {
	s = strings.TrimSpace(s)
	for _, prefix := range []string{"https://arxiv.org/abs/", "http://arxiv.org/abs/", "https://arxiv.org/pdf/", "http://arxiv.org/pdf/", "arXiv:", "arxiv:"} {
		s = strings.TrimPrefix(s, prefix)
	}
	s = strings.TrimSuffix(s, ".pdf")
	if !arxivPattern.MatchString(s) {
		return ""
	}
	if i := strings.LastIndex(s, "v"); i >= 0 {
		if _, err := strconv.Atoi(s[i+1:]); err == nil {
			s = s[:i]
		}
	}
	return strings.ToLower(s)
}

// Normalize 仅重排已有字段并保守提升明确的正式日级日期；不新增科学判定。
func Normalize(topic string, original map[string]any) (Record, int, error) {
	cleaned, redactions := sanitize(original, "")
	raw := cleaned.(map[string]any)
	r := Record{Topic: topic, OriginalID: text(raw["id"]), Title: firstText(raw, "title", "title_en"), TitleZh: text(raw["title_zh"]), Abstract: firstText(raw, "abstract", "abstract_text"), Authors: list(raw["authors"]), Year: integer(raw["year"]), Venue: text(raw["venue"]), PublicationType: strings.Join(list(raw["paper_types"]), " / "), DOI: NormalizeDOI(text(raw["doi"])), URL: text(raw["url"]), Tier: firstText(raw, "collection", "publication_eligibility", "inclusion_status", "eligibility_status", "tier"), PrimaryCategory: firstText(raw, "category", "primary_category"), Categories: list(raw["category"]), Dimensions: map[string][]string{}, ReadStatus: "unread", Source: "historical-html:2026-10-26", Raw: raw, Evidence: []Evidence{}}
	if r.PublicationType == "" {
		r.PublicationType = text(raw["paper_type"])
	}
	if r.URL == "" {
		if urls := list(raw["urls"]); len(urls) > 0 {
			r.URL = urls[0]
		}
	}
	if r.Year == 0 {
		if d := text(raw["date_formal"]); len(d) >= 4 {
			r.Year, _ = strconv.Atoi(d[:4])
		}
		if r.Year == 0 {
			r.Year = integer(raw["volume_year"])
		}
	}
	if r.OriginalID == "" || r.Title == "" {
		return r, redactions, fmt.Errorf("missing original ID or title")
	}
	switch topic {
	case "llm-agent", "biomed-ai", "biomed-worldmodel":
	default:
		return r, redactions, fmt.Errorf("unknown topic")
	}
	for _, key := range dimensionKeys {
		if values := list(raw[key]); len(values) > 0 {
			r.Dimensions[key] = values
		}
	}
	if len(r.Categories) == 0 {
		r.Categories = list(raw["primary_category"])
	}
	if len(r.Categories) == 0 {
		r.Categories = list(raw["biomedical_domains"])
	}
	if r.PrimaryCategory == "" && len(r.Categories) > 0 {
		r.PrimaryCategory = r.Categories[0]
	}
	for _, key := range []string{"arxiv_id", "arxiv", "arxivId", "url", "doi"} {
		if r.ArxivID == "" {
			r.ArxivID = NormalizeArxiv(text(raw[key]))
		}
	}
	if status := text(raw["read_status"]); status == "unread" || status == "reading" || status == "read" {
		r.ReadStatus = status
	}
	// 审稿/摘要核读状态不是用户阅读状态，也不冒充新的真实或 mock 审稿。
	r.DateProvenance = "historical evidence retained; untyped or partial dates are not promoted"
	typedEventDates := map[string]map[string]bool{}
	for _, key := range []string{"publication_events", "date_online_events", "date_issue_events"} {
		if events, ok := raw[key].([]any); ok {
			for _, item := range events {
				event, ok := item.(map[string]any)
				if !ok {
					continue
				}
				kind := firstText(event, "event", "type")
				date := text(event["date"])
				r.Evidence = append(r.Evidence, Evidence{Source: firstText(event, "source_kind", "source"), URL: firstText(event, "source_url", "url"), DateType: kind, Date: date, Raw: event})
				if event["identity_verified"] != true || event["qualifying_date_type"] != true {
					continue
				}
				if event["precision"] != "day" {
					continue
				}
				parsed, err := time.Parse("2006-01-02", date)
				if err != nil {
					continue
				}
				if kind == "online" || kind == "issue" {
					if typedEventDates[kind] == nil {
						typedEventDates[kind] = map[string]bool{}
					}
					typedEventDates[kind][date] = true
				}
				if kind == "online" && (r.FormalOnlineDate == nil || parsed.Before(*r.FormalOnlineDate)) {
					t := parsed
					r.FormalOnlineDate = &t
				}
				if kind == "issue" && (r.IssueDate == nil || parsed.Before(*r.IssueDate)) {
					t := parsed
					r.IssueDate = &t
				}
			}
		}
	}
	if len(typedEventDates["online"]) > 1 {
		r.FormalOnlineDate = nil
		r.DateProvenance += "; conflicting verified online dates retained without selecting a day"
	}
	if len(typedEventDates["issue"]) > 1 {
		r.IssueDate = nil
		r.DateProvenance += "; conflicting verified issue dates retained without selecting a day"
	}
	for _, kind := range []string{"online", "issue"} {
		key := "date_" + kind
		date := text(raw[key])
		if date == "" {
			continue
		}
		source := text(raw[key+"_source"])
		precision := text(raw[key+"_precision"])
		r.Evidence = append(r.Evidence, Evidence{Source: source, URL: source, DateType: kind, Date: date, Raw: map[string]any{"precision": raw[key+"_precision"], "verified": raw[key+"_verified"], "field": key}})
		if raw[key+"_verified"] != true || (precision != "day" && precision != "日") {
			continue
		}
		parsed, err := time.Parse("2006-01-02", date)
		if err != nil {
			continue
		}
		t := parsed
		if kind == "online" {
			r.FormalOnlineDate = &t
		} else {
			r.IssueDate = &t
		}
	}
	for _, key := range []string{"source_urls", "evidence_urls", "urls"} {
		for _, url := range list(raw[key]) {
			r.Evidence = append(r.Evidence, Evidence{Source: "historical:" + key, URL: url, Raw: map[string]any{}})
		}
	}
	return r, redactions, nil
}
func text(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}
func firstText(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if s := text(m[key]); s != "" {
			return s
		}
	}
	return ""
}
func list(v any) []string {
	values := []string{}
	switch x := v.(type) {
	case string:
		if strings.TrimSpace(x) != "" {
			values = append(values, x)
		}
	case []any:
		for _, item := range x {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				values = append(values, s)
			}
		}
	}
	return values
}
func integer(v any) int {
	switch n := v.(type) {
	case json.Number:
		i, _ := strconv.Atoi(n.String())
		return i
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return 0
}
