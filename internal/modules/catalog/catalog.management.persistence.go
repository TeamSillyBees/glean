package catalog

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/teamsillybees/glean/internal/data"
	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/gleancatalogchange"
	"github.com/teamsillybees/glean/internal/data/ent/gleanmembership"
	"github.com/teamsillybees/glean/internal/data/ent/gleanpaper"
	"github.com/teamsillybees/glean/internal/data/ent/gleanpapercorrection"
	"github.com/teamsillybees/glean/internal/data/ent/gleanpaperreview"
	"github.com/teamsillybees/glean/internal/data/ent/gleantopic"
	"github.com/teamsillybees/glean/internal/data/ent/sysuser"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/retrieval"
)

func canManage(ctx context.Context) bool {
	p, ok := platformauth.PrincipalFromContext(ctx)
	return ok && p.IsSuperAdmin
}
func requireManager(ctx context.Context) (idgen.ID, error) {
	uid, err := userID(ctx)
	if err != nil {
		return 0, err
	}
	if !canManage(ctx) {
		return 0, bizerrors.CatalogForbidden()
	}
	return uid, nil
}
func lockManager(ctx context.Context, c *ent.Client, uid idgen.ID) error {
	_, err := c.SysUser.Query().Where(sysuser.ID(uid), sysuser.DeletedAtIsNil(), sysuser.IsEnable(true), sysuser.IsSuperAdmin(true)).ForShare().Only(ctx)
	if ent.IsNotFound(err) {
		return bizerrors.CatalogForbidden()
	}
	return err
}
func object(value any) map[string]any {
	raw, _ := json.Marshal(value)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}
func recordChange(ctx context.Context, c *ent.Client, uid, paperID idgen.ID, topic, kind string, before, after any) error {
	return c.GleanCatalogChange.Create().SetPaperID(paperID).SetTopic(topic).SetKind(kind).SetBefore(object(before)).SetAfter(object(after)).SetCreatedBy(uid).SetUpdatedBy(uid).Exec(ctx)
}
func reviewVO(r *ent.GleanPaperReview) ManualReviewVO {
	return ManualReviewVO{Status: string(r.Status), Note: r.Note, Labels: r.Labels, Version: r.Version, UpdatedAt: r.UpdatedAt, UpdatedBy: r.UpdatedBy, Evidence: r.Evidence}
}
func correctionVO(r *ent.GleanPaperCorrection) CorrectionVO {
	var fields CorrectionFields
	b, _ := json.Marshal(r.Fields)
	_ = json.Unmarshal(b, &fields)
	return CorrectionVO{Fields: fields, Reason: r.Reason, Version: r.Version, UpdatedAt: r.UpdatedAt, UpdatedBy: r.UpdatedBy}
}

