package retrieval

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Protocol 保存可交换的研究协议；计划和执行结果使用不同对象。
type Protocol struct {
	SchemaVersion    string          `json:"schema_version"`
	ProfileID        string          `json:"profile_id"`
	ProfileVersion   string          `json:"profile_version"`
	Topic            string          `json:"topic"`
	ResearchQuestion string          `json:"research_question"`
	CoreScope        string          `json:"core_scope"`
	AdjacentScope    string          `json:"adjacent_scope"`
	UserRequirements []string        `json:"user_requirements"`
	Assumptions      []string        `json:"assumptions"`
	Mode             string          `json:"mode"`
	DateRange        DateRange       `json:"date_range"`
	Languages        []string        `json:"languages"`
	DocumentTypes    []string        `json:"document_types"`
	VersionPolicy    string          `json:"version_policy"`
	VenuePolicy      string          `json:"venue_policy"`
	Sources          []string        `json:"sources"`
	SourceMatrix     []SourcePlan    `json:"source_matrix"`
	Pathways         []Pathway       `json:"pathways"`
	Concepts         []Concept       `json:"concepts"`
	Variants         []Variant       `json:"variants"`
	Seeds            []Seed          `json:"seeds"`
	Budget           Budget          `json:"budget"`
	ExternalQueries  []ExternalQuery `json:"external_queries"`
	QualityChecks    []QualityCheck  `json:"quality_checks"`
	Notes            string          `json:"notes"`
	MaxCombinations  int             `json:"max_combinations_per_variant,omitempty"`
}

// DateRange 区分检索截止、当地日期与独立发表资格规则。
type DateRange struct {
	From            string `json:"from"`
	To              string `json:"to"`
	Timezone        string `json:"timezone"`
	Semantics       string `json:"semantics"`
	EligibilityRule string `json:"eligibility_rule"`
}

// Concept 保留词项出处与歧义，受控词核验不由编译器推断。
type Concept struct {
	ID                 string   `json:"id"`
	Definition         string   `json:"definition"`
	Required           bool     `json:"required"`
	Terms              []string `json:"terms"`
	TermSource         string   `json:"term_source"`
	Ambiguities        []string `json:"ambiguities"`
	BroaderTerms       []string `json:"broader_terms"`
	NarrowerTerms      []string `json:"narrower_terms"`
	ControlledTerms    []string `json:"controlled_terms"`
	ControlledEvidence string   `json:"controlled_evidence"`
}

// Variant 是概念交集通道；多个通道取并集，不静默展开上下位词。
type Variant struct {
	ID         string   `json:"id"`
	ConceptIDs []string `json:"concept_ids"`
	Purpose    string   `json:"purpose"`
	Sources    []string `json:"sources,omitempty"`
}

// SourcePlan 声明来源覆盖及权限；access_status 是协议声明，不是联网验收结果。
type SourcePlan struct {
	Source          string   `json:"source"`
	Discipline      string   `json:"discipline"`
	Role            string   `json:"role"`
	Fields          []string `json:"fields"`
	Coverage        string   `json:"coverage"`
	DocumentTypes   []string `json:"document_types"`
	Endpoint        string   `json:"endpoint"`
	Reason          string   `json:"reason"`
	AccessStatus    string   `json:"access_status"`
	Cost            string   `json:"cost"`
	License         string   `json:"license"`
	Pagination      string   `json:"pagination"`
	Alternative     string   `json:"alternative"`
	Risk            string   `json:"risk"`
	SyntaxReference string   `json:"syntax_reference"`
	CheckedOn       string   `json:"checked_on"`
}

// Pathway 要求主题、目录、引文与网页路线各自说明选择或缺口。
type Pathway struct {
	Route      string `json:"route"`
	Selected   bool   `json:"selected"`
	Reason     string `json:"reason"`
	NextAction string `json:"next_action"`
}

