package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/teamsillybees/glean/internal/platform/httpclient"
)

type adapterRequest struct {
	path    string
	options *httpclient.RequestOptions
}
type adapterExecutor struct {
	responses []*httpclient.Response
	requests  []adapterRequest
	err       error
}

func (f *adapterExecutor) Do(_ context.Context, method, path string, opts ...httpclient.RequestOption) (*httpclient.Response, error) {
	if method != http.MethodGet {
		panic("source adapters must be read-only GET")
	}
	options, err := httpclient.ApplyRequestOptions(opts...)
	if err != nil {
		return nil, err
	}
	f.requests = append(f.requests, adapterRequest{path: path, options: &options})
	if len(f.responses) == 0 {
		return nil, f.err
	}
	r := f.responses[0]
	f.responses = f.responses[1:]
	return r, f.err
}
func adapterFixture(t *testing.T, name string) *httpclient.Response {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return &httpclient.Response{StatusCode: 200, Header: make(http.Header), Body: b}
}
func adapterWindow() FetchRequest {
	return FetchRequest{Window: Window{Start: time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)}, Template: SearchTemplate{ID: "agents", Version: "v1", Topic: "llm-agent", Query: "language agents", Terms: []string{"language agents"}}}
}

// TestCrossrefMetadataDates 验证正式发表不被索引/接收日期污染，缺日日期保留原始证据而不伪造。
func TestCrossrefMetadataDates(t *testing.T) {
	f := &adapterExecutor{responses: []*httpclient.Response{adapterFixture(t, "crossref-page.json"), {StatusCode: 200, Body: []byte(`{"status":"ok","message":{"items":[]}}`)}}}
	s := NewCrossrefSource(f)
	req := adapterWindow()
	page, err := s.Fetch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if page.Complete || page.NextCursor == "" || len(page.Records) != 1 {
		t.Fatalf("unexpected page: %+v", page)
	}
	r := page.Records[0]
	if r.DOI != "10.5555/glean.1" || len(r.PublicationEvents) != 1 || r.PublicationEvents[0].Kind != "online" || r.PublicationEvents[0].Date.Day() != 20 {
		t.Fatalf("wrong identity/publication evidence: %+v", r)
	}
	if !r.AcquiredAt.IsZero() || r.SourceUpdatedAt.Day() != 23 || !json.Valid(r.Raw) {
		t.Fatal("adapter must separate acquisition timestamp and source indexed metadata")
	}
	q := f.requests[0].options.QueryParams
	if !strings.Contains(q.Get("filter"), "from-index-date:2026-10-19T00:00:00Z,until-index-date:2026-10-26T00:00:00Z") || q.Get("cursor") != "*" {
		t.Fatalf("query not fixed: %v", q)
	}
	req.Cursor = page.NextCursor
	page, err = s.Fetch(context.Background(), req)
	if f.requests[1].options.QueryParams.Get("cursor") != "next-cursor-token" {
		t.Fatal("opaque durable cursor must unwrap to upstream cursor")
	}
	if err != nil || !page.Complete || page.NextCursor != "" {
		t.Fatalf("empty terminal page: %+v %v", page, err)
	}
}

func TestCrossrefPostedContentNotFormal(t *testing.T) {
	response := adapterFixture(t, "crossref-page.json")
	response.Body = []byte(strings.ReplaceAll(string(response.Body), `"type":"journal-article"`, `"type":"posted-content","posted":{"date-parts":[[2026,10,12]]}`))
	page, err := NewCrossrefSource(&adapterExecutor{responses: []*httpclient.Response{response}}).Fetch(context.Background(), adapterWindow())
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records[0].PublicationEvents) != 1 || page.Records[0].PublicationEvents[0].Kind != "preprint" {
		t.Fatal("posted-content online date must not be formal publication")
	}
}

