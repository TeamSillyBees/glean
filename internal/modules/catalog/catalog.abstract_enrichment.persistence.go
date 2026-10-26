package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	sql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"github.com/teamsillybees/glean/internal/abstractenrichment"
	"github.com/teamsillybees/glean/internal/data"
	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/gleanmembership"
	"github.com/teamsillybees/glean/internal/data/ent/gleanobservation"
	"github.com/teamsillybees/glean/internal/data/ent/gleanobservationmatch"
	"github.com/teamsillybees/glean/internal/data/ent/gleanpaper"
	"github.com/teamsillybees/glean/internal/data/ent/sysuser"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/entx"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

const historicalAbstractKind = "historical-abstract-enrichment"

// EnrichHistoricalAbstract 仅向已存在且精确匹配的历史成员追加固定白名单来源摘要，绝不创建论文、主题成员或科学判定。
func (s *Service) EnrichHistoricalAbstract(ctx context.Context, record abstractenrichment.Record, runID idgen.ID, enrichedAt time.Time) (ObservationResult, error) {
	if err := abstractenrichment.ValidateRecord(record); err != nil {
		return ObservationResult{}, &observationRejection{kind: "invalid_source_record", err: bizerrors.BadRequest("historical abstract is not a verified allowlisted response")}
	}
	if runID <= 0 || enrichedAt.IsZero() || record.OriginalID == "BAI2E6C229787" {
		return ObservationResult{}, &observationRejection{kind: "invalid_source_record", err: bizerrors.BadRequest("historical abstract requires a valid audit run and time")}
	}
	// PostgreSQL timestamptz 只保存微秒；元数据与列使用同一精度，真实 time.Now 输入也可精确重放。
	enrichedAt = enrichedAt.UTC().Truncate(time.Microsecond)
	operator, ok := entx.OperatorIDFromContext(ctx)
	if !ok || operator <= 0 {
		return ObservationResult{}, bizerrors.BadRequest("explicit enrichment audit operator is required")
	}
	metadata := historicalAbstractMetadata(record, runID, enrichedAt.UTC())
	// 只对稳定来源事实及定位身份求散列；重复运行和补录时间不产生新版本。
	canonical := record
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return ObservationResult{}, err
	}
	digest := sha256.Sum256(append([]byte(historicalAbstractKind+"\n"), encoded...))
	hash := hex.EncodeToString(digest[:])
	for attempt := 0; attempt < 3; attempt++ {
		result := ObservationResult{}
		err = data.WithinTx(ctx, s.client, func(ctx context.Context, c *ent.Client) error {
			if _, err := c.SysUser.Query().Where(sysuser.ID(operator), sysuser.DeletedAtIsNil(), sysuser.IsEnable(true)).ForShare().Only(ctx); err != nil {
				return err
			}
			membership, err := c.GleanMembership.Query().Where(gleanmembership.Topic(record.Topic), gleanmembership.OriginalID(record.OriginalID), gleanmembership.InclusionStatus("historical")).ForShare().Only(ctx)
			if ent.IsNotFound(err) {
				return fmt.Errorf("%w: enrichment target historical membership does not exist", errIdentityConflict)
			}
			if err != nil {
				return err
			}
			paper, err := c.GleanPaper.Query().Where(gleanpaper.ID(membership.PaperID)).ForUpdate().Only(ctx)
			if err != nil {
				return err
			}
			if paper.Origin != "historical" || paper.Doi == nil || normalizeDOI(*paper.Doi) != record.DOI {
				return fmt.Errorf("%w: enrichment DOI does not match the established historical paper", errIdentityConflict)
			}
			conflict, err := c.GleanObservation.Query().Where(gleanobservation.Source(record.Source), gleanobservation.SourceID(record.SourceID), gleanobservation.PaperIDNEQ(paper.ID)).Exist(ctx)
			if err != nil {
				return err
			}
			if conflict {
				return fmt.Errorf("%w: source record is already attached to another paper", errIdentityConflict)
			}
			conflict, err = c.GleanObservation.Query().Where(gleanobservation.PaperID(paper.ID), func(sel *sql.Selector) {
				sel.Where(sql.And(sqljson.ValueNEQ(sel.C(gleanobservation.FieldMetadata), "", sqljson.Path("doi")), sqljson.ValueNEQ(sel.C(gleanobservation.FieldMetadata), record.DOI, sqljson.Path("doi"))))
			}).Exist(ctx)
			if err != nil {
				return err
			}
			if conflict {
				return fmt.Errorf("%w: enrichment contradicts an already observed DOI", errIdentityConflict)
			}
			observation, err := c.GleanObservation.Query().Where(gleanobservation.Source(record.Source), gleanobservation.SourceID(record.SourceID), gleanobservation.ContentHash(hash)).Only(ctx)
			if ent.IsNotFound(err) {
				payload, err := json.Marshal(metadata)
				if err != nil {
					return err
				}
				observation, err = c.GleanObservation.Create().SetPaperID(paper.ID).SetRunID(runID).SetSource(record.Source).SetSourceID(record.SourceID).SetVersion(record.SourceVersion).SetContentHash(hash).SetObservedAt(enrichedAt.UTC()).SetMetadata(payload).SetPublicationEvents([]map[string]any{}).SetReview(map[string]any{}).Save(ctx)
				if err != nil {
					return err
				}
				result.Created = true
			} else if err != nil {
				return err
			} else {
				var stored ObservationInput
				if json.Unmarshal(observation.Metadata, &stored) != nil {
					return fmt.Errorf("%w: stored enrichment metadata cannot be verified", errIdentityConflict)
				}
				verified := selectedStoredHistoricalAbstract(observation, stored)
				if verified == nil || verified.Abstract != record.Abstract || verified.DOI != record.DOI || verified.Source != record.Source || verified.SourceID != record.SourceID {
					return fmt.Errorf("%w: stored enrichment content or provenance failed revalidation", errIdentityConflict)
				}
			}
			if observation.PaperID != paper.ID {
				return fmt.Errorf("%w: enrichment observation belongs to another paper", errIdentityConflict)
			}
			exists, err := c.GleanObservationMatch.Query().Where(gleanobservationmatch.ObservationID(observation.ID), gleanobservationmatch.Topic(record.Topic)).Exist(ctx)
			if err != nil {
				return err
			}
			if !exists {
				if _, err := c.GleanObservationMatch.Create().SetObservationID(observation.ID).SetTopic(record.Topic).SetTemplateVersion("historical-abstract-v1").Save(ctx); err != nil {
					return err
				}
			}
			result.PaperID, result.ObservationID = paper.ID, observation.ID
			return nil
		})
		if err == nil {
			return result, nil
		}
		if errors.Is(err, errIdentityConflict) {
			return ObservationResult{}, &observationRejection{kind: "identity_conflict", err: bizerrors.BadRequest(err.Error())}
		}
		if !ent.IsConstraintError(err) {
			return ObservationResult{}, bizerrors.WrapDBContext(ctx, err, "persist historical abstract failed")
		}
	}
	return ObservationResult{}, bizerrors.WrapDBContext(ctx, err, "historical abstract retry required")
}

