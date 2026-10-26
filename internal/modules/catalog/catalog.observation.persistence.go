package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"entgo.io/ent/dialect"
	sql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"github.com/teamsillybees/glean/internal/data"
	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/gleanmembership"
	"github.com/teamsillybees/glean/internal/data/ent/gleanobservation"
	"github.com/teamsillybees/glean/internal/data/ent/gleanobservationmatch"
	"github.com/teamsillybees/glean/internal/data/ent/gleanpaper"
	"github.com/teamsillybees/glean/internal/data/ent/predicate"
	"github.com/teamsillybees/glean/internal/data/ent/sysuser"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/entx"
)

func normalizeObservation(in *ObservationInput) (string, error) {
	in.EventType = ""
	if in.RunID <= 0 || strings.TrimSpace(in.Source) == "" || strings.TrimSpace(in.SourceID) == "" || strings.TrimSpace(in.Title) == "" || in.ObservedAt.IsZero() {
		return "", bizerrors.BadRequest("source observation requires run, source, sourceId, title and observedAt")
	}
	if len(in.Source) > 100 || len(in.SourceID) > 1024 || len(in.Version) > 300 {
		return "", bizerrors.BadRequest("source identity too long")
	}
	if len(in.Topics) == 0 {
		return "", bizerrors.BadRequest("source observation requires at least one query-hit topic")
	}
	for _, topic := range in.Topics {
		if !validTopic(topic) {
			return "", bizerrors.BadRequest("unknown source query topic")
		}
	}
	if !json.Valid(in.Raw) {
		return "", bizerrors.BadRequest("source observation requires valid raw JSON")
	}
	// 来源元数据是事实；模拟模型只能产生未审核状态，不得提升科学纳入或阅读标记。
	if in.Review.IsMock || !in.Review.ScientificReviewed {
		in.Review.ScientificReviewed = false
		in.Review.InclusionStatus = "pending-review"
	}
	in.DOI = normalizeDOI(in.DOI)
	in.ArxivID = normalizeArxiv(in.ArxivID)
	in.PMID = strings.TrimSpace(in.PMID)
	if in.PMID != "" {
		pmid, err := strconv.ParseUint(in.PMID, 10, 64)
		if err != nil || pmid == 0 {
			return "", bizerrors.BadRequest("source PMID must be a positive decimal identifier")
		}
		in.PMID = strconv.FormatUint(pmid, 10)
	}
	if in.Authors == nil {
		in.Authors = []string{}
	}
	if in.ISSNs == nil {
		in.ISSNs = []string{}
	}
	if in.PublicationEvents == nil {
		in.PublicationEvents = []PublicationEventVO{}
	}
	in.Topics = uniqueSorted(in.Topics)
	in.ISSNs = uniqueSorted(in.ISSNs)
	// 内容散列不含采集时间、运行编号、查询主题和 LLM 输出；重试不制造新来源版本。
	canonical := *in
	canonical.RunID = 0
	canonical.ObservedAt = time.Time{}
	canonical.Topics = nil
	canonical.TemplateVersion = ""
	canonical.CoreDiscovery = false
	canonical.Review = ReviewVO{}
	var raw any
	decoder := json.NewDecoder(bytes.NewReader(in.Raw))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return "", err
	}
	canonical.Raw, _ = json.Marshal(raw)
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}
func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	sort.Strings(result)
	return result
}
func exactFormalDate(events []PublicationEventVO, kind string) *time.Time {
	var result *time.Time
	for _, event := range events {
		if event.Kind != kind || event.Date.IsZero() || event.Evidence == "" {
			continue
		}
		date := event.Date.UTC()
		if result != nil && !result.Equal(date) {
			return nil
		}
		result = &date
	}
	return result
}

