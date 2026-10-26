package catalog

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"

	"entgo.io/ent/dialect"
	sql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/gleanmembership"
	"github.com/teamsillybees/glean/internal/data/ent/gleanpaper"
	"github.com/teamsillybees/glean/internal/data/ent/gleanpaperstate"
	"github.com/teamsillybees/glean/internal/data/ent/gleanvenue"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

func membershipSelector(q PaperQuery) *sql.Selector {
	t := sql.Table(gleanmembership.Table)
	selector := sql.Dialect(dialect.Postgres).Select(t.C(gleanmembership.FieldPaperID)).From(t)
	if q.Topic != "" {
		selector.Where(sql.EQ(t.C(gleanmembership.FieldTopic), q.Topic))
	}
	if q.Tier != "" {
		selector.Where(sql.EQ(t.C(gleanmembership.FieldTier), q.Tier))
	}
	if q.Category != "" {
		selector.Where(sql.Or(sql.EQ(t.C(gleanmembership.FieldPrimaryCategory), q.Category), sqljson.ValueContains(t.C(gleanmembership.FieldCategories), q.Category)))
	}
	// 别名跨主题匹配各来源的原始维度；显式 dimension 始终按精确键匹配。
	for _, alias := range []struct {
		value string
		keys  []string
	}{
		{q.Task, []string{"technical_tasks", "tasks"}},
		{q.Role, []string{"llm_role", "ai_leadership", "roles"}},
		{q.Direction, []string{"relation_direction"}},
		{q.Method, []string{"ai_methods"}},
	} {
		if alias.value != "" {
			predicates := []*sql.Predicate{}
			for _, key := range alias.keys {
				predicates = append(predicates, sqljson.ValueContains(t.C(gleanmembership.FieldDimensions), alias.value, sqljson.Path(key)))
			}
			selector.Where(sql.Or(predicates...))
		}
	}
	dimensions, _ := queryDimensions(q)
	keys := make([]string, 0, len(dimensions))
	for k := range dimensions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		selector.Where(sqljson.ValueContains(t.C(gleanmembership.FieldDimensions), map[string][]string{key: {dimensions[key]}}))
	}
	return selector
}
func stateSelector(uid idgen.ID) *sql.Selector {
	t := sql.Table(gleanpaperstate.Table)
	return sql.Dialect(dialect.Postgres).Select(t.C(gleanpaperstate.FieldPaperID)).From(t).Where(sql.EQ(t.C(gleanpaperstate.FieldUserID), uid))
}
func (s *Service) paperQuery(q PaperQuery, uid idgen.ID) *ent.GleanPaperQuery {
	query := s.client.GleanPaper.Query().Where(func(sel *sql.Selector) { sel.Where(sql.In(sel.C(gleanpaper.FieldID), membershipSelector(q))) })
	if q.Q != "" {
		query.Where(gleanpaper.Or(gleanpaper.TitleContainsFold(q.Q), gleanpaper.TitleZhContainsFold(q.Q), gleanpaper.AbstractContainsFold(q.Q), gleanpaper.DoiContainsFold(q.Q), gleanpaper.ArxivIDContainsFold(q.Q), gleanpaper.VenueContainsFold(q.Q)))
	}
	if q.Year > 0 {
		query.Where(gleanpaper.Year(q.Year))
	}
	if q.Venue != "" {
		query.Where(gleanpaper.Venue(q.Venue))
	}
	if q.PublicationType != "" {
		query.Where(gleanpaper.PublicationType(q.PublicationType))
	}
	if q.Hidden != "include" {
		query.Where(func(sel *sql.Selector) {
			hidden := stateSelector(uid).Where(sql.EQ(gleanpaperstate.FieldHidden, true))
			if q.Hidden == "only" {
				sel.Where(sql.In(sel.C(gleanpaper.FieldID), hidden))
			} else {
				sel.Where(sql.NotIn(sel.C(gleanpaper.FieldID), hidden))
			}
		})
	}
	if q.ReadStatus != "" {
		query.Where(func(sel *sql.Selector) {
			matching := stateSelector(uid).Where(sql.EQ(gleanpaperstate.FieldReadStatus, q.ReadStatus))
			p := sql.In(sel.C(gleanpaper.FieldID), matching)
			if q.ReadStatus == "unread" {
				p = sql.Or(p, sql.NotIn(sel.C(gleanpaper.FieldID), stateSelector(uid)))
			}
			sel.Where(p)
		})
	}
	return query
}

