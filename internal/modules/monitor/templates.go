package monitor

import (
	"fmt"
	"strings"
)

// TemplateVersion 固定此次部署采用的统一检索模板版本。
const TemplateVersion = "2026-10-26.1"

// SearchTemplates 返回三个主题共用的检索定义；纳入策略留在审核阶段。
func SearchTemplates() []SearchTemplate {
	return []SearchTemplate{
		{ID: "biomed-ai-core", Version: TemplateVersion, Topic: "biomed-ai", TermGroups: [][]string{{"artificial intelligence", "machine learning", "deep learning", "foundation model"}, {"biomedical", "medicine", "clinical", "biology"}}, Query: `("artificial intelligence" OR "machine learning" OR "deep learning" OR "foundation model") AND (biomedical OR medicine OR clinical OR biology)`, Terms: []string{"artificial intelligence", "machine learning", "deep learning", "foundation model", "biomedical", "clinical"}, Core: true, Policy: InclusionPolicy{JIFMinimum: 10, JIFStrict: true, CCF: []string{"A"}, FullPaperRequired: true, MainTrackRequired: true}},
		{ID: "llm-agent-core", Version: TemplateVersion, Topic: "llm-agent", TermGroups: [][]string{{"large language model", "LLM", "language agent"}, {"agent", "tool use", "planning", "reasoning"}}, Query: `("large language model" OR LLM OR "language agent") AND (agent OR "tool use" OR planning OR reasoning)`, Terms: []string{"large language model", "LLM", "language agent", "tool use", "planning"}, Core: true, Policy: InclusionPolicy{JIFMinimum: 4, CCF: []string{"A", "B", "C"}, FullPaperRequired: true}},
		{ID: "biomed-worldmodel-core", Version: TemplateVersion, Topic: "biomed-worldmodel", TermGroups: [][]string{{"world model", "digital twin", "virtual cell", "generative simulation"}, {"biomedical", "biology", "clinical", "medicine"}}, Query: `("world model" OR "digital twin" OR "virtual cell" OR "generative simulation") AND (biomedical OR biology OR clinical OR medicine)`, Terms: []string{"world model", "digital twin", "virtual cell", "generative simulation", "biomedical"}, Core: true, Policy: InclusionPolicy{JIFMinimum: 4, CCF: []string{"A", "B", "C"}, FullPaperRequired: true}},
	}
}

// NormalizeISSN 校验 ISSN 校验位并规范为 xxxx-xxxx；未知值不能猜测匹配。
func NormalizeISSN(value string) (string, error) {
	raw := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(value), "-", ""))
	if len(raw) != 8 {
		return "", fmt.Errorf("invalid ISSN length")
	}
	sum := 0
	for i := 0; i < 7; i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return "", fmt.Errorf("invalid ISSN digit")
		}
		sum += int(raw[i]-'0') * (8 - i)
	}
	check := (11 - sum%11) % 11
	actual := int(raw[7] - '0')
	if raw[7] == 'X' {
		actual = 10
	} else if raw[7] < '0' || raw[7] > '9' {
		return "", fmt.Errorf("invalid ISSN check digit")
	}
	if check != actual {
		return "", fmt.Errorf("invalid ISSN checksum")
	}
	return raw[:4] + "-" + raw[4:], nil
}

// MatchJournal 只按有效 ISSN 匹配，不把相似期刊名或旧指标伪装为已核验。
func MatchJournal(issns []string, registry []JournalEntry) *JournalEntry {
	keys := map[string]bool{}
	for _, raw := range issns {
		if v, e := NormalizeISSN(raw); e == nil {
			keys[v] = true
		}
	}
	for _, entry := range registry {
		for _, raw := range entry.ISSNs {
			if v, e := NormalizeISSN(raw); e == nil && keys[v] {
				result := entry
				return &result
			}
		}
	}
	return nil
}
