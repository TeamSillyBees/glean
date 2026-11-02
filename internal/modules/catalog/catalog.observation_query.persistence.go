package catalog

import (
	"context"
	"encoding/json"
	"entgo.io/ent/dialect"
	sql "entgo.io/ent/dialect/sql"
	"time"

	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/gleanobservation"
	"github.com/teamsillybees/glean/internal/data/ent/gleanobservationmatch"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

func (s *Service) attachObservations(ctx context.Context, items []PaperVO, detail bool) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]idgen.ID, 0, len(items))
	indices := map[idgen.ID]int{}
	for i := range items {
		ids = append(ids, items[i].ID)
		indices[items[i].ID] = i
		items[i].Observations = []SourceObservationVO{}
	}
	query := s.client.GleanObservation.Query().Where(gleanobservation.PaperIDIn(ids...)).Order(ent.Desc(gleanobservation.FieldObservedAt), ent.Desc(gleanobservation.FieldID))
	if !detail {
		var counts []struct {
			PaperID idgen.ID `json:"paper_id"`
			Count   int      `json:"count"`
		}
		if err := s.client.GleanObservation.Query().Where(gleanobservation.PaperIDIn(ids...)).GroupBy(gleanobservation.FieldPaperID).Aggregate(ent.Count()).Scan(ctx, &counts); err != nil {
			return bizerrors.WrapDBContext(ctx, err, "count source observations failed")
		}
		for _, count := range counts {
			items[indices[count.PaperID]].ObservationCount = count.Count
		}
		var updates []struct {
			PaperID         idgen.ID   `json:"paper_id"`
			SourceUpdatedAt *time.Time `json:"max"`
		}
		if err := s.client.GleanObservation.Query().Where(gleanobservation.PaperIDIn(ids...), func(sel *sql.Selector) { sel.Where(liveObservationSQL(sel.C(gleanobservation.FieldMetadata))) }).GroupBy(gleanobservation.FieldPaperID).Aggregate(ent.Max(gleanobservation.FieldSourceUpdatedAt)).Scan(ctx, &updates); err != nil {
			return bizerrors.WrapDBContext(ctx, err, "load live source update times failed")
		}
		for _, update := range updates {
			items[indices[update.PaperID]].LatestSourceUpdatedAt = update.SourceUpdatedAt
		}
		// 列表只取每篇最新观察，不随来源版本历史增长而加载所有 JSON。
		query.Where(func(sel *sql.Selector) {
			sel.Where(liveObservationSQL(sel.C(gleanobservation.FieldMetadata)))
			newer := sql.Table(gleanobservation.Table).As("newer_observation")
			sub := sql.Dialect(dialect.Postgres).Select(newer.C(gleanobservation.FieldID)).From(newer).Where(sql.And(liveObservationSQL(newer.C(gleanobservation.FieldMetadata)), sql.ColumnsEQ(newer.C(gleanobservation.FieldPaperID), sel.C(gleanobservation.FieldPaperID)), sql.Or(sql.ColumnsGT(newer.C(gleanobservation.FieldObservedAt), sel.C(gleanobservation.FieldObservedAt)), sql.And(sql.ColumnsEQ(newer.C(gleanobservation.FieldObservedAt), sel.C(gleanobservation.FieldObservedAt)), sql.ColumnsGT(newer.C(gleanobservation.FieldID), sel.C(gleanobservation.FieldID))))))
			sel.Where(sql.Not(sql.Exists(sub)))
		})
		query.Select(gleanobservation.FieldID, gleanobservation.FieldPaperID, gleanobservation.FieldObservedAt, gleanobservation.FieldReview)
	}
	observations, err := query.All(ctx)
	if err != nil {
		return bizerrors.WrapDBContext(ctx, err, "load source observation history failed")
	}
	matched := map[idgen.ID][]string{}
	if detail {
		observationIDs := make([]idgen.ID, 0, len(observations))
		for _, o := range observations {
			observationIDs = append(observationIDs, o.ID)
		}
		for start := 0; start < len(observationIDs); start += 1000 {
			end := min(start+1000, len(observationIDs))
			matches, err := s.client.GleanObservationMatch.Query().Where(gleanobservationmatch.ObservationIDIn(observationIDs[start:end]...)).All(ctx)
			if err != nil {
				return bizerrors.WrapDBContext(ctx, err, "load observation query-hit topics failed")
			}
			for _, m := range matches {
				matched[m.ObservationID] = append(matched[m.ObservationID], m.Topic)
			}
		}
	}
	for _, o := range observations {
		item := &items[indices[o.PaperID]]
		var metadata ObservationInput
		if detail {
			if err := json.Unmarshal(o.Metadata, &metadata); err != nil {
				return bizerrors.WrapDBContext(ctx, err, "decode source metadata failed")
			}
			if metadata.Kind != historicalAbstractKind && o.SourceUpdatedAt != nil && (item.LatestSourceUpdatedAt == nil || o.SourceUpdatedAt.After(*item.LatestSourceUpdatedAt)) {
				at := *o.SourceUpdatedAt
				item.LatestSourceUpdatedAt = &at
			}
			item.ObservationCount++
		}
		review := ReviewVO{}
		encoded, err := json.Marshal(o.Review)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(encoded, &review); err != nil {
			return bizerrors.WrapDBContext(ctx, err, "decode source review failed")
		}
		if metadata.Kind != historicalAbstractKind && item.LastObservedAt == nil {
			observed := o.ObservedAt
			item.LastObservedAt = &observed
			item.Review = &review
		}
		if detail {
			metadata.Review = review
			metadata.Pipeline = review.Pipeline
			metadata.Topics = uniqueSorted(matched[o.ID])
			observation := SourceObservationVO{ObservationInput: metadata, ID: o.ID, ContentHash: o.ContentHash}
			item.Observations = append(item.Observations, observation)
			if item.AbstractEnrichment == nil {
				if selected := selectedStoredHistoricalAbstract(o, metadata); selected != nil && item.DOI == selected.DOI {
					item.AbstractEnrichment = selected
				}
			}
		}
	}
	return nil
}
