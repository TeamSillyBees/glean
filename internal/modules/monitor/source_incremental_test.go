package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/teamsillybees/glean/internal/platform/httpclient"
)

func jsonResponse(body string) *httpclient.Response {
	return &httpclient.Response{StatusCode: 200, Body: []byte(body)}
}

func TestPubMedLargeDateWindowPartitionsThenCompletesAllLeaves(t *testing.T) {
	f := &adapterExecutor{responses: []*httpclient.Response{jsonResponse(`{"esearchresult":{"count":"12000","idlist":["42000000"]}}`), jsonResponse(`{"esearchresult":{"count":"0"}}`), jsonResponse(`{"esearchresult":{"count":"0"}}`)}}
	req := adapterWindow()
	req.Window.Start = time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	req.Window.End = req.Window.Start.Add(48 * time.Hour)
	s := NewPubMedSource(f)
	page, e := s.Fetch(context.Background(), req)
	if e != nil || page.Complete || page.NextCursor == "" {
		t.Fatalf("must persist subdivision: %+v %v", page, e)
	}
	c, e := decodePubmedCursor(page.NextCursor)
	if e != nil || len(c.Pending) != 2 || c.Pending[0].Until != "2026-10-22" || c.Pending[1].From != "2026-10-23" {
		t.Fatalf("bad partition: %+v %v", c, e)
	}
	req.Cursor = page.NextCursor
	page, e = s.Fetch(context.Background(), req)
	if e != nil || page.Complete {
		t.Fatalf("first leaf must not finish other day: %+v %v", page, e)
	}
	req.Cursor = page.NextCursor
	page, e = s.Fetch(context.Background(), req)
	if e != nil || !page.Complete {
		t.Fatalf("all leaves must finish: %+v %v", page, e)
	}
	if len(f.requests) != 3 {
		t.Fatal("each Fetch permits one HTTP request")
	}
}

func TestPubMedOversizedDayPartitionsUIDWithoutAssumedUpperBound(t *testing.T) {
	f := &adapterExecutor{responses: []*httpclient.Response{jsonResponse(`{"esearchresult":{"count":"10000","idlist":["42000000"]}}`), jsonResponse(`{"esearchresult":{"count":"0"}}`), jsonResponse(`{"esearchresult":{"count":"0"}}`)}}
	req := adapterWindow()
	req.Window.End = req.Window.Start.Add(24 * time.Hour)
	s := NewPubMedSource(f)
	page, e := s.Fetch(context.Background(), req)
	if e != nil || page.Complete {
		t.Fatalf("UID split failed: %+v %v", page, e)
	}
	c, _ := decodePubmedCursor(page.NextCursor)
	if len(c.Pending) != 2 || c.Pending[1].High != 0 || c.Pending[1].Low != c.Pending[0].High+1 {
		t.Fatalf("upper PMID range must remain open: %+v", c)
	}
	req.Cursor = page.NextCursor
	page, e = s.Fetch(context.Background(), req)
	if e != nil {
		t.Fatal(e)
	}
	req.Cursor = page.NextCursor
	page, e = s.Fetch(context.Background(), req)
	if e != nil || !page.Complete {
		t.Fatalf("partition continuation: %+v %v", page, e)
	}
	if !strings.Contains(f.requests[2].options.QueryParams.Get("term"), " NOT 1:") {
		t.Fatal("unbounded upper UID partition must not truncate future identifiers")
	}
}