// TestPubMedHistoryPagination 验证每次 Fetch 至多一次请求、history 快照分页与发布日期来源。
func TestPubMedHistoryPagination(t *testing.T) {
	f := &adapterExecutor{responses: []*httpclient.Response{adapterFixture(t, "pubmed-search.json"), adapterFixture(t, "pubmed-fetch.xml")}}
	s := NewPubMedSource(f)
	req := adapterWindow()
	page, err := s.Fetch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.requests) != 1 || page.Complete || page.NextCursor == "" || len(page.Records) != 0 {
		t.Fatal("ESearch must yield before EFetch for durable pacing")
	}
	term := f.requests[0].options.QueryParams.Get("term")
	if !strings.Contains(term, "[crdt] OR") || !strings.Contains(term, "[lr]") || strings.Contains(term, "[pdat]") {
		t.Fatalf("expected creation/revision date query: %s", term)
	}
	req.Cursor = page.NextCursor
	page, err = s.Fetch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !page.Complete || len(page.Records) != 1 || f.requests[1].options.QueryParams.Get("WebEnv") != "fixture-history-token" {
		t.Fatalf("history not consumed: %+v", page)
	}
	r := page.Records[0]
	if r.Title != "Clinical language agents" || r.Abstract != "An offline fixture." || r.DOI != "10.5555/glean.1" || !json.Valid(r.Raw) {
		t.Fatalf("bad PubMed normalization: %+v", r)
	}
	if len(r.PublicationEvents) != 1 || r.PublicationEvents[0].Date.Day() != 20 || r.PublicationEvents[0].Kind != "online" {
		t.Fatalf("accepted/indexed/partial dates must not be formal: %+v", r.PublicationEvents)
	}
}

func TestPubMedPreprintDoesNotBecomeFormal(t *testing.T) {
	search := adapterFixture(t, "pubmed-search.json")
	fetch := adapterFixture(t, "pubmed-fetch.xml")
	fetch.Body = []byte(strings.ReplaceAll(string(fetch.Body), ">Journal Article<", ">Preprint<"))
	s := NewPubMedSource(&adapterExecutor{responses: []*httpclient.Response{search, fetch}})
	req := adapterWindow()
	page, err := s.Fetch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	req.Cursor = page.NextCursor
	page, err = s.Fetch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if page.Records[0].PublicationEvents[0].Kind != "preprint" {
		t.Fatal("PubMed Preprint is not formal publication")
	}
}

func TestPubMedTooLargeAndExpiredHistoryFailClosed(t *testing.T) {
	for _, body := range []string{`{"esearchresult":{"count":"10001","webenv":"x","querykey":"1"}}`, `{"esearchresult":{"count":"1"}}`, `{"esearchresult":{"count":"0","errorlist":{"phrasesnotfound":["x"]}}}`} {
		s := NewPubMedSource(&adapterExecutor{responses: []*httpclient.Response{{StatusCode: 200, Body: []byte(body)}}})
		page, err := s.Fetch(context.Background(), adapterWindow())
		if err == nil || page.Complete {
			t.Fatalf("must not acknowledge incomplete query: %+v %v", page, err)
		}
	}
	req := adapterWindow()
	req.Cursor = (pubmedCursor{Count: 1, WebEnv: "expired", Key: "1"}).encode()
	page, err := NewPubMedSource(&adapterExecutor{responses: []*httpclient.Response{{StatusCode: 200, Body: []byte(`<PubmedArticleSet><ERROR>history expired</ERROR></PubmedArticleSet>`)}}}).Fetch(context.Background(), req)
	if err == nil || page.Complete {
		t.Fatal("expired history must not advance checkpoint")
	}
}

func TestArXivOldPreprintNewVersionAndWindow(t *testing.T) {
	f := &adapterExecutor{responses: []*httpclient.Response{adapterFixture(t, "arxiv-page.xml")}}
	page, err := NewArXivSource(f).Fetch(context.Background(), adapterWindow())
	if err != nil {
		t.Fatal(err)
	}
	if !page.Complete || len(page.Records) != 1 {
		t.Fatalf("wrong date-filtered page: %+v", page)
	}
	r := page.Records[0]
	if r.SourceID != "2608.00001" || r.Version != "2" || r.SourceUpdatedAt.Day() != 23 {
		t.Fatalf("version normalization failed: %+v", r)
	}
	if len(r.PublicationEvents) != 1 || r.PublicationEvents[0].Kind != "preprint" || r.PublicationEvents[0].Date.Month() != time.August {
		t.Fatal("arXiv publication date means preprint, even with DOI/journal_ref")
	}
	q := f.requests[0].options.QueryParams
	if q.Get("sortBy") != "lastUpdatedDate" || q.Get("sortOrder") != "descending" || strings.Contains(q.Get("search_query"), "submittedDate") {
		t.Fatal("submittedDate filter would miss old preprint new versions")
	}
}

