package retrieval

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// CriterionEvidence 保留未知与明确否定的区别；短证据必须能定位到主源。
type CriterionEvidence struct {
	Decision  string `json:"decision"`
	Evidence  string `json:"evidence"`
	SourceURL string `json:"source_url"`
	Location  string `json:"location"`
}

// ScreeningEvidence 保存当前 profile 的实际审读和分项资格依据。
type ScreeningEvidence struct {
	ProfileID        string                       `json:"profile_id"`
	ProfileVersion   string                       `json:"profile_version"`
	ReadingLevel     string                       `json:"reading_level"`
	RequiresFulltext bool                         `json:"requires_fulltext"`
	IdentityBasis    string                       `json:"identity_basis"`
	ResearchObject   string                       `json:"research_object"`
	MethodRole       string                       `json:"method_role"`
	TopicRole        string                       `json:"topic_role"`
	Criteria         map[string]CriterionEvidence `json:"criteria"`
}

// ValidateScreening 阻止取得、排序和未知项直接提升为正式纳入或排除。
func ValidateScreening(status, reason string, e *ScreeningEvidence) error {
	if status == "pending" && e == nil {
		return nil
	}
	if strings.TrimSpace(reason) == "" && status != "pending" {
		return fmt.Errorf("纳入或排除必须填写具体审核理由")
	}
	if e == nil {
		return fmt.Errorf("纳入或排除需要可追溯的分项证据；证据不足时请保存为待核")
	}
	raw, err := json.Marshal(e)
	if err != nil || len(raw) > 64<<10 {
		return fmt.Errorf("审核证据超过64 KiB")
	}
	if err = validateSafeJSON(raw); err != nil {
		return err
	}
	if e.ProfileID == "" || e.ProfileVersion == "" || len(e.Criteria) > 10 {
		return fmt.Errorf("审核证据缺少 profile 和规则版本")
	}
	switch e.ReadingLevel {
	case "metadata", "abstract_available", "abstract_reviewed", "fulltext_fragment_reviewed", "fulltext_accessible", "fulltext_reviewed":
	default:
		return fmt.Errorf("无效的实际阅读层级")
	}
	negative := false
	for key, c := range e.Criteria {
		switch key {
		case "identity", "topic", "method", "date", "venue", "document_type", "version":
		default:
			return fmt.Errorf("未知审核分项")
		}
		switch c.Decision {
		case "pass", "fail", "unknown", "not_applicable":
		default:
			return fmt.Errorf("审核分项必须明确通过、否定、未知或不适用")
		}
		if len(c.Evidence) > 4000 || len(c.SourceURL) > 2000 || len(c.Location) > 1000 {
			return fmt.Errorf("审核证据过长")
		}
		if c.Decision == "fail" {
			negative = true
		}
		if c.Decision != "unknown" && (strings.TrimSpace(c.Evidence) == "" || (c.Decision != "not_applicable" && (strings.TrimSpace(c.SourceURL) == "" || strings.TrimSpace(c.Location) == ""))) {
			return fmt.Errorf("明确结论必须提供短证据、主源与定位")
		}
		if c.SourceURL != "" {
			u, err := url.Parse(c.SourceURL)
			if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
				return fmt.Errorf("审核证据主源必须是无凭据的 HTTP(S) 链接")
			}
		}
		if c.Decision == "not_applicable" && key != "venue" {
			return fmt.Errorf("仅未启用的载体门槛可以声明不适用")
		}
	}
	if status == "excluded" && !negative {
		return fmt.Errorf("排除需要至少一项明确否定证据；unknown 不能当作不满足")
	}
	if status == "included" {
		for _, key := range []string{"identity", "topic", "method", "date", "venue", "document_type", "version"} {
			c, ok := e.Criteria[key]
			if !ok || (c.Decision != "pass" && !(key == "venue" && c.Decision == "not_applicable")) {
				return fmt.Errorf("正式纳入需要身份、主题、方法、日期、载体、文类与版本证据全部满足")
			}
		}
		if e.IdentityBasis == "" || e.ResearchObject == "" || e.MethodRole == "" {
			return fmt.Errorf("正式纳入需要明确身份依据、研究对象与方法角色")
		}
		if e.ReadingLevel != "abstract_reviewed" && e.ReadingLevel != "fulltext_fragment_reviewed" && e.ReadingLevel != "fulltext_reviewed" {
			return fmt.Errorf("取得题名、摘要或全文不等于完成方法核验；请保存为待核")
		}
		if e.RequiresFulltext && e.ReadingLevel != "fulltext_reviewed" {
			return fmt.Errorf("此判断需要全文审读，当前证据不足")
		}
	}
	return nil
}
