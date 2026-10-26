package monitor

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/teamsillybees/glean/internal/platform/httpclient"
)

// 合成混合页刻意放置一条类型错误；不是新捕获的真实来源证据。
func TestJSONSourcesQuarantineOneItemAndKeepTwoGoodRecords(t *testing.T) {
	for _, source := range []string{"crossref", "europepmc", "biorxiv", "medrxiv"} {
		t.Run(source, func(t *testing.T) {
			good1, bad, good2, final := rejectionFixtureItems(source)
			f := &adapterExecutor{responses: []*httpclient.Response{
				jsonResponse(rejectionFixturePage(source, good1+","+bad+","+good2, false)),
				jsonResponse(rejectionFixturePage(source, final, true)),
			}}
			s := &remoteSource{name: source, executor: f}
			req := adapterWindow()
			page, err := s.Fetch(context.Background(), req)
			if err != nil || page.Complete || page.NextCursor == "" || len(page.Records) != 3 {
				t.Fatalf("individual rejection must not fail or truncate page: %+v %v", page, err)
			}
			for _, i := range []int{0, 2} {
				if page.Records[i].ValidationError != "" || page.Records[i].Title == "" {
					t.Fatalf("valid neighbor rejected: %+v", page.Records[i])
				}
			}
			r := page.Records[1]
			if r.ValidationError != "invalid_source_record" || r.Source != source || r.SourceID == "" || r.SourceURL == "" || !bytes.Equal(r.Raw, []byte(bad)) {
				t.Fatalf("rejection must retain raw bytes, stable identity and safe reason: %+v", r)
			}
			switch source {
			case "crossref":
				c, e := decodeCrossrefCursor(page.NextCursor)
				if e != nil || c.Seen != 3 {
					t.Fatalf("raw count lost: %+v %v", c, e)
				}
			case "europepmc":
				c, e := decodeEuropePMCCursor(page.NextCursor)
				if e != nil || c.Seen != 3 {
					t.Fatalf("raw count lost: %+v %v", c, e)
				}
			default:
				if page.NextCursor != "3" {
					t.Fatal("preprint cursor must count rejected items")
				}
			}
			req.Cursor = page.NextCursor
			page, err = s.Fetch(context.Background(), req)
			if err != nil || !page.Complete || page.NextCursor != "" || len(page.Records) != 1 || page.Records[0].ValidationError != "" {
				t.Fatalf("raw-count continuation must reach final valid record: %+v %v", page, err)
			}
		})
	}
}

func rejectionFixtureItems(source string) (string, string, string, string) {
	var good, bad string
	switch source {
	case "crossref":
		good = `{"DOI":"10.5555/synthetic-1","type":"journal-article","title":["Clinical language agents"],"indexed":{"date-time":"2026-10-23T10:00:00Z"},"published-print":{"date-parts":[[2026,10]]}}`
		bad = `{"DOI":"10.5555/synthetic-bad","title":42,"indexed":{"date-time":"2026-10-23T10:00:00Z"}}`
	case "europepmc":
		good = `{"id":"synthetic-1","source":"MED","title":"Clinical language agents","electronicPublicationDate":"2026-10"}`
		bad = `{"id":"synthetic-bad","source":"MED","title":"Clinical language agents","versionNumber":{}}`
	default:
		good = `{"doi":"10.5555/synthetic-1","title":"Clinical language agents","date":"2026-10-23","version":"1"}`
		bad = `{"doi":"10.5555/synthetic-bad","title":"Clinical language agents","authors":[],"date":"2026-10-23","version":"1"}`
	}
	return good, bad, strings.ReplaceAll(good, "synthetic-1", "synthetic-2"), strings.ReplaceAll(good, "synthetic-1", "synthetic-3")
}

func rejectionFixturePage(source, items string, final bool) string {
	switch source {
	case "crossref":
		next := `,"next-cursor":"synthetic-next"`
		if final {
			next = ""
		}
		return `{"status":"ok","message":{"total-results":4` + next + `,"items":[` + items + `]}}`
	case "europepmc":
		return `{"hitCount":4,"nextCursorMark":"synthetic-next","resultList":{"result":[` + items + `]}}`
	default:
		cursor, count := "0", "3"
		if final {
			cursor, count = "3", "1"
		}
		return `{"messages":[{"status":"ok","cursor":` + cursor + `,"count":` + count + `,"total":4}],"collection":[` + items + `]}`
	}
}

func TestJSONSourcesUnidentifiableItemHasStableQuarantineHash(t *testing.T) {
	for _, source := range []string{"crossref", "europepmc", "biorxiv", "medrxiv"} {
		for _, raw := range []string{`null`, `42`, `[]`, `{"title":"Synthetic missing identity"}`} {
			a, b := rejectedJSONRecord(source, []byte(raw)), rejectedJSONRecord(source, []byte(raw))
			if a.SourceID != b.SourceID || !strings.HasPrefix(a.SourceID, "unparsed-sha256:") || a.ValidationError != "invalid_source_record" || !bytes.Equal(a.Raw, []byte(raw)) || a.SourceURL != "" {
				t.Fatalf("bad fallback identity/evidence for %s: %+v", source, a)
			}
		}
	}
}

