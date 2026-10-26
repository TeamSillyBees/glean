package monitor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

type europePMCCursor struct {
	Mark     string `json:"mark"`
	Seen     int    `json:"seen"`
	Total    int    `json:"total"`
	Restarts int    `json:"restarts,omitempty"`
}

func (c europePMCCursor) encode() string {
	b, _ := json.Marshal(c)
	return "ep1:" + base64.RawURLEncoding.EncodeToString(b)
}
func decodeEuropePMCCursor(value string) (europePMCCursor, error) {
	var c europePMCCursor
	if !strings.HasPrefix(value, "ep1:") {
		return c, malformed("invalid Europe PMC cursor")
	}
	b, e := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, "ep1:"))
	if e != nil || json.Unmarshal(b, &c) != nil || c.Mark == "" || c.Seen < 0 || c.Restarts < 0 || c.Restarts > 3 || (c.Total < 0 && !(c.Total == -1 && c.Mark == "*" && c.Seen == 0)) || (c.Total >= 0 && c.Seen >= c.Total) {
		return c, malformed("invalid Europe PMC cursor")
	}
	return c, nil
}

// europepmc 同时查询首次索引与更新日期，避免用发表日期漏掉回填记录。
// firstPublicationDate 官方注明会填补缺失月/日，故不能用作精确正式出版证据。
func (s *remoteSource) europepmc(ctx context.Context, req FetchRequest) (Page, error) {
	c := europePMCCursor{Mark: "*", Total: -1}
	if req.Cursor != "" {
		var e error
		c, e = decodeEuropePMCCursor(req.Cursor)
		if e != nil {
			return Page{}, e
		}
	}
	query := req.Template.Query
	if req.Template.Core {
		issns, e := registryISSNs(req)
		if e != nil {
			return Page{}, e
		}
		terms := make([]string, 0, len(issns))
		for _, v := range issns {
			terms = append(terms, "ISSN:"+strconv.Quote(v))
		}
		query = "(" + query + ") AND (" + strings.Join(terms, " OR ") + ")"
	}
	span := "[" + dateOnly(req.Window.Start) + " TO " + dateOnly(req.Window.End.Add(-time.Nanosecond)) + "]"
	query = "(" + query + ") AND (FIRST_IDATE:" + span + " OR UPDATE_DATE:" + span + ")"
	body, e := s.get(ctx, "/europepmc/webservices/rest/search", map[string]string{"query": query, "format": "json", "resultType": "core", "pageSize": strconv.Itoa(requestPageSize(req)), "cursorMark": c.Mark})
	if e != nil {
		return Page{}, e
	}
	var r struct {
		HitCount   *int   `json:"hitCount"`
		Next       string `json:"nextCursorMark"`
		ResultList struct {
			Results []json.RawMessage `json:"result"`
		} `json:"resultList"`
	}
	if json.Unmarshal(body, &r) != nil || r.HitCount == nil || *r.HitCount < 0 || r.ResultList.Results == nil {
		return Page{}, malformed("invalid Europe PMC search envelope")
	}
	if c.Total >= 0 && *r.HitCount != c.Total {
		if c.Restarts >= 3 {
			return Page{}, malformed("Europe PMC index changed during three restarts; incomplete fixed window retained")
		}
		return Page{NextCursor: (europePMCCursor{Mark: "*", Total: -1, Restarts: c.Restarts + 1}).encode()}, nil
	}
	seen := c.Seen + len(r.ResultList.Results)
	if seen > *r.HitCount || (len(r.ResultList.Results) == 0 && seen < *r.HitCount) {
		return Page{}, malformed("inconsistent Europe PMC page count")
	}
	page := Page{Complete: seen == *r.HitCount}
	if !page.Complete {
		if r.Next == "" || r.Next == c.Mark {
			return Page{}, malformed("Europe PMC continuation missing or stalled")
		}
		page.NextCursor = (europePMCCursor{Mark: r.Next, Seen: seen, Total: *r.HitCount, Restarts: c.Restarts}).encode()
	}
	for _, raw := range r.ResultList.Results {
		var a struct {
			ID           string      `json:"id"`
			Source       string      `json:"source"`
			DOI          string      `json:"doi"`
			Title        string      `json:"title"`
			Abstract     string      `json:"abstractText"`
			AuthorString string      `json:"authorString"`
			FirstIndex   string      `json:"firstIndexDate"`
			Revised      string      `json:"dateOfRevision"`
			Electronic   string      `json:"electronicPublicationDate"`
			Version      json.Number `json:"versionNumber"`
			PubTypeList  struct {
				Types []string `json:"pubType"`
			} `json:"pubTypeList"`
			JournalInfo struct {
				PrintDate   string `json:"printPublicationDate"`
				DisplayDate string `json:"dateOfPublication"`
				Volume      string `json:"volume"`
				Issue       string `json:"issue"`
				Journal     struct {
					Title string `json:"title"`
					ISSN  string `json:"ISSN"`
					ESSN  string `json:"essn"`
				} `json:"journal"`
			} `json:"journalInfo"`
		}
		if json.Unmarshal(raw, &a) != nil || a.ID == "" || a.Source == "" || strings.TrimSpace(a.Title) == "" {
			return Page{}, malformed("invalid Europe PMC article identity")
		}
		updated, _ := time.Parse("2006-01-02", a.FirstIndex)
		revised, _ := time.Parse("2006-01-02", a.Revised)
		if revised.After(updated) {
			updated = revised
		}
		// dateOfRevision 属于引用源的修订日，不是 Europe PMC UPDATE_DATE；
		// 服务器可能在 FIRST_IDATE 当天已返回较晚的修订日期，不能据它丢掉真实索引命中。
		record := Record{Source: s.name, SourceID: a.Source + ":" + a.ID, DOI: cleanDOI(a.DOI), Title: xmlText(a.Title), Abstract: xmlText(a.Abstract), Journal: a.JournalInfo.Journal.Title, Version: string(a.Version), SourceUpdatedAt: updated, Topics: []string{req.Template.Topic}, Raw: append(json.RawMessage(nil), raw...), PublicationType: strings.Join(a.PubTypeList.Types, "; "), URL: "https://europepmc.org/article/" + a.Source + "/" + a.ID}
		if a.Source == "MED" {
			record.PMID = a.ID
		}
		if a.AuthorString != "" {
			record.Authors = []string{a.AuthorString}
		}
		for _, v := range []string{a.JournalInfo.Journal.ISSN, a.JournalInfo.Journal.ESSN} {
			if v != "" {
				record.ISSNs = append(record.ISSNs, v)
			}
		}
		preprint := a.Source == "PPR"
		for _, typ := range a.PubTypeList.Types {
			if strings.Contains(strings.ToLower(typ), "preprint") {
				preprint = true
			}
		}
		online, _ := time.Parse("2006-01-02", a.Electronic)
		issue := preciseEuropePMCIssue(a.JournalInfo.DisplayDate, a.JournalInfo.PrintDate)
		if a.JournalInfo.Volume == "" && a.JournalInfo.Issue == "" {
			issue = time.Time{}
		}
		if preprint {
			record.PublicationEvents = addEvent(record.PublicationEvents, "preprint", online, "Europe PMC electronicPublicationDate (preprint)")
		} else {
			record.PublicationEvents = addEvent(record.PublicationEvents, "online", online, "Europe PMC electronicPublicationDate")
			record.PublicationEvents = addEvent(record.PublicationEvents, "issue", issue, "Europe PMC day-precision dateOfPublication agrees with printPublicationDate and volume/issue")
		}
		record.SourceURL = sourceRecordURL(s.name, record.SourceID)
		page.Records = append(page.Records, record)
	}
	return page, nil
}

// preciseEuropePMCIssue 拒绝 month/year-only 经归一化补成 1 日的 printPublicationDate。
func preciseEuropePMCIssue(display, normalized string) time.Time {
	normalizedDate, e := time.Parse("2006-01-02", normalized)
	if e != nil {
		return time.Time{}
	}
	for _, layout := range []string{"2006 Jan 2", "2006 Jan 02", "2006 January 2", "2006-01-02", "2006 01 02"} {
		if d, e := time.Parse(layout, strings.TrimSpace(display)); e == nil && d.Equal(normalizedDate) {
			return d
		}
	}
	return time.Time{}
}
