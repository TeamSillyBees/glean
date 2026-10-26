package monitor

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/teamsillybees/glean/internal/platform/httpclient"
)

// capturedResponse 只读取原样保存的公开 API 响应；fixture 测试不等于当前网络可用性验证。
func capturedResponse(t *testing.T, name string) *httpclient.Response {
	t.Helper()
	f, e := os.Open(filepath.Join("testdata/live", name+".gz"))
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	z, e := gzip.NewReader(f)
	if e != nil {
		t.Fatal(e)
	}
	defer z.Close()
	b, e := io.ReadAll(z)
	if e != nil {
		t.Fatal(e)
	}
	return &httpclient.Response{StatusCode: 200, Body: b}
}

func TestCapturedPublicSourceResponseHashes(t *testing.T) {
	b, e := os.ReadFile("testdata/live/manifest.json")
	if e != nil {
		t.Fatal(e)
	}
	var m []struct {
		File   string `json:"file"`
		SHA    string `json:"sha256"`
		Bytes  int    `json:"bytes"`
		Status int    `json:"status"`
	}
	if e = json.Unmarshal(b, &m); e != nil {
		t.Fatal(e)
	}
	for _, v := range m {
		t.Run(v.File, func(t *testing.T) {
			r := capturedResponse(t, v.File[:len(v.File)-3])
			sum := sha256.Sum256(r.Body)
			if len(r.Body) != v.Bytes || hex.EncodeToString(sum[:]) != v.SHA || v.Status != 200 {
				t.Fatal("captured data must not be silently edited")
			}
		})
	}
}

func TestCapturedCrossrefJournalResponse(t *testing.T) {
	req := adapterWindow()
	req.Window.Start = time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	req.Window.End = req.Window.Start.Add(24 * time.Hour)
	req.Template.Core = true
	req.RegistryISSNs = []string{"0028-0836"}
	f := &adapterExecutor{responses: []*httpclient.Response{capturedResponse(t, "crossref-live.json")}}
	page, e := NewCrossrefSource(f).Fetch(context.Background(), req)
	if e != nil {
		t.Fatal(e)
	}
	if len(page.Records) != 1 || page.Complete || page.NextCursor == "" || page.Records[0].IsMock {
		t.Fatalf("captured one-of-3638 sample must stay incomplete: %+v", page)
	}
}

func TestCapturedPubMedSearchAndFetch(t *testing.T) {
	req := adapterWindow()
	f := &adapterExecutor{responses: []*httpclient.Response{capturedResponse(t, "pubmed-search-live.json"), capturedResponse(t, "pubmed-fetch-live.xml")}}
	s := NewPubMedSource(f)
	page, e := s.Fetch(context.Background(), req)
	if e != nil {
		t.Fatal(e)
	}
	req.Cursor = page.NextCursor
	page, e = s.Fetch(context.Background(), req)
	if e != nil {
		t.Fatal(e)
	}
	if len(page.Records) != 1 || page.Records[0].SourceID != "42920233" || page.Complete || len(page.Records[0].Authors) == 0 {
		t.Fatalf("one fetched item is not entire 144-hit history: %+v", page)
	}
}

func TestCapturedEuropePMCRejectsNormalizedIssuePrecision(t *testing.T) {
	req := adapterWindow()
	f := &adapterExecutor{responses: []*httpclient.Response{capturedResponse(t, "europepmc-core-live.json")}}
	page, e := NewEuropePMCSource(f).Fetch(context.Background(), req)
	if e != nil {
		t.Fatal(e)
	}
	if len(page.Records) != 1 || page.Complete {
		t.Fatalf("one captured result is incomplete: %+v", page)
	}
	r := page.Records[0]
	if r.SourceID != "MED:42886459" || len(r.ISSNs) == 0 {
		t.Fatalf("wrong identity/ISSN: %+v", r)
	}
	for _, v := range r.PublicationEvents {
		if v.Kind == "issue" {
			t.Fatal("real API dateOfPublication 2026 Oct cannot become exact 2026-10-01 issue")
		}
	}
}

func TestCapturedEuropePMCIncrementalDateQuery(t *testing.T) {
	page, e := NewEuropePMCSource(&adapterExecutor{responses: []*httpclient.Response{capturedResponse(t, "europepmc-incremental-live.json")}}).Fetch(context.Background(), adapterWindow())
	if e != nil || len(page.Records) != 1 || page.Complete {
		t.Fatalf("actual FIRST_IDATE/UPDATE_DATE response: %+v %v", page, e)
	}
}