// Seed 保存已知相关集的主源身份；未核验的种子不授权引文扩展。
type Seed struct {
	Source           string `json:"source"`
	SourceID         string `json:"source_id"`
	DOI              string `json:"doi"`
	Title            string `json:"title"`
	IdentityVerified bool   `json:"identity_verified"`
	Evidence         string `json:"evidence"`
}

// Budget 为冻结协议的累计采集边界；模型费用由独立共享账本控制。
type Budget struct {
	MaxRequests    int `json:"max_requests"`
	MaxPages       int `json:"max_pages"`
	MaxRecords     int `json:"max_records"`
	MaxQueue       int `json:"max_queue"`
	MaxHistoryDays int `json:"max_history_days"`
}

// ExternalQuery 原样保留受限执行器无法等价执行的查询，状态只能是 planned。
type ExternalQuery struct {
	QueryID         string         `json:"query_id"`
	QueryVersion    string         `json:"query_version"`
	Source          string         `json:"source"`
	Route           string         `json:"route"`
	VariantID       string         `json:"variant_id"`
	Purpose         string         `json:"purpose"`
	NativeQuery     string         `json:"native_query"`
	Method          string         `json:"method"`
	Endpoint        string         `json:"endpoint"`
	Params          map[string]any `json:"params"`
	URL             string         `json:"url_without_credentials"`
	Paging          string         `json:"paging"`
	Caveat          string         `json:"caveat"`
	SyntaxReference string         `json:"syntax_reference"`
	SyntaxCheckedOn string         `json:"syntax_checked_on"`
	Verification    string         `json:"verification"`
	Status          string         `json:"status"`
	ReportedHits    *int           `json:"reported_hits"`
	FetchedCount    *int           `json:"fetched_count"`
}

// QualityCheck 只记录明确抽样框内的人工校准，不推断总体召回率。
type QualityCheck struct {
	ID           string     `json:"id"`
	Kind         string     `json:"kind"`
	Source       string     `json:"source"`
	Frame        string     `json:"frame"`
	Method       string     `json:"method"`
	SampleSize   int        `json:"sample_size"`
	Missed       *int       `json:"missed"`
	SourceAbsent *int       `json:"source_absent"`
	Evidence     []string   `json:"evidence"`
	Reviewer     string     `json:"reviewer"`
	ReviewedAt   *time.Time `json:"reviewed_at"`
	NextAction   string     `json:"next_action"`
}

// ProtocolPreview 保存显式映射和需确认的语义差异；不会发送来源请求。
type ProtocolPreview struct {
	Protocol        Protocol   `json:"protocol"`
	SearchRoutes    []Route    `json:"searchRoutes"`
	Queries         []Compiled `json:"queries"`
	Warnings        []string   `json:"warnings"`
	ExternalSources []string   `json:"externalSources"`
	SemanticVersion string     `json:"semanticVersion"`
	Acknowledgement string     `json:"acknowledgement"`
	Executable      bool       `json:"executable"`
}

