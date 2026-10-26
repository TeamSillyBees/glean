package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/teamsillybees/glean/internal/data"
	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/gleanmembership"
	"github.com/teamsillybees/glean/internal/data/ent/gleanpaper"
	"github.com/teamsillybees/glean/internal/data/ent/gleanvenue"
	"github.com/teamsillybees/glean/internal/data/ent/predicate"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
)

var errIdentityConflict = errors.New("identifier conflict")

// Import 在一个事务中保存单条记录；相同主题和原始标识的重放不改历史元数据。
func (s *Service) Import(ctx context.Context, r ImportRecord) (ImportResult, error) {
	if err := normalizeImport(&r); err != nil {
		return ImportResult{}, err
	}
	var result ImportResult
	// 唯一约束负责跨进程并发，冲突后以新事务重读，绝不忽略冲突覆盖历史。
	for attempt := 0; attempt < 3; attempt++ {
		result = ImportResult{}
		err := data.WithinTx(ctx, s.client, func(ctx context.Context, c *ent.Client) error {
			existing, err := c.GleanMembership.Query().Where(gleanmembership.Topic(r.Topic), gleanmembership.OriginalID(r.OriginalID)).Only(ctx)
			if err == nil {
				result = ImportResult{PaperID: existing.PaperID, MembershipID: existing.ID}
				return nil
			}
			if !ent.IsNotFound(err) {
				return err
			}
			matches := []predicate.GleanPaper{gleanpaper.IdentityKey(identityKey(r))}
			if r.DOI != "" {
				matches = append(matches, gleanpaper.Doi(r.DOI))
			}
			if r.ArxivID != "" {
				matches = append(matches, gleanpaper.ArxivID(r.ArxivID))
			}
			papers, err := c.GleanPaper.Query().Where(gleanpaper.Or(matches...)).ForUpdate().All(ctx)
			if err != nil {
				return err
			}
			if len(papers) > 1 {
				return fmt.Errorf("%w: DOI and arXiv resolve to different historical papers", errIdentityConflict)
			}
			var paper *ent.GleanPaper
			if len(papers) == 1 {
				paper = papers[0]
				// 一个确定标识只能补全缺失别名；已有不同标识不允许自动合并。
				if r.DOI != "" && paper.Doi != nil && *paper.Doi != r.DOI {
					return fmt.Errorf("%w: conflicting DOI on arXiv identity", errIdentityConflict)
				}
				if r.ArxivID != "" && paper.ArxivID != nil && *paper.ArxivID != r.ArxivID {
					return fmt.Errorf("%w: conflicting arXiv identity on DOI", errIdentityConflict)
				}
				update := c.GleanPaper.UpdateOneID(paper.ID)
				changed := false
				if r.DOI != "" && paper.Doi == nil {
					update.SetDoi(r.DOI)
					changed = true
				}
				if r.ArxivID != "" && paper.ArxivID == nil {
					update.SetArxivID(r.ArxivID)
					changed = true
				}
				if changed {
					if err := update.Exec(ctx); err != nil {
						return err
					}
				}
			} else {
				evidence := []map[string]any{}
				encoded, _ := json.Marshal(r.Evidence)
				if err := json.Unmarshal(encoded, &evidence); err != nil {
					return err
				}
				create := c.GleanPaper.Create().SetIdentityKey(identityKey(r)).SetTitle(r.Title).SetTitleZh(r.TitleZh).SetAbstract(r.Abstract).SetAuthors(r.Authors).SetYear(r.Year).SetVenue(r.Venue).SetPublicationType(r.PublicationType).SetURL(r.URL).SetNillableFormalOnlineDate(r.FormalOnlineDate).SetNillableIssueDate(r.IssueDate).SetDateProvenance(r.DateProvenance).SetEvidence(evidence).SetIsMock(r.IsMock)
				if r.DOI != "" {
					create.SetDoi(r.DOI)
				}
				if r.ArxivID != "" {
					create.SetArxivID(r.ArxivID)
				}
				paper, err = create.Save(ctx)
				if err != nil {
					return err
				}
			}
			membership, err := c.GleanMembership.Create().SetPaperID(paper.ID).SetTopic(r.Topic).SetOriginalID(r.OriginalID).SetTier(r.Tier).SetPrimaryCategory(r.PrimaryCategory).SetCategories(r.Categories).SetDimensions(r.Dimensions).SetReadStatus(r.ReadStatus).SetSource(r.Source).SetIsMock(r.IsMock).SetRaw(r.Raw).Save(ctx)
			if err != nil {
				return err
			}
			result = ImportResult{PaperID: paper.ID, MembershipID: membership.ID, Created: true}
			return nil
		})
		if err == nil {
			return result, nil
		}
		if errors.Is(err, errIdentityConflict) {
			return ImportResult{}, bizerrors.BadRequest(err.Error())
		}
		if !ent.IsConstraintError(err) {
			return ImportResult{}, bizerrors.WrapDBContext(ctx, err, "import historical paper failed")
		}
	}
	return ImportResult{}, bizerrors.WrapDBContext(ctx, fmt.Errorf("concurrent identity conflict"), "import requires retry")
}

// RegisterVenue 追加版本化期刊资格；相同版本重复导入保持原始证据不变。
func (s *Service) RegisterVenue(ctx context.Context, v VenueVO) error {
	if v.Name == "" || v.RegistryVersion == "" {
		return bizerrors.BadRequest("venue name and registryVersion required")
	}
	exists, err := s.client.GleanVenue.Query().Where(gleanvenue.Name(v.Name), gleanvenue.RegistryVersion(v.RegistryVersion)).Exist(ctx)
	if err != nil {
		return bizerrors.WrapDBContext(ctx, err, "query venue failed")
	}
	if exists {
		return nil
	}
	if v.Kind == "" {
		v.Kind = "journal"
	}
	if v.QualificationStatus == "" {
		v.QualificationStatus = "pending"
	}
	if v.Evidence == nil {
		v.Evidence = map[string]any{}
	}
	if v.EligibleTopics == nil {
		v.EligibleTopics = []string{}
	}
	_, err = s.client.GleanVenue.Create().SetName(v.Name).SetKind(v.Kind).SetNillableJif(v.JIF).SetCcf(v.CCF).SetMetricYear(v.MetricYear).SetRegistryVersion(v.RegistryVersion).SetSource(v.Source).SetEvidence(v.Evidence).SetEligibleTopics(v.EligibleTopics).SetQualificationStatus(v.QualificationStatus).Save(ctx)
	if ent.IsConstraintError(err) {
		exists, lookupErr := s.client.GleanVenue.Query().Where(gleanvenue.Name(v.Name), gleanvenue.RegistryVersion(v.RegistryVersion)).Exist(ctx)
		if lookupErr == nil && exists {
			return nil
		}
	}
	if err != nil {
		return bizerrors.WrapDBContext(ctx, err, "register venue failed")
	}
	return nil
}
