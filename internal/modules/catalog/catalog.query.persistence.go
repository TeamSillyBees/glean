package catalog

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"entgo.io/ent/dialect"
	sql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/gleanmembership"
	"github.com/teamsillybees/glean/internal/data/ent/gleanobservation"
	"github.com/teamsillybees/glean/internal/data/ent/gleanpaper"
	"github.com/teamsillybees/glean/internal/data/ent/gleanpaperreview"
	"github.com/teamsillybees/glean/internal/data/ent/gleanpaperstate"
	"github.com/teamsillybees/glean/internal/data/ent/gleanvenue"
	"github.com/teamsillybees/glean/internal/data/ent/predicate"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

func membershipSelector(q PaperQuery) *sql.Selector {
	t := sql.Table(gleanmembership.Table)
	selector := sql.Dialect(dialect.Postgres).Select(t.C(gleanmembership.FieldPaperID)).From(t)
	if q.Topic != "" {
		selector.Where(sql.EQ(t.C(gleanmembership.FieldTopic), q.Topic))
	}
	if scope := collectionPredicate(t.C("paper_id"), t.C("topic"), t.C("inclusion_status"), t.C("dimensions"), q.CollectionScope); scope != nil {
		selector.Where(scope)
	}
	if q.InclusionStatus != "" {
		switch q.InclusionStatus {
		case "included", "excluded":
			selector.Where(reviewPredicate(t.C("paper_id"), t.C("topic"), q.InclusionStatus))
		case "pending", "pending-review":
			selector.Where(collectionPredicate(t.C("paper_id"), t.C("topic"), t.C("inclusion_status"), t.C("dimensions"), "pending"))
		default:
			selector.Where(sql.And(sql.Not(reviewPredicate(t.C("paper_id"), t.C("topic"), "")), sql.EQ(t.C("inclusion_status"), q.InclusionStatus)))
		}
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
		if key == "manual_labels" {
			r := sql.Table(gleanpaperreview.Table).As("label_review")
			selector.Where(sql.Exists(sql.Dialect(dialect.Postgres).Select(r.C("id")).From(r).Where(sql.And(sql.ColumnsEQ(r.C("paper_id"), t.C("paper_id")), sql.ColumnsEQ(r.C("topic"), t.C("topic")), sqljson.ValueContains(r.C("labels"), dimensions[key])))))
			continue
		}
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
	if q.Source != "" {
		query.Where(func(sel *sql.Selector) {
			o := sql.Table(gleanobservation.Table)
			sel.Where(sql.In(sel.C(gleanpaper.FieldID), sql.Dialect(dialect.Postgres).Select(o.C(gleanobservation.FieldPaperID)).From(o).Where(sql.EQ(o.C(gleanobservation.FieldSource), q.Source))))
		})
	}
	if q.Q != "" {
		query.Where(gleanpaper.Or(func(sel *sql.Selector) {
			ps := []*sql.Predicate{}
			for _, field := range []string{"title", "title_zh", "abstract", "venue"} {
				ps = append(ps, effectiveContains(effectiveColumn(`"glean_paper"`, field), q.Q))
			}
			ps = append(ps, sql.ContainsFold(sel.C("doi"), q.Q), sql.ContainsFold(sel.C("arxiv_id"), q.Q))
			sel.Where(sql.Or(ps...))
		}, observedTextPredicate(q.Q)))
		// 来源文本匹配保留在独立 OR 分支。
	}
	if q.Year > 0 {
		query.Where(func(sel *sql.Selector) {
			sel.Where(sql.P(func(b *sql.Builder) { b.WriteString(effectiveColumn(`"glean_paper"`, "year") + " = ").Arg(q.Year) }))
		})
	}
	if q.Venue != "" {
		query.Where(func(sel *sql.Selector) {
			sel.Where(sql.P(func(b *sql.Builder) { b.WriteString(effectiveColumn(`"glean_paper"`, "venue") + " = ").Arg(q.Venue) }))
		})
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

func observedTextPredicate(text string) predicate.GleanPaper {
	return func(sel *sql.Selector) {
		o := sql.Table(gleanobservation.Table)
		matches := []*sql.Predicate{}
		// 仅搜索来源事实字段，不搜索 raw、模拟模型输出或用户提供的 JSON 路径。
		for _, key := range []string{"title", "abstract", "doi", "arxivId", "pmid", "venue"} {
			matches = append(matches, sql.P(func(b *sql.Builder) {
				b.Ident(o.C(gleanobservation.FieldMetadata)).WriteString("->>'" + key + "'").Join(sql.ContainsFold("", text))
			}))
		}
		sub := sql.Dialect(dialect.Postgres).Select(o.C(gleanobservation.FieldID)).From(o).Where(sql.And(sql.ColumnsEQ(o.C(gleanobservation.FieldPaperID), sel.C(gleanpaper.FieldID)), sql.Or(matches...)))
		// EXISTS 保持一篇论文一行，多个来源或版本命中不会膨胀分页和分面计数。
		sel.Where(sql.Exists(sub))
	}
}

// Topics 返回内置及自定义主题、原始成员数量和有效审核集合数量。
func (s *Service) Topics(ctx context.Context) ([]TopicVO, error) {
	result, err := s.topicConfigs(ctx)
	if err != nil {
		return nil, err
	}
	counts, err := s.topicMembershipCounts(ctx)
	if err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "count topic papers failed")
	}
	historical, err := s.topicMembershipCounts(ctx, gleanmembership.InclusionStatus("historical"))
	if err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "count historical members failed")
	}
	collection := func(scope string) predicate.GleanMembership {
		return func(sel *sql.Selector) {
			sel.Where(collectionPredicate(sel.C("paper_id"), sel.C("topic"), sel.C("inclusion_status"), sel.C("dimensions"), scope))
		}
	}
	pending, err := s.topicMembershipCounts(ctx, collection("pending"))
	if err != nil {
		return nil, err
	}
	curated, err := s.topicMembershipCounts(ctx, collection("curated"))
	if err != nil {
		return nil, bizerrors.WrapDBContext(ctx, err, "count curated historical members failed")
	}
	for i := range result {
		slug := result[i].Slug
		result[i].MembershipCount = counts[slug]
		result[i].HistoricalCount = historical[slug]
		result[i].CandidateCount = pending[slug]
		result[i].CuratedCount = curated[slug]
	}
	return result, nil
}

