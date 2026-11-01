package catalog

import (
	"context"
	"fmt"
	"strings"

	"entgo.io/ent/dialect"
	sql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/gleanpapercorrection"
	"github.com/teamsillybees/glean/internal/data/ent/gleanpaperreview"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

func reviewPredicate(paperColumn, topicColumn, status string) *sql.Predicate {
	r := sql.Table(gleanpaperreview.Table).As("manual_review")
	q := sql.Dialect(dialect.Postgres).Select(r.C("id")).From(r).Where(sql.And(sql.ColumnsEQ(r.C("paper_id"), paperColumn), sql.ColumnsEQ(r.C("topic"), topicColumn)))
	if status != "" {
		q.Where(sql.EQ(r.C("status"), status))
	}
	return sql.Exists(q)
}
func collectionPredicate(paperColumn, topicColumn, statusColumn, dimensionsColumn, scope string) *sql.Predicate {
	manual := func(status string) *sql.Predicate { return reviewPredicate(paperColumn, topicColumn, status) }
	switch scope {
	case "pending":
		return sql.Or(manual("pending"), sql.And(sql.Not(manual("")), sql.EQ(statusColumn, "pending-review")))
	case "excluded":
		return manual("excluded")
	case "curated":
		historical := []*sql.Predicate{}
		for _, t := range topics {
			historical = append(historical, sql.And(sql.EQ(topicColumn, t.Slug), sqljson.ValueContains(dimensionsColumn, historicalCuratedDimensions(t.Slug))))
		}
		return sql.Or(manual("included"), sql.And(sql.Not(manual("")), sql.EQ(statusColumn, "historical"), sql.Or(historical...)))
	}
	return nil
}

// effectiveColumn 只接受代码内常量字段名；外部输入始终通过参数绑定。
func effectiveColumn(table, field string) string {
	key := field
	if field == "title_zh" {
		key = "titleZh"
	}
	value := fmt.Sprintf(`(SELECT "manual_correction"."fields"->>'%s' FROM "glean_paper_correction" AS "manual_correction" WHERE "manual_correction"."paper_id" = %s."id")`, key, table)
	if field == "year" {
		value = "(" + value + ")::bigint"
	}
	return fmt.Sprintf(`COALESCE(%s, %s."%s")`, value, table, field)
}
func effectiveContains(column, value string) *sql.Predicate {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(value))
	return sql.P(func(b *sql.Builder) { b.WriteString("LOWER(" + column + ") LIKE ").Arg("%" + escaped + "%") })
}
func applyCorrection(item *PaperVO, c CorrectionVO) {
	f := c.Fields
	if f.Title != nil {
		item.Title = *f.Title
	}
	if f.TitleZh != nil {
		item.TitleZh = *f.TitleZh
	}
	if f.Abstract != nil {
		item.Abstract = *f.Abstract
	}
	if f.Authors != nil {
		item.Authors = *f.Authors
	}
	if f.Year != nil {
		item.Year = *f.Year
	}
	if f.Venue != nil {
		item.Venue = *f.Venue
	}
	item.Correction = &c
}
func (s *Service) attachManagement(ctx context.Context, items []PaperVO) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]idgen.ID, 0, len(items))
	indices := map[idgen.ID]int{}
	for i := range items {
		ids = append(ids, items[i].ID)
		indices[items[i].ID] = i
	}
	corrections, err := s.client.GleanPaperCorrection.Query().Where(gleanpapercorrection.PaperIDIn(ids...)).All(ctx)
	if err != nil {
		return err
	}
	for _, c := range corrections {
		applyCorrection(&items[indices[c.PaperID]], correctionVO(c))
	}
	reviews, err := s.client.GleanPaperReview.Query().Where(gleanpaperreview.PaperIDIn(ids...)).All(ctx)
	if err != nil {
		return err
	}
	for _, r := range reviews {
		item := &items[indices[r.PaperID]]
		for j := range item.Memberships {
			if item.Memberships[j].Topic == r.Topic {
				v := reviewVO(r)
				item.Memberships[j].ManualReview = &v
				item.Memberships[j].InclusionStatus = v.Status
				item.Memberships[j].EffectiveStatus = v.Status
			}
		}
	}
	return nil
}
func (s *Service) applyFacetCorrections(ctx context.Context, papers []*ent.GleanPaper, batchSize int) error {
	for start := 0; start < len(papers); start += batchSize {
		end := min(start+batchSize, len(papers))
		ids := []idgen.ID{}
		index := map[idgen.ID]*ent.GleanPaper{}
		for _, p := range papers[start:end] {
			ids = append(ids, p.ID)
			index[p.ID] = p
		}
		rows, err := s.client.GleanPaperCorrection.Query().Where(gleanpapercorrection.PaperIDIn(ids...)).All(ctx)
		if err != nil {
			return err
		}
		for _, r := range rows {
			c := correctionVO(r)
			if c.Fields.Year != nil {
				index[r.PaperID].Year = *c.Fields.Year
			}
			if c.Fields.Venue != nil {
				index[r.PaperID].Venue = *c.Fields.Venue
			}
		}
	}
	return nil
}

func historicalEffectiveStatus(m *ent.GleanMembership) string {
	if m.InclusionStatus == "pending-review" {
		return "pending"
	}
	if m.InclusionStatus != "historical" {
		return ""
	}
	expected := historicalCuratedDimensions(m.Topic)
	if len(expected) == 0 {
		return ""
	}
	for key, values := range expected {
		for _, want := range values {
			found := false
			for _, value := range m.Dimensions[key] {
				if value == want {
					found = true
					break
				}
			}
			if !found {
				return ""
			}
		}
	}
	return "included"
}
