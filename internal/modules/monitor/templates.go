package monitor

import (
	"fmt"
	"strconv"
	"strings"
)

// TemplateVersion 固定此次部署采用的统一检索模板版本。
const TemplateVersion = "2026-10-26.3"

// SearchTemplates 返回三个主题共用的检索定义；纳入策略留在审核阶段。
func SearchTemplates() []SearchTemplate {
	templates := []SearchTemplate{
		{ID: "biomed-ai-core", Topic: "biomed-ai", TermGroups: [][]string{
			{"artificial intelligence", "machine learning", "deep learning", "foundation model", "neural network", "graph neural network", "reinforcement learning", "generative model", "diffusion model", "transformer", "self-supervised", "representation learning", "random forest", "gradient boosting", "support vector machine"},
			{"biomedical", "medicine", "clinical", "biology", "medical", "patient", "disease", "cancer", "drug", "protein", "molecular", "cell", "cellular", "genomic", "omics", "tissue", "organ", "physiological", "neuroscience", "brain", "imaging", "radiology", "pathology"},
		}, Policy: InclusionPolicy{JIFMinimum: 10, JIFStrict: true, CCF: []string{"A"}, FullPaperRequired: true, MainTrackRequired: true}},
		{ID: "llm-agent-core", Topic: "llm-agent", TermGroups: [][]string{
			{"large language model", "language model", "LLM", "LLMs", "ChatGPT", "GPT", "T5", "text-to-text", "generative language model", "language agent", "agentic"},
			{"knowledge graph", "knowledge construction", "knowledge extraction", "knowledge integration", "knowledge fusion", "knowledge completion", "knowledge verification", "knowledge acquisition", "knowledge engineering", "structured knowledge", "ontology", "ontologies", "information extraction", "relation extraction", "entity extraction", "entity linking", "schema induction"},
		}, Policy: InclusionPolicy{JIFMinimum: 4, CCF: []string{"A", "B", "C"}, FullPaperRequired: true}},
		{ID: "biomed-worldmodel-core", Topic: "biomed-worldmodel", TermGroups: [][]string{
			{"world model", "digital twin", "virtual cell", "generative simulation", "JEPA", "joint embedding predictive", "state transition", "perturbation prediction", "perturbation response", "cell state", "latent dynamics", "dynamics model", "dynamical model", "molecular dynamics", "cellular simulation", "counterfactual simulation", "foundation model"},
			{"biomedical", "biological", "biology", "clinical", "medicine", "medical", "cell", "cellular", "protein", "molecular", "genomic", "omics", "drug", "tissue", "organ", "physiological", "neuroscience", "brain", "patient"},
		}, Policy: InclusionPolicy{JIFMinimum: 4, CCF: []string{"A", "B", "C"}, FullPaperRequired: true}},
	}
	for i := range templates {
		templates[i].Version = TemplateVersion
		templates[i].Core = true
		groups := make([]string, 0, len(templates[i].TermGroups))
		terms := []string{}
		for _, group := range templates[i].TermGroups {
			quoted := make([]string, 0, len(group))
			for _, term := range group {
				quoted = append(quoted, strconv.Quote(term))
				terms = append(terms, term)
			}
			groups = append(groups, "("+strings.Join(quoted, " OR ")+")")
		}
		templates[i].Query = strings.Join(groups, " AND ")
		templates[i].Terms = uniqueStrings(terms)
	}
	return templates
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