// topicMembershipCounts 按主题统计成员行，不将同一论文的不同原始编号去重。
func (s *Service) topicMembershipCounts(ctx context.Context, filters ...predicate.GleanMembership) (map[string]int, error) {
	var rows []struct {
		Topic string `json:"topic"`
		Count int    `json:"count"`
	}
	if err := s.client.GleanMembership.Query().Where(filters...).GroupBy(gleanmembership.FieldTopic).Aggregate(ent.Count()).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[row.Topic] = row.Count
	}
	return counts, nil
}

// Papers 返回过滤后的全局去重分页目录。
func (s *Service) Papers(ctx context.Context, q PaperQuery) (PaperPageVO, error) {
	if err := s.topicExists(ctx, q.Topic); err != nil {
		return PaperPageVO{}, err
	}
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
	case "model-priority":
		query.Order(modelPriorityOrder(q.Topic), ent.Desc(gleanpaper.FieldID))
	case "observed", "source-updated":
		query.Order(func(sel *sql.Selector) {
			o := sql.Table(gleanobservation.Table)
			field := gleanobservation.FieldObservedAt
			if q.Sort == "source-updated" {
				field = gleanobservation.FieldSourceUpdatedAt
			}
			latest := sql.Dialect(dialect.Postgres).Select(sql.Max(o.C(field))).From(o).Where(sql.And(sql.ColumnsEQ(o.C(gleanobservation.FieldPaperID), sel.C(gleanpaper.FieldID)), liveObservationSQL(o.C(gleanobservation.FieldMetadata))))
			sel.OrderExpr(sql.ExprFunc(func(b *sql.Builder) { b.WriteByte('(').Join(latest).WriteString(") DESC NULLS LAST") }))
		}, ent.Desc(gleanpaper.FieldID))
	case "title":
		query.Order(func(sel *sql.Selector) { sel.OrderExpr(sql.Expr(effectiveColumn(`"glean_paper"`, "title") + " ASC")) }, ent.Asc(gleanpaper.FieldID))
	case "oldest":
		query.Order(func(sel *sql.Selector) { sel.OrderExpr(sql.Expr(effectiveColumn(`"glean_paper"`, "year") + " ASC")) }, gleanpaper.ByFormalOnlineDate(sql.OrderNullsLast()), ent.Asc(gleanpaper.FieldID))
	default:
		query.Order(func(sel *sql.Selector) { sel.OrderExpr(sql.Expr(effectiveColumn(`"glean_paper"`, "year") + " DESC")) }, gleanpaper.ByFormalOnlineDate(sql.OrderDesc(), sql.OrderNullsLast()), ent.Desc(gleanpaper.FieldID))
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
		mq.Select(gleanmembership.FieldID, gleanmembership.FieldPaperID, gleanmembership.FieldTopic, gleanmembership.FieldOriginalID, gleanmembership.FieldTier, gleanmembership.FieldPrimaryCategory, gleanmembership.FieldCategories, gleanmembership.FieldDimensions, gleanmembership.FieldReadStatus, gleanmembership.FieldInclusionStatus, gleanmembership.FieldSource, gleanmembership.FieldIsMock, gleanmembership.FieldRaw)
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
		notes := historicalReadingNotes(raw)
		if !detail {
			raw = map[string]any{}
		}
		memberships[m.PaperID] = append(memberships[m.PaperID], MembershipVO{EffectiveStatus: historicalEffectiveStatus(m), ReadingNotes: notes, InclusionStatus: m.InclusionStatus, Topic: m.Topic, OriginalID: m.OriginalID, Tier: m.Tier, PrimaryCategory: m.PrimaryCategory, Categories: m.Categories, Dimensions: m.Dimensions, ReadStatus: m.ReadStatus, Source: m.Source, IsMock: m.IsMock, Raw: raw})
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
		item := PaperVO{Origin: p.Origin, ID: p.ID, Title: p.Title, TitleZh: p.TitleZh, Abstract: p.Abstract, Authors: p.Authors, Year: p.Year, Venue: p.Venue, PublicationType: p.PublicationType, URL: p.URL, FormalOnlineDate: p.FormalOnlineDate, IssueDate: p.IssueDate, DateProvenance: p.DateProvenance, ReadStatus: "unread", IsMock: p.IsMock, Memberships: memberships[p.ID], Evidence: []SourceEvidence{}}
		created := p.CreatedAt
		if p.Origin == "live" {
			item.FirstSeenAt = &created
		} else {
			item.ImportedAt = &created
		}
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
			for _, membership := range item.Memberships {
				if value, ok := membership.Raw["abstract_zh"].(string); ok && strings.TrimSpace(value) != "" {
					item.AbstractZh = value
					item.TranslationStatus = "historical-source-text"
					break
				}
			}
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
	if err := s.attachObservations(ctx, result, detail); err != nil {
		return nil, err
	}
	if err := s.attachManagement(ctx, result); err != nil {
		return nil, err
	}
	return result, nil
}

