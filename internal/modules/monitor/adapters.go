package monitor

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/teamsillybees/glean/internal/platform/httpclient"
	"github.com/teamsillybees/glean/internal/retrieval"
)

const sourcePageSize = 100

// SourceError 表示不含上游响应正文或凭据的采集错误；调用层负责持久化退避，不在适配器内立即重试。
type SourceError struct {
	Kind       string
	StatusCode int
	RetryAfter time.Duration
	Retryable  bool
	Message    string
}

// Error 返回可安全展示的错误摘要。
func (e *SourceError) Error() string { return e.Kind + ": " + e.Message }

// ParseRetryAfter 解析上游 Retry-After 秒数或 HTTP 日期，过去日期不产生负等待。
func ParseRetryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		// 避免非可信响应溢出 time.Duration；超过上限仍维持长等待。
		if seconds > int64((1<<63-1)/int64(time.Second)) {
			return time.Duration(1<<63 - 1)
		}
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}

type remoteSource struct {
	name     string
	executor httpclient.Executor
}

// NewCrossrefSource 构造 Crossref 适配器；仅注入 Executor，不发送网络请求。
func NewCrossrefSource(executor httpclient.Executor) Source {
	return &remoteSource{name: "crossref", executor: executor}
}

// NewPubMedSource 构造 PubMed ESearch/EFetch 适配器，Executor BaseURL 应为 https://eutils.ncbi.nlm.nih.gov。
func NewPubMedSource(executor httpclient.Executor) Source {
	return &remoteSource{name: "pubmed", executor: executor}
}

// NewArXivSource 构造 arXiv OAI-PMH 适配器，Executor BaseURL 应为 https://oaipmh.arxiv.org。
func NewArXivSource(executor httpclient.Executor) Source {
	return &remoteSource{name: "arxiv", executor: executor}
}

// NewBioRxivSource 构造 bioRxiv 版本发现适配器，不把关联期刊 DOI 当作正式发表日期。
func NewBioRxivSource(executor httpclient.Executor) Source {
	return &remoteSource{name: "biorxiv", executor: executor}
}

// NewEuropePMCSource 构造 Europe PMC 增量检索适配器，不发送网络请求。
func NewEuropePMCSource(executor httpclient.Executor) Source {
	return &remoteSource{name: "europepmc", executor: executor}
}

// NewMedRxivSource 构造 medRxiv 版本发现适配器，与 bioRxiv 共用官方 API 域名。
func NewMedRxivSource(executor httpclient.Executor) Source {
	return &remoteSource{name: "medrxiv", executor: executor}
}

// Name 返回统一来源名称。
func (s *remoteSource) Name() string { return s.name }

// Fetch 执行单页只读请求；退避、节流和重试由持久化运行服务统一控制。
func (s *remoteSource) Fetch(ctx context.Context, req FetchRequest) (Page, error) {
	if req.Window.Start.IsZero() || !req.Window.Start.Before(req.Window.End) || req.PageSize < 0 || req.PageSize > 1000 {
		return Page{}, malformed("invalid fixed query window")
	}
	if s.executor == nil {
		return Page{}, &SourceError{Kind: "disabled", Message: "HTTP executor not configured"}
	}
	// 显式路线使用共享编译器；空字段模式保留旧快照语义。
	if req.Template.FieldMode != "" && !req.Template.Core {
		compiled, err := retrieval.Compile(s.name, req.Template.TermGroups, req.Template.FieldMode)
		if err != nil {
			return Page{}, &SourceError{Kind: "invalid_query", Message: err.Error()}
		}
		if compiled.Mode == "boolean" {
			req.Template.Query = compiled.Query
		}
	}
	switch s.name {
	case "crossref":
		return s.crossref(ctx, req)
	case "pubmed":
		return s.pubmed(ctx, req)
	case "arxiv":
		return s.arxiv(ctx, req)
	case "biorxiv", "medrxiv":
		return s.biorxiv(ctx, req)
	case "europepmc":
		return s.europepmc(ctx, req)
	}
	return Page{}, malformed("unknown source")
}

