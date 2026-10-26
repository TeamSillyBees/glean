package catalog

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

// TestHistoricalLLMCollectionPreservesHTMLRules 验证历史展示规则顺序和缺失年份的语义。
func TestHistoricalLLMCollectionPreservesHTMLRules(t *testing.T) {
	cases := []struct {
		raw  map[string]any
		want string
	}{
		{map[string]any{"publication_eligibility": "符合", "topic_status": "已核相关或参考", "year": 2020}, "正式纳入"},
		{map[string]any{"year": 2024, "status": "预印本"}, "窗口外背景"},
		{map[string]any{"year": "2025", "status": "预印本候选", "publication_eligibility": "特殊分轨"}, "预印本候选"},
		{map[string]any{"publication_eligibility": "特殊分轨"}, "特殊分轨"},
		{map[string]any{"topic_status": "范围外／方法对照"}, "条件外或方法对照"},
		{map[string]any{}, "待核实"},
		{map[string]any{"year": nil}, "窗口外背景"},
		{map[string]any{"year": " "}, "窗口外背景"},
		{map[string]any{"year": json.Number("2024")}, "窗口外背景"},
		{map[string]any{"year": "unknown"}, "待核实"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, historicalLLMCollection(tc.raw), "raw=%v", tc.raw)
	}
}
