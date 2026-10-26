package monitor

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// pubmedPartition 保存尚未完成的闭合日区间；UID 上界 0 表示无上界，不能假定未来 PMID 最大值。
type pubmedPartition struct {
	From  string `json:"from"`
	Until string `json:"until"`
	Low   int64  `json:"low,omitempty"`
	High  int64  `json:"high,omitempty"`
}

type pubmedCursor struct {
	CoreFeed      int               `json:"core_feed,omitempty"`
	CoreBatchSize int               `json:"core_batch_size,omitempty"`
	CoreRegistry  string            `json:"core_registry,omitempty"`
	CoreFull      bool              `json:"core_full,omitempty"`
	Offset        int               `json:"offset"`
	Count         int               `json:"count"`
	WebEnv        string            `json:"web_env"`
	Key           string            `json:"key"`
	Pending       []pubmedPartition `json:"pending,omitempty"`
	Restarts      int               `json:"restarts,omitempty"`
}

func (c pubmedCursor) encode() string {
	b, _ := json.Marshal(c)
	return "pm2:" + base64.RawURLEncoding.EncodeToString(b)
}
func decodePubmedCursor(value string) (pubmedCursor, error) {
	var c pubmedCursor
	if !strings.HasPrefix(value, "pm2:") {
		return c, malformed("invalid PubMed cursor")
	}
	b, e := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, "pm2:"))
	if e != nil || json.Unmarshal(b, &c) != nil || c.CoreFeed < 0 || c.CoreBatchSize < 0 || c.Offset < 0 || c.Count < 0 || c.Count > 9999 || c.Offset > c.Count || (c.Count > 0 && (c.Offset == c.Count || c.WebEnv == "" || c.Key == "")) || c.Restarts < 0 || c.Restarts > 3 || len(c.Pending) > 128 {
		return c, malformed("invalid PubMed cursor")
	}
	for _, p := range c.Pending {
		f, e := time.Parse("2006-01-02", p.From)
		u, e2 := time.Parse("2006-01-02", p.Until)
		if e != nil || e2 != nil || f.After(u) || p.Low < 0 || p.High < 0 || (p.High > 0 && p.High < p.Low) {
			return c, malformed("invalid PubMed partition")
		}
	}
	return c, nil
}

func pubmedNext(c pubmedCursor, req FetchRequest, feedCount int) Page {
	if len(c.Pending) == 0 {
		if !req.Template.Core || c.CoreFeed+1 >= feedCount {
			return Page{Complete: true}
		}
		c.CoreFeed++
		c.Pending = []pubmedPartition{{From: dateOnly(req.Window.Start), Until: dateOnly(req.Window.End.Add(-time.Nanosecond))}}
		c.Offset, c.Count, c.Restarts = 0, 0, 0
		c.Key, c.WebEnv = "", ""
	}
	return Page{NextCursor: c.encode()}
}