// SaveReview 原子写入人工决定与审计，不修改任何来源分类或其他主题。
func (s *Service) SaveReview(ctx context.Context, paperID idgen.ID, topic string, input ReviewBody) (ManualReviewVO, error) {
	uid, err := requireManager(ctx)
	if err != nil {
		return ManualReviewVO{}, err
	}
	if input.Status != "pending" && input.Status != "included" && input.Status != "excluded" {
		return ManualReviewVO{}, bizerrors.BadRequest("invalid review status")
	}
	if err := retrieval.ValidateScreening(input.Status, input.Note, input.Evidence); err != nil {
		return ManualReviewVO{}, bizerrors.BadRequest(err.Error())
	}
	if input.ExpectedVersion < 0 || len(input.Note) > 16000 || strings.ContainsRune(input.Note, 0) || len(input.Labels) > 30 {
		return ManualReviewVO{}, bizerrors.BadRequest("invalid review fields")
	}
	for i, label := range input.Labels {
		input.Labels[i] = strings.TrimSpace(label)
		if input.Labels[i] == "" || len(label) > 300 || strings.ContainsRune(label, 0) {
			return ManualReviewVO{}, bizerrors.BadRequest("invalid label")
		}
	}
	input.Labels = uniqueSorted(input.Labels)
	if input.Labels == nil {
		input.Labels = []string{}
	}
	var result ManualReviewVO
	err = data.WithinTx(ctx, s.client, func(ctx context.Context, c *ent.Client) error {
		if err := lockManager(ctx, c, uid); err != nil {
			return err
		}
		// 同一父论文行序列化首次创建和后续更新，避免检查版本后插入竞态。
		if _, err := c.GleanPaper.Query().Where(gleanpaper.ID(paperID)).ForUpdate().Only(ctx); err != nil {
			return err
		}
		exists, err := c.GleanMembership.Query().Where(gleanmembership.PaperID(paperID), gleanmembership.Topic(topic)).Exist(ctx)
		if err != nil {
			return err
		}
		if !exists {
			return bizerrors.BadRequest("paper does not belong to this topic")
		}
		if input.Evidence != nil {
			config, e := c.GleanTopic.Query().Where(gleantopic.Slug(topic)).ForShare().Only(ctx)
			if e != nil && !ent.IsNotFound(e) {
				return e
			}
			if config != nil && config.SearchProtocol != nil {
				p := config.SearchProtocol
				if input.Evidence.ProfileID != p.ProfileID || input.Evidence.ProfileVersion != p.ProfileVersion {
					return bizerrors.BadRequest("审核证据必须使用当前主题的协议版本")
				}
			} else if input.Evidence.ProfileID != topic {
				return bizerrors.BadRequest("审核证据的 profile 必须对应当前主题")
			}
		}
		previous, err := c.GleanPaperReview.Query().Where(gleanpaperreview.PaperID(paperID), gleanpaperreview.Topic(topic)).Only(ctx)
		var before any = map[string]any{}
		var record *ent.GleanPaperReview
		if ent.IsNotFound(err) {
			if input.ExpectedVersion != 0 {
				return bizerrors.CatalogConflict()
			}
			record, err = c.GleanPaperReview.Create().SetPaperID(paperID).SetTopic(topic).SetStatus(gleanpaperreview.Status(input.Status)).SetNote(input.Note).SetLabels(input.Labels).SetEvidence(input.Evidence).SetCreatedBy(uid).SetUpdatedBy(uid).Save(ctx)
		} else if err == nil {
			if previous.Version != input.ExpectedVersion {
				return bizerrors.CatalogConflict()
			}
			before = reviewVO(previous)
			record, err = c.GleanPaperReview.UpdateOneID(previous.ID).SetStatus(gleanpaperreview.Status(input.Status)).SetNote(input.Note).SetLabels(input.Labels).SetEvidence(input.Evidence).SetVersion(previous.Version + 1).SetUpdatedBy(uid).Save(ctx)
		}
		if err != nil {
			return err
		}
		result = reviewVO(record)
		return recordChange(ctx, c, uid, paperID, topic, "review", before, result)
	})
	return result, managementError(ctx, err)
}
func validateCorrection(input CorrectionBody) error {
	f := input.Fields
	for _, value := range []*string{f.Title, f.TitleZh, f.Venue, f.Abstract} {
		if value != nil && strings.ContainsRune(*value, 0) {
			return bizerrors.BadRequest("text cannot contain NUL")
		}
	}
	if input.ExpectedVersion < 0 || len(input.Reason) > 8000 || strings.ContainsRune(input.Reason, 0) {
		return bizerrors.BadRequest("invalid correction")
	}
	if f.Title != nil && (strings.TrimSpace(*f.Title) == "" || len(*f.Title) > 8000) {
		return bizerrors.BadRequest("title must not be empty or too long")
	}
	if f.TitleZh != nil && len(*f.TitleZh) > 8000 || f.Venue != nil && len(*f.Venue) > 2000 || f.Abstract != nil && len(*f.Abstract) > 120000 {
		return bizerrors.BadRequest("correction field too long")
	}
	if f.Year != nil && (*f.Year < 0 || *f.Year > 9999) {
		return bizerrors.BadRequest("invalid year")
	}
	if f.Authors != nil {
		if len(*f.Authors) > 100 {
			return bizerrors.BadRequest("too many authors")
		}
		for _, a := range *f.Authors {
			if strings.TrimSpace(a) == "" || len(a) > 1000 || strings.ContainsRune(a, 0) {
				return bizerrors.BadRequest("invalid author")
			}
		}
	}
	return nil
}

// SaveCorrection 保存完整书目覆盖层与审计，省略字段即恢复来源值。
func (s *Service) SaveCorrection(ctx context.Context, paperID idgen.ID, input CorrectionBody) (CorrectionVO, error) {
	uid, err := requireManager(ctx)
	if err != nil {
		return CorrectionVO{}, err
	}
	if err = validateCorrection(input); err != nil {
		return CorrectionVO{}, err
	}
	var result CorrectionVO
	err = data.WithinTx(ctx, s.client, func(ctx context.Context, c *ent.Client) error {
		if err := lockManager(ctx, c, uid); err != nil {
			return err
		}
		if _, err := c.GleanPaper.Query().Where(gleanpaper.ID(paperID)).ForUpdate().Only(ctx); err != nil {
			return err
		}
		previous, err := c.GleanPaperCorrection.Query().Where(gleanpapercorrection.PaperID(paperID)).Only(ctx)
		var before any = map[string]any{}
		var record *ent.GleanPaperCorrection
		if ent.IsNotFound(err) {
			if input.ExpectedVersion != 0 {
				return bizerrors.CatalogConflict()
			}
			record, err = c.GleanPaperCorrection.Create().SetPaperID(paperID).SetFields(object(input.Fields)).SetReason(input.Reason).SetCreatedBy(uid).SetUpdatedBy(uid).Save(ctx)
		} else if err == nil {
			if previous.Version != input.ExpectedVersion {
				return bizerrors.CatalogConflict()
			}
			before = correctionVO(previous)
			record, err = c.GleanPaperCorrection.UpdateOneID(previous.ID).SetFields(object(input.Fields)).SetReason(input.Reason).SetVersion(previous.Version + 1).SetUpdatedBy(uid).Save(ctx)
		}
		if err != nil {
			return err
		}
		result = correctionVO(record)
		return recordChange(ctx, c, uid, paperID, "", "correction", before, result)
	})
	return result, managementError(ctx, err)
}
func managementError(ctx context.Context, err error) error {
	if ent.IsNotFound(err) {
		return bizerrors.WrapNotFoundContext(ctx, err, "catalog record not found")
	}
	if ent.IsConstraintError(err) {
		return bizerrors.CatalogConflict()
	}
	return err
}

var topicSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,79}$`)

func normalizeTopic(input *TopicBody) error { return normalizeTopicInput(input, true) }

func normalizeTopicInput(input *TopicBody, requireSearch bool) error {
	for _, value := range []string{input.Name, input.Description, input.Rule} {
		if strings.ContainsRune(value, 0) {
			return bizerrors.BadRequest("topic text cannot contain NUL")
		}
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Slug = strings.TrimSpace(input.Slug)
	if input.Slug == "" {
		raw := strings.ToLower(input.Name)
		raw = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(raw, "-")
		raw = strings.Trim(raw, "-")
		if len(raw) > 80 {
			raw = strings.TrimRight(raw[:80], "-")
		}
		if raw == "" {
			raw = "topic-" + idgen.NextID().String()
		}
		input.Slug = raw
	}
	if !topicSlugPattern.MatchString(input.Slug) || input.Name == "" || len(input.Name) > 400 || len(input.Description) > 8000 || len(input.Rule) > 16000 || input.ExpectedVersion < 0 || len(input.TermGroups) > 10 {
		return bizerrors.BadRequest("invalid topic configuration")
	}
	for i, g := range input.TermGroups {
		if len(g) == 0 || len(g) > 50 {
			return bizerrors.BadRequest("each keyword group needs 1 to 50 terms")
		}
		for j, term := range g {
			input.TermGroups[i][j] = strings.TrimSpace(term)
			if input.TermGroups[i][j] == "" || len(term) > 300 || strings.ContainsAny(term, "\r\n\x00") {
				return bizerrors.BadRequest("invalid search term")
			}
		}
		input.TermGroups[i] = uniqueSorted(input.TermGroups[i])
	}
	if input.SearchRoutes != nil {
		if err := retrieval.ValidateRoutes(*input.SearchRoutes, time.Now()); err != nil {
			return bizerrors.BadRequest(err.Error())
		}
		routes := retrieval.NormalizeRoutes(*input.SearchRoutes)
		input.SearchRoutes = &routes
	}
	if requireSearch && len(input.TermGroups) == 0 && (input.SearchRoutes == nil || len(*input.SearchRoutes) == 0) && !validTopic(input.Slug) && input.SearchEnabled {
		return bizerrors.BadRequest("enabled custom topic requires keywords or search routes")
	}
	if input.TermGroups == nil {
		input.TermGroups = [][]string{}
	}
	return nil
}
func topicVO(t *ent.GleanTopic) TopicVO {
	return TopicVO{Slug: t.Slug, Name: t.Name, Description: t.Description, Rule: t.Rule, SearchEnabled: t.SearchEnabled, TermGroups: t.TermGroups, SearchRoutes: t.SearchRoutes, SearchProtocol: t.SearchProtocol, Version: t.Version}
}
func (s *Service) topicConfigs(ctx context.Context) ([]TopicVO, error) {
	result := append([]TopicVO(nil), topics...)
	for i := range result {
		result[i].SearchEnabled = true
		result[i].TermGroups = [][]string{}
	}
	records, err := s.client.GleanTopic.Query().Order(ent.Asc(gleantopic.FieldCreatedAt), ent.Asc(gleantopic.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		v := topicVO(record)
		found := false
		for i := range result {
			if result[i].Slug == v.Slug {
				result[i] = v
				found = true
				break
			}
		}
		if !found {
			result = append(result, v)
		}
	}
	return result, nil
}

// SearchTopicConfigs 返回最新配置；采集端必须在新运行开始时冻结快照。
func (s *Service) SearchTopicConfigs(ctx context.Context) ([]SearchTopicConfig, error) {
	configs, err := s.topicConfigs(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]SearchTopicConfig, 0, len(configs))
	for _, v := range configs {
		result = append(result, SearchTopicConfig{Slug: v.Slug, SearchEnabled: v.SearchEnabled, TermGroups: v.TermGroups, SearchRoutes: v.SearchRoutes, SearchProtocol: v.SearchProtocol, Version: v.Version})
	}
	return result, nil
}

// SaveTopic 创建或更新主题，默认内置主题可使用版本 0 首次覆盖。
func (s *Service) SaveTopic(ctx context.Context, path string, input TopicBody, create bool) (TopicVO, error) {
	uid, err := requireManager(ctx)
	if err != nil {
		return TopicVO{}, err
	}
	if path != "" {
		if input.Slug != "" && input.Slug != path {
			return TopicVO{}, bizerrors.BadRequest("topic slug is immutable")
		}
		input.Slug = path
	}
	if err = normalizeTopicInput(&input, false); err != nil {
		return TopicVO{}, err
	}
	var result TopicVO
	err = data.WithinTx(ctx, s.client, func(ctx context.Context, c *ent.Client) error {
		// 已有配置行锁保护版本；首次创建由唯一索引阻止覆盖竞争。
		if err := lockManager(ctx, c, uid); err != nil {
			return err
		}
		previous, err := c.GleanTopic.Query().Where(gleantopic.Slug(input.Slug)).ForUpdate().Only(ctx)
		var before any = map[string]any{}
		var record *ent.GleanTopic
		if ent.IsNotFound(err) {
			if input.ExpectedVersion != 0 {
				return bizerrors.CatalogConflict()
			}
			if create && validTopic(input.Slug) {
				return bizerrors.CatalogConflict()
			}
			if !create && !validTopic(input.Slug) {
				return bizerrors.BadRequest("unknown topic")
			}
			if !create {
				for _, v := range topics {
					if v.Slug == input.Slug {
						v.SearchEnabled = true
						v.TermGroups = [][]string{}
						before = v
						break
					}
				}
			}
			if err := prepareTopicProtocol(&input, nil); err != nil {
				return err
			}
			if err := normalizeTopic(&input); err != nil {
				return err
			}
			routes := []retrieval.Route{}
			if input.SearchRoutes != nil {
				routes = *input.SearchRoutes
			}
			record, err = c.GleanTopic.Create().SetSlug(input.Slug).SetName(input.Name).SetDescription(input.Description).SetRule(input.Rule).SetSearchEnabled(input.SearchEnabled).SetTermGroups(input.TermGroups).SetSearchRoutes(routes).SetSearchProtocol(input.SearchProtocol).SetCreatedBy(uid).SetUpdatedBy(uid).Save(ctx)
		} else if err == nil {
			if create || previous.Version != input.ExpectedVersion {
				return bizerrors.CatalogConflict()
			}
			before = topicVO(previous)
			if err := prepareTopicProtocol(&input, previous.SearchProtocol); err != nil {
				return err
			}
			if input.SearchRoutes == nil {
				routes := previous.SearchRoutes
				input.SearchRoutes = &routes
			}
			if err := normalizeTopic(&input); err != nil {
				return err
			}
			record, err = c.GleanTopic.UpdateOneID(previous.ID).SetName(input.Name).SetDescription(input.Description).SetRule(input.Rule).SetSearchEnabled(input.SearchEnabled).SetTermGroups(input.TermGroups).SetSearchRoutes(*input.SearchRoutes).SetSearchProtocol(input.SearchProtocol).SetVersion(previous.Version + 1).SetUpdatedBy(uid).Save(ctx)
		}
		if err != nil {
			return err
		}
		result = topicVO(record)
		return recordChange(ctx, c, uid, 0, input.Slug, "topic", before, result)
	})
	return result, managementError(ctx, err)
}
func (s *Service) topicExists(ctx context.Context, slug string) error {
	if slug == "" || validTopic(slug) {
		return nil
	}
	if !topicSlugPattern.MatchString(slug) {
		return bizerrors.BadRequest("invalid topic slug")
	}
	exists, err := s.client.GleanTopic.Query().Where(gleantopic.Slug(slug)).Exist(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return bizerrors.BadRequest("unknown topic")
	}
	return nil
}

// History 返回最近 100 次共享目录编辑；原始科学来源与人工记录分开呈现。
func (s *Service) History(ctx context.Context, paperID idgen.ID) ([]CatalogChangeVO, error) {
	if _, err := userID(ctx); err != nil {
		return nil, err
	}
	records, err := s.client.GleanCatalogChange.Query().Where(gleancatalogchange.PaperID(paperID)).Order(ent.Desc(gleancatalogchange.FieldID)).Limit(100).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]CatalogChangeVO, 0, len(records))
	for _, r := range records {
		out = append(out, CatalogChangeVO{ID: r.ID, Kind: r.Kind, Topic: r.Topic, Before: r.Before, After: r.After, ActorID: r.CreatedBy, CreatedAt: r.CreatedAt})
	}
	return out, nil
}