func (s *remoteSource) get(ctx context.Context, path string, query map[string]string) ([]byte, error) {
	resp, err := s.executor.Do(ctx, http.MethodGet, path, httpclient.WithQueryParams(query), httpclient.WithHeader("Accept", "application/json, application/xml, application/atom+xml"))
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}
	var sourceErr *SourceError
	if errors.As(err, &sourceErr) {
		return nil, sourceErr
	}
	if resp != nil && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
		return nil, &SourceError{Kind: "http", StatusCode: resp.StatusCode, RetryAfter: ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now()), Retryable: resp.StatusCode == 429 || resp.StatusCode >= 500, Message: fmt.Sprintf("source returned HTTP %d", resp.StatusCode)}
	}
	if err != nil || resp == nil {
		return nil, &SourceError{Kind: "transport", Retryable: true, Message: "source request failed"}
	}
	if len(resp.Body) > 16<<20 {
		return nil, malformed("source page exceeds 16 MiB")
	}
	return resp.Body, nil
}
func malformed(message string) error { return &SourceError{Kind: "invalid_response", Message: message} }
func rawXML(value string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"format": "xml", "value": value})
	return b
}
func firstString(values []string) string {
	if len(values) > 0 {
		return values[0]
	}
	return ""
}
func cleanDOI(value string) string {
	value = strings.TrimSpace(value)
	for _, p := range []string{"https://doi.org/", "http://doi.org/", "http://dx.doi.org/", "https://dx.doi.org/", "doi:"} {
		if strings.HasPrefix(strings.ToLower(value), p) {
			value = value[len(p):]
			break
		}
	}
	return strings.ToLower(strings.TrimSpace(value))
}
func exactDate(year, month, day int) time.Time {
	if year < 1000 || month < 1 || month > 12 || day < 1 || day > 31 {
		return time.Time{}
	}
	result := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if result.Year() != year || int(result.Month()) != month || result.Day() != day {
		return time.Time{}
	}
	return result
}
func addEvent(events []PublicationEvent, kind string, date time.Time, evidence string) []PublicationEvent {
	if !date.IsZero() {
		return append(events, PublicationEvent{Kind: kind, Date: date, Evidence: evidence})
	}
	return events
}
func dateOnly(date time.Time) string { return date.UTC().Format("2006-01-02") }