func TestArXivFutureVersionExcluded(t *testing.T) {
	fixture := adapterFixture(t, "arxiv-page.xml")
	fixture.Body = []byte(strings.ReplaceAll(string(fixture.Body), "2026-10-23T10:00:00Z", "2026-10-27T10:00:00Z"))
	page, err := NewArXivSource(&adapterExecutor{responses: []*httpclient.Response{fixture}}).Fetch(context.Background(), adapterWindow())
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 0 {
		t.Fatal("updates beyond fixed watermark must not enter current acquisition")
	}
}

func TestBioRxivPagingUsesRawCountAndNoInventedFormalDate(t *testing.T) {
	f := &adapterExecutor{responses: []*httpclient.Response{adapterFixture(t, "biorxiv-page.json")}}
	page, err := NewBioRxivSource(f).Fetch(context.Background(), adapterWindow())
	if err != nil {
		t.Fatal(err)
	}
	if page.Complete || page.NextCursor != "2" || len(page.Records) != 1 {
		t.Fatalf("filtered records must not affect source cursor: %+v", page)
	}
	r := page.Records[0]
	if r.Version != "2" || len(r.PublicationEvents) != 1 || r.PublicationEvents[0].Kind != "preprint" {
		t.Fatal("linked published DOI does not prove formal publication date")
	}
	if !strings.Contains(string(r.Raw), "formal-link-without-date") {
		t.Fatal("retain linkage evidence for later publisher resolution")
	}
	if f.requests[0].path != "/details/biorxiv/2026-10-19/2026-10-26/0/json" {
		t.Fatalf("bad bioRxiv path: %s", f.requests[0].path)
	}
}

func TestSourceBackoffDoesNotRetryAndDoesNotLeakResponse(t *testing.T) {
	for _, status := range []int{429, 500, 401, 400} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			f := &adapterExecutor{responses: []*httpclient.Response{{StatusCode: status, Header: http.Header{"Retry-After": []string{"120"}}, Body: []byte("private token from upstream")}}, err: errors.New("transport may include sensitive URL")}
			_, err := NewCrossrefSource(f).Fetch(context.Background(), adapterWindow())
			var se *SourceError
			if !errors.As(err, &se) || se.StatusCode != status || se.RetryAfter != 2*time.Minute || se.Retryable != (status == 429 || status >= 500) || len(f.requests) != 1 {
				t.Fatalf("wrong error policy: %+v, requests=%d", err, len(f.requests))
			}
			if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "sensitive") {
				t.Fatal("source error leaked upstream text")
			}
		})
	}
}