// PreviewProtocol 校验完整协议并映射六源子集，未支持的计划始终保留。
func PreviewProtocol(p Protocol, now time.Time) (ProtocolPreview, error) {
	result := ProtocolPreview{Protocol: p, SearchRoutes: []Route{}, Queries: []Compiled{}, Warnings: []string{}, ExternalSources: []string{}}
	if err := validateProtocol(p, now); err != nil {
		return result, err
	}
	concepts := map[string]Concept{}
	for _, c := range p.Concepts {
		concepts[c.ID] = c
	}
	var start *time.Time
	if p.DateRange.From != "" {
		v, _, _ := ProtocolWindow(p)
		start = &v
	}
	for _, variant := range p.Variants {
		groups := [][]string{}
		for _, id := range variant.ConceptIDs {
			groups = append(groups, concepts[id].Terms)
		}
		sources := variant.Sources
		if len(sources) == 0 {
			sources = p.Sources
		}
		for _, source := range sources {
			mode := "default"
			if source == "pubmed" || source == "europepmc" {
				mode = "title_abstract"
			}
			if !ExecutableSource(source) {
				result.ExternalSources = append(result.ExternalSources, source)
				continue
			}
			route := Route{ID: variant.ID + "-" + source, Name: variant.Purpose, Sources: []string{source}, TermGroups: groups, FieldMode: mode, BackfillFrom: start}
			compiled, err := Compile(source, groups, mode)
			if err != nil {
				return result, err
			}
			result.SearchRoutes = append(result.SearchRoutes, route)
			result.Queries = append(result.Queries, compiled)
			if source == "crossref" {
				result.Warnings = append(result.Warnings, "Crossref 使用首概念种子并集，不等价于完整布尔式或外部组合式；按索引更新日发现后独立核验发表资格。")
			}
			if source == "arxiv" || source == "biorxiv" || source == "medrxiv" {
				result.Warnings = append(result.Warnings, source+" 执行完整日期 feed，不执行外部主题 API；题名摘要需后续审核，更新时间或预印本日期不等于正式发表日期。")
			}
		}
	}
	if err := ValidateRoutes(result.SearchRoutes, now); err != nil {
		return result, err
	}
	result.ExternalSources = unique(result.ExternalSources)
	sort.Strings(result.ExternalSources)
	if len(result.ExternalSources) > 0 {
		result.Warnings = append(result.Warnings, "外部来源仅保留计划，当前执行器未接入："+strings.Join(result.ExternalSources, ", "))
	}
	if start == nil {
		result.Warnings = append(result.Warnings, "协议未指定历史起点，含义为建库以来；Glean 的默认 30 日不能代表该范围。启用采集前必须明确回溯起点。")
	}
	if len(p.ExternalQueries) > 0 {
		result.Warnings = append(result.Warnings, "外部原生查询保留为独立 planned 项；Glean 从概念与通道重新编译执行子集，两者不会被视为等价或共享完成状态。")
	}
	result.Warnings = append(result.Warnings, "Glean 采集窗口按各源索引/更新日期执行；协议的发表日期、语言、文类与载体条件在证据审核阶段判定。来源声明和保存预览不证明当前运行环境已启用或可访问。")
	result.Warnings = unique(result.Warnings)
	result.Executable = p.Mode != "plan" && start != nil && len(result.SearchRoutes) > 0
	result.SemanticVersion = ProtocolSemanticVersion(p)
	raw, _ := json.Marshal(struct {
		Protocol Protocol
		Routes   []Route
		Warnings []string
	}{p, result.SearchRoutes, result.Warnings})
	h := sha256.Sum256(raw)
	result.Acknowledgement = hex.EncodeToString(h[:])
	return result, nil
}

// ExecutableSource 仅描述代码具备的能力，不代表本次运行已装配该来源。
func ExecutableSource(source string) bool {
	switch source {
	case "crossref", "pubmed", "europepmc", "arxiv", "biorxiv", "medrxiv":
		return true
	}
	return false
}