// Crossref 同步依赖 indexed 时间，而正式发表日期只能来自 published-online/print。
// 官方语义：https://www.crossref.org/documentation/retrieve-metadata/rest-api/tips-for-using-the-crossref-rest-api/
func (s *remoteSource) crossref(ctx context.Context, req FetchRequest) (Page, error) {
	cursor := crossrefCursor{Token: "*"}
	if req.Cursor != "" {
		var err error
		cursor, err = decodeCrossrefCursor(req.Cursor)
		if err != nil {
			return Page{}, err
		}
	}
	feeds := crossrefDiscoveryQueries(req.Template)
	if req.Template.FieldMode != "" && !req.Template.Core {
		compiled, err := retrieval.Compile("crossref", req.Template.TermGroups, req.Template.FieldMode)
		if err != nil {
			return Page{}, &SourceError{Kind: "invalid_query", Message: err.Error()}
		}
		feeds = compiled.Queries
	}
	path := "/works"
	if req.Template.Core {
		var e error
		feeds, e = registryISSNs(req)
		if e != nil {
			return Page{}, e
		}
	}
	if len(feeds) == 0 || cursor.Feed < 0 || cursor.Feed >= len(feeds) {
		return Page{}, malformed("invalid Crossref feed cursor")
	}
	query := map[string]string{"filter": "from-index-date:" + crossrefFilterTime(req.Window.Start, false) + ",until-index-date:" + crossrefFilterTime(req.Window.End, true), "rows": strconv.Itoa(requestPageSize(req)), "cursor": cursor.Token}
	if req.Template.Core {
		path = "/journals/" + feeds[cursor.Feed] + "/works"
	} else {
		query["query.bibliographic"] = feeds[cursor.Feed]
	}
	body, err := s.get(ctx, path, query)
	if err != nil {
		var se *SourceError
		if cursor.Token != "*" && cursor.Restarts < 3 && errors.As(err, &se) && (se.StatusCode == 400 || se.StatusCode == 404) {
			return Page{NextCursor: (crossrefCursor{Token: "*", Feed: cursor.Feed, Restarts: cursor.Restarts + 1}).encode()}, nil
		}
		return Page{}, err
	}
	var response struct {
		Status  string `json:"status"`
		Message struct {
			Items []json.RawMessage `json:"items"`
			Next  string            `json:"next-cursor"`
			Total *int              `json:"total-results"`
		} `json:"message"`
	}
	if json.Unmarshal(body, &response) != nil || response.Status != "ok" || response.Message.Items == nil {
		return Page{}, malformed("invalid Crossref envelope")
	}
	// 后续页缺少 total 不能抹掉首页已证明的总量，否则空页可能提前完成。
	if response.Message.Total == nil {
		response.Message.Total = cursor.Total
	}
	// 新版 Crossref 末页可省略 next-cursor；累计原始条目数与 total-results 一致才确认该末页。
	// 分页证据不足或与数量矛盾时保留原 checkpoint，不能把缺字段当成功。
	if cursor.Total != nil && response.Message.Total != nil && *cursor.Total != *response.Message.Total {
		if cursor.Restarts >= 3 {
			return Page{}, malformed("Crossref index changed during three restarts; incomplete fixed window retained")
		}
		return Page{NextCursor: (crossrefCursor{Token: "*", Feed: cursor.Feed, Restarts: cursor.Restarts + 1}).encode()}, nil
	}
	seen := cursor.Seen + len(response.Message.Items)
	if response.Message.Total != nil && (*response.Message.Total < 0 || seen > *response.Message.Total) {
		return Page{}, malformed("Crossref page exceeds declared total")
	}
	page := Page{}
	if len(response.Message.Items) == 0 {
		if response.Message.Total != nil && seen < *response.Message.Total {
			return Page{}, malformed("prematurely empty Crossref page")
		}
		page.Complete = true
	} else if response.Message.Next == "" {
		if response.Message.Total == nil || seen != *response.Message.Total {
			return Page{}, malformed("nonempty Crossref page has no continuation cursor; completion not proven")
		}
		page.Complete = true
	} else {
		page.NextCursor = (crossrefCursor{Token: response.Message.Next, Seen: seen, Feed: cursor.Feed, Total: response.Message.Total, Restarts: cursor.Restarts}).encode()
	}
	if page.Complete && cursor.Feed+1 < len(feeds) {
		page.Complete = false
		page.NextCursor = (crossrefCursor{Token: "*", Feed: cursor.Feed + 1}).encode()
	}
	for _, raw := range response.Message.Items {
		var item struct {
			DOI      string   `json:"DOI"`
			Type     string   `json:"type"`
			Title    []string `json:"title"`
			Abstract string   `json:"abstract"`
			URL      string   `json:"URL"`
			Journal  []string `json:"container-title"`
			ISSN     []string `json:"ISSN"`
			Author   []struct {
				Given  string `json:"given"`
				Family string `json:"family"`
				Name   string `json:"name"`
			} `json:"author"`
			Indexed struct {
				Date string `json:"date-time"`
			} `json:"indexed"`
			Online crossrefDate `json:"published-online"`
			Print  crossrefDate `json:"published-print"`
			Posted crossrefDate `json:"posted"`
		}
		if json.Unmarshal(raw, &item) != nil || strings.TrimSpace(item.DOI) == "" || strings.TrimSpace(strings.Join(item.Title, " ")) == "" {
			page.Records = append(page.Records, rejectedJSONRecord(s.name, raw))
			continue
		}
		updated, err := time.Parse(time.RFC3339, item.Indexed.Date)
		if err != nil {
			page.Records = append(page.Records, rejectedJSONRecord(s.name, raw))
			continue
		}
		if updated.Before(req.Window.Start) || !updated.Before(req.Window.End) {
			continue
		}
		record := Record{Source: s.name, SourceID: cleanDOI(item.DOI), DOI: cleanDOI(item.DOI), Title: strings.Join(item.Title, " "), Abstract: item.Abstract, URL: item.URL, Journal: firstString(item.Journal), ISSNs: item.ISSN, SourceUpdatedAt: updated.UTC(), Topics: []string{req.Template.Topic}, Raw: append(json.RawMessage(nil), raw...)}
		record.SourceURL = sourceRecordURL(s.name, record.SourceID)
		record.PublicationType = item.Type
		for _, author := range item.Author {
			name := strings.TrimSpace(author.Given + " " + author.Family)
			if name == "" {
				name = author.Name
			}
			if name != "" {
				record.Authors = append(record.Authors, name)
			}
		}
		if item.Type == "posted-content" {
			record.PublicationEvents = addEvent(record.PublicationEvents, "preprint", item.Posted.exact(), "Crossref posted (preprint)")
		} else if item.Type != "" {
			record.PublicationEvents = addEvent(record.PublicationEvents, "online", item.Online.exact(), "Crossref published-online")
			record.PublicationEvents = addEvent(record.PublicationEvents, "issue", item.Print.exact(), "Crossref published-print")
		}
		page.Records = append(page.Records, record)
	}
	return page, nil
}