// Topics 返回固定主题和真实保存的成员数量。
func (s *Service) Topics(ctx context.Context) ([]TopicVO, error) {
	result := append([]TopicVO(nil), topics...)
	for i := range result {
		count, err := s.client.GleanMembership.Query().Where(gleanmembership.Topic(result[i].Slug)).Count(ctx)
		if err != nil {
			return nil, bizerrors.WrapDBContext(ctx, err, "count topic papers failed")
		}
		result[i].MembershipCount = count
	}
	return result, nil
}

// Papers 返回过滤后的全局去重分页目录。
func (s *Service) Papers(ctx context.Context, q PaperQuery) (PaperPageVO, error) {
	if err := validateQuery(&q); err != nil {
		return PaperPageVO{}, err
	}
	uid, err := userID(ctx)
	if err != nil {
		return PaperPageVO{}, err
	}
	query := s.paperQuery(q, uid)
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return PaperPageVO{}, bizerrors.WrapDBContext(ctx, err, "count papers failed")
	}
	switch q.Sort {
	case "title":
		query.Order(ent.Asc(gleanpaper.FieldTitle), ent.Asc(gleanpaper.FieldID))
	case "oldest":
		query.Order(ent.Asc(gleanpaper.FieldYear), gleanpaper.ByFormalOnlineDate(sql.OrderNullsLast()), ent.Asc(gleanpaper.FieldID))
	default:
		query.Order(ent.Desc(gleanpaper.FieldYear), gleanpaper.ByFormalOnlineDate(sql.OrderDesc(), sql.OrderNullsLast()), ent.Desc(gleanpaper.FieldID))
	}
	papers, err := query.Limit(q.PageSize).Offset((q.Page - 1) * q.PageSize).All(ctx)
	if err != nil {
		return PaperPageVO{}, bizerrors.WrapDBContext(ctx, err, "list papers failed")
	}
	items, err := s.paperVOs(ctx, papers, uid, false)
	if err != nil {
		return PaperPageVO{}, err
	}
	return PaperPageVO{Items: items, Total: total, Page: q.Page, PageSize: q.PageSize}, nil
}

