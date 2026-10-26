package baseline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// DisplayTaxonomy 是原 HTML 已交付的显示分组表，不是新的科学分类。
type DisplayTaxonomy struct {
	Version      string                         `json:"version"`
	SourceSHA256 string                         `json:"source_sha256"`
	Maps         map[string]map[string][]string `json:"maps"`
}

// ReadDisplayTaxonomy 只解析固定名称后的 JSON 字面量，从不运行 HTML/JavaScript。
func ReadDisplayTaxonomy(html []byte) (*DisplayTaxonomy, []byte, error) {
	marker := []byte("const UI_TAXONOMY=")
	i := bytes.Index(html, marker)
	if i < 0 {
		return nil, nil, nil
	}
	d := json.NewDecoder(bytes.NewReader(html[i+len(marker):]))
	var raw json.RawMessage
	if err := d.Decode(&raw); err != nil {
		return nil, nil, fmt.Errorf("invalid display taxonomy JSON")
	}
	var taxonomy DisplayTaxonomy
	if err := json.Unmarshal(raw, &taxonomy); err != nil || taxonomy.Maps == nil {
		return nil, nil, fmt.Errorf("invalid display taxonomy structure")
	}
	return &taxonomy, raw, nil
}
func applyDisplay(r *Record, taxonomy *DisplayTaxonomy) {
	switch r.Topic {
	case "llm-agent":
		r.Dimensions["display_review_status"] = list(r.Raw["current_review_status"])
		r.Dimensions["display_scope"] = list(r.Raw["scope"])
	case "biomed-ai":
		r.Dimensions["display_review_status"] = list(r.Raw["verification_work_status"])
	case "biomed-worldmodel":
		label := "证据待核"
		if r.Raw["method_checked"] == true {
			label = "已核方法（详见证据）"
		} else if r.Raw["abstract_read"] == true {
			label = "已读摘要"
		} else if r.Raw["fragment_read"] == true || len(list(r.Raw["fulltext_sections_read"])) > 0 {
			label = "已读正文片段（非全文）"
		} else if r.Raw["abstract_read"] == false {
			if r.Raw["abstract_fetched"] == true {
				label = "摘要已获取，待阅读"
			} else {
				label = "仅题录，待阅读"
			}
		}
		r.Dimensions["display_review_status"] = []string{label}
	}

	if taxonomy != nil {
		for _, key := range []string{"type", "scale", "tasks", "roles"} {
			values := list(r.Raw[key])
			pending := map[string]string{"type": "其他／类型待核", "scale": "领域待核", "tasks": "任务待核", "roles": "职责待核"}[key]
			if key == "type" {
				v := text(r.Raw["paper_type"])
				names := map[string]string{"research": "研究论文", "review": "综述", "perspective": "观点", "benchmark": "评测基准", "dataset": "数据集", "other": "其他", "pending": "论文类型待核"}
				if label := names[v]; label != "" {
					v = label
				}
				if v == "" {
					v = firstText(r.Raw, "article_type", "document_type")
				}
				if v == "" {
					if text(r.Raw["category"]) == "D" {
						v = "综述/观点/基准（细类待核）"
					} else {
						v = "论文类型待核"
					}
				}
				values = []string{v}
			}
			labels := []string{}
			seen := map[string]bool{}
			for _, value := range values {
				mapped := taxonomy.Maps[key][value]
				if len(mapped) == 0 {
					mapped = []string{pending}
				}
				for _, label := range mapped {
					if !seen[label] {
						labels = append(labels, label)
						seen[label] = true
					}
				}
			}
			if len(labels) == 0 {
				labels = []string{pending}
			}
			r.Dimensions["display_"+key] = labels
		}
	}
	if r.Topic == "biomed-ai" || r.Topic == "biomed-worldmodel" {
		r.Dimensions["display_scope"] = []string{legacyScope(r)}
	}
	if years := list(r.Raw["publication_years"]); len(years) > 0 {
		r.Dimensions["display_years"] = years
	} else if r.Topic == "biomed-worldmodel" {
		years := []string{}
		seen := map[string]bool{}
		for _, key := range []string{"date_online", "date_issue", "date_formal"} {
			v := text(r.Raw[key])
			if len(v) >= 4 && !seen[v[:4]] {
				years = append(years, v[:4])
				seen[v[:4]] = true
			}
		}
		if len(years) == 0 {
			v := text(r.Raw["date_preprint"])
			if len(v) >= 4 {
				years = []string{v[:4]}
			}
		}
		r.Dimensions["display_years"] = years
	}
}

var specialBiomedTrack = regexp.MustCompile(`(?i)workshop|findings|industry|demo|resources|datasets|工作坊|工业|演示|资源轨道|数据集轨道`)
var specialWorldTrack = regexp.MustCompile(`(?i)workshop|findings|industry|datasets|special track|工作坊|特殊轨道`)

func legacyScope(r *Record) string {
	world := r.Topic == "biomed-worldmodel"
	unknown := "其他／待核"
	if world {
		unknown = "特殊轨道／指标待核"
	}
	track := text(r.Raw["track"])
	if (!world && specialBiomedTrack.MatchString(track)) || (world && specialWorldTrack.MatchString(track)) {
		return unknown
	}
	ccf := text(r.Raw["ccf"])
	conference := r.Raw["is_conference"] == true
	if world {
		conference = text(r.Raw["venue_kind"]) == "conference"
	}
	if conference {
		if ccf == "A" || ccf == "B" || ccf == "C" {
			return "会议 · CCF " + ccf
		}
		if world {
			return unknown
		}
	}
	jif, ok := number(r.Raw["jif"])
	if !ok || jif < 0 {
		return unknown
	}
	if world {
		if jif < 4 {
			return "期刊 · 影响因子低于4"
		}
		if jif > 10 {
			return "期刊 · 影响因子高于10"
		}
		return "期刊 · 影响因子4–10"
	}
	if jif < 4 {
		return "期刊 · JIF < 4"
	}
	if jif > 10 {
		return "期刊 · JIF > 10"
	}
	return "期刊 · JIF 4–10"
}
func number(v any) (float64, bool) {
	switch x := v.(type) {
	case json.Number:
		n, err := x.Float64()
		return n, err == nil
	case float64:
		return x, true
	case string:
		if strings.TrimSpace(x) == "" {
			return 0, false
		}
		n, err := strconv.ParseFloat(x, 64)
		return n, err == nil
	}
	return 0, false
}