func TestRetryAfterDateAndInvalidValues(t *testing.T) {
	now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{{"30", 30 * time.Second}, {"Mon, 26 Oct 2026 00:01:00 GMT", time.Minute}, {"-1", 0}, {"not a date", 0}, {"Sun, 25 Oct 2026 00:00:00 GMT", 0}} {
		if got := ParseRetryAfter(tc.value, now); got != tc.want {
			t.Errorf("%q => %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestAdaptersMalformedPagesNeverComplete(t *testing.T) {
	cases := []struct {
		name    string
		factory func(httpclient.Executor) Source
		body    string
	}{{"crossref", NewCrossrefSource, `{"status":"ok","message":{}}`}, {"pubmed", NewPubMedSource, `{"esearchresult":{}}`}, {"arxiv", NewArXivSource, `<feed><entry><id>error</id></entry></feed>`}, {"biorxiv", NewBioRxivSource, `{"messages":[{"status":"ok","cursor":0,"total":10}],"collection":[]}`}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page, err := tc.factory(&adapterExecutor{responses: []*httpclient.Response{{StatusCode: 200, Body: []byte(tc.body)}}}).Fetch(context.Background(), adapterWindow())
			if err == nil || page.Complete {
				t.Fatalf("bad source must not complete: %+v %v", page, err)
			}
		})
	}
}

func TestDisabledAdaptersAndInvalidWindowNeverCallNetwork(t *testing.T) {
	if _, err := NewCrossrefSource(nil).Fetch(context.Background(), adapterWindow()); err == nil {
		t.Fatal("nil executor must fail disabled")
	}
	f := &adapterExecutor{}
	req := adapterWindow()
	req.Window.End = req.Window.Start
	if _, err := NewCrossrefSource(f).Fetch(context.Background(), req); err == nil || len(f.requests) > 0 {
		t.Fatal("invalid window must not issue request")
	}
}

func TestAdapterFixedEndIsExclusive(t *testing.T) {
	cases := []struct {
		name           string
		factory        func(httpclient.Executor) Source
		file, from, to string
	}{
		{"crossref", NewCrossrefSource, "crossref-page.json", "2026-10-23T10:00:00Z", "2026-10-26T00:00:00Z"},
		{"arxiv", NewArXivSource, "arxiv-page.xml", "2026-10-23T10:00:00Z", "2026-10-26T00:00:00Z"},
		{"biorxiv", NewBioRxivSource, "biorxiv-page.json", "2026-10-23", "2026-10-26"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := adapterFixture(t, tc.file)
			r.Body = []byte(strings.ReplaceAll(string(r.Body), tc.from, tc.to))
			page, err := tc.factory(&adapterExecutor{responses: []*httpclient.Response{r}}).Fetch(context.Background(), adapterWindow())
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Records) != 0 {
				t.Fatal("End is an exclusive watermark, including exact-boundary updates would violate the window contract")
			}
		})
	}
}

func TestArXivAPIResultCapFailsClosed(t *testing.T) {
	req := adapterWindow()
	req.Cursor = "30000"
	f := &adapterExecutor{}
	page, err := NewArXivSource(f).Fetch(context.Background(), req)
	var se *SourceError
	if !errors.As(err, &se) || se.Kind != "window_too_large" || page.Complete || len(f.requests) != 0 {
		t.Fatalf("result cap must require window/template refinement without claiming completion: %+v %v", page, err)
	}
}

func TestPubMedFixedEndIsExclusive(t *testing.T) {
	search := adapterFixture(t, "pubmed-search.json")
	fetch := adapterFixture(t, "pubmed-fetch.xml")
	fetch.Body = []byte(strings.ReplaceAll(string(fetch.Body), "<Day>23</Day>", "<Day>26</Day>"))
	s := NewPubMedSource(&adapterExecutor{responses: []*httpclient.Response{search, fetch}})
	req := adapterWindow()
	page, err := s.Fetch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	req.Cursor = page.NextCursor
	page, err = s.Fetch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !page.Complete || len(page.Records) != 0 {
		t.Fatal("daily-resolution PubMed search may be broad, but post-watermark records must not be acquired")
	}
}

func TestCrossrefMissingContinuationFailsClosed(t *testing.T) {
	fixture := adapterFixture(t, "crossref-page.json")
	fixture.Body = []byte(strings.ReplaceAll(string(fixture.Body), `"next-cursor":"next-cursor-token",`, ""))
	page, err := NewCrossrefSource(&adapterExecutor{responses: []*httpclient.Response{fixture}}).Fetch(context.Background(), adapterWindow())
	if err == nil || page.Complete || len(page.Records) != 0 {
		t.Fatalf("missing continuation without completion evidence must not acknowledge page: %+v %v", page, err)
	}
}

