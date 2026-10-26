package catalog

import (
	"encoding/json"
	"strconv"
	"strings"
)

func historicalCuratedDimensions(topic string) map[string][]string {
	key := map[string]string{"biomed-ai": "collection", "llm-agent": "display_collection", "biomed-worldmodel": "eligibility_status"}[topic]
	if key == "" {
		return nil
	}
	return map[string][]string{key: {"正式纳入"}}
}

// historicalLLMCollection 精确复用已交付 HTML 的 collection 显示规则，不生成新的科学判定。
func historicalLLMCollection(raw map[string]any) string {
	eligibility, _ := raw["publication_eligibility"].(string)
	topic, _ := raw["topic_status"].(string)
	status, _ := raw["status"].(string)
	if eligibility == "符合" && topic == "已核相关或参考" {
		return "正式纳入"
	}
	if rawYear, exists := raw["year"]; exists {
		var year float64
		valid := true
		switch value := rawYear.(type) {
		case nil:
			year = 0
		case float64:
			year = value
		case int:
			year = float64(value)
		case json.Number:
			n, err := value.Float64()
			year = n
			valid = err == nil
		case string:
			if strings.TrimSpace(value) != "" {
				n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
				year = n
				valid = err == nil
			}
		case bool:
			if value {
				year = 1
			}
		default:
			valid = false
		}
		if valid && year < 2025 {
			return "窗口外背景"
		}
	}
	if strings.Contains(status, "预印本") {
		return "预印本候选"
	}
	if eligibility == "特殊分轨" {
		return "特殊分轨"
	}
	if eligibility == "条件外" || topic == "范围外／方法对照" {
		return "条件外或方法对照"
	}
	return "待核实"
}