type crossrefCursor struct {
	Token    string `json:"token"`
	Seen     int    `json:"seen"`
	Feed     int    `json:"feed"`
	Total    *int   `json:"total,omitempty"`
	Restarts int    `json:"restarts,omitempty"`
}

func (c crossrefCursor) encode() string {
	b, _ := json.Marshal(c)
	return "cr2:" + base64.RawURLEncoding.EncodeToString(b)
}
func decodeCrossrefCursor(value string) (crossrefCursor, error) {
	var c crossrefCursor
	if !strings.HasPrefix(value, "cr2:") {
		return c, malformed("invalid Crossref cursor")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, "cr2:"))
	if err != nil || json.Unmarshal(b, &c) != nil || c.Token == "" || c.Seen < 0 || c.Feed < 0 || c.Restarts < 0 || c.Restarts > 3 {
		return c, malformed("invalid Crossref cursor")
	}
	return c, nil
}

type crossrefDate struct {
	Parts [][]int `json:"date-parts"`
}

func (d crossrefDate) exact() time.Time {
	if len(d.Parts) > 0 && len(d.Parts[0]) == 3 {
		return exactDate(d.Parts[0][0], d.Parts[0][1], d.Parts[0][2])
	}
	return time.Time{}
}

type pubmedDate struct {
	Year   int    `xml:"Year"`
	Month  string `xml:"Month"`
	Day    int    `xml:"Day"`
	Type   string `xml:"DateType,attr"`
	Status string `xml:"PubStatus,attr"`
}

func (d pubmedDate) exact() time.Time {
	month, _ := strconv.Atoi(d.Month)
	if month == 0 {
		if parsed, err := time.Parse("Jan", d.Month); err == nil {
			month = int(parsed.Month())
		}
	}
	return exactDate(d.Year, month, d.Day)
}

type innerText struct {
	Value string `xml:",innerxml"`
}
type pubmedArticle struct {
	Raw      string `xml:",innerxml"`
	Citation struct {
		PMID struct {
			Value string `xml:",chardata"`
		} `xml:"PMID"`
		Revised     pubmedDate `xml:"DateRevised"`
		ISSNLinking string     `xml:"MedlineJournalInfo>ISSNLinking"`
		Article     struct {
			Title   innerText `xml:"ArticleTitle"`
			Authors []struct {
				ForeName       string `xml:"ForeName"`
				LastName       string `xml:"LastName"`
				CollectiveName string `xml:"CollectiveName"`
			} `xml:"AuthorList>Author"`
			Types    []string `xml:"PublicationTypeList>PublicationType"`
			Abstract struct {
				Text []innerText `xml:"AbstractText"`
			} `xml:"Abstract"`
			Dates   []pubmedDate `xml:"ArticleDate"`
			Journal struct {
				Title string `xml:"Title"`
				ISSN  string `xml:"ISSN"`
				Issue struct {
					Date   pubmedDate `xml:"PubDate"`
					Volume string     `xml:"Volume"`
					Number string     `xml:"Issue"`
				} `xml:"JournalIssue"`
			} `xml:"Journal"`
		} `xml:"Article"`
	} `xml:"MedlineCitation"`
	Data struct {
		Status string `xml:"PublicationStatus"`
		IDs    []struct {
			Type  string `xml:"IdType,attr"`
			Value string `xml:",chardata"`
		} `xml:"ArticleIdList>ArticleId"`
		History []pubmedDate `xml:"History>PubMedPubDate"`
	} `xml:"PubmedData"`
}

func xmlText(value string) string {
	decoder := xml.NewDecoder(strings.NewReader("<text>" + value + "</text>"))
	var out strings.Builder
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return strings.TrimSpace(value)
		}
		if text, ok := token.(xml.CharData); ok {
			out.Write(text)
		}
	}
	return strings.Join(strings.Fields(out.String()), " ")
}