func TestCapturedBioAndMedRxivUseActualThirtyItemPages(t *testing.T) {
	for _, tc := range []struct {
		Name    string
		Factory func(httpclient.Executor) Source
	}{{"biorxiv", NewBioRxivSource}, {"medrxiv", NewMedRxivSource}} {
		t.Run(tc.Name, func(t *testing.T) {
			req := adapterWindow()
			req.Template.Terms = nil
			req.Template.TermGroups = nil
			page, e := tc.Factory(&adapterExecutor{responses: []*httpclient.Response{capturedResponse(t, tc.Name+"-live.json")}}).Fetch(context.Background(), req)
			if e != nil {
				t.Fatal(e)
			}
			if len(page.Records) != 30 || page.NextCursor != "30" || page.Complete {
				t.Fatalf("provider returns 30 not hard-coded 100: %+v", page)
			}
			for _, r := range page.Records {
				for _, p := range r.PublicationEvents {
					if p.Kind != "preprint" {
						t.Fatal("source posted dates cannot become journal publication")
					}
				}
			}
		})
	}
}

func TestCapturedArxivRawAllVersionHistory(t *testing.T) {
	response := capturedResponse(t, "arxiv-raw.xml")
	var r struct {
		Records []arxivOAIRecord `xml:"GetRecord>record"`
	}
	if e := xml.Unmarshal(response.Body, &r); e != nil {
		t.Fatal(e)
	}
	if len(r.Records) != 1 || r.Records[0].Metadata.Raw.ID != "1706.03762" || len(r.Records[0].Metadata.Raw.Versions) != 7 {
		t.Fatal("actual OAI GetRecord includes all seven historical versions")
	}
}

func TestCapturedArxivOAIListParser(t *testing.T) {
	// 捕获请求有 cs:cs:AI 子集；这里只验证真实 OAI 解析，不能宣称该样本覆盖全 arXiv。
	req := adapterWindow()
	req.Window.Start = time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	req.Window.End = req.Window.Start.Add(24 * time.Hour)
	req.Template.Terms = nil
	req.Template.TermGroups = nil
	page, e := NewArXivSource(&adapterExecutor{responses: []*httpclient.Response{capturedResponse(t, "arxiv-list-live.xml")}}).Fetch(context.Background(), req)
	if e != nil {
		t.Fatal(e)
	}
	if len(page.Records) != 363 || !page.Complete {
		t.Fatalf("actual 363-item OAI subset list failed normalization: records=%d complete=%v", len(page.Records), page.Complete)
	}
}

func TestCapturedBioRxivExplicitEmptyInterval(t *testing.T) {
	req := adapterWindow()
	req.Window.Start = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
	req.Window.End = req.Window.Start.Add(24 * time.Hour)
	page, e := NewBioRxivSource(&adapterExecutor{responses: []*httpclient.Response{capturedResponse(t, "biorxiv-empty-live.json")}}).Fetch(context.Background(), req)
	if e != nil || !page.Complete || len(page.Records) != 0 {
		t.Fatalf("official no posts found response should complete empty interval: %+v %v", page, e)
	}
	req.Cursor = "30"
	page, e = NewBioRxivSource(&adapterExecutor{responses: []*httpclient.Response{capturedResponse(t, "biorxiv-empty-live.json")}}).Fetch(context.Background(), req)
	if e == nil || page.Complete {
		t.Fatal("empty continuation cannot erase prior pagination evidence")
	}
}

func TestCapturedEuropePMCMixedNumericPreprintVersions(t *testing.T) {
	req := adapterWindow()
	req.Window.Start = time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	req.Window.End = req.Window.Start.Add(24 * time.Hour)
	page, e := NewEuropePMCSource(&adapterExecutor{responses: []*httpclient.Response{capturedResponse(t, "europepmc-day100-live.json")}}).Fetch(context.Background(), req)
	if e != nil {
		t.Fatal(e)
	}
	if len(page.Records) != 100 || page.Complete || page.NextCursor == "" {
		t.Fatalf("actual mixed 100/199 page: %d complete=%v", len(page.Records), page.Complete)
	}
	preprints := 0
	for _, r := range page.Records {
		if r.Version != "" {
			preprints++
			for _, v := range r.PublicationEvents {
				if v.Kind != "preprint" {
					t.Fatal("numeric-version PPR cannot gain formal events")
				}
			}
		}
		if r.SourceURL == "" {
			t.Fatal("metadata source URL required")
		}
	}
	if preprints != 9 {
		t.Fatalf("must normalize nine integer-version preprints, got %d", preprints)
	}
}
