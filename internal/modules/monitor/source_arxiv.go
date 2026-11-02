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

type arxivCursor struct {
	Day   string `json:"day"`
	Token string `json:"token,omitempty"`
}

func (c arxivCursor) encode() string {
	b, _ := json.Marshal(c)
	return "oa1:" + base64.RawURLEncoding.EncodeToString(b)
}
func decodeArxivCursor(value string) (arxivCursor, error) {
	var c arxivCursor
	if !strings.HasPrefix(value, "oa1:") {
		return c, malformed("invalid arXiv OAI cursor")
	}
	b, e := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, "oa1:"))
	if e != nil || json.Unmarshal(b, &c) != nil {
		return c, malformed("invalid arXiv OAI cursor")
	}
	if _, e = time.Parse("2006-01-02", c.Day); e != nil {
		return c, malformed("invalid arXiv cursor day")
	}
	return c, nil
}

type arxivOAIRecord struct {
	Raw    string `xml:",innerxml"`
	Header struct {
		Identifier string `xml:"identifier"`
		Datestamp  string `xml:"datestamp"`
		Status     string `xml:"status,attr"`
	} `xml:"header"`
	Metadata struct {
		Raw struct {
			ID       string `xml:"id"`
			Title    string `xml:"title"`
			Authors  string `xml:"authors"`
			Abstract string `xml:"abstract"`
			DOI      string `xml:"doi"`
			Journal  string `xml:"journal-ref"`
			Versions []struct {
				Number string `xml:"version,attr"`
				Date   string `xml:"date"`
			} `xml:"version"`
		} `xml:"arXivRaw"`
	} `xml:"metadata"`
}

