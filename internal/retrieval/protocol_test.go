package retrieval

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"os"
	"strings"
	"testing"
	"time"
)

func exampleProtocol(t *testing.T) Protocol {
	t.Helper()
	b, err := os.ReadFile("testdata/hydrogen-plan.json")
	require.NoError(t, err)
	p, err := ImportProtocol(b, time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	return p.Protocol
}

// TestProtocolImportPreservesNeutralPlan 对需求包原始非医学计划做真实导入，不能丢失外部原生式或发明执行结果。
func TestProtocolImportPreservesNeutralPlan(t *testing.T) {
	p := exampleProtocol(t)
	require.Equal(t, "example-hydrogen-catalysis", p.ProfileID)
	require.NotEmpty(t, p.ExternalQueries)
	require.NotEmpty(t, p.Assumptions)
	preview, err := PreviewProtocol(p, time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.False(t, preview.Executable)
	require.Contains(t, preview.ExternalSources, "openalex")
	require.Contains(t, preview.ExternalSources, "semantic-scholar")
	require.Len(t, preview.SearchRoutes, 4)
	for _, q := range p.ExternalQueries {
		require.Equal(t, "planned", q.Status)
		require.Nil(t, q.ReportedHits)
		require.Nil(t, q.FetchedCount)
	}
	for _, r := range preview.SearchRoutes {
		for _, group := range r.TermGroups {
			for _, word := range group {
				require.NotContains(t, strings.ToLower(word), "biomed")
			}
		}
	}
	require.Equal(t, "no metric threshold requested", p.VenuePolicy)
	require.Equal(t, []string{"undetermined; no English-only restriction"}, p.Languages)
	require.Equal(t, "2020-01-01", p.DateRange.From)
}
func TestProtocolExecutionDatesBudgetsAndSemanticVersions(t *testing.T) {
	p := exampleProtocol(t)
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	p.Mode = "execute"
	_, err := PreviewProtocol(p, now)
	require.ErrorContains(t, err, "execution requires")
	p.Budget = Budget{MaxRequests: 100, MaxPages: 100, MaxRecords: 5000, MaxQueue: 1000, MaxHistoryDays: 3000}
	a, err := PreviewProtocol(p, now)
	require.NoError(t, err)
	require.True(t, a.Executable)
	p.ProfileVersion = "2"
	p.Topic = "仅改名称"
	p.Notes = "说明"
	p.Budget.MaxRequests = 200
	b, err := PreviewProtocol(p, now)
	require.NoError(t, err)
	require.Equal(t, a.SemanticVersion, b.SemanticVersion)
	require.NotEqual(t, a.Acknowledgement, b.Acknowledgement, "预算和文本改动仍需重新预览")
	p.Concepts[0].Terms = append(p.Concepts[0].Terms, "electrolysis")
	require.NotEqual(t, b.SemanticVersion, ProtocolSemanticVersion(p))
	p.DateRange = DateRange{From: "2026-03-08", To: "2026-03-08", Timezone: "America/New_York"}
	from, to, err := ProtocolWindow(p)
	require.NoError(t, err)
	require.Equal(t, 23*time.Hour, to.Sub(from), "当地日期必须使用日历日，不能机械加24小时")
}
func TestProtocolRejectsResultsCredentialsAndUnverifiedVocabulary(t *testing.T) {
	p := exampleProtocol(t)
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	zero := 0
	p.ExternalQueries[0].FetchedCount = &zero
	_, err := PreviewProtocol(p, now)
	require.ErrorContains(t, err, "planned")
	p = exampleProtocol(t)
	p.Concepts[0].ControlledTerms = []string{"invented MeSH"}
	_, err = PreviewProtocol(p, now)
	require.ErrorContains(t, err, "verification")
	for _, payload := range []string{`{"api_key":"not-a-real-secret"}`, `{"url":"https://example.org/?access_token=not-a-real-secret"}`, `{"url":"https://user:pass@example.org"}`} {
		_, err = ImportProtocol(json.RawMessage(payload), now)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "not-a-real-secret")
	}
	p = exampleProtocol(t)
	p.Concepts[0].Terms = []string{strings.Repeat("词", 101)}
	_, err = PreviewProtocol(p, now)
	require.Error(t, err)
}