func TestJSONSourceInvalidEnvelopesNeverAdvance(t *testing.T) {
	cases := []struct{ source, body string }{
		{"crossref", `{"status":"ok","message":{"items":{},"total-results":3}}`},
		{"crossref", `{"status":"ok","message":{"items":[{},`},
		{"europepmc", `{"hitCount":3,"resultList":{"result":{}}}`},
		{"europepmc", `{"hitCount":"bad","resultList":{"result":[{}]}}`},
		{"biorxiv", `{"messages":[{"status":"ok","cursor":0,"count":3,"total":3}],"collection":{}}`},
		{"medrxiv", `{"messages":[{"status":"ok","cursor":0,"count":3,"total":2}],"collection":[{},{},{}]}`},
	}
	for _, tc := range cases {
		s := &remoteSource{name: tc.source, executor: &adapterExecutor{responses: []*httpclient.Response{jsonResponse(tc.body)}}}
		page, err := s.Fetch(context.Background(), adapterWindow())
		if err == nil || page.Complete || page.NextCursor != "" || len(page.Records) != 0 {
			t.Fatalf("corrupt envelope cannot become quarantine success: %s %+v %v", tc.source, page, err)
		}
	}
}

func TestPartialOrMissingPublicationDateKeepsPaperWithUnknownDate(t *testing.T) {
	for _, source := range []string{"crossref", "europepmc", "biorxiv", "medrxiv"} {
		good, _, _, _ := rejectionFixtureItems(source)
		if source == "biorxiv" || source == "medrxiv" {
			good = strings.ReplaceAll(good, `"date":"2026-10-23"`, `"date":"2026-10"`)
		}
		for _, dateCase := range []string{good, strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(good, `,"published-print":{"date-parts":[[2026,10]]}`, ""), `,"electronicPublicationDate":"2026-10"`, ""), `,"date":"2026-10"`, "")} {
			body := rejectionFixturePage(source, dateCase, false)
			// bio/med envelope count is validated independently of date precision.
			if source == "biorxiv" || source == "medrxiv" {
				body = strings.ReplaceAll(body, `"count":3`, `"count":1`)
			}
			page, err := (&remoteSource{name: source, executor: &adapterExecutor{responses: []*httpclient.Response{jsonResponse(body)}}}).Fetch(context.Background(), adapterWindow())
			if err != nil || len(page.Records) != 1 || page.Records[0].ValidationError != "" || len(page.Records[0].PublicationEvents) != 0 {
				t.Fatalf("%s partial/missing date is unknown, not a broken paper: %+v %v", source, page, err)
			}
		}
	}
}

func TestCorePubMedAndEuropePMCQueryWholeRegistryWithoutTopicAND(t *testing.T) {
	for _, source := range []string{"pubmed", "europepmc"} {
		for _, core := range []bool{false, true} {
			body, param := `{"esearchresult":{"count":"0"}}`, "term"
			ordinary, scoped := "(language agents) AND (2026/10/19:2026/10/25[crdt] OR 2026/10/19:2026/10/25[lr])", `("0028-0836"[issn] OR "1476-4687"[issn]) AND (2026/10/19:2026/10/25[crdt] OR 2026/10/19:2026/10/25[lr])`
			if source == "europepmc" {
				body, param = `{"hitCount":0,"resultList":{"result":[]}}`, "query"
				ordinary = "(language agents) AND (FIRST_IDATE:[2026-10-19 TO 2026-10-25] OR UPDATE_DATE:[2026-10-19 TO 2026-10-25])"
				scoped = `(ISSN:"0028-0836" OR ISSN:"1476-4687") AND (FIRST_IDATE:[2026-10-19 TO 2026-10-25] OR UPDATE_DATE:[2026-10-19 TO 2026-10-25])`
			}
			f := &adapterExecutor{responses: []*httpclient.Response{jsonResponse(body)}}
			req := adapterWindow()
			req.Template.Core = core
			req.RegistryISSNs = []string{"1476-4687", "0028-0836", "0028-0836"}
			page, err := (&remoteSource{name: source, executor: f}).Fetch(context.Background(), req)
			if err != nil || !page.Complete {
				t.Fatalf("%s core=%t %+v %v", source, core, page, err)
			}
			want := ordinary
			if core {
				want = scoped
			}
			if got := f.requests[0].options.QueryParams.Get(param); got != want {
				t.Fatalf("%s core=%t query=%s want=%s", source, core, got, want)
			}
		}
	}
}

