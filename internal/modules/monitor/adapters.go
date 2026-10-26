package monitor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/teamsillybees/glean/internal/platform/httpclient"
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

// NewArXivSource 构造 arXiv Atom 适配器，不发送网络请求。
func NewArXivSource(executor httpclient.Executor) Source {
	return &remoteSource{name: "arxiv", executor: executor}
}

// NewBioRxivSource 构造 bioRxiv 版本发现适配器，不把关联期刊 DOI 当作正式发表日期。
func NewBioRxivSource(executor httpclient.Executor) Source {
	return &remoteSource{name: "biorxiv", executor: executor}
}

// Name 返回统一来源名称。
func (s *remoteSource) Name() string { return s.name }

// Fetch 执行单页只读请求；退避、节流和重试由持久化运行服务统一控制。
func (s *remoteSource) Fetch(ctx context.Context, req FetchRequest) (Page, error) {
	if req.Window.Start.IsZero() || !req.Window.Start.Before(req.Window.End) {
		return Page{}, malformed("invalid fixed query window")
	}
	if s.executor == nil {
		return Page{}, &SourceError{Kind: "disabled", Message: "HTTP executor not configured"}
	}
	switch s.name {
	case "crossref":
		return s.crossref(ctx, req)
	case "pubmed":
		return s.pubmed(ctx, req)
	case "arxiv":
		return s.arxiv(ctx, req)
	case "biorxiv":
		return s.biorxiv(ctx, req)
	}
	return Page{}, malformed("unknown source")
}

func (s *remoteSource) get(ctx context.Context, path string, query map[string]string) ([]byte, error) {
	resp, err := s.executor.Do(ctx, http.MethodGet, path, httpclient.WithQueryParams(query), httpclient.WithHeader("Accept", "application/json, application/xml, application/atom+xml"))
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, err
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
	body, err := s.get(ctx, "/works", map[string]string{"query.bibliographic": crossrefDiscoveryQuery(req.Template), "filter": "from-index-date:" + req.Window.Start.UTC().Format(time.RFC3339) + ",until-index-date:" + req.Window.End.UTC().Format(time.RFC3339), "rows": strconv.Itoa(sourcePageSize), "cursor": cursor.Token})
	if err != nil {
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
	// 新版 Crossref 末页可省略 next-cursor；累计原始条目数与 total-results 一致才确认该末页。
	// 分页证据不足或与数量矛盾时保留原 checkpoint，不能把缺字段当成功。
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
		page.NextCursor = (crossrefCursor{Token: response.Message.Next, Seen: seen}).encode()
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
			Indexed  struct {
				Date string `json:"date-time"`
			} `json:"indexed"`
			Online crossrefDate `json:"published-online"`
			Print  crossrefDate `json:"published-print"`
			Posted crossrefDate `json:"posted"`
		}
		if json.Unmarshal(raw, &item) != nil || item.DOI == "" || len(item.Title) == 0 {
			return Page{}, malformed("invalid Crossref work identity")
		}
		updated, err := time.Parse(time.RFC3339, item.Indexed.Date)
		if err != nil {
			return Page{}, malformed("missing Crossref indexed timestamp")
		}
		if updated.Before(req.Window.Start) || !updated.Before(req.Window.End) {
			continue
		}
		record := Record{Source: s.name, SourceID: cleanDOI(item.DOI), DOI: cleanDOI(item.DOI), Title: strings.Join(item.Title, " "), Abstract: item.Abstract, URL: item.URL, Journal: firstString(item.Journal), ISSNs: item.ISSN, SourceUpdatedAt: updated.UTC(), Topics: []string{req.Template.Topic}, Raw: append(json.RawMessage(nil), raw...)}
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
	Token string `json:"token"`
	Seen  int    `json:"seen"`
}