// rxivCursor 只保存在内部；上游路径仍使用数值 offset。
type rxivCursor struct {
	Offset   int `json:"offset"`
	Total    int `json:"total"`
	Restarts int `json:"restarts"`
}

func (c rxivCursor) encode() string {
	b, _ := json.Marshal(c)
	return "rx1:" + base64.RawURLEncoding.EncodeToString(b)
}

func decodeRxivCursor(value string) (rxivCursor, error) {
	var wire struct {
		Offset   *int `json:"offset"`
		Total    *int `json:"total"`
		Restarts *int `json:"restarts"`
	}
	invalid := malformed("invalid preprint pagination cursor")
	if len(value) > 1024 || !strings.HasPrefix(value, "rx1:") {
		return rxivCursor{}, invalid
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, "rx1:"))
	if err != nil || json.Unmarshal(b, &wire) != nil || wire.Offset == nil || wire.Total == nil || wire.Restarts == nil {
		return rxivCursor{}, invalid
	}
	c := rxivCursor{Offset: *wire.Offset, Total: *wire.Total, Restarts: *wire.Restarts}
	if c.Offset < 0 || c.Restarts < 0 || c.Restarts > 3 || (c.Total == -1 && c.Offset != 0) || c.Total < -1 || (c.Total >= 0 && (c.Offset == 0 || c.Offset >= c.Total)) {
		return rxivCursor{}, invalid
	}
	return c, nil
}

func (c rxivCursor) restart() (Page, error) {
	if c.Restarts >= 3 {
		return Page{}, malformed("preprint index changed during three restarts; incomplete fixed window retained")
	}
	return Page{NextCursor: (rxivCursor{Total: -1, Restarts: c.Restarts + 1}).encode()}, nil
}

