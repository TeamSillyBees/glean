package catalog

import (
	"context"
	"strings"

	sql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"

	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/gleanvenue"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
)

func normalizeISSNs(values []string) []string {
	result := []string{}
	for _, value := range values {
		v := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(value), "-", ""))
		if len(v) != 8 {
			continue
		}
		sum := 0
		valid := true
		for i := 0; i < 7; i++ {
			if v[i] < '0' || v[i] > '9' {
				valid = false
				break
			}
			sum += int(v[i]-'0') * (8 - i)
		}
		if !valid {
			continue
		}
		last := int(v[7] - '0')
		if v[7] == 'X' {
			last = 10
		} else if v[7] < '0' || v[7] > '9' {
			continue
		}
		if (sum+last)%11 != 0 {
			continue
		}
		result = append(result, v[:4]+"-"+v[4:])
	}
	return uniqueSorted(result)
}

// CoreJournals 返回有证据与校验 ISSN 的发现目标；历史收录可启用发现而无需伪造当期指标资格。
func (s *Service) CoreJournals(ctx context.Context) ([]CoreJournalVO, error) {
	records, err := s.client.GleanVenue.Query().Where(gleanvenue.Or(gleanvenue.And(gleanvenue.JifNotNil(), gleanvenue.MetricYearGT(0)), func(sel *sql.Selector) {
		sel.Where(sqljson.ValueContains(sel.C(gleanvenue.FieldEvidence), map[string]bool{"discoveryEnabled": true}))
	})).Order(ent.Desc(gleanvenue.FieldMetricYear), ent.Desc(gleanvenue.FieldCreatedAt), ent.Desc(gleanvenue.FieldID)).All(ctx)
	if err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "load core journal registry failed")
	}
	result := []CoreJournalVO{}
	seen := map[string]bool{}
	for _, v := range records {
		if v.Kind != "journal" || v.Source == "" {
			continue
		}
		issns := normalizeISSNs(v.Issns)
		if len(issns) == 0 {
			continue
		}
		qualified := (v.QualificationStatus == "verified" || v.QualificationStatus == "historical-verified" || v.QualificationStatus == "historical-qualified" || v.Evidence["metricVerified"] == true) && v.Jif != nil && *v.Jif >= 4 && v.MetricYear > 0
		eligible := []string{}
		if qualified {
			eligible = []string{"llm-agent", "biomed-worldmodel"}
			if *v.Jif > 10 {
				eligible = append([]string{"biomed-ai"}, eligible...)
			}
		}
		discovery := append([]string{}, eligible...)
		if v.Evidence["discoveryEnabled"] == true {
			switch values := v.Evidence["discoveryTopics"].(type) {
			case []string:
				for _, topic := range values {
					if validTopic(topic) {
						discovery = append(discovery, topic)
					}
				}
			case []any:
				for _, value := range values {
					if topic, ok := value.(string); ok && validTopic(topic) {
						discovery = append(discovery, topic)
					}
				}
			}
		}
		discovery = uniqueSorted(discovery)
		if len(discovery) == 0 {
			continue
		}
		// 不把同 ISSN 的新刊名静默继承旧刊名指标；只折叠同名、同身份与同发现范围的重复版本。
		key := strings.ToLower(v.Name) + "|" + strings.Join(issns, ",") + "|" + strings.Join(discovery, ",")
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, CoreJournalVO{Name: v.Name, ISSNs: issns, JIF: v.Jif, MetricYear: v.MetricYear, MetricSource: v.Source, EligibleTopics: eligible, DiscoveryTopics: discovery, QualificationVerified: qualified, RegistryVersion: v.RegistryVersion})
	}
	return result, nil
}