func TestCrossrefCoreUsesEveryRegistryISSNWithoutKeywordAND(t *testing.T) {
	f := &adapterExecutor{responses: []*httpclient.Response{jsonResponse(`{"status":"ok","message":{"total-results":0,"items":[]}}`), jsonResponse(`{"status":"ok","message":{"total-results":0,"items":[]}}`)}}
	req := adapterWindow()
	req.Template.Core = true
	req.RegistryISSNs = []string{"1476-4687", "0028-0836", "0028-0836"}
	s := NewCrossrefSource(f)
	page, e := s.Fetch(context.Background(), req)
	if e != nil || page.Complete || page.NextCursor == "" {
		t.Fatalf("first ISSN is not complete registry: %+v %v", page, e)
	}
	if f.requests[0].path != "/journals/0028-0836/works" || f.requests[0].options.QueryParams.Get("query.bibliographic") != "" {
		t.Fatal("core backfill must be server-side journal feed, not narrow keyword AND")
	}
	req.Cursor = page.NextCursor
	page, e = s.Fetch(context.Background(), req)
	if e != nil || !page.Complete || f.requests[1].path != "/journals/1476-4687/works" {
		t.Fatalf("remaining ISSN not visited: %+v %v", page, e)
	}
}

func TestCoreWithoutRegistryNeverPretendsCoverage(t *testing.T) {
	for _, factory := range []func(httpclient.Executor) Source{NewCrossrefSource, NewPubMedSource, NewEuropePMCSource} {
		f := &adapterExecutor{}
		req := adapterWindow()
		req.Template.Core = true
		page, e := factory(f).Fetch(context.Background(), req)
		var se *SourceError
		if !errors.As(e, &se) || se.Kind != "missing_registry" || page.Complete || len(f.requests) > 0 {
			t.Fatalf("missing registry must not become successful broad query: %+v %v", page, e)
		}
	}
}

func TestCrossrefSynonymsAreSeparateUnionFeeds(t *testing.T) {
	f := &adapterExecutor{responses: []*httpclient.Response{jsonResponse(`{"status":"ok","message":{"total-results":0,"items":[]}}`), jsonResponse(`{"status":"ok","message":{"total-results":0,"items":[]}}`)}}
	req := adapterWindow()
	req.Template.TermGroups = [][]string{{"world model", "digital twin"}, {"biology", "clinical"}}
	s := NewCrossrefSource(f)
	page, e := s.Fetch(context.Background(), req)
	if e != nil || page.Complete {
		t.Fatalf("OR feeds not persisted: %+v %v", page, e)
	}
	req.Cursor = page.NextCursor
	page, e = s.Fetch(context.Background(), req)
	if e != nil || !page.Complete {
		t.Fatalf("second OR feed incomplete: %+v %v", page, e)
	}
	if f.requests[0].options.QueryParams.Get("query.bibliographic") != "world model" || f.requests[1].options.QueryParams.Get("query.bibliographic") != "digital twin" {
		t.Fatal("mutually alternative terms cannot be concatenated as AND")
	}
}

func TestPubMedAheadOfPrintIssueCopyNotFormalIssue(t *testing.T) {
	search := adapterFixture(t, "pubmed-search.json")
	fetch := adapterFixture(t, "pubmed-fetch.xml")
	body := string(fetch.Body)
	body = strings.ReplaceAll(body, "<JournalIssue>", "<JournalIssue><Volume>1</Volume><Issue>2</Issue>")
	body = strings.ReplaceAll(body, "<Month>Oct</Month></PubDate>", "<Month>Oct</Month><Day>20</Day></PubDate>")
	body = strings.ReplaceAll(body, "<PubmedData>", "<PubmedData><PublicationStatus>aheadofprint</PublicationStatus>")
	fetch.Body = []byte(body)
	req := adapterWindow()
	s := NewPubMedSource(&adapterExecutor{responses: []*httpclient.Response{search, fetch}})
	page, e := s.Fetch(context.Background(), req)
	if e != nil {
		t.Fatal(e)
	}
	req.Cursor = page.NextCursor
	page, e = s.Fetch(context.Background(), req)
	if e != nil {
		t.Fatal(e)
	}
	for _, event := range page.Records[0].PublicationEvents {
		if event.Kind == "issue" {
			t.Fatal("ahead-of-print repeated PubDate must not invent an issue date")
		}
	}
}

