package baseline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHistoricalJournalRegistryPreservesEvidence 验证发现目标覆盖实际历史纳入期刊，而不伪造当期指标资格。
func TestHistoricalJournalRegistryPreservesEvidence(t *testing.T) {
	directory := filepath.Join("..", "..", "data", "journals")
	entries, err := ReadJournalRegistry(directory)
	require.NoError(t, err)
	require.Len(t, entries, 570)
	seed, discovery, withISSNs := 0, 0, 0
	byName := map[string]Venue{}
	for _, v := range entries {
		require.NotEmpty(t, v.Source)
		require.NotEmpty(t, v.Evidence["sourceSHA256"])
		if len(v.ISSNs) > 0 {
			withISSNs++
		}
		switch v.QualificationStatus {
		case "historical-verified":
			seed++
			require.Equal(t, 2025, v.MetricYear)
		case "historical-curated-discovery":
			discovery++
			require.Equal(t, "journal", v.Kind)
			require.Equal(t, true, v.Evidence["discoveryEnabled"])
			require.Equal(t, false, v.Evidence["metricVerified"])
			require.NotEmpty(t, v.Evidence["discoveryTopics"])
			require.NotEmpty(t, v.Evidence["canonicalMetadataSHA256"])
			require.Empty(t, v.EligibleTopics, "discovery is not current metric qualification")
			byName[strings.ToLower(v.Name)] = v
		default:
			t.Fatalf("unexpected registry status %s", v.QualificationStatus)
		}
	}
	require.Equal(t, 208, seed)
	require.Equal(t, 362, discovery)
	require.Equal(t, 540, withISSNs)
	mia := byName["medical image analysis"]
	require.Equal(t, []string{"1361-8415", "1361-8423"}, mia.ISSNs)
	require.NotNil(t, mia.JIF)
	require.Equal(t, 14.0, *mia.JIF)
	require.Zero(t, mia.MetricYear, "official historical source left metric year unknown")
	require.Equal(t, []any{"biomed-ai", "llm-agent"}, mia.Evidence["discoveryTopics"])
	require.Equal(t, []string{"1872-7409"}, byName["knowledge-based systems"].ISSNs)
	require.Equal(t, []string{"1873-6793"}, byName["expert systems with applications"].ISSNs)
	require.Contains(t, byName, "proceedings of the ieee. institute of electrical and electronics engineers", "real journal must not be excluded by a Proceedings substring")
	require.NotContains(t, byName, "aistats", "explicit PMLR conference records are not journal targets")
	require.Contains(t, byName["information fusion"].Evidence["discoveryTopics"], "llm-agent", "exact existing issuer identity restores its missing topic route")
	require.Contains(t, byName["communications medicine"].Evidence["discoveryTopics"], "biomed-worldmodel")
	require.Contains(t, byName["computers and electronics in agriculture."].Evidence["discoveryTopics"], "llm-agent")
	require.Zero(t, byName["computers and electronics in agriculture."].MetricYear, "route repair cannot fill unknown metric years")
	raw, err := os.ReadFile(filepath.Join(directory, "coverage.json"))
	require.NoError(t, err)
	var coverage struct {
		Repairs  map[string]int `json:"knownRegistryNameTopicRepairs"`
		Resolved []struct {
			IdentityResolution struct {
				OriginalRecordISSNs     []string          `json:"originalRecordISSNs"`
				MatchedIdentityEvidence []json.RawMessage `json:"matchedIdentityEvidence"`
			} `json:"identityResolution"`
		} `json:"resolvedTopicRouteRecords"`
		Counts     map[string]int `json:"curatedJournalMembershipsWithRecordedValidISSNs"`
		Misses     int            `json:"unexplainedCuratedJournalWithValidISSNMisses"`
		Exclusions []struct {
			Reason string `json:"reason"`
		} `json:"explicitExclusions"`
	}
	require.NoError(t, json.Unmarshal(raw, &coverage))
	require.Equal(t, map[string]int{"biomed-ai": 7376, "llm-agent": 316, "biomed-worldmodel": 63}, coverage.Counts)
	require.Equal(t, map[string]int{"llm-agent": 10, "biomed-worldmodel": 22}, coverage.Repairs)
	require.Len(t, coverage.Resolved, 32)
	for _, resolved := range coverage.Resolved {
		require.Empty(t, resolved.IdentityResolution.OriginalRecordISSNs)
		require.NotEmpty(t, resolved.IdentityResolution.MatchedIdentityEvidence)
	}
	require.Zero(t, coverage.Misses)
	require.NotEmpty(t, coverage.Exclusions)
}
