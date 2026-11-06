package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"entgo.io/ent/dialect"
	sql "entgo.io/ent/dialect/sql"
	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/gleancatalogchange"
	"github.com/teamsillybees/glean/internal/data/ent/gleanobservation"
	"github.com/teamsillybees/glean/internal/data/ent/gleanpaper"
	"github.com/teamsillybees/glean/internal/data/ent/gleansearchhit"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/retrieval"
)

// RetrievalExportPage 按稳定论文 ID 导出运行关联目录，包括范围外和当前用户的隐藏状态。
func (s *Service) RetrievalExportPage(ctx context.Context, runID, after idgen.ID, limit int) ([]retrieval.CatalogExportRecord, error) {
	uid, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	if runID <= 0 || after < 0 || limit < 1 || limit > 500 {
		return nil, bizerrors.BadRequest("invalid catalog audit page")
	}
	papers, err := s.client.GleanPaper.Query().Where(gleanpaper.IDGT(after), func(sel *sql.Selector) {
		o := sql.Table(gleanobservation.Table)
		h := sql.Table(gleansearchhit.Table)
		hit := sql.Dialect(dialect.Postgres).Select(h.C(gleansearchhit.FieldObservationID)).From(h).Where(sql.EQ(h.C(gleansearchhit.FieldRunID), runID))
		observed := sql.Dialect(dialect.Postgres).Select(o.C(gleanobservation.FieldPaperID)).From(o).Where(sql.Or(sql.EQ(o.C(gleanobservation.FieldRunID), runID), sql.In(o.C(gleanobservation.FieldID), hit)))
		sel.Where(sql.In(sel.C(gleanpaper.FieldID), observed))
	}).Order(ent.Asc(gleanpaper.FieldID)).Limit(limit).All(ctx)
	if err != nil {
		return nil, err
	}
	views, err := s.paperVOs(ctx, papers, uid, false)
	if err != nil {
		return nil, err
	}
	result := make([]retrieval.CatalogExportRecord, 0, len(views))
	ids := []idgen.ID{}
	for _, p := range papers {
		ids = append(ids, p.ID)
	}
	observations, err := s.client.GleanObservation.Query().Where(gleanobservation.PaperIDIn(ids...)).Order(ent.Asc(gleanobservation.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	byPaper := map[idgen.ID][]*ent.GleanObservation{}
	for _, o := range observations {
		byPaper[o.PaperID] = append(byPaper[o.PaperID], o)
	}
	for _, v := range views {
		h := sha256.Sum256([]byte(v.Abstract))
		r := retrieval.CatalogExportRecord{ID: v.ID, Record: map[string]any{"record_id": v.ID, "title": v.Title, "authors": v.Authors, "doi": v.DOI, "arxiv_id": v.ArxivID, "venue": v.Venue, "document_type": v.PublicationType, "formal_online_date": v.FormalOnlineDate, "issue_date": v.IssueDate, "date_provenance": v.DateProvenance, "source_url": v.URL, "abstract_available": v.Abstract != "", "abstract_sha256": hex.EncodeToString(h[:]), "abstract_reviewed": nil, "fulltext_reviewed": nil, "personal_state": map[string]any{"hidden": v.Hidden, "reading": v.ReadStatus}, "content_policy": "metadata and short review evidence only; abstract/fulltext bodies omitted pending per-source redistribution permission"}, Screening: []map[string]any{}, IdentityEdges: []map[string]any{}}
		for _, m := range v.Memberships {
			decision := m.EffectiveStatus
			if m.ManualReview != nil {
				decision = m.ManualReview.Status
			}
			r.Screening = append(r.Screening, map[string]any{"record_id": v.ID, "profile_id": m.Topic, "decision": decision, "historical_status": m.InclusionStatus, "source": m.Source, "reading_notes": m.ReadingNotes, "manual_review": m.ManualReview, "original_id": m.OriginalID, "topic_role": m.Tier, "categories": m.Categories})
		}
		fields := []map[string]any{}
		for _, o := range byPaper[v.ID] {
			var m ObservationInput
			if json.Unmarshal(o.Metadata, &m) != nil {
				return nil, bizerrors.BadRequest("invalid archived observation")
			}
			fields = append(fields, map[string]any{"observation_id": o.ID, "source": o.Source, "source_id": o.SourceID, "content_hash": o.ContentHash, "title": m.Title, "doi": m.DOI, "pmid": m.PMID, "arxiv_id": m.ArxivID, "version": m.Version, "source_url": m.SourceURL, "publication_events": m.PublicationEvents, "observed_at": o.ObservedAt})
			r.IdentityEdges = append(r.IdentityEdges, map[string]any{"type": "same_record", "from": o.Source + ":" + o.SourceID, "to_record_id": v.ID, "observation_id": o.ID, "basis": "stored source/DOI/PMID/arXiv identity association; title similarity is not an identity key", "confidence": "identifier_match"})
			if o.Source == "biorxiv" || o.Source == "medrxiv" {
				var preprint struct {
					Published string `json:"published"`
				}
				_ = json.Unmarshal(m.Raw, &preprint)
				published := normalizeDOI(preprint.Published)
				if published != "" && published != "na" && published != m.DOI {
					r.IdentityEdges = append(r.IdentityEdges, map[string]any{"type": "version_of", "from": "doi:" + m.DOI, "to": "doi:" + published, "observation_id": o.ID, "basis": "official preprint API published DOI field", "confidence": "source_asserted", "independent_identity_review": "pending"})
				}
			}
		}
		r.Record["field_sources"] = fields
		observationIDs := make([]idgen.ID, 0, len(byPaper[v.ID]))
		for _, o := range byPaper[v.ID] {
			observationIDs = append(observationIDs, o.ID)
		}
		hits := []map[string]any{}
		for afterHit := idgen.ID(0); ; {
			rows, err := s.client.GleanSearchHit.Query().Where(gleansearchhit.RunID(runID), gleansearchhit.ObservationIDIn(observationIDs...), gleansearchhit.IDGT(afterHit)).Order(ent.Asc(gleansearchhit.FieldID)).Limit(500).All(ctx)
			if err != nil {
				return nil, err
			}
			for _, hit := range rows {
				afterHit = hit.ID
				hits = append(hits, map[string]any{"hit_id": hit.ID, "observation_id": hit.ObservationID, "run_id": hit.RunID, "query_id": hit.QueryID, "query_version": hit.QueryVersion, "plan_version": hit.PlanVersion, "source": hit.Source, "native_query": hit.QueryText, "mode": hit.Mode, "discovered_at": hit.DiscoveredAt})
			}
			if len(rows) < 500 {
				break
			}
		}
		r.Record["discovery_paths"] = hits
		changes := []map[string]any{}
		for afterChange := idgen.ID(0); ; {
			rows, err := s.client.GleanCatalogChange.Query().Where(gleancatalogchange.PaperID(v.ID), gleancatalogchange.IDGT(afterChange)).Order(ent.Asc(gleancatalogchange.FieldID)).Limit(500).All(ctx)
			if err != nil {
				return nil, err
			}
			for _, change := range rows {
				afterChange = change.ID
				changes = append(changes, map[string]any{"change_id": change.ID, "kind": change.Kind, "profile_id": change.Topic, "before": auditMetadataOnly(change.Before), "after": auditMetadataOnly(change.After), "actor_id": change.CreatedBy, "created_at": change.CreatedAt})
			}
			if len(rows) < 500 {
				break
			}
		}
		r.Record["catalog_changes"] = changes
		result = append(result, r)
	}
	return result, nil
}

// auditMetadataOnly 保留修改历史和内容哈希，不能通过纠错历史绕过正文再分发边界。
func auditMetadataOnly(value any) any {
	switch v := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			if key == "abstract" || key == "fulltext" || key == "raw" {
				b, _ := json.Marshal(item)
				h := sha256.Sum256(b)
				result[key+"_omitted"] = map[string]any{"sha256": hex.EncodeToString(h[:]), "reason": "content redistribution not authorized"}
			} else {
				result[key] = auditMetadataOnly(item)
			}
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i := range v {
			result[i] = auditMetadataOnly(v[i])
		}
		return result
	default:
		return value
	}
}