// Paper 返回论文全部主题来源及个人状态。
func (s *Service) Paper(ctx context.Context, id idgen.ID) (PaperVO, error) {
	uid, err := userID(ctx)
	if err != nil {
		return PaperVO{}, err
	}
	record, err := s.client.GleanPaper.Get(ctx, id)
	if ent.IsNotFound(err) {
		return PaperVO{}, bizerrors.WrapNotFoundContext(ctx, err, "paper not found")
	}
	if err != nil {
		return PaperVO{}, bizerrors.WrapDBContext(ctx, err, "get paper failed")
	}
	items, err := s.paperVOs(ctx, []*ent.GleanPaper{record}, uid, true)
	if err != nil {
		return PaperVO{}, err
	}
	return items[0], nil
}
func (s *Service) paperVOs(ctx context.Context, papers []*ent.GleanPaper, uid idgen.ID, detail bool) ([]PaperVO, error) {
	result := make([]PaperVO, 0, len(papers))
	if len(papers) == 0 {
		return result, nil
	}
	ids := make([]idgen.ID, 0, len(papers))
	for _, p := range papers {
		ids = append(ids, p.ID)
	}
	mq := s.client.GleanMembership.Query().Where(gleanmembership.PaperIDIn(ids...)).Order(ent.Asc(gleanmembership.FieldTopic), ent.Asc(gleanmembership.FieldOriginalID))
	if !detail {
		mq.Select(gleanmembership.FieldID, gleanmembership.FieldPaperID, gleanmembership.FieldTopic, gleanmembership.FieldOriginalID, gleanmembership.FieldTier, gleanmembership.FieldPrimaryCategory, gleanmembership.FieldCategories, gleanmembership.FieldDimensions, gleanmembership.FieldReadStatus, gleanmembership.FieldSource, gleanmembership.FieldIsMock)
	}
	members, err := mq.All(ctx)
	if err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "load memberships failed")
	}
	memberships := map[idgen.ID][]MembershipVO{}
	for _, m := range members {
		raw := m.Raw
		if raw == nil {
			raw = map[string]any{}
		}
		memberships[m.PaperID] = append(memberships[m.PaperID], MembershipVO{Topic: m.Topic, OriginalID: m.OriginalID, Tier: m.Tier, PrimaryCategory: m.PrimaryCategory, Categories: m.Categories, Dimensions: m.Dimensions, ReadStatus: m.ReadStatus, Source: m.Source, IsMock: m.IsMock, Raw: raw})
	}
	states, err := s.client.GleanPaperState.Query().Where(gleanpaperstate.UserID(uid), gleanpaperstate.PaperIDIn(ids...)).All(ctx)
	if err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "load personal state failed")
	}
	stateMap := map[idgen.ID]*ent.GleanPaperState{}
	for _, state := range states {
		stateMap[state.PaperID] = state
	}
	for _, p := range papers {
		item := PaperVO{ID: p.ID, Title: p.Title, TitleZh: p.TitleZh, Abstract: p.Abstract, Authors: p.Authors, Year: p.Year, Venue: p.Venue, PublicationType: p.PublicationType, URL: p.URL, FormalOnlineDate: p.FormalOnlineDate, IssueDate: p.IssueDate, DateProvenance: p.DateProvenance, ReadStatus: "unread", IsMock: p.IsMock, Memberships: memberships[p.ID], Evidence: []SourceEvidence{}}
		if p.Doi != nil {
			item.DOI = *p.Doi
		}
		if p.ArxivID != nil {
			item.ArxivID = *p.ArxivID
		}
		if item.Authors == nil {
			item.Authors = []string{}
		}
		if item.Memberships == nil {
			item.Memberships = []MembershipVO{}
		}
		if detail {
			encoded, err := json.Marshal(p.Evidence)
			if err != nil {
				return nil, bizerrors.WrapDBContext(ctx, err, "decode source evidence failed")
			}
			if err := json.Unmarshal(encoded, &item.Evidence); err != nil {
				return nil, bizerrors.WrapDBContext(ctx, err, "decode source evidence failed")
			}
		}
		if state := stateMap[p.ID]; state != nil {
			item.Hidden = state.Hidden
			item.ReadStatus = state.ReadStatus
		}
		result = append(result, item)
	}
	return result, nil
}

// Venues 返回已保存的版本化资格证据。
func (s *Service) Venues(ctx context.Context, q, topic string) ([]VenueVO, error) {
	if topic != "" && !validTopic(topic) {
		return nil, bizerrors.BadRequest("unknown topic")
	}
	query := s.client.GleanVenue.Query()
	if q != "" {
		query.Where(gleanvenue.NameContainsFold(q))
	}
	if topic != "" {
		query.Where(func(sel *sql.Selector) {
			sel.Where(sql.Or(sqljson.ValueContains(sel.C(gleanvenue.FieldEligibleTopics), topic), sqljson.ValueContains(sel.C(gleanvenue.FieldEvidence), map[string][]string{"sourceTopics": {topic}})))
		})
	}
	records, err := query.Order(ent.Asc(gleanvenue.FieldName), ent.Desc(gleanvenue.FieldMetricYear), ent.Desc(gleanvenue.FieldRegistryVersion)).All(ctx)
	if err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "load venue registry failed")
	}
	result := make([]VenueVO, 0, len(records))
	for _, v := range records {
		result = append(result, VenueVO{ID: v.ID, Name: v.Name, Kind: v.Kind, JIF: v.Jif, CCF: v.Ccf, MetricYear: v.MetricYear, RegistryVersion: v.RegistryVersion, Source: v.Source, Evidence: v.Evidence, EligibleTopics: v.EligibleTopics, QualificationStatus: v.QualificationStatus})
	}
	return result, nil
}