// IngestObservation 原子追加真实来源版本及待审核主题发现；不覆盖任何已存历史结论或论文元数据。
func (s *Service) IngestObservation(ctx context.Context, input ObservationInput) (ObservationResult, error) {
	if input.Kind != "" || input.AbstractEvidence != nil {
		return ObservationResult{}, &observationRejection{kind: "invalid_source_record", err: bizerrors.BadRequest("historical abstract evidence requires the restricted enrichment path")}
	}
	hash, err := normalizeObservation(&input)
	if err != nil {
		return ObservationResult{}, &observationRejection{kind: "invalid_source_record", err: err}
	}
	operator, ok := entx.OperatorIDFromContext(ctx)
	if !ok || operator <= 0 {
		return ObservationResult{}, bizerrors.BadRequest("explicit monitor audit operator is required")
	}
	var result ObservationResult
	for attempt := 0; attempt < 3; attempt++ {
		result = ObservationResult{}
		err = data.WithinTx(ctx, s.client, func(ctx context.Context, c *ent.Client) error {
			if _, err := c.SysUser.Query().Where(sysuser.ID(operator), sysuser.DeletedAtIsNil(), sysuser.IsEnable(true)).ForShare().Only(ctx); err != nil {
				return err
			}
			identity := "source:" + input.Source + ":" + input.SourceID
			predicates := []predicate.GleanPaper{gleanpaper.IdentityKey(identity), func(sel *sql.Selector) {
				observations := sql.Table(gleanobservation.Table)
				sub := sql.Dialect(dialect.Postgres).Select(observations.C(gleanobservation.FieldPaperID)).From(observations).Where(sql.And(sql.EQ(observations.C(gleanobservation.FieldSource), input.Source), sql.EQ(observations.C(gleanobservation.FieldSourceID), input.SourceID)))
				sel.Where(sql.In(sel.C(gleanpaper.FieldID), sub))
			}}
			if input.DOI != "" {
				predicates = append(predicates, gleanpaper.Doi(input.DOI), observedPaperIdentity("doi", input.DOI))
			}
			if input.ArxivID != "" {
				predicates = append(predicates, gleanpaper.ArxivID(input.ArxivID), observedPaperIdentity("arxivId", input.ArxivID))
			}
			if input.PMID != "" {
				predicates = append(predicates, gleanpaper.IdentityKey("pmid:"+input.PMID), observedPaperIdentity("pmid", input.PMID))
			}
			papers, err := c.GleanPaper.Query().Where(gleanpaper.Or(predicates...)).ForUpdate().All(ctx)
			if err != nil {
				return err
			}
			if len(papers) > 1 {
				return fmt.Errorf("%w: source metadata resolves to multiple historical identities", errIdentityConflict)
			}
			var paper *ent.GleanPaper
			if len(papers) == 1 {
				paper = papers[0]
				if (input.DOI != "" && paper.Doi != nil && *paper.Doi != input.DOI) || (input.ArxivID != "" && paper.ArxivID != nil && *paper.ArxivID != input.ArxivID) {
					return fmt.Errorf("%w: source metadata contradicts an established DOI or arXiv identity", errIdentityConflict)
				}
				for key, value := range map[string]string{"doi": input.DOI, "arxivId": input.ArxivID, "pmid": input.PMID} {
					if value == "" {
						continue
					}
					differs, err := c.GleanObservation.Query().Where(gleanobservation.PaperID(paper.ID), func(sel *sql.Selector) {
						sel.Where(sql.And(sqljson.ValueNEQ(sel.C(gleanobservation.FieldMetadata), "", sqljson.Path(key)), sqljson.ValueNEQ(sel.C(gleanobservation.FieldMetadata), value, sqljson.Path(key))))
					}).Exist(ctx)
					if err != nil {
						return err
					}
					if differs {
						return fmt.Errorf("%w: source metadata contradicts an observed %s identity", errIdentityConflict, key)
					}
				}
			} else {
				if input.PMID != "" {
					// 同 PMID 的跨来源并发首次写入共享唯一键，即使仅一方提供 DOI。
					identity = "pmid:" + input.PMID
				} else if input.DOI != "" {
					identity = "doi:" + input.DOI
				} else if input.ArxivID != "" {
					identity = "arxiv:" + input.ArxivID
				}
				// 只投影唯一、明示的正式事件；冲突日期不择早，全部事件仍保存在不可变 observation。
				online := exactFormalDate(input.PublicationEvents, "online")
				issue := exactFormalDate(input.PublicationEvents, "issue")
				year := 0
				if online != nil {
					year = online.Year()
				} else if issue != nil {
					year = issue.Year()
				}
				create := c.GleanPaper.Create().SetIdentityKey(identity).SetOrigin("live").SetTitle(input.Title).SetAbstract(input.Abstract).SetAuthors(input.Authors).SetYear(year).SetVenue(input.Venue).SetPublicationType(input.PublicationType).SetURL(input.URL).SetIsMock(false).SetNillableFormalOnlineDate(online).SetNillableIssueDate(issue).SetDateProvenance("source-declared formal events; see immutable observations")
				if input.DOI != "" {
					create.SetDoi(input.DOI)
				}
				if input.ArxivID != "" {
					create.SetArxivID(input.ArxivID)
				}
				paper, err = create.Save(ctx)
				if err != nil {
					return err
				}
			}
			observation, err := c.GleanObservation.Query().Where(gleanobservation.Source(input.Source), gleanobservation.SourceID(input.SourceID), gleanobservation.ContentHash(hash)).Only(ctx)
			if ent.IsNotFound(err) {
				input.EventType = "source_observed"
				previous, previousErr := c.GleanObservation.Query().Where(gleanobservation.Source(input.Source), gleanobservation.SourceID(input.SourceID), func(sel *sql.Selector) { sel.Where(liveObservationSQL(sel.C(gleanobservation.FieldMetadata))) }).Order(ent.Desc(gleanobservation.FieldID)).First(ctx)
				if previousErr == nil {
					input.EventType = sourceChangeType(input.Source, previous.Version, input.Version)
				} else if !ent.IsNotFound(previousErr) {
					return previousErr
				}
				metadata, marshalErr := json.Marshal(input)
				if marshalErr != nil {
					return marshalErr
				}
				events := []map[string]any{}
				encoded, _ := json.Marshal(input.PublicationEvents)
				if err := json.Unmarshal(encoded, &events); err != nil {
					return err
				}
				review := map[string]any{}
				encoded, _ = json.Marshal(input.Review)
				if err := json.Unmarshal(encoded, &review); err != nil {
					return err
				}
				observation, err = c.GleanObservation.Create().SetPaperID(paper.ID).SetRunID(input.RunID).SetSource(input.Source).SetSourceID(input.SourceID).SetVersion(input.Version).SetContentHash(hash).SetObservedAt(input.ObservedAt).SetNillableSourceUpdatedAt(input.SourceUpdatedAt).SetMetadata(metadata).SetPublicationEvents(events).SetReview(review).Save(ctx)
				result.Created = err == nil
			}
			if err != nil {
				return err
			}
			if observation.PaperID != paper.ID {
				return fmt.Errorf("%w: observation parent changed", errIdentityConflict)
			}
			result.PaperID = paper.ID
			result.ObservationID = observation.ID
			for _, topic := range input.Topics {
				match, err := c.GleanObservationMatch.Query().Where(gleanobservationmatch.ObservationID(observation.ID), gleanobservationmatch.Topic(topic)).Exist(ctx)
				if err != nil {
					return err
				}
				if !match {
					if _, err = c.GleanObservationMatch.Create().SetObservationID(observation.ID).SetTopic(topic).SetTemplateVersion(input.TemplateVersion).Save(ctx); err != nil {
						return err
					}
				}
				exists, err := c.GleanMembership.Query().Where(gleanmembership.PaperID(paper.ID), gleanmembership.Topic(topic)).Exist(ctx)
				if err != nil {
					return err
				}
				if exists {
					continue
				}
				_, err = c.GleanMembership.Create().SetPaperID(paper.ID).SetTopic(topic).SetOriginalID("discovered:" + paper.ID.String()).SetSource(input.Source).SetInclusionStatus("pending-review").SetReadStatus("unread").SetIsMock(false).SetDimensions(map[string][]string{"inclusion_status": {"pending-review"}, "display_review_status": {"待审核（来源检索命中）"}}).SetRaw(map[string]any{"observationId": observation.ID.String(), "source": input.Source, "sourceId": input.SourceID, "note": "query hit only; not a scientific inclusion decision"}).Save(ctx)
				if err != nil {
					return err
				}
				result.MembershipsAdded++
			}
			return nil
		})
		if err == nil {
			return result, nil
		}
		if errors.Is(err, errIdentityConflict) {
			return ObservationResult{}, &observationRejection{kind: "identity_conflict", err: bizerrors.BadRequest(err.Error())}
		}
		if !ent.IsConstraintError(err) {
			return ObservationResult{}, bizerrors.WrapDBContext(ctx, err, "persist source observation failed")
		}
	}
	return ObservationResult{}, bizerrors.WrapDBContext(ctx, fmt.Errorf("concurrent observation conflict"), "persist source observation requires retry")
}

// observedPaperIdentity 只由内部固定标识键调用；证据中的新增标识可桥接来源而无需改写历史论文。
func observedPaperIdentity(key, value string) predicate.GleanPaper {
	return func(sel *sql.Selector) {
		observations := sql.Table(gleanobservation.Table)
		sub := sql.Dialect(dialect.Postgres).Select(observations.C(gleanobservation.FieldPaperID)).From(observations).Where(sqljson.ValueEQ(observations.C(gleanobservation.FieldMetadata), value, sqljson.Path(key)))
		sel.Where(sql.In(sel.C(gleanpaper.FieldID), sub))
	}
}

func sourceChangeType(source, previous, current string) string {
	if source == "arxiv" || source == "biorxiv" || source == "medrxiv" {
		before, errBefore := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(previous), "v"))
		after, errAfter := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(current), "v"))
		if errBefore == nil && errAfter == nil && before > 0 && after > before {
			return "version_updated"
		}
	}
	return "metadata_updated"
}