// Venues 返回已保存的版本化资格证据。
func (s *Service) Venues(ctx context.Context, q, topic string) ([]VenueVO, error) {
	if err := s.topicExists(ctx, topic); err != nil {
		return nil, err
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
		result = append(result, VenueVO{ISSNs: v.Issns, Aliases: v.Aliases, ID: v.ID, Name: v.Name, Kind: v.Kind, JIF: v.Jif, CCF: v.Ccf, MetricYear: v.MetricYear, RegistryVersion: v.RegistryVersion, Source: v.Source, Evidence: v.Evidence, EligibleTopics: v.EligibleTopics, QualificationStatus: v.QualificationStatus})
	}
	return result, nil
}

// Facets 在数据库过滤后的集合上聚合，每个值按全局论文标识去重。
func (s *Service) Facets(ctx context.Context, q PaperQuery) (FacetsVO, error) {
	return s.facetsWithBatchSize(ctx, q, facetAssociationBatchSize)
}

// 五类关联查询按论文 ID 有界加载；5000 为 PostgreSQL 参数上限留足固定筛选参数空间。
const facetAssociationBatchSize = 5000

// 内部参数仅用于同库对照测试，不暴露配置或改变过滤/聚合语义。
func (s *Service) facetsWithBatchSize(ctx context.Context, q PaperQuery, batchSize int) (FacetsVO, error) {
	if batchSize < 1 || batchSize > facetAssociationBatchSize {
		batchSize = facetAssociationBatchSize
	}
	if err := s.topicExists(ctx, q.Topic); err != nil {
		return FacetsVO{}, err
	}
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
	if err := s.applyFacetCorrections(ctx, papers, batchSize); err != nil {
		return FacetsVO{}, err
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
	for start := 0; start < len(ids); start += batchSize {
		end := min(start+batchSize, len(ids))
		batch := ids[start:end]
		query := s.client.GleanMembership.Query().Where(gleanmembership.PaperIDIn(batch...))
		if q.Topic != "" {
			query.Where(gleanmembership.Topic(q.Topic))
		}
		if q.CollectionScope != "" && q.CollectionScope != "all" {
			query.Where(func(sel *sql.Selector) {
				sel.Where(collectionPredicate(sel.C("paper_id"), sel.C("topic"), sel.C("inclusion_status"), sel.C("dimensions"), q.CollectionScope))
			})
		}
		records, err := query.Select(gleanmembership.FieldID, gleanmembership.FieldPaperID, gleanmembership.FieldTier, gleanmembership.FieldPrimaryCategory, gleanmembership.FieldCategories, gleanmembership.FieldDimensions, gleanmembership.FieldInclusionStatus, gleanmembership.FieldTopic).All(ctx)
		if err != nil {
			return FacetsVO{}, bizerrors.WrapDBContext(ctx, err, "load facet memberships failed")
		}
		reviews, err := s.client.GleanPaperReview.Query().Where(gleanpaperreview.PaperIDIn(batch...)).All(ctx)
		if err != nil {
			return FacetsVO{}, err
		}
		reviewMap := map[idgen.ID]map[string]*ent.GleanPaperReview{}
		for _, r := range reviews {
			if reviewMap[r.PaperID] == nil {
				reviewMap[r.PaperID] = map[string]*ent.GleanPaperReview{}
			}
			reviewMap[r.PaperID][r.Topic] = r
		}
		for _, m := range records {
			status := m.InclusionStatus
			if r := reviewMap[m.PaperID][m.Topic]; r != nil {
				status = string(r.Status)
				for _, label := range r.Labels {
					dimensionKeys["manual_labels"] = true
					add("dimension:manual_labels", label, m.PaperID)
				}
			}
			add("inclusionStatuses", status, m.PaperID)
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

		var sourceRecords []struct {
			PaperID idgen.ID `json:"paper_id"`
			Source  string   `json:"source"`
		}
		if err := s.client.GleanObservation.Query().Where(gleanobservation.PaperIDIn(batch...)).GroupBy(gleanobservation.FieldPaperID, gleanobservation.FieldSource).Scan(ctx, &sourceRecords); err != nil {
			return FacetsVO{}, bizerrors.WrapDBContext(ctx, err, "load source facets failed")
		}
		for _, source := range sourceRecords {
			add("sources", source.Source, source.PaperID)
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
	return FacetsVO{Sources: facets("sources"), InclusionStatuses: facets("inclusionStatuses"), Years: facets("years"), Venues: facets("venues"), PublicationTypes: facets("publicationTypes"), Categories: facets("categories"), Tiers: facets("tiers"), ReadStatuses: facets("readStatuses"), Tasks: facets("tasks"), Roles: facets("roles"), Directions: facets("directions"), Methods: facets("methods"), Dimensions: dimensions}, nil
}

func historicalReadingNotes(raw map[string]any) map[string]string {
	notes := map[string]string{}
	for _, key := range []string{"contribution", "method_summary", "evidence_note", "uncertainties", "summary", "review_notes"} {
		if text, ok := raw[key].(string); ok && strings.TrimSpace(text) != "" {
			notes[key] = text
		}
	}
	return notes
}

// modelPriorityOrder 仅按最新真实来源版本的当前主题模型分排序；未知后置，不回退旧版本，不改变科学分类。
func modelPriorityOrder(topic string) func(*sql.Selector) {
	return func(sel *sql.Selector) {
		o := sql.Table(gleanobservation.Table)
		latest := sql.Dialect(dialect.Postgres).Select(o.C(gleanobservation.FieldReview)).From(o).
			Where(sql.And(sql.ColumnsEQ(o.C(gleanobservation.FieldPaperID), sel.C(gleanpaper.FieldID)), liveObservationSQL(o.C(gleanobservation.FieldMetadata)))).
			OrderBy(sql.Desc(o.C(gleanobservation.FieldObservedAt)), sql.Desc(o.C(gleanobservation.FieldID))).Limit(1)
		sel.OrderExpr(sql.ExprFunc(func(b *sql.Builder) {
			b.WriteString("(SELECT MAX(CASE WHEN jsonb_typeof(decision->'score') = 'number' THEN CASE WHEN (decision->>'score')::numeric BETWEEN 0 AND 1 THEN (decision->>'score')::numeric END END) FROM (").Join(latest).
				WriteString(") latest_pipeline CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(latest_pipeline.review->'pipeline'->'stages') = 'array' THEN latest_pipeline.review->'pipeline'->'stages' ELSE '[]'::jsonb END) stage CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(stage->'result'->'topics') = 'array' THEN stage->'result'->'topics' ELSE '[]'::jsonb END) decision WHERE stage->>'name' = 'classification' AND stage->>'status' = 'completed'")
			if topic != "" {
				b.WriteString(" AND decision->>'topic' = ").Arg(topic)
			}
			b.WriteString(") DESC NULLS LAST")
		}))
	}
}