// ProtocolSemanticVersion 将真正影响召回或资格的协议和执行器版本绑定。
func ProtocolSemanticVersion(p Protocol) string {
	original, _ := json.Marshal(p)
	var copied Protocol
	_ = json.Unmarshal(original, &copied)
	p = copied
	p.ProfileVersion = ""
	p.Topic = ""
	p.Notes = ""
	p.ResearchQuestion = ""
	p.QualityChecks = nil
	p.Seeds = nil
	p.Budget = Budget{}
	for i := range p.Variants {
		p.Variants[i].Purpose = ""
	}
	// 深拷贝由调用端保存；此处不排序共享切片，避免改写传入协议。
	raw, _ := json.Marshal(struct {
		Executor string
		Protocol Protocol
	}{"glean-six-source-v2", p})
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

// ProtocolWindow 将协议的当地闭合日期范围转换为 UTC 半开窗口。
func ProtocolWindow(p Protocol) (time.Time, time.Time, error) {
	loc, err := time.LoadLocation(p.DateRange.Timezone)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	start, err := time.ParseInLocation("2006-01-02", p.DateRange.From, loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end, err := time.ParseInLocation("2006-01-02", p.DateRange.To, loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return start.UTC(), end.AddDate(0, 0, 1).UTC(), nil
}

// ImportProtocol 接受本系统协议或需求包编译器的 profile/requests，不执行原生 URL。
func ImportProtocol(raw json.RawMessage, now time.Time) (ProtocolPreview, error) {
	if len(raw) > 120*1024 {
		return ProtocolPreview{}, fmt.Errorf("protocol import exceeds 120 KiB")
	}
	if err := validateSafeJSON(raw); err != nil {
		return ProtocolPreview{}, err
	}
	var probe struct {
		Profile json.RawMessage `json:"profile"`
		Schema  string          `json:"schema_version"`
	}
	if json.Unmarshal(raw, &probe) != nil {
		return ProtocolPreview{}, fmt.Errorf("invalid protocol JSON")
	}
	var p Protocol
	decode := func(raw []byte, out any) error {
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		return d.Decode(out)
	}
	if len(probe.Profile) > 0 {
		var bundle struct {
			Schema             string          `json:"schema_version"`
			Mode               string          `json:"mode"`
			Profile            Protocol        `json:"profile"`
			GeneratedAt        string          `json:"generated_at"`
			ExecutionPerformed bool            `json:"execution_performed"`
			ScopeNote          string          `json:"scope_note"`
			Requests           []ExternalQuery `json:"requests"`
		}
		if err := decode(raw, &bundle); err != nil {
			return ProtocolPreview{}, fmt.Errorf("invalid compiled protocol fields: %w", err)
		}
		if bundle.ExecutionPerformed || bundle.Mode != "plan" {
			return ProtocolPreview{}, fmt.Errorf("only unexecuted plan bundles can be imported")
		}
		p = bundle.Profile
		p.ExternalQueries = append(p.ExternalQueries, bundle.Requests...)
	} else if err := decode(raw, &p); err != nil {
		return ProtocolPreview{}, fmt.Errorf("invalid protocol fields: %w", err)
	}
	// 需求包旧 profile 缺失的字段以明确假设补齐；不会补造词表或检索结果。
	if p.SchemaVersion == "" {
		p.SchemaVersion = "1.0"
	}
	if p.DateRange.Timezone == "" {
		p.DateRange.Timezone = "UTC"
		p.Assumptions = append(p.Assumptions, "原计划未提供时区，临时使用 UTC。")
	}
	if p.DateRange.EligibilityRule == "" {
		p.DateRange.EligibilityRule = "pending: independently verify formal publication dates"
		p.Assumptions = append(p.Assumptions, "发表资格日期规则尚未确认；发现时间不作正式发表依据。")
	}
	if p.VersionPolicy == "" {
		p.VersionPolicy = "retain preprints, published versions, corrections and retractions separately"
		p.Assumptions = append(p.Assumptions, "保留预印本、正式版、更正与撤稿关系，待按研究范围判定。")
	}
	if p.VenuePolicy == "" {
		p.VenuePolicy = "no metric threshold requested"
		p.Assumptions = append(p.Assumptions, "未设置 JIF 或 CCF 门槛。")
	}
	if len(p.SourceMatrix) == 0 {
		for _, s := range p.Sources {
			p.SourceMatrix = append(p.SourceMatrix, SourcePlan{Source: s, Reason: "Imported source selection; discipline fit needs pilot review", AccessStatus: "untested", License: "Review source terms before execution or content export", Risk: "Index and content coverage unverified; does not imply exhaustive recall"})
		}
		p.Assumptions = append(p.Assumptions, "旧计划未提供完整来源矩阵；来源权限、许可及适配均未测试。")
	}
	if len(p.Pathways) == 0 {
		for _, r := range []string{"topic", "venue", "citation", "web"} {
			p.Pathways = append(p.Pathways, Pathway{Route: r, Selected: r == "topic", Reason: "Imported plan only compiles topic queries; other routes remain planned gaps", NextAction: "Assess and document this pathway before claiming scope completion"})
		}
	}
	return PreviewProtocol(p, now)
}

func validateProtocol(p Protocol, now time.Time) error {
	raw, err := json.Marshal(p)
	if err != nil || len(raw) > 120*1024 {
		return fmt.Errorf("protocol exceeds 120 KiB")
	}
	if err = validateSafeJSON(raw); err != nil {
		return err
	}
	if p.SchemaVersion != "1.0" || !routeID.MatchString(p.ProfileID) || len(p.ProfileVersion) == 0 || len(p.ProfileVersion) > 80 || strings.TrimSpace(p.Topic) == "" || len(p.Topic) > 2000 {
		return fmt.Errorf("protocol requires schema_version 1.0, profile_id, profile_version and topic")
	}
	if p.Mode != "plan" && p.Mode != "pilot" && p.Mode != "execute" && p.Mode != "update" {
		return fmt.Errorf("invalid protocol mode")
	}
	end, err := time.Parse("2006-01-02", p.DateRange.To)
	if err != nil || end.After(now.UTC().Add(24*time.Hour)) {
		return fmt.Errorf("explicit nonfuture freeze date is required")
	}
	if p.DateRange.Timezone == "" || p.DateRange.Semantics == "" || p.DateRange.EligibilityRule == "" {
		return fmt.Errorf("timezone, discovery date semantics and publication eligibility rule are required")
	}
	if _, err = time.LoadLocation(p.DateRange.Timezone); err != nil {
		return fmt.Errorf("unknown protocol timezone")
	}
	if p.DateRange.From != "" {
		start, e := time.Parse("2006-01-02", p.DateRange.From)
		if e != nil || start.After(end) || start.Year() < 1000 {
			return fmt.Errorf("invalid protocol history range")
		}
	}
	if len(p.Concepts) == 0 || len(p.Concepts) > 20 || len(p.Variants) == 0 || len(p.Variants) > 20 || len(p.Sources) == 0 || len(p.Sources) > 20 {
		return fmt.Errorf("protocol requires bounded concepts, variants and sources")
	}
	concepts := map[string]bool{}
	sources := map[string]bool{}
	for _, s := range p.Sources {
		if !routeID.MatchString(s) || sources[s] {
			return fmt.Errorf("source IDs must be unique safe slugs")
		}
		sources[s] = true
	}
	for _, c := range p.Concepts {
		if !routeID.MatchString(c.ID) || concepts[c.ID] || c.TermSource == "" {
			return fmt.Errorf("concepts require unique IDs and term sources")
		}
		if err := validateGroups([][]string{c.Terms}); err != nil {
			return err
		}
		concepts[c.ID] = true
		if len(c.ControlledTerms) > 0 && c.ControlledEvidence == "" {
			return fmt.Errorf("controlled vocabulary requires verification evidence")
		}
	}
	variants := map[string]bool{}
	for _, v := range p.Variants {
		if !routeID.MatchString(v.ID) || len(v.ID) > 60 || variants[v.ID] || v.Purpose == "" || len(v.ConceptIDs) == 0 || len(v.ConceptIDs) > 10 {
			return fmt.Errorf("invalid search variant")
		}
		variants[v.ID] = true
		seen := map[string]bool{}
		for _, id := range v.ConceptIDs {
			if !concepts[id] || seen[id] {
				return fmt.Errorf("variant references an unknown or repeated concept")
			}
			seen[id] = true
		}
		for _, source := range v.Sources {
			if !sources[source] {
				return fmt.Errorf("variant references an unselected source")
			}
		}
	}
	seenSources := map[string]bool{}
	for _, s := range p.SourceMatrix {
		if !routeID.MatchString(s.Source) || seenSources[s.Source] || s.Reason == "" || s.Risk == "" || s.License == "" {
			return fmt.Errorf("source matrix requires unique sources, rationale, license and risks")
		}
		seenSources[s.Source] = true
		switch s.AccessStatus {
		case "verified", "untested", "needs_auth", "no_subscription", "blocked", "not_selected":
		default:
			return fmt.Errorf("invalid source access status")
		}
	}
	for s := range sources {
		if !seenSources[s] {
			return fmt.Errorf("selected source %s is missing from source matrix", s)
		}
	}
	pathways := map[string]bool{}
	for _, p := range p.Pathways {
		if p.Reason == "" || pathways[p.Route] {
			return fmt.Errorf("pathways require distinct routes and reasons")
		}
		pathways[p.Route] = true
	}
	for _, p := range []string{"topic", "venue", "citation", "web"} {
		if !pathways[p] {
			return fmt.Errorf("topic, venue, citation and web pathways must each be assessed")
		}
	}
	for _, q := range p.ExternalQueries {
		if q.QueryID == "" || q.QueryVersion == "" || q.Source == "" || q.Status != "planned" || q.ReportedHits != nil || q.FetchedCount != nil {
			return fmt.Errorf("imported queries must be planned with null result counts")
		}
	}
	for _, s := range p.Seeds {
		if s.IdentityVerified && s.Evidence == "" {
			return fmt.Errorf("verified seed requires primary-source evidence")
		}
	}
	for _, q := range p.QualityChecks {
		if q.ID == "" || q.Frame == "" || q.Method == "" || q.SampleSize < 0 || (q.Missed != nil && (*q.Missed < 0 || *q.Missed > q.SampleSize)) || (q.SourceAbsent != nil && (*q.SourceAbsent < 0 || *q.SourceAbsent > q.SampleSize)) {
			return fmt.Errorf("invalid quality sampling frame or counts")
		}
		if q.ReviewedAt == nil && (q.Missed != nil || q.SourceAbsent != nil) {
			return fmt.Errorf("unreviewed quality samples cannot claim results")
		}
	}
	b := p.Budget
	if b.MaxRequests < 0 || b.MaxRequests > 1000000 || b.MaxPages < 0 || b.MaxPages > 1000000 || b.MaxRecords < 0 || b.MaxRecords > 10000000 || b.MaxQueue < 0 || b.MaxQueue > 1000000 || b.MaxHistoryDays < 0 || b.MaxHistoryDays > 400000 {
		return fmt.Errorf("invalid protocol budget")
	}
	if p.Mode != "plan" && (p.DateRange.From == "" || b.MaxRequests == 0 || b.MaxPages == 0 || b.MaxRecords == 0 || b.MaxQueue == 0 || b.MaxHistoryDays == 0) {
		return fmt.Errorf("execution requires an explicit history start and positive request, page, record, queue and history budgets")
	}
	return nil
}

// validateSafeJSON 拒绝将凭据或带认证信息的 URL 混入长期协议。
func validateSafeJSON(raw []byte) error {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return fmt.Errorf("invalid JSON")
	}
	var walk func(any) error
	walk = func(v any) error {
		switch x := v.(type) {
		case map[string]any:
			for key, child := range x {
				k := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
				if k == "authorization" || k == "password" || k == "api_key" || k == "apikey" || k == "access_token" || k == "secret" || k == "cookie" || k == "webenv" {
					return fmt.Errorf("credentials and session values are not allowed in protocol data")
				}
				if err := walk(child); err != nil {
					return err
				}
			}
		case []any:
			for _, c := range x {
				if err := walk(c); err != nil {
					return err
				}
			}
		case string:
			if strings.ContainsRune(x, 0) {
				return fmt.Errorf("protocol text cannot contain NUL")
			}
			if strings.HasPrefix(x, "https://") || strings.HasPrefix(x, "http://") {
				u, e := url.Parse(x)
				if e != nil || u.User != nil {
					return fmt.Errorf("protocol URL must not contain credentials")
				}
				for key := range u.Query() {
					if err := walk(map[string]any{key: ""}); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	return walk(value)
}
