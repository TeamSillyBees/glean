package monitor

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTemplateQueriesAreDerivedFromApprovedTermGroups 验证三个来源翻译入口共享同一术语定义，不出现 Query/Terms 漂移。
func TestTemplateQueriesAreDerivedFromApprovedTermGroups(t *testing.T) {
	templates := SearchTemplates()
	require.Len(t, templates, 3)
	require.Equal(t, "2026-10-26.3", TemplateVersion)
	for _, template := range templates {
		expectedGroups := []string{}
		expectedTerms := []string{}
		for _, group := range template.TermGroups {
			quoted := []string{}
			for _, term := range group {
				quoted = append(quoted, strconv.Quote(term))
				expectedTerms = append(expectedTerms, term)
			}
			expectedGroups = append(expectedGroups, "("+strings.Join(quoted, " OR ")+")")
		}
		require.Equal(t, strings.Join(expectedGroups, " AND "), template.Query)
		require.ElementsMatch(t, uniqueStrings(expectedTerms), template.Terms)
		require.Equal(t, TemplateVersion, template.Version)
	}
	require.Equal(t, "llm-agent-core", templates[1].ID)
	for _, generic := range []string{"agent", "tool use", "planning", "reasoning"} {
		require.NotContains(t, templates[1].TermGroups[1], generic)
	}
	require.Equal(t, float64(10), templates[0].Policy.JIFMinimum)
	require.True(t, templates[0].Policy.JIFStrict)
	require.Equal(t, []string{"A"}, templates[0].Policy.CCF)
	require.Equal(t, float64(4), templates[1].Policy.JIFMinimum)
	require.Equal(t, []string{"A", "B", "C"}, templates[2].Policy.CCF)
}

// TestDiscoveryRecallFixturesCoverKnowledgeAndCellDynamics 验证知识构建不依赖 agent 词、JEPA/扰动/细胞动力学不再漏召回。
func TestDiscoveryRecallFixturesCoverKnowledgeAndCellDynamics(t *testing.T) {
	var fixtures []struct {
		Name     string `json:"name"`
		Topic    string `json:"topic"`
		Title    string `json:"title"`
		Abstract string `json:"abstract"`
		Match    bool   `json:"match"`
	}
	raw, err := os.ReadFile("testdata/template-recall-v3.json")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &fixtures))
	templates := map[string]SearchTemplate{}
	for _, template := range SearchTemplates() {
		templates[template.Topic] = template
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			require.Equal(t, fixture.Match, matchesTemplateGroups(fixture.Title+" "+fixture.Abstract, templates[fixture.Topic]))
		})
	}
}