func (c crossrefCursor) encode() string {
	b, _ := json.Marshal(c)
	return "cr1:" + base64.RawURLEncoding.EncodeToString(b)
}
func decodeCrossrefCursor(value string) (crossrefCursor, error) {
	var c crossrefCursor
	if !strings.HasPrefix(value, "cr1:") {
		return c, malformed("invalid Crossref cursor")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, "cr1:"))
	if err != nil || json.Unmarshal(b, &c) != nil || c.Token == "" || c.Seen < 0 {
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

type pubmedCursor struct {
	Offset int    `json:"offset"`
	Count  int    `json:"count"`
	WebEnv string `json:"web_env"`
	Key    string `json:"key"`
}

func (c pubmedCursor) encode() string {
	b, _ := json.Marshal(c)
	return "pm1:" + base64.RawURLEncoding.EncodeToString(b)
}
func decodePubmedCursor(value string) (pubmedCursor, error) {
	var c pubmedCursor
	if !strings.HasPrefix(value, "pm1:") {
		return c, malformed("invalid PubMed cursor")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, "pm1:"))
	if err != nil || json.Unmarshal(b, &c) != nil || c.Offset < 0 || c.Count < 1 || c.Count > 10000 || c.Offset >= c.Count || c.WebEnv == "" || c.Key == "" {
		return c, malformed("invalid PubMed cursor")
	}
	return c, nil
}