func TestEuropePMCIncrementalCursorAndNormalizedDateSafety(t *testing.T) {
	body := `{"hitCount":2,"nextCursorMark":"next","resultList":{"result":[{"id":"123","source":"MED","title":"Clinical agents","doi":"10.5555/real-shape-only","firstIndexDate":"2026-10-23","electronicPublicationDate":"2026-10-20","firstPublicationDate":"2026-01-01","journalInfo":{"volume":"2","dateOfPublication":"2026 Oct","printPublicationDate":"2026-10-01","journal":{"title":"Synthetic journal","issn":"0028-0836"}}}]}}`
	f := &adapterExecutor{responses: []*httpclient.Response{jsonResponse(body)}}
	page, e := NewEuropePMCSource(f).Fetch(context.Background(), adapterWindow())
	if e != nil {
		t.Fatal(e)
	}
	if page.Complete || page.NextCursor == "" || len(page.Records) != 1 {
		t.Fatalf("wrong cursor: %+v", page)
	}
	if len(page.Records[0].PublicationEvents) != 1 || page.Records[0].PublicationEvents[0].Kind != "online" {
		t.Fatal("normalized first/print dates must not create invented precision")
	}
	q := f.requests[0].options.QueryParams.Get("query")
	if !strings.Contains(q, "FIRST_IDATE:[2026-10-19 TO 2026-10-25]") || !strings.Contains(q, "OR UPDATE_DATE:") {
		t.Fatalf("missing create/revision query: %s", q)
	}
	if !json.Valid(page.Records[0].Raw) || page.Records[0].IsMock {
		t.Fatal("real-source record must keep raw evidence without being labeled mock")
	}
}

func TestMedRxivCountMetadataAndNoFormalDOIReplacement(t *testing.T) {
	f := &adapterExecutor{responses: []*httpclient.Response{adapterFixture(t, "biorxiv-page.json")}}
	page, e := NewMedRxivSource(f).Fetch(context.Background(), adapterWindow())
	if e != nil {
		t.Fatal(e)
	}
	if !strings.HasPrefix(f.requests[0].path, "/details/medrxiv/") || len(page.Records) != 1 || page.Records[0].Source != "medrxiv" || strings.Contains(page.Records[0].DOI, "formal-link") {
		t.Fatalf("medRxiv must retain preprint identity: %+v", page)
	}
	mismatch := jsonResponse(`{"messages":[{"status":"ok","cursor":0,"count":30,"total":30}],"collection":[]}`)
	page, e = NewMedRxivSource(&adapterExecutor{responses: []*httpclient.Response{mismatch}}).Fetch(context.Background(), adapterWindow())
	if e == nil || page.Complete {
		t.Fatal("count disagreement must not advance checkpoint")
	}
}

func TestArXivExpiredTokenRestartsOnlyUnfinishedDay(t *testing.T) {
	f := &adapterExecutor{responses: []*httpclient.Response{{StatusCode: 200, Body: []byte(`<OAI-PMH><error code="badResumptionToken">expired</error></OAI-PMH>`)}}}
	req := adapterWindow()
	req.Cursor = (arxivCursor{Day: "2026-10-23", Token: "expired"}).encode()
	page, e := NewArXivSource(f).Fetch(context.Background(), req)
	if e != nil || page.Complete {
		t.Fatalf("expired OAI token should safely replay unfinished day: %+v %v", page, e)
	}
	c, _ := decodeArxivCursor(page.NextCursor)
	if c.Day != "2026-10-23" || c.Token != "" {
		t.Fatalf("lost completed day progress: %+v", c)
	}
	q := f.requests[0].options.QueryParams
	if len(q) != 2 || q.Get("metadataPrefix") != "" || q.Get("resumptionToken") != "expired" {
		t.Fatal("OAI continuation must use token only (plus verb)")
	}
}