// bioRxiv 日期窗口返回新帖和版本；published 字段仅为关联 DOI，不含可证明正式发表的日期。
// 官方接口：https://api.biorxiv.org/
func (s *remoteSource) biorxiv(ctx context.Context, req FetchRequest) (Page, error) {
	cursor := rxivCursor{Total: -1}
	if req.Cursor != "" {
		// 旧 offset 没有原始总数，不能证明已读前缀仍有效；先持久化有界重放。
		if !strings.HasPrefix(req.Cursor, "rx1:") {
			if len(req.Cursor) > 20 || strings.Trim(req.Cursor, "0123456789") != "" {
				return Page{}, malformed("invalid preprint pagination cursor")
			}
			if _, err := strconv.Atoi(req.Cursor); err != nil {
				return Page{}, malformed("invalid preprint pagination cursor")
			}
			return cursor.restart()
		}
		var err error
		cursor, err = decodeRxivCursor(req.Cursor)
		if err != nil {
			return Page{}, err
		}
	}
	offset := cursor.Offset
	body, err := s.get(ctx, "/details/"+s.name+"/"+dateOnly(req.Window.Start)+"/"+dateOnly(req.Window.End.Add(-time.Nanosecond))+"/"+strconv.Itoa(offset)+"/json", nil)
	if err != nil {
		return Page{}, err
	}
	var r struct {
		Messages []struct {
			Status string      `json:"status"`
			Cursor json.Number `json:"cursor"`
			Total  json.Number `json:"total"`
			Count  json.Number `json:"count"`
		} `json:"messages"`
		Collection []json.RawMessage `json:"collection"`
	}
	if json.Unmarshal(body, &r) != nil || len(r.Messages) != 1 || r.Collection == nil {
		return Page{}, malformed("invalid preprint API envelope")
	}
	// 官方空区间响应无 count/total/cursor，只有明确的 no posts found；续页出现时按总数漂移有界重放。
	if r.Messages[0].Status == "no posts found" && len(r.Collection) == 0 {
		if cursor.Total >= 0 {
			return cursor.restart()
		}
		if offset == 0 {
			return Page{Complete: true}, nil
		}
	}
	if r.Messages[0].Status != "ok" {
		return Page{}, malformed("preprint API returned non-success status")
	}
	total, err := strconv.Atoi(string(r.Messages[0].Total))
	if err != nil || total < 0 {
		return Page{}, malformed("invalid bioRxiv result count")
	}
	responseOffset, err := strconv.Atoi(string(r.Messages[0].Cursor))
	if err != nil || responseOffset != offset {
		return Page{}, malformed("invalid bioRxiv result cursor")
	}
	// 必须先检测总数漂移；缩至 offset 以下也需重放，而非误报完成。
	if cursor.Total >= 0 && cursor.Total != total {
		return cursor.restart()
	}
	if offset > total || len(r.Collection) > total-offset {
		return Page{}, malformed("bioRxiv page exceeds declared total")
	}
	if len(r.Collection) == 0 && offset < total {
		return Page{}, malformed("prematurely empty bioRxiv page")
	}
	if r.Messages[0].Count != "" {
		count, e := strconv.Atoi(string(r.Messages[0].Count))
		if e != nil || count != len(r.Collection) {
			return Page{}, malformed("preprint page count differs from collection")
		}
	}
	page := Page{}
	for _, raw := range r.Collection {
		var item struct {
			DOI       string `json:"doi"`
			Title     string `json:"title"`
			Abstract  string `json:"abstract"`
			Date      string `json:"date"`
			Version   string `json:"version"`
			Published string `json:"published"`
			Authors   string `json:"authors"`
		}
		if json.Unmarshal(raw, &item) != nil || strings.TrimSpace(item.DOI) == "" || strings.TrimSpace(item.Title) == "" {
			page.Records = append(page.Records, rejectedJSONRecord(s.name, raw))
			continue
		}
		// 日期缺失或精度不足只意味着出版日期未知，不能因此隔离整篇论文。
		updated, _ := time.Parse("2006-01-02", item.Date)
		if !updated.IsZero() && (updated.Before(dayStart(req.Window.Start)) || !updated.Before(req.Window.End)) {
			continue
		}
		// bioRxiv 无全文关键词参数。统一模板只作宽松发现，最终纳入仍由独立审核处理。
		// 已取回记录全部保留，不以本地关键词不可逆丢弃。
		record := Record{Source: s.name, SourceID: cleanDOI(item.DOI), DOI: cleanDOI(item.DOI), Title: item.Title, Abstract: item.Abstract, URL: "https://doi.org/" + cleanDOI(item.DOI), Version: item.Version, SourceUpdatedAt: updated, Topics: []string{req.Template.Topic}, Raw: append(json.RawMessage(nil), raw...)}
		record.SourceURL = sourceRecordURL(s.name, record.SourceID)
		record.PublicationType = "preprint"
		if item.Authors != "" {
			record.Authors = strings.Split(item.Authors, "; ")
		}
		record.PublicationEvents = addEvent(record.PublicationEvents, "preprint", updated, s.name+" posted date for source version "+item.Version)
		page.Records = append(page.Records, record)
	}
	next := offset + len(r.Collection)
	page.Complete = next >= total
	if !page.Complete {
		cursor.Offset, cursor.Total = next, total
		page.NextCursor = cursor.encode()
	}
	return page, nil
}
func matchesAnyTerm(text string, terms []string) bool {
	if len(terms) == 0 {
		return true
	}
	text = strings.ToLower(text)
	for _, term := range terms {
		if strings.Contains(text, strings.ToLower(term)) {
			return true
		}
	}
	return false
}

// Crossref 不支持 Boolean OR，逐个第一组 seed 取并集，避免把互斥同义词 AND 在一个查询里。
func crossrefDiscoveryQueries(template SearchTemplate) []string {
	terms := template.Terms
	if len(template.TermGroups) > 0 {
		terms = template.TermGroups[0]
	}
	if len(terms) == 0 {
		terms = []string{template.Query}
	}
	seen := map[string]bool{}
	out := []string{}
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term != "" && !seen[term] {
			out = append(out, term)
			seen[term] = true
		}
	}
	return out
}

func dayStart(value time.Time) time.Time {
	return time.Date(value.UTC().Year(), value.UTC().Month(), value.UTC().Day(), 0, 0, 0, 0, time.UTC)
}

func registryISSNs(req FetchRequest) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, raw := range req.RegistryISSNs {
		n, e := NormalizeISSN(raw)
		if e != nil {
			return nil, malformed("invalid registry ISSN")
		}
		if !seen[n] {
			out = append(out, n)
			seen[n] = true
		}
	}
	sort.Strings(out)
	if req.Template.Core && len(out) == 0 {
		return nil, &SourceError{Kind: "missing_registry", Message: "core journal query requires a nonempty verified ISSN registry"}
	}
	return out, nil
}

