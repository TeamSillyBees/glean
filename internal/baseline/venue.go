package baseline

import (
	"encoding/json"
	"strings"
)

// Venue 是已有载体证据的逐主题版本，不自行推断指标年份或纳入资格。
type Venue struct {
	ISSNs               []string       `json:"issns"`
	Aliases             []string       `json:"aliases"`
	Name                string         `json:"name"`
	Kind                string         `json:"kind"`
	JIF                 *float64       `json:"jif"`
	CCF                 string         `json:"ccf"`
	MetricYear          int            `json:"metricYear"`
	RegistryVersion     string         `json:"registryVersion"`
	Source              string         `json:"source"`
	Evidence            map[string]any `json:"evidence"`
	EligibleTopics      []string       `json:"eligibleTopics"`
	QualificationStatus string         `json:"qualificationStatus"`
}

// VenueFromRecord 提取原指标与来源；显式核验旗标缺失时保持待核。
func VenueFromRecord(r Record) (Venue, bool) {
	v := Venue{Name: r.Venue, Kind: firstText(r.Raw, "venue_kind", "verified_venue_kind"), CCF: text(r.Raw["ccf"]), MetricYear: integer(r.Raw["jif_year"]), Source: firstText(r.Raw, "metric_url", "jif_url", "ccf_source"), Evidence: map[string]any{"sourceTopics": []string{r.Topic}, "baselineDate": "2026-10-26", "reviewStatus": "historical evidence imported; not reverified by this importer"}, EligibleTopics: []string{}, QualificationStatus: "pending"}
	v.ISSNs = list(r.Raw["journal_issns"])
	if len(v.ISSNs) == 0 {
		v.ISSNs = list(r.Raw["issn"])
	}
	v.Aliases = list(r.Raw["venue_aliases"])
	if strings.TrimSpace(v.Name) == "" {
		return v, false
	}
	if v.Kind == "" {
		v.Kind = "unknown"
	}
	if n, ok := number(r.Raw["jif"]); ok && n >= 0 {
		v.JIF = &n
	}
	if v.MetricYear == 0 {
		v.MetricYear = integer(r.Raw["ccf_year"])
	}
	for _, key := range []string{"jif", "jif_year", "ccf", "ccf_year", "metric_url", "jif_url", "ccf_source", "journal_issns_verified", "journal_issns", "venue_qualification_evidence", "venue_metric_evidence", "venue_current_status", "venue_status_evidence", "venue_match_method", "venue_gate_decision", "venue_gate_reason", "venue_metric_conflicts", "venue_qualification_verified"} {
		if value, ok := r.Raw[key]; ok && value != nil {
			v.Evidence[key] = value
		}
	}
	if r.Raw["venue_qualification_verified"] == true {
		v.QualificationStatus = "historical-qualified"
		v.EligibleTopics = []string{r.Topic}
	}
	body, _ := json.Marshal(v)
	v.RegistryVersion = "baseline-20261026-" + r.Topic + "-" + digest(body)[:16]
	return v, true
}