// Facets 在数据库过滤后的集合上聚合，每个值按全局论文标识去重。
func (s *Service) Facets(ctx context.Context, q PaperQuery) (FacetsVO, error) {
	if err := validateQuery(&q); err != nil {
		return FacetsVO{}, err
	}
	uid, err := userID(ctx)
	if err != nil {
		return FacetsVO{}, err
	}
	papers, err := s.paperQuery(q, uid).Select(gleanpaper.FieldID, gleanpaper.FieldYear, gleanpaper.FieldVenue, gleanpaper.FieldPublicationType).All(ctx)
	if err != nil {
		return FacetsVO{}, bizerrors.WrapDBContext(ctx, err, "load facet papers failed")
	}
	counts := map[string]map[string]map[idgen.ID]bool{}
	add := func(kind, value string, id idgen.ID) {
		if value == "" {
			return
		}
		if counts[kind] == nil {
			counts[kind] = map[string]map[idgen.ID]bool{}
		}
		if counts[kind][value] == nil {
			counts[kind][value] = map[idgen.ID]bool{}
		}
		counts[kind][value][id] = true
	}
	ids := make([]idgen.ID, 0, len(papers))
	for _, p := range papers {
		ids = append(ids, p.ID)
		if p.Year > 0 {
			add("years", strconv.Itoa(p.Year), p.ID)
		}
		add("venues", p.Venue, p.ID)
		add("publicationTypes", p.PublicationType, p.ID)
	}
	dimensionKeys := map[string]bool{}
	// 限制 IN 参数批次，避免大型目录超出 PostgreSQL 参数数量限制。
	for start := 0; start < len(ids); start += 1000 {
		end := min(start+1000, len(ids))
		batch := ids[start:end]
		query := s.client.GleanMembership.Query().Where(gleanmembership.PaperIDIn(batch...))
		if q.Topic != "" {
			query.Where(gleanmembership.Topic(q.Topic))
		}
		records, err := query.Select(gleanmembership.FieldID, gleanmembership.FieldPaperID, gleanmembership.FieldTier, gleanmembership.FieldPrimaryCategory, gleanmembership.FieldCategories, gleanmembership.FieldDimensions).All(ctx)
		if err != nil {
			return FacetsVO{}, bizerrors.WrapDBContext(ctx, err, "load facet memberships failed")
		}
		for _, m := range records {
			add("tiers", m.Tier, m.PaperID)
			add("categories", m.PrimaryCategory, m.PaperID)
			for _, c := range m.Categories {
				add("categories", c, m.PaperID)
			}
			for key, values := range m.Dimensions {
				dimensionKeys[key] = true
				for _, v := range values {
					add("dimension:"+key, v, m.PaperID)
					switch key {
					case "technical_tasks", "tasks":
						add("tasks", v, m.PaperID)
					case "llm_role", "ai_leadership", "roles":
						add("roles", v, m.PaperID)
					case "relation_direction":
						add("directions", v, m.PaperID)
					case "ai_methods":
						add("methods", v, m.PaperID)
					}
				}
			}
		}
		states, err := s.client.GleanPaperState.Query().Where(gleanpaperstate.UserID(uid), gleanpaperstate.PaperIDIn(batch...)).Select(gleanpaperstate.FieldID, gleanpaperstate.FieldPaperID, gleanpaperstate.FieldReadStatus).All(ctx)
		if err != nil {
			return FacetsVO{}, bizerrors.WrapDBContext(ctx, err, "load facet states failed")
		}
		stateMap := map[idgen.ID]string{}
		for _, st := range states {
			stateMap[st.PaperID] = st.ReadStatus
		}
		for _, id := range batch {
			v := stateMap[id]
			if v == "" {
				v = "unread"
			}
			add("readStatuses", v, id)
		}
	}
	facets := func(kind string) []FacetVO {
		values := []FacetVO{}
		for value, ids := range counts[kind] {
			values = append(values, FacetVO{Value: value, Count: len(ids)})
		}
		sort.Slice(values, func(i, j int) bool {
			if kind == "years" {
				return values[i].Value > values[j].Value
			}
			if values[i].Count != values[j].Count {
				return values[i].Count > values[j].Count
			}
			return values[i].Value < values[j].Value
		})
		return values
	}
	dimensions := map[string][]FacetVO{}
	for key := range dimensionKeys {
		dimensions[key] = facets("dimension:" + key)
	}
	return FacetsVO{Years: facets("years"), Venues: facets("venues"), PublicationTypes: facets("publicationTypes"), Categories: facets("categories"), Tiers: facets("tiers"), ReadStatuses: facets("readStatuses"), Tasks: facets("tasks"), Roles: facets("roles"), Directions: facets("directions"), Methods: facets("methods"), Dimensions: dimensions}, nil
}