func TestArXivTranslatesTermGroupsWithoutDroppingContext(t *testing.T) {
	req := adapterWindow()
	req.Template.TermGroups = [][]string{{"language agent", "LLM"}, {"clinical", "biology"}}
	f := &adapterExecutor{responses: []*httpclient.Response{adapterFixture(t, "arxiv-page.xml")}}
	if _, err := NewArXivSource(f).Fetch(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	got := f.requests[0].options.QueryParams.Get("search_query")
	want := `(all:"language agent" OR all:"LLM") AND (all:"clinical" OR all:"biology")`
	if got != want {
		t.Fatalf("unified AND/OR intent lost: %q, want %q", got, want)
	}
}

func TestBioRxivRequiresEachContextGroup(t *testing.T) {
	req := adapterWindow()
	req.Template.TermGroups = [][]string{{"language agent", "LLM"}, {"clinical", "cell biology"}}
	fixture := adapterFixture(t, "biorxiv-page.json")
	page, err := NewBioRxivSource(&adapterExecutor{responses: []*httpclient.Response{fixture}}).Fetch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || page.NextCursor != "2" {
		t.Fatal("a discovery hit must match at least one term in each group, without changing upstream page offsets")
	}
	req.Template.TermGroups[1] = []string{"digital twin", "world model"}
	page, err = NewBioRxivSource(&adapterExecutor{responses: []*httpclient.Response{fixture}}).Fetch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 0 || page.NextCursor != "2" {
		t.Fatal("a language-agent-only hit must not bypass required context group")
	}
}

func TestCrossrefUsesBroadDiscoveryNotUnsupportedBoolean(t *testing.T) {
	req := adapterWindow()
	req.Template.TermGroups = [][]string{{"language agent", "LLM"}, {"clinical", "biology"}}
	f := &adapterExecutor{responses: []*httpclient.Response{adapterFixture(t, "crossref-page.json")}}
	page, err := NewCrossrefSource(f).Fetch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	got := f.requests[0].options.QueryParams.Get("query.bibliographic")
	if got != "language agent LLM clinical biology" || len(page.Records) != 1 {
		t.Fatalf("Crossref should retain broad candidates, not pretend bibliographic is Boolean: %q %+v", got, page)
	}
}

func TestCrossrefTerminalPageWithoutCursorRequiresCountProof(t *testing.T) {
	first := adapterFixture(t, "crossref-page.json")
	first.Body = []byte(strings.ReplaceAll(string(first.Body), `"message":{`, `"message":{"total-results":2,`))
	last := adapterFixture(t, "crossref-page.json")
	last.Body = []byte(strings.ReplaceAll(string(last.Body), `"next-cursor":"next-cursor-token",`, `"total-results":2,`))
	last.Body = []byte(strings.ReplaceAll(string(last.Body), "Glean.1", "Glean.2"))
	f := &adapterExecutor{responses: []*httpclient.Response{first, last}}
	source := NewCrossrefSource(f)
	req := adapterWindow()
	page, err := source.Fetch(context.Background(), req)
	if err != nil || page.Complete {
		t.Fatalf("first page: %+v %v", page, err)
	}
	req.Cursor = page.NextCursor
	page, err = source.Fetch(context.Background(), req)
	if err != nil || !page.Complete || len(page.Records) != 1 || page.Records[0].DOI != "10.5555/glean.2" {
		t.Fatalf("official terminal cursor omission should succeed only when cumulative total is proven: %+v %v", page, err)
	}
}

func TestCrossrefPrematureTerminalSignalsFailClosed(t *testing.T) {
	for _, empty := range []bool{false, true} {
		fixture := adapterFixture(t, "crossref-page.json")
		fixture.Body = []byte(strings.ReplaceAll(string(fixture.Body), `"next-cursor":"next-cursor-token",`, `"total-results":2,`))
		if empty {
			fixture.Body = []byte(`{"status":"ok","message":{"total-results":2,"items":[]}}`)
		}
		page, err := NewCrossrefSource(&adapterExecutor{responses: []*httpclient.Response{fixture}}).Fetch(context.Background(), adapterWindow())
		if err == nil || page.Complete {
			t.Fatalf("declared remaining count forbids silent completion (empty=%v): %+v %v", empty, page, err)
		}
	}
}