func historicalAbstractMetadata(r abstractenrichment.Record, runID idgen.ID, at time.Time) ObservationInput {
	raw, _ := json.Marshal(map[string]string{"baselineTopic": r.Topic, "originalId": r.OriginalID})
	return ObservationInput{Kind: historicalAbstractKind, RunID: runID, Source: r.Source, SourceID: r.SourceID, SourceURL: r.SourceURL, Version: r.SourceVersion, Title: r.Title, Abstract: r.Abstract, URL: r.SourceURL, DOI: r.DOI, ObservedAt: at, Topics: []string{r.Topic}, TemplateVersion: "historical-abstract-v1", Raw: raw, Authors: []string{}, ISSNs: []string{}, PublicationEvents: []PublicationEventVO{},
		AbstractEvidence: &AbstractEvidenceVO{Source: r.Source, SourceID: r.SourceID, SourceURL: r.SourceURL, DOI: r.DOI, SourceVersion: r.SourceVersion, Completeness: r.Completeness, VerificationStatus: "source-response-verified", AbstractHash: r.AbstractSHA256, ResponseHash: r.ResponseSHA256, ParserVersion: r.ParserVersion, SourceField: r.SourceField, EnrichedAt: at}}
}

func selectedHistoricalAbstract(o SourceObservationVO) *AbstractEnrichmentVO {
	if o.Kind != historicalAbstractKind || o.AbstractEvidence == nil || o.AbstractEvidence.VerificationStatus != "source-response-verified" || o.AbstractEvidence.EnrichedAt.IsZero() {
		return nil
	}
	var binding struct {
		Topic      string `json:"baselineTopic"`
		OriginalID string `json:"originalId"`
	}
	if json.Unmarshal(o.Raw, &binding) != nil {
		return nil
	}
	e := o.AbstractEvidence
	r := abstractenrichment.Record{Abstract: o.Abstract, Spec: abstractenrichment.Spec{Topic: binding.Topic, OriginalID: binding.OriginalID, DOI: o.DOI, Title: o.Title, Source: e.Source, SourceID: e.SourceID, SourceURL: e.SourceURL, SourceVersion: e.SourceVersion, ResponseSHA256: e.ResponseHash, AbstractSHA256: e.AbstractHash, ParserVersion: e.ParserVersion, SourceField: e.SourceField, Completeness: e.Completeness}}
	if abstractenrichment.ValidateRecord(r) != nil || e.DOI != o.DOI || e.Source != o.Source || e.SourceID != o.SourceID || e.SourceURL != o.SourceURL || e.SourceVersion != o.Version {
		return nil
	}
	return &AbstractEnrichmentVO{AbstractEvidenceVO: *e, ObservationID: o.ID, Abstract: o.Abstract}
}

func selectedStoredHistoricalAbstract(o *ent.GleanObservation, metadata ObservationInput) *AbstractEnrichmentVO {
	if metadata.Source != o.Source || metadata.SourceID != o.SourceID || metadata.Version != o.Version || metadata.RunID != o.RunID || !metadata.ObservedAt.Equal(o.ObservedAt) || metadata.AbstractEvidence == nil || !metadata.AbstractEvidence.EnrichedAt.Equal(o.ObservedAt) {
		return nil
	}
	return selectedHistoricalAbstract(SourceObservationVO{ObservationInput: metadata, ID: o.ID, ContentHash: o.ContentHash})
}

// liveObservationSQL 保持缓存补录与实时采集的更新、排序及模型审读独立；旧记录缺少 kind 时仍为实时观察。
func liveObservationSQL(metadataColumn string) *sql.Predicate {
	return sql.P(func(b *sql.Builder) {
		b.WriteString("COALESCE(").Ident(metadataColumn).WriteString("->>'kind', '') <> ").Arg(historicalAbstractKind)
	})
}