// matchesTemplateGroups 只翻译检索意图：组间 AND、组内 OR，不赋予命中记录科学审核结论。
func matchesTemplateGroups(text string, template SearchTemplate) bool {
	if len(template.TermGroups) == 0 {
		return matchesAnyTerm(text, template.Terms)
	}
	for _, group := range template.TermGroups {
		if len(group) == 0 || !matchesAnyTerm(text, group) {
			return false
		}
	}
	return true
}

// sourceRecordURL 指向可复取原始元数据的官方页面/API，和论文/出版商 URL 分开保存。
func sourceRecordURL(source, id string) string {
	switch source {
	case "crossref":
		return "https://api.crossref.org/works/" + url.PathEscape(id)
	case "pubmed":
		return "https://eutils.ncbi.nlm.nih.gov/entrez/eutils/efetch.fcgi?db=pubmed&retmode=xml&id=" + url.QueryEscape(id)
	case "arxiv":
		return "https://oaipmh.arxiv.org/oai?verb=GetRecord&metadataPrefix=arXivRaw&identifier=" + url.QueryEscape("oai:arXiv.org:"+id)
	case "biorxiv", "medrxiv":
		return "https://api.biorxiv.org/details/" + source + "/" + url.PathEscape(id) + "/na/json"
	case "europepmc":
		parts := strings.SplitN(id, ":", 2)
		if len(parts) == 2 {
			return "https://www.ebi.ac.uk/europepmc/webservices/rest/search?format=json&resultType=core&query=" + url.QueryEscape("EXT_ID:"+strconv.Quote(parts[1])+" AND SRC:"+strconv.Quote(parts[0]))
		}
	}
	return ""
}

func requestPageSize(req FetchRequest) int {
	if req.PageSize > 0 {
		return req.PageSize
	}
	return sourcePageSize
}

// Crossref 查询只支持秒精度。上界向上取整后再本地半开过滤，不能漏掉 End 前最后一秒的毫秒记录。
func crossrefFilterTime(value time.Time, upper bool) string {
	value = value.UTC()
	if upper && value.Nanosecond() != 0 {
		value = value.Add(time.Second - time.Duration(value.Nanosecond()))
	}
	return value.Format("2006-01-02T15:04:05")
}

// rejectedJSONRecord 保存单条坏数据；标记必须先于审核/发布处理，哈希只作隔离身份而非论文标识。
func rejectedJSONRecord(source string, raw json.RawMessage) Record {
	r := Record{Source: source, Raw: append(json.RawMessage(nil), raw...), ValidationError: "invalid_source_record"}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	stringField := func(key string) string {
		var value string
		_ = json.Unmarshal(fields[key], &value)
		return strings.TrimSpace(value)
	}
	switch source {
	case "crossref":
		r.SourceID = cleanDOI(stringField("DOI"))
		var titles []string
		if json.Unmarshal(fields["title"], &titles) == nil {
			r.Title = strings.Join(titles, " ")
		}
	case "biorxiv", "medrxiv":
		r.SourceID = cleanDOI(stringField("doi"))
		r.Title = stringField("title")
	case "europepmc":
		if id, origin := stringField("id"), stringField("source"); id != "" && origin != "" {
			r.SourceID = origin + ":" + id
		}
		r.Title = stringField("title")
	}
	if r.SourceID == "" {
		r.SourceID = fmt.Sprintf("unparsed-sha256:%x", sha256.Sum256(raw))
	} else {
		r.SourceURL = sourceRecordURL(source, r.SourceID)
	}
	return r
}

// coreISSNBatchSize 保守限制核心全刊查询的 OR 项数量，避免登记表扩展后 GET URL 过长。
const coreISSNBatchSize = 40

func coreJournalFeeds(req FetchRequest) ([][]string, string, error) {
	if !req.Template.Core {
		return nil, "", nil
	}
	issns, err := registryISSNs(req)
	if err != nil {
		return nil, "", err
	}
	feeds := make([][]string, 0, (len(issns)+coreISSNBatchSize-1)/coreISSNBatchSize)
	for start := 0; start < len(issns); start += coreISSNBatchSize {
		end := start + coreISSNBatchSize
		if end > len(issns) {
			end = len(issns)
		}
		feeds = append(feeds, issns[start:end])
	}
	return feeds, fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(issns, ",")))), nil
}