func TestArXivNoRecordsMatchAdvancesOneDayNotWholeWindow(t *testing.T) {
	f := &adapterExecutor{responses: []*httpclient.Response{{StatusCode: 200, Body: []byte(`<OAI-PMH><error code="noRecordsMatch">none</error></OAI-PMH>`)}}}
	page, e := NewArXivSource(f).Fetch(context.Background(), adapterWindow())
	if e != nil || page.Complete {
		t.Fatalf("empty day not whole window: %+v %v", page, e)
	}
	c, _ := decodeArxivCursor(page.NextCursor)
	if c.Day != "2026-10-20" {
		t.Fatal("must advance exactly one partition")
	}
}

func TestMutableIndexesRestartSameScopeWithoutAdvancing(t *testing.T) {
	req := adapterWindow()
	total := 2
	req.Cursor = (crossrefCursor{Token: "previous", Seen: 1, Total: &total}).encode()
	page, e := NewCrossrefSource(&adapterExecutor{responses: []*httpclient.Response{jsonResponse(`{"status":"ok","message":{"total-results":1,"items":[]}}`)}}).Fetch(context.Background(), req)
	if e != nil || page.Complete || len(page.Records) != 0 {
		t.Fatalf("changed index must restart safely: %+v %v", page, e)
	}
	c, e := decodeCrossrefCursor(page.NextCursor)
	if e != nil || c.Token != "*" || c.Seen != 0 || c.Restarts != 1 {
		t.Fatalf("bad Crossref reset: %+v %v", c, e)
	}
	req.Cursor = (europePMCCursor{Mark: "previous", Seen: 1, Total: 2}).encode()
	page, e = NewEuropePMCSource(&adapterExecutor{responses: []*httpclient.Response{jsonResponse(`{"hitCount":1,"resultList":{"result":[]}}`)}}).Fetch(context.Background(), req)
	if e != nil || page.Complete {
		t.Fatalf("EuropePMC reset: %+v %v", page, e)
	}
	ep, e := decodeEuropePMCCursor(page.NextCursor)
	if e != nil || ep.Mark != "*" || ep.Restarts != 1 {
		t.Fatalf("bad EuropePMC reset: %+v %v", ep, e)
	}
}

func TestPubMedExpiredHistoryReplaysOnlyUnfinishedPartition(t *testing.T) {
	req := adapterWindow()
	req.Cursor = (pubmedCursor{Count: 40, Offset: 20, WebEnv: "expired", Key: "1", Pending: []pubmedPartition{{From: "2026-10-23", Until: "2026-10-23", Low: 100, High: 200}}}).encode()
	page, e := NewPubMedSource(&adapterExecutor{responses: []*httpclient.Response{{StatusCode: 200, Body: []byte(`<PubmedArticleSet><ERROR>history expired</ERROR></PubmedArticleSet>`)}}}).Fetch(context.Background(), req)
	if e != nil || page.Complete {
		t.Fatalf("history expiry must not permanently lose partition: %+v %v", page, e)
	}
	c, e := decodePubmedCursor(page.NextCursor)
	if e != nil || c.Count != 0 || c.Offset != 0 || c.Restarts != 1 || len(c.Pending) != 1 || c.Pending[0].Low != 100 || c.Pending[0].High != 200 {
		t.Fatalf("lost frozen scope on recovery: %+v %v", c, e)
	}
}

func TestDateOnlySourcesKeepSameDayOverlap(t *testing.T) {
	req := adapterWindow()
	req.Window.Start = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	req.Window.End = time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		Name    string
		Factory func(httpclient.Executor) Source
		Fixture string
	}{{"biorxiv", NewBioRxivSource, "biorxiv-page.json"}, {"arxiv", NewArXivSource, "arxiv-oai-synthetic.xml"}} {
		t.Run(tc.Name, func(t *testing.T) {
			page, e := tc.Factory(&adapterExecutor{responses: []*httpclient.Response{adapterFixture(t, tc.Fixture)}}).Fetch(context.Background(), req)
			if e != nil || len(page.Records) != 1 {
				t.Fatalf("date-only midnight cannot drop same-day discoveries for noon lower bound: %+v %v", page, e)
			}
		})
	}
}