func TestLegacyNarrowCoreCursorsReplayOriginalFixedWindow(t *testing.T) {
	req := adapterWindow()
	req.Template.Core = true
	req.RegistryISSNs = []string{"0028-0836"}
	req.Cursor = (pubmedCursor{Count: 2, Offset: 1, WebEnv: "old-narrow-history", Key: "1", Pending: []pubmedPartition{{From: "2026-10-24", Until: "2026-10-25"}}}).encode()
	f := &adapterExecutor{responses: []*httpclient.Response{jsonResponse(`{"esearchresult":{"count":"1","webenv":"new-full-journal-history","querykey":"1"}}`)}}
	page, err := NewPubMedSource(f).Fetch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	c, err := decodePubmedCursor(page.NextCursor)
	if err != nil || !c.CoreFull || c.WebEnv != "new-full-journal-history" || c.Pending[0].From != "2026-10-19" || !strings.HasSuffix(f.requests[0].path, "esearch.fcgi") {
		t.Fatalf("old narrow PMID snapshot cannot cover new full-journal semantics: %+v %v", c, err)
	}
	req.Cursor = (europePMCCursor{Mark: "old-narrow-query", Seen: 1, Total: 2}).encode()
	f = &adapterExecutor{responses: []*httpclient.Response{jsonResponse(`{"hitCount":2,"nextCursorMark":"new-journal-query","resultList":{"result":[{"id":"synthetic-unrelated","source":"MED","title":"A deliberately unrelated core journal paper"}]}}`)}}
	page, err = NewEuropePMCSource(f).Fetch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	ec, err := decodeEuropePMCCursor(page.NextCursor)
	if err != nil || !ec.CoreFull || ec.Seen != 1 || f.requests[0].options.QueryParams.Get("cursorMark") != "*" || len(page.Records) != 1 {
		t.Fatalf("old narrow EPMC query must restart while retaining non-keyword core paper: %+v %v", ec, err)
	}
}

func TestPubMedMixedArticlesAndBooksUseRawPagingAndQuarantine(t *testing.T) {
	article := func(id, title, year string) string {
		return `<PubmedArticle><MedlineCitation><PMID>` + id + `</PMID><DateRevised><Year>` + year + `</Year><Month>10</Month><Day>23</Day></DateRevised><Article><ArticleTitle>` + title + `</ArticleTitle><Journal><JournalIssue><PubDate><Year>2026</Year><Month>Oct</Month></PubDate></JournalIssue></Journal></Article></MedlineCitation></PubmedArticle>`
	}
	bookRaw := `<BookDocument><PMID>synthetic-book</PMID><Book><BookTitle>A synthetic book, not a journal paper</BookTitle></Book></BookDocument>`
	body := `<PubmedArticleSet>` + article("synthetic-good-1", "Clinical language agents one", "2026") + article("", "Missing identity", "2026") + `<PubmedBookArticle>` + bookRaw + `</PubmedBookArticle>` + article("synthetic-bad-year", "Invalid numeric metadata", "not-a-year") + article("synthetic-good-2", "Clinical language agents two", "2026") + `</PubmedArticleSet>`
	f := &adapterExecutor{responses: []*httpclient.Response{
		jsonResponse(`{"esearchresult":{"count":"6","webenv":"synthetic-mixed-history","querykey":"1"}}`),
		jsonResponse(body),
		jsonResponse(`<PubmedArticleSet>` + article("synthetic-good-3", "Clinical language agents final", "2026") + `</PubmedArticleSet>`),
	}}
	s, req := NewPubMedSource(f), adapterWindow()
	req.PageSize = 5
	page, err := s.Fetch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	req.Cursor = page.NextCursor
	page, err = s.Fetch(context.Background(), req)
	if err != nil || page.Complete || len(page.Records) != 5 {
		t.Fatalf("mixed valid XML page failed: %+v %v", page, err)
	}
	for _, i := range []int{0, 4} {
		if r := page.Records[i]; r.ValidationError != "" || r.Title == "" || len(r.PublicationEvents) != 0 {
			t.Fatalf("good article/unknown date lost: %+v", r)
		}
	}
	if r := page.Records[1]; r.ValidationError != "invalid_source_record" || !strings.HasPrefix(r.SourceID, "unparsed-sha256:") || !strings.Contains(string(r.Raw), "Missing identity") {
		t.Fatalf("missing PMID must be isolated: %+v", r)
	}
	if r := page.Records[2]; r.ValidationError != "unsupported_record_type" || r.PMID != "synthetic-book" || r.Title != "" || r.PublicationType != "PubmedBookArticle" || !bytes.Equal(r.Raw, rawXML(bookRaw)) {
		t.Fatalf("book must stay explicit unsupported raw evidence, not a fabricated paper: %+v", r)
	}
	if r := page.Records[3]; r.ValidationError != "invalid_source_record" || r.PMID != "synthetic-bad-year" || !strings.Contains(string(r.Raw), "not-a-year") {
		t.Fatalf("per-item decoder error must retain PMID and raw evidence: %+v", r)
	}
	c, err := decodePubmedCursor(page.NextCursor)
	if err != nil || c.Offset != 5 {
		t.Fatalf("books and rejected articles must advance raw offset: %+v %v", c, err)
	}
	req.Cursor = page.NextCursor
	page, err = s.Fetch(context.Background(), req)
	if err != nil || !page.Complete || len(page.Records) != 1 || f.requests[2].options.QueryParams.Get("retstart") != "5" {
		t.Fatalf("final mixed page not consumed exactly: %+v %v", page, err)
	}
}