// arxiv 使用 OAI 元数据修改日而非 submittedDate，完整取得旧论文的新版本及全部版本时间。
// 每日分区使 daily-expiring resumptionToken 可安全重放当前日，已完成的日不会重新全扫。
// https://info.arxiv.org/help/oa/index.html （2025 年新端点、日过期游标、arXivRaw 版本历史）
func (s *remoteSource) arxiv(ctx context.Context, req FetchRequest) (Page, error) {
	c := arxivCursor{Day: dateOnly(req.Window.Start)}
	if req.Cursor != "" {
		var e error
		c, e = decodeArxivCursor(req.Cursor)
		if e != nil {
			return Page{}, e
		}
	}
	day, _ := time.Parse("2006-01-02", c.Day)
	if day.Before(dayStart(req.Window.Start)) || !day.Before(req.Window.End) {
		return Page{}, malformed("arXiv cursor outside fixed window")
	}
	q := map[string]string{"verb": "ListRecords"}
	if c.Token != "" {
		q["resumptionToken"] = c.Token
	} else {
		q["metadataPrefix"] = "arXivRaw"
		q["from"] = c.Day
		q["until"] = c.Day
	}
	body, e := s.get(ctx, "/oai", q)
	if e != nil {
		return Page{}, e
	}
	var r struct {
		XMLName xml.Name `xml:"OAI-PMH"`
		Error   struct {
			Code    string `xml:"code,attr"`
			Message string `xml:",chardata"`
		} `xml:"error"`
		List *struct {
			Records []arxivOAIRecord `xml:"record"`
			Token   string           `xml:"resumptionToken"`
		} `xml:"ListRecords"`
	}
	if xml.Unmarshal(body, &r) != nil {
		return Page{}, malformed("invalid arXiv OAI envelope")
	}
	if r.Error.Code == "badResumptionToken" && c.Token != "" {
		c.Token = ""
		return Page{NextCursor: c.encode()}, nil
	}
	// 初始日查询可为空；未完成续页的空集错误不足以证明该日完整，保留持久断点等待诊断。
	if r.Error.Code == "noRecordsMatch" && c.Token != "" {
		return Page{}, malformed("arXiv OAI noRecordsMatch during continuation")
	}
	if r.Error.Code != "" && r.Error.Code != "noRecordsMatch" {
		return Page{}, malformed("arXiv OAI error: " + r.Error.Code)
	}
	if r.List == nil && r.Error.Code != "noRecordsMatch" {
		return Page{}, malformed("missing arXiv OAI ListRecords")
	}
	page := Page{}
	if r.List != nil {
		if len(r.List.Records) == 0 {
			return Page{}, malformed("empty arXiv OAI list without noRecordsMatch")
		}
	records:
		for _, item := range r.List.Records {
			// 删除的 OAI 头不包含论文元数据；不据此发明撤稿结论，保留源记录现状。
			if item.Header.Status == "deleted" {
				continue
			}
			updated, e := time.Parse("2006-01-02", item.Header.Datestamp)
			if e != nil {
				updated, e = time.Parse(time.RFC3339, item.Header.Datestamp)
			}
			if e != nil {
				page.Records = append(page.Records, rejectedArxivRecord(item))
				continue records
			}
			if updated.Before(dayStart(req.Window.Start)) || !updated.Before(req.Window.End) {
				continue
			}
			a := item.Metadata.Raw
			if a.ID == "" || strings.TrimSpace(a.Title) == "" || len(a.Versions) == 0 || item.Header.Identifier != "oai:arXiv.org:"+a.ID {
				page.Records = append(page.Records, rejectedArxivRecord(item))
				continue records
			}
			record := Record{Source: s.name, SourceID: a.ID, ArxivID: a.ID, Title: strings.Join(strings.Fields(a.Title), " "), Abstract: strings.TrimSpace(a.Abstract), DOI: cleanDOI(a.DOI), Journal: a.Journal, SourceUpdatedAt: updated, Topics: []string{req.Template.Topic}, Raw: rawXML(item.Raw), PublicationType: "preprint"}
			// 已取回记录全部保留；主题仅为发现候选，相关性由后续阶段判断。
			maxVersion := 0
			for _, v := range a.Versions {
				n, e := strconv.Atoi(strings.TrimPrefix(v.Number, "v"))
				if e != nil || n < 1 {
					page.Records = append(page.Records, rejectedArxivRecord(item))
					continue records
				}
				date, e := time.Parse(time.RFC1123, v.Date)
				if e != nil {
					date, e = time.Parse(time.RFC1123Z, v.Date)
				}
				if e != nil {
					page.Records = append(page.Records, rejectedArxivRecord(item))
					continue records
				}
				if !date.Before(req.Window.End) {
					continue
				}
				if n > maxVersion {
					maxVersion = n
					record.Version = strconv.Itoa(n)
				}
				record.PublicationEvents = addEvent(record.PublicationEvents, "preprint", date.UTC(), "arXivRaw "+v.Number+" submission/replacement (not journal publication)")
			}
			// OAI 元数据是当前最新文本，不能将未来版本的文本假称为历史版本。
			for _, v := range a.Versions {
				n, _ := strconv.Atoi(strings.TrimPrefix(v.Number, "v"))
				if n > maxVersion {
					maxVersion = 0
					break
				}
			}
			if maxVersion == 0 {
				continue
			}
			record.URL = "https://arxiv.org/abs/" + a.ID + "v" + record.Version
			if strings.TrimSpace(a.Authors) != "" {
				record.Authors = []string{strings.Join(strings.Fields(a.Authors), " ")}
			}
			record.SourceURL = sourceRecordURL(s.name, record.SourceID)
			page.Records = append(page.Records, record)
		}
		c.Token = strings.TrimSpace(r.List.Token)
		if c.Token != "" {
			if c.Token == q["resumptionToken"] {
				return Page{}, malformed("arXiv OAI cursor did not advance")
			}
			page.NextCursor = c.encode()
			return page, nil
		}
	}
	next := day.AddDate(0, 0, 1)
	if !next.Before(req.Window.End) {
		page.Complete = true
	} else {
		page.NextCursor = (arxivCursor{Day: dateOnly(next)}).encode()
	}
	return page, nil
}

// rejectedArxivRecord 保留坏记录的原始 XML；冲突或缺失的身份仅使用隔离哈希，不猜测论文归属。
func rejectedArxivRecord(item arxivOAIRecord) Record {
	r := Record{Source: "arxiv", Raw: rawXML(item.Raw), ValidationError: "invalid_source_record"}
	id := strings.TrimSpace(item.Metadata.Raw.ID)
	if id != "" && item.Header.Identifier == "oai:arXiv.org:"+id {
		r.SourceID = id
		r.SourceURL = sourceRecordURL("arxiv", id)
	} else {
		r.SourceID = fmt.Sprintf("unparsed-sha256:%x", sha256.Sum256([]byte(item.Raw)))
	}
	return r
}
