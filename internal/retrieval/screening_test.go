package retrieval

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func evidenceFixture() *ScreeningEvidence {
	e := &ScreeningEvidence{ProfileID: "test", ProfileVersion: "1", ReadingLevel: "abstract_reviewed", IdentityBasis: "DOI", ResearchObject: "object", MethodRole: "primary method", Criteria: map[string]CriterionEvidence{}}
	for _, k := range []string{"identity", "topic", "method", "date", "venue", "document_type", "version"} {
		e.Criteria[k] = CriterionEvidence{Decision: "pass", Evidence: "fixture short evidence", SourceURL: "https://example.org/paper", Location: "abstract"}
	}
	return e
}

// TestScreeningEvidenceGate 确认取得不等于阅读，未知不等于否定，并按需要提高证据层级。
func TestScreeningEvidenceGate(t *testing.T) {
	require.NoError(t, ValidateScreening("pending", "", nil))
	require.Error(t, ValidateScreening("included", "reason", nil))
	e := evidenceFixture()
	require.NoError(t, ValidateScreening("included", "reason", e))
	for _, level := range []string{"metadata", "abstract_available", "fulltext_accessible"} {
		e.ReadingLevel = level
		require.Error(t, ValidateScreening("included", "reason", e))
	}
	e = evidenceFixture()
	e.RequiresFulltext = true
	require.Error(t, ValidateScreening("included", "reason", e))
	e.ReadingLevel = "fulltext_reviewed"
	require.NoError(t, ValidateScreening("included", "reason", e))
	e = evidenceFixture()
	c := e.Criteria["method"]
	c.Decision = "unknown"
	e.Criteria["method"] = c
	require.Error(t, ValidateScreening("included", "reason", e))
	require.Error(t, ValidateScreening("excluded", "reason", e))
	require.NoError(t, ValidateScreening("pending", "", e))
	c.Decision = "fail"
	e.Criteria["method"] = c
	require.NoError(t, ValidateScreening("excluded", "explicit negative", e))
	c.SourceURL = ""
	e.Criteria["method"] = c
	require.Error(t, ValidateScreening("excluded", "reason", e))
}