func TestOptionalSmallPageSizeAndBoundedValidation(t *testing.T) {
	req := adapterWindow()
	req.PageSize = 1
	f := &adapterExecutor{responses: []*httpclient.Response{adapterFixture(t, "crossref-page.json")}}
	if _, e := NewCrossrefSource(f).Fetch(context.Background(), req); e != nil {
		t.Fatal(e)
	}
	if f.requests[0].options.QueryParams.Get("rows") != "1" {
		t.Fatal("canary must request one Crossref row")
	}
	f = &adapterExecutor{}
	req.PageSize = 1001
	if _, e := NewCrossrefSource(f).Fetch(context.Background(), req); e == nil || len(f.requests) > 0 {
		t.Fatal("unbounded page size must fail before network")
	}
}

func TestCrossrefSecondPrecisionQueryDoesNotLoseSubsecondEnd(t *testing.T) {
	req := adapterWindow()
	req.Window.Start = time.Date(2026, 10, 23, 10, 0, 0, 500000000, time.UTC)
	req.Window.End = req.Window.Start.Add(time.Second)
	fixture := adapterFixture(t, "crossref-page.json")
	fixture.Body = []byte(strings.ReplaceAll(string(fixture.Body), "2026-10-23T10:00:00Z", "2026-10-23T10:00:01.250Z"))
	f := &adapterExecutor{responses: []*httpclient.Response{fixture}}
	page, e := NewCrossrefSource(f).Fetch(context.Background(), req)
	if e != nil || len(page.Records) != 1 {
		t.Fatalf("subsecond item before exact End must not be lost: %+v %v", page, e)
	}
	want := "from-index-date:2026-10-23T10:00:00,until-index-date:2026-10-23T10:00:02"
	if f.requests[0].options.QueryParams.Get("filter") != want {
		t.Fatalf("Crossref accepted grammar / safe rounded bounds lost: %v", f.requests[0].options.QueryParams)
	}
}

func TestProviderGateBackoffRemainsTypedWithoutNetworkRetry(t *testing.T) {
	cooldown := &SourceError{Kind: "provider_backoff", StatusCode: 429, RetryAfter: 2 * time.Minute, Retryable: true, Message: "provider requested cooldown"}
	f := &adapterExecutor{err: cooldown}
	_, e := NewMedRxivSource(f).Fetch(context.Background(), adapterWindow())
	var got *SourceError
	if !errors.As(e, &got) || got != cooldown || got.RetryAfter != 2*time.Minute {
		t.Fatalf("shared provider cooldown must not become generic transport failure: %v", e)
	}
}

func TestLegacyDiscoveryCursorCannotResumeDifferentQuery(t *testing.T) {
	req := adapterWindow()
	req.Cursor = "cr1:" + strings.TrimPrefix((crossrefCursor{Token: "old-and-query", Seen: 100}).encode(), "cr2:")
	f := &adapterExecutor{}
	page, e := NewCrossrefSource(f).Fetch(context.Background(), req)
	if e == nil || page.Complete || len(f.requests) > 0 {
		t.Fatal("legacy all-keywords cursor must not resume new OR feed silently")
	}
}

func TestCrossrefMissingLaterTotalDoesNotEraseExpectedCount(t *testing.T) {
	req := adapterWindow()
	total := 2
	req.Cursor = (crossrefCursor{Token: "next", Seen: 1, Total: &total}).encode()
	page, e := NewCrossrefSource(&adapterExecutor{responses: []*httpclient.Response{jsonResponse(`{"status":"ok","message":{"items":[]}}`)}}).Fetch(context.Background(), req)
	if e == nil || page.Complete {
		t.Fatal("missing later total cannot acknowledge one-of-two records as complete")
	}
}