// pubmed 逐页查询 CRDT/LR，超 9999 条时先二分日期，再按 UID 分区；每个 Fetch 至多一次 HTTP。
// UID 范围由 NCBI 官方确认支持：https://ncbiinsights.ncbi.nlm.nih.gov/2022/11/22/updated-pubmed-eutilities-live/
func (s *remoteSource) pubmed(ctx context.Context, req FetchRequest) (Page, error) {
	feeds, registry, err := coreJournalFeeds(req)
	if err != nil {
		return Page{}, err
	}
	c := pubmedCursor{CoreFull: req.Template.Core, Pending: []pubmedPartition{{From: dateOnly(req.Window.Start), Until: dateOnly(req.Window.End.Add(-time.Nanosecond))}}}
	if req.Cursor != "" {
		var e error
		c, e = decodePubmedCursor(req.Cursor)
		if e != nil {
			return Page{}, e
		}
		if len(c.Pending) == 0 {
			return Page{}, malformed("PubMed cursor has no unfinished partition")
		}
	}
	// 旧核心 history 未分批或登记快照改变时，从原固定窗口重新开始，不能跳过任何 ISSN feed。
	if req.Template.Core && (!c.CoreFull || c.CoreBatchSize != coreISSNBatchSize || c.CoreRegistry != registry) {
		c = pubmedCursor{CoreFull: true, CoreBatchSize: coreISSNBatchSize, CoreRegistry: registry, Pending: []pubmedPartition{{From: dateOnly(req.Window.Start), Until: dateOnly(req.Window.End.Add(-time.Nanosecond))}}}
	}
	if req.Template.Core && c.CoreFeed >= len(feeds) {
		return Page{}, malformed("invalid PubMed core feed cursor")
	}
	if c.Count == 0 {
		p := c.Pending[0]
		span := strings.ReplaceAll(p.From, "-", "/") + ":" + strings.ReplaceAll(p.Until, "-", "/")
		query := req.Template.Query
		if req.Template.Core {
			parts := make([]string, 0, len(feeds[c.CoreFeed]))
			for _, v := range feeds[c.CoreFeed] {
				parts = append(parts, strconv.Quote(v)+"[issn]")
			}
			query = strings.Join(parts, " OR ")
		}
		term := "(" + query + ") AND (" + span + "[crdt] OR " + span + "[lr])"
		if p.High > 0 {
			term += " AND " + strconv.FormatInt(p.Low, 10) + ":" + strconv.FormatInt(p.High, 10) + "[uid]"
		} else if p.Low > 1 {
			term += " NOT 1:" + strconv.FormatInt(p.Low-1, 10) + "[uid]"
		}
		body, e := s.get(ctx, "/entrez/eutils/esearch.fcgi", map[string]string{"db": "pubmed", "term": term, "retmode": "json", "retmax": "1", "usehistory": "y", "tool": "glean"})
		if e != nil {
			return Page{}, e
		}
		var r struct {
			Result struct {
				Count     string          `json:"count"`
				WebEnv    string          `json:"webenv"`
				Key       string          `json:"querykey"`
				IDs       []string        `json:"idlist"`
				Error     string          `json:"ERROR"`
				ErrorList json.RawMessage `json:"errorlist"`
			} `json:"esearchresult"`
		}
		if json.Unmarshal(body, &r) != nil || r.Result.Error != "" || (len(r.Result.ErrorList) > 0 && string(r.Result.ErrorList) != "{}") {
			return Page{}, malformed("invalid PubMed ESearch envelope")
		}
		count, e := strconv.Atoi(r.Result.Count)
		if e != nil || count < 0 {
			return Page{}, malformed("invalid PubMed result count")
		}
		if count > 9999 {
			left, right := p, p
			from, _ := time.Parse("2006-01-02", p.From)
			until, _ := time.Parse("2006-01-02", p.Until)
			if from.Before(until) {
				days := int(until.Sub(from) / (24 * time.Hour))
				mid := from.AddDate(0, 0, days/2)
				left.Until = dateOnly(mid)
				right.From = dateOnly(mid.AddDate(0, 0, 1))
			} else {
				low := p.Low
				if low < 1 {
					low = 1
				}
				var pivot int64
				if p.High > 0 {
					pivot = low + (p.High-low)/2
				} else {
					if len(r.Result.IDs) != 1 {
						return Page{}, malformed("oversized PubMed day has no UID partition evidence")
					}
					id, e := strconv.ParseInt(r.Result.IDs[0], 10, 64)
					if e != nil || id < low {
						return Page{}, malformed("invalid PubMed partition UID")
					}
					pivot = low + (id-low)/2
				}
				if pivot < low || (p.High > 0 && pivot >= p.High) {
					return Page{}, malformed("PubMed UID partition cannot make progress")
				}
				left.Low = low
				left.High = pivot
				right.Low = pivot + 1
			}
			c.Pending = append([]pubmedPartition{left, right}, c.Pending[1:]...)
			return pubmedNext(c, req, len(feeds)), nil
		}
		if count == 0 {
			c.Pending = c.Pending[1:]
			return pubmedNext(c, req, len(feeds)), nil
		}
		if r.Result.WebEnv == "" || r.Result.Key == "" {
			return Page{}, malformed("PubMed history missing")
		}
		c.Count = count
		c.WebEnv = r.Result.WebEnv
		c.Key = r.Result.Key
		return pubmedNext(c, req, len(feeds)), nil
	}
	body, e := s.get(ctx, "/entrez/eutils/efetch.fcgi", map[string]string{"db": "pubmed", "retmode": "xml", "rettype": "abstract", "retstart": strconv.Itoa(c.Offset), "retmax": strconv.Itoa(requestPageSize(req)), "WebEnv": c.WebEnv, "query_key": c.Key, "tool": "glean"})
	if e != nil {
		return Page{}, e
	}
	var r struct {
		XMLName xml.Name `xml:"PubmedArticleSet"`
		Items   []struct {
			XMLName xml.Name
			Raw     string `xml:",innerxml"`
		} `xml:",any"`
		Error string `xml:"ERROR"`
	}
	if xml.Unmarshal(body, &r) != nil {
		return Page{}, malformed("invalid PubMed EFetch XML")
	}
	if r.Error != "" {
		if c.Restarts >= 3 {
			return Page{}, malformed("PubMed history failed after three restarts; incomplete partition retained")
		}
		c.Count = 0
		c.Offset = 0
		c.Key = ""
		c.WebEnv = ""
		c.Restarts++
		return pubmedNext(c, req, len(feeds)), nil
	}
	if len(r.Items) == 0 {
		return Page{}, malformed("invalid or prematurely empty PubMed EFetch page")
	}
	if len(r.Items) > c.Count-c.Offset {
		return Page{}, malformed("PubMed history count changed")
	}
	// 页级结构仍失败关闭；书籍是 EFetch 的合法记录，必须计入原始 retstart 进度。
	for _, entry := range r.Items {
		if entry.XMLName.Local != "PubmedArticle" && entry.XMLName.Local != "PubmedBookArticle" {
			return Page{}, malformed("unexpected PubMed EFetch envelope element")
		}
	}
	page := Page{}
	for _, entry := range r.Items {
		if entry.XMLName.Local == "PubmedBookArticle" {
			page.Records = append(page.Records, rejectedPubmedRecord(entry.Raw, "PubmedBookArticle", "unsupported_record_type"))
			continue
		}
		var item pubmedArticle
		if xml.Unmarshal([]byte("<PubmedArticle>"+entry.Raw+"</PubmedArticle>"), &item) != nil {
			page.Records = append(page.Records, rejectedPubmedRecord(entry.Raw, "PubmedArticle", "invalid_source_record"))
			continue
		}
		pmid := strings.TrimSpace(item.Citation.PMID.Value)
		a := item.Citation.Article
		if pmid == "" || strings.TrimSpace(xmlText(a.Title.Value)) == "" {
			page.Records = append(page.Records, rejectedPubmedRecord(entry.Raw, "PubmedArticle", "invalid_source_record"))
			continue
		}
		record := Record{Source: s.name, SourceID: pmid, PMID: pmid, Title: xmlText(a.Title.Value), Journal: a.Journal.Title, URL: "https://pubmed.ncbi.nlm.nih.gov/" + pmid + "/", SourceUpdatedAt: item.Citation.Revised.exact(), Topics: []string{req.Template.Topic}, Raw: rawXML(item.Raw), PublicationType: strings.Join(a.Types, "; ")}
		preprint := false
		for _, typ := range a.Types {
			if strings.EqualFold(strings.TrimSpace(typ), "Preprint") {
				preprint = true
			}
		}
		if a.Journal.ISSN != "" {
			record.ISSNs = []string{a.Journal.ISSN}
		}
		if item.Citation.ISSNLinking != "" {
			record.ISSNs = append(record.ISSNs, item.Citation.ISSNLinking)
		}
		for _, author := range a.Authors {
			name := strings.TrimSpace(author.ForeName + " " + author.LastName)
			if name == "" {
				name = author.CollectiveName
			}
			if name != "" {
				record.Authors = append(record.Authors, name)
			}
		}
		for _, v := range a.Abstract.Text {
			if record.Abstract != "" {
				record.Abstract += "\n"
			}
			record.Abstract += xmlText(v.Value)
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
		// ahead-of-print 的 PubDate 常重复 Electronic 日期，不能凭此伪造卷期出版。
		if item.Data.Status != "aheadofprint" && (a.Journal.Issue.Volume != "" || a.Journal.Issue.Number != "") {
			record.PublicationEvents = addEvent(record.PublicationEvents, "issue", a.Journal.Issue.Date.exact(), "PubMed JournalIssue PubDate with volume/issue")
		}
		for _, date := range item.Data.History {
			switch date.Status {
			case "epublish":
				record.PublicationEvents = addEvent(record.PublicationEvents, "online", date.exact(), "PubMed history epublish")
			case "ppublish":
				record.PublicationEvents = addEvent(record.PublicationEvents, "issue", date.exact(), "PubMed history ppublish")
			}
			// entrez 是 CRDT；pubmed/medline 可能回填，均只作源时间、不作出版日期。
			if (date.Status == "entrez" || date.Status == "pubmed" || date.Status == "medline") && date.exact().After(record.SourceUpdatedAt) {
				record.SourceUpdatedAt = date.exact()
			}
		}
		// ESearch history 是命中 PMID 的快照，不是 EFetch 元数据的历史版本。
		// CRDT 在窗口内的命中后来可被修订；保留其最新修订证据，不能用 DateRevised 再丢弃权威命中。
		if preprint {
			for i := range record.PublicationEvents {
				record.PublicationEvents[i].Kind = "preprint"
				record.PublicationEvents[i].Evidence += " (PubMed Preprint publication type)"
			}
		}
		record.SourceURL = sourceRecordURL(s.name, record.SourceID)
		page.Records = append(page.Records, record)
	}
	c.Offset += len(r.Items)
	if c.Offset >= c.Count {
		c.Pending = c.Pending[1:]
		c.Restarts = 0
		c.Offset = 0
		c.Count = 0
		c.WebEnv = ""
		c.Key = ""
	}
	next := pubmedNext(c, req, len(feeds))
	page.Complete = next.Complete
	page.NextCursor = next.NextCursor
	return page, nil
}

// rejectedPubmedRecord 不把书籍猜成论文，保留原始 XML 和可读出的 PMID，仅供隔离审计。
func rejectedPubmedRecord(raw, element, reason string) Record {
	var identity struct {
		ArticlePMID string `xml:"MedlineCitation>PMID"`
		BookPMID    string `xml:"BookDocument>PMID"`
	}
	_ = xml.Unmarshal([]byte("<"+element+">"+raw+"</"+element+">"), &identity)
	pmid := strings.TrimSpace(identity.ArticlePMID)
	if element == "PubmedBookArticle" {
		pmid = strings.TrimSpace(identity.BookPMID)
	}
	r := Record{Source: "pubmed", SourceID: pmid, PMID: pmid, PublicationType: element, Raw: rawXML(raw), ValidationError: reason}
	if pmid == "" {
		r.SourceID = fmt.Sprintf("unparsed-sha256:%x", sha256.Sum256([]byte(raw)))
	} else {
		r.SourceURL = sourceRecordURL("pubmed", pmid)
	}
	return r
}
