package catalog

import "github.com/teamsillybees/glean/internal/retrieval"

// screeningFixture 仅为事务测试提供完整证据，真实论文不得使用此夹具。
func screeningFixture(topic, status string) *retrieval.ScreeningEvidence {
	if status == "pending" {
		return nil
	}
	e := &retrieval.ScreeningEvidence{ProfileID: topic, ProfileVersion: "test-v1", ReadingLevel: "abstract_reviewed", IdentityBasis: "test identifier", ResearchObject: "test object", MethodRole: "test method", Criteria: map[string]retrieval.CriterionEvidence{}}
	for _, key := range []string{"identity", "topic", "method", "date", "venue", "document_type", "version"} {
		e.Criteria[key] = retrieval.CriterionEvidence{Decision: "pass", Evidence: "transaction fixture only", SourceURL: "https://example.org/fixture", Location: "fixture abstract"}
	}
	if status == "excluded" {
		c := e.Criteria["topic"]
		c.Decision = "fail"
		e.Criteria["topic"] = c
	}
	return e
}