// PubMed 使用服务器 history 快照分页；创建 history 独占一页，便于调用层统一限速。
// 查询新建和修订日期，不使用发表日期来判断首次采集时间。
func (s *remoteSource) pubmed(ctx context.Context, req FetchRequest) (Page, error) {
	if req.Cursor == "" {
		span := req.Window.Start.UTC().Format("2006/01/02") + ":" + req.Window.End.UTC().Format("2006/01/02")
		term := "(" + req.Template.Query + ") AND (" + span + "[crdt] OR " + span + "[lr])"
		body, err := s.get(ctx, "/entrez/eutils/esearch.fcgi", map[string]string{"db": "pubmed", "term": term, "retmode": "json", "retmax": "0", "usehistory": "y"})
		if err != nil {
			return Page{}, err
		}
		var r struct {
			Result struct {
				Count     string          `json:"count"`
				WebEnv    string          `json:"webenv"`
				Key       string          `json:"querykey"`
				Error     string          `json:"ERROR"`
				ErrorList json.RawMessage `json:"errorlist"`
			} `json:"esearchresult"`
		}
		if json.Unmarshal(body, &r) != nil || r.Result.Error != "" || (len(r.Result.ErrorList) > 0 && string(r.Result.ErrorList) != "{}") {
			return Page{}, malformed("invalid PubMed ESearch envelope")
		}
		count, err := strconv.Atoi(r.Result.Count)
		if err != nil || count < 0 {
			return Page{}, malformed("invalid PubMed result count")
		}
		if count > 10000 {
			return Page{}, &SourceError{Kind: "window_too_large", Message: "PubMed window exceeds 10000 results; split query window before advancing checkpoint"}
		}
		if count == 0 {
			return Page{Complete: true}, nil
		}
		if r.Result.WebEnv == "" || r.Result.Key == "" {
			return Page{}, malformed("PubMed history missing")
		}
		return Page{NextCursor: (pubmedCursor{Count: count, WebEnv: r.Result.WebEnv, Key: r.Result.Key}).encode()}, nil
	}
	cursor, err := decodePubmedCursor(req.Cursor)
	if err != nil {
		return Page{}, err
	}
	body, err := s.get(ctx, "/entrez/eutils/efetch.fcgi", map[string]string{"db": "pubmed", "retmode": "xml", "rettype": "abstract", "retstart": strconv.Itoa(cursor.Offset), "retmax": strconv.Itoa(sourcePageSize), "WebEnv": cursor.WebEnv, "query_key": cursor.Key})
	if err != nil {
		return Page{}, err
	}
	var r struct {
		XMLName  xml.Name        `xml:"PubmedArticleSet"`
		Articles []pubmedArticle `xml:"PubmedArticle"`
		Error    string          `xml:"ERROR"`
	}
	if xml.Unmarshal(body, &r) != nil || r.Error != "" || len(r.Articles) == 0 {
		return Page{}, malformed("invalid or prematurely empty PubMed EFetch page")
	}
	if len(r.Articles) > cursor.Count-cursor.Offset {
		return Page{}, malformed("PubMed history count changed")
	}
	page := Page{}
	for _, item := range r.Articles {
		pmid := strings.TrimSpace(item.Citation.PMID.Value)
		if pmid == "" || item.Citation.Article.Title.Value == "" {
			return Page{}, malformed("invalid PubMed article identity")
		}
		a := item.Citation.Article
		preprint := false
		for _, publicationType := range a.Types {
			if strings.EqualFold(strings.TrimSpace(publicationType), "Preprint") {
				preprint = true
			}
		}
		record := Record{Source: s.name, SourceID: pmid, Title: xmlText(a.Title.Value), Journal: a.Journal.Title, URL: "https://pubmed.ncbi.nlm.nih.gov/" + pmid + "/", SourceUpdatedAt: item.Citation.Revised.exact(), Topics: []string{req.Template.Topic}, Raw: rawXML(item.Raw)}
		if a.Journal.ISSN != "" {
			record.ISSNs = []string{a.Journal.ISSN}
		}
		for _, text := range a.Abstract.Text {
			if record.Abstract != "" {
				record.Abstract += "\n"
			}
			record.Abstract += xmlText(text.Value)
		}
		for _, id := range item.Data.IDs {
			if strings.EqualFold(id.Type, "doi") {
				record.DOI = cleanDOI(id.Value)
			}
		}
		for _, date := range a.Dates {
			if strings.EqualFold(date.Type, "Electronic") {
				record.PublicationEvents = addEvent(record.PublicationEvents, "online", date.exact(), "PubMed ArticleDate Electronic")
			}
		}
		record.PublicationEvents = addEvent(record.PublicationEvents, "issue", a.Journal.Issue.Date.exact(), "PubMed JournalIssue PubDate")
		for _, date := range item.Data.History {
			switch date.Status {
			case "epublish":
				record.PublicationEvents = addEvent(record.PublicationEvents, "online", date.exact(), "PubMed history epublish")
			case "ppublish":
				record.PublicationEvents = addEvent(record.PublicationEvents, "issue", date.exact(), "PubMed history ppublish")
			}
			// 索引历史只能补充源更新元数据，永远不能生成正式发表事件。
			if (date.Status == "entrez" || date.Status == "pubmed" || date.Status == "medline") && date.exact().After(record.SourceUpdatedAt) {
				record.SourceUpdatedAt = date.exact()
			}
		}
		if !record.SourceUpdatedAt.IsZero() && !record.SourceUpdatedAt.Before(req.Window.End) {
			continue
		}
		if preprint {
			for i := range record.PublicationEvents {
				record.PublicationEvents[i].Kind = "preprint"
				record.PublicationEvents[i].Evidence += " (PubMed Preprint publication type)"
			}
		}
		page.Records = append(page.Records, record)
	}
	cursor.Offset += len(r.Articles)
	page.Complete = cursor.Offset >= cursor.Count
	if !page.Complete {
		page.NextCursor = cursor.encode()
	}
	return page, nil
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
		Revised pubmedDate `xml:"DateRevised"`
		Article struct {
			Title    innerText `xml:"ArticleTitle"`
			Types    []string  `xml:"PublicationTypeList>PublicationType"`
			Abstract struct {
				Text []innerText `xml:"AbstractText"`
			} `xml:"Abstract"`
			Dates   []pubmedDate `xml:"ArticleDate"`
			Journal struct {
				Title string `xml:"Title"`
				ISSN  string `xml:"ISSN"`
				Issue struct {
					Date pubmedDate `xml:"PubDate"`
				} `xml:"JournalIssue"`
			} `xml:"Journal"`
		} `xml:"Article"`
	} `xml:"MedlineCitation"`
	Data struct {
		IDs []struct {
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

// arXiv 仅提供 submittedDate 查询过滤；按 updated 倒序扫描并在本地应用固定窗口，避免漏掉旧预印本新版本。
// 搜索 API 不提供稳定快照；跨页变动由持久化去重和下一次 overlap 补偿。禁止把 submitted 时间当期刊发表时间。
func (s *remoteSource) arxiv(ctx context.Context, req FetchRequest) (Page, error) {
	offset, err := parseOffset(req.Cursor)
	if err != nil {
		return Page{}, err
	}
	if offset >= 30000 {
		return Page{}, &SourceError{Kind: "window_too_large", Message: "arXiv search pagination exceeds 30000; refine template or use OAI-PMH"}
	}
	query := req.Template.Query
	groups := req.Template.TermGroups
	if len(groups) == 0 && len(req.Template.Terms) > 0 {
		groups = [][]string{req.Template.Terms}
	}
	if len(groups) > 0 {
		clauses := make([]string, 0, len(groups))
		for _, group := range groups {
			if len(group) == 0 {
				return Page{}, malformed("search template contains empty term group")
			}
			terms := make([]string, 0, len(group))
			for _, term := range group {
				terms = append(terms, "all:"+strconv.Quote(term))
			}
			clauses = append(clauses, "("+strings.Join(terms, " OR ")+")")
		}
		query = strings.Join(clauses, " AND ")
	}
	body, err := s.get(ctx, "/api/query", map[string]string{"search_query": query, "start": strconv.Itoa(offset), "max_results": strconv.Itoa(sourcePageSize), "sortBy": "lastUpdatedDate", "sortOrder": "descending"})
	if err != nil {
		return Page{}, err
	}
	var r struct {
		XMLName xml.Name     `xml:"feed"`
		Total   *int         `xml:"totalResults"`
		Start   *int         `xml:"startIndex"`
		Entries []arxivEntry `xml:"entry"`
	}
	if xml.Unmarshal(body, &r) != nil || r.Total == nil || *r.Total < 0 || r.Start == nil || *r.Start != offset {
		return Page{}, malformed("invalid arXiv Atom pagination")
	}
	if len(r.Entries) == 0 && offset < *r.Total {
		return Page{}, malformed("prematurely empty arXiv page")
	}
	if len(r.Entries) > *r.Total-offset {
		return Page{}, malformed("arXiv page exceeds declared total")
	}
	page := Page{}
	pastStart := false
	var previous time.Time
	for _, item := range r.Entries {
		if strings.Contains(item.ID, "/api/errors") {
			return Page{}, malformed("arXiv returned Atom error")
		}
		updated, err := time.Parse(time.RFC3339, item.Updated)
		if err != nil {
			return Page{}, malformed("invalid arXiv updated timestamp")
		}
		if !previous.IsZero() && updated.After(previous) {
			return Page{}, malformed("arXiv page is not updated-descending")
		}
		previous = updated
		if updated.Before(req.Window.Start) {
			pastStart = true
			continue
		}
		if !updated.Before(req.Window.End) {
			continue
		}
		id := strings.TrimPrefix(strings.TrimPrefix(item.ID, "https://arxiv.org/abs/"), "http://arxiv.org/abs/")
		if id == item.ID || id == "" || strings.TrimSpace(item.Title) == "" {
			return Page{}, malformed("invalid arXiv entry identity")
		}
		version := ""
		if index := strings.LastIndex(id, "v"); index > 0 {
			if _, err := strconv.Atoi(id[index+1:]); err == nil {
				version = id[index+1:]
				id = id[:index]
			}
		}
		record := Record{Source: s.name, SourceID: id, Version: version, DOI: cleanDOI(item.DOI), Title: strings.Join(strings.Fields(item.Title), " "), Abstract: strings.TrimSpace(item.Summary), URL: item.ID, Journal: item.Journal, SourceUpdatedAt: updated.UTC(), Topics: []string{req.Template.Topic}, Raw: rawXML(item.Raw)}
		if published, err := time.Parse(time.RFC3339, item.Published); err == nil {
			record.PublicationEvents = addEvent(record.PublicationEvents, "preprint", published, "arXiv published (initial preprint, not journal publication)")
		}
		page.Records = append(page.Records, record)
	}
	next := offset + len(r.Entries)
	page.Complete = pastStart || next >= *r.Total
	if !page.Complete {
		page.NextCursor = strconv.Itoa(next)
	}
	return page, nil
}

type arxivEntry struct {
	Raw       string `xml:",innerxml"`
	ID        string `xml:"id"`
	Updated   string `xml:"updated"`
	Published string `xml:"published"`
	Title     string `xml:"title"`
	Summary   string `xml:"summary"`
	DOI       string `xml:"doi"`
	Journal   string `xml:"journal_ref"`
}

func parseOffset(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(cursor)
	if err != nil || offset < 0 {
		return 0, malformed("invalid numeric source cursor")
	}
	return offset, nil
}

// bioRxiv 日期窗口返回新帖和版本；published 字段仅为关联 DOI，不含可证明正式发表的日期。
// 官方接口：https://api.biorxiv.org/
func (s *remoteSource) biorxiv(ctx context.Context, req FetchRequest) (Page, error) {
	offset, err := parseOffset(req.Cursor)
	if err != nil {
		return Page{}, err
	}
	body, err := s.get(ctx, "/details/biorxiv/"+dateOnly(req.Window.Start)+"/"+dateOnly(req.Window.End)+"/"+strconv.Itoa(offset)+"/json", nil)
	if err != nil {
		return Page{}, err
	}
	var r struct {
		Messages []struct {
			Status string      `json:"status"`
			Cursor json.Number `json:"cursor"`
			Total  json.Number `json:"total"`
		} `json:"messages"`
		Collection []json.RawMessage `json:"collection"`
	}
	if json.Unmarshal(body, &r) != nil || len(r.Messages) == 0 || r.Messages[0].Status != "ok" || r.Collection == nil {
		return Page{}, malformed("invalid bioRxiv envelope")
	}
	total, err := strconv.Atoi(string(r.Messages[0].Total))
	if err != nil || total < 0 {
		return Page{}, malformed("invalid bioRxiv result count")
	}
	responseOffset, err := strconv.Atoi(string(r.Messages[0].Cursor))
	if err != nil || responseOffset != offset {
		return Page{}, malformed("invalid bioRxiv result cursor")
	}
	if offset+len(r.Collection) > total {
		return Page{}, malformed("bioRxiv page exceeds declared total")
	}
	if len(r.Collection) == 0 && offset < total {
		return Page{}, malformed("prematurely empty bioRxiv page")
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
		}
		if json.Unmarshal(raw, &item) != nil || item.DOI == "" || item.Title == "" {
			return Page{}, malformed("invalid bioRxiv record identity")
		}
		updated, err := time.Parse("2006-01-02", item.Date)
		if err != nil {
			return Page{}, malformed("invalid bioRxiv posted date")
		}
		if updated.Before(req.Window.Start) || !updated.Before(req.Window.End) {
			continue
		}
		// bioRxiv 无全文关键词参数。统一模板只作宽松发现，最终纳入仍由独立审核处理。
		if !matchesTemplateGroups(item.Title+" "+item.Abstract, req.Template) {
			continue
		}
		record := Record{Source: s.name, SourceID: cleanDOI(item.DOI), DOI: cleanDOI(item.DOI), Title: item.Title, Abstract: item.Abstract, URL: "https://doi.org/" + cleanDOI(item.DOI), Version: item.Version, SourceUpdatedAt: updated, Topics: []string{req.Template.Topic}, Raw: append(json.RawMessage(nil), raw...)}
		record.PublicationEvents = addEvent(record.PublicationEvents, "preprint", updated, "bioRxiv posted date for source version "+item.Version)
		page.Records = append(page.Records, record)
	}
	next := offset + len(r.Collection)
	page.Complete = next >= total
	if !page.Complete {
		page.NextCursor = strconv.Itoa(next)
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

// Crossref bibliographic 查询不是 Boolean 引擎，只发送宽松词袋并保留全部候选。
// 不对常缺失 abstract 的 Crossref 记录作本地 AND 过滤，以免将发现阶段误当科学纳入判断。
func crossrefDiscoveryQuery(template SearchTemplate) string {
	terms := template.Terms
	if len(template.TermGroups) > 0 {
		terms = nil
		for _, group := range template.TermGroups {
			terms = append(terms, group...)
		}
	}
	if len(terms) == 0 {
		return template.Query
	}
	return strings.Join(terms, " ")
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
