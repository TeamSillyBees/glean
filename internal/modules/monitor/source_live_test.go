package monitor

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/teamsillybees/glean/internal/platform/httpclient"
)

// TestLiveSourceCanary 只在显式启用时访问官方 API；每来源最多两次请求，不追完分页、不调用 LLM。
// GLEAN_LIVE_SOURCE_CANARY=1 [GLEAN_LIVE_SOURCE=pubmed] go test ./internal/modules/monitor -run '^TestLiveSourceCanary$' -v -count=1
func TestLiveSourceCanary(t *testing.T) {
	if os.Getenv("GLEAN_LIVE_SOURCE_CANARY") != "1" {
		t.Skip("real official-API canary is opt-in, not deterministic fixture coverage")
	}
	factory, e := httpclient.NewFactory(httpclient.Config{Enabled: true, Services: SourceHTTPServices(""), MaxIdleConnsPerHost: 1}, nil, SourceHTTPFactoryOptions()...)
	if e != nil {
		t.Fatal(e)
	}
	defer factory.ClearAll()
	names := DefaultSourceNames()
	if n := strings.TrimSpace(os.Getenv("GLEAN_LIVE_SOURCE")); n != "" {
		names = []string{n}
	}
	sources, e := NewHTTPSourceSet(factory, names)
	if e != nil {
		t.Fatal(e)
	}
	for _, source := range sources {
		t.Run(source.Name(), func(t *testing.T) {
			req := FetchRequest{PageSize: 1, Window: Window{Start: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)}, Template: SearchTemplate{ID: "bounded-live-canary", Version: "1", Topic: "llm-agent", Query: `"large language model"`, Terms: []string{"language"}}}
			if source.Name() == "biorxiv" || source.Name() == "medrxiv" {
				req.Template.Terms = nil
			}
			if source.Name() == "crossref" {
				req.Template.Core = true
				req.RegistryISSNs = []string{"0028-0836"}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
			defer cancel()
			page, e := source.Fetch(ctx, req)
			if e != nil {
				t.Fatalf("official source %s: %v (no retries or bypass)", source.Name(), e)
			}
			requests := 1
			if source.Name() == "pubmed" && !page.Complete {
				timer := time.NewTimer(3 * time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					t.Fatal(ctx.Err())
				case <-timer.C:
				}
				req.Cursor = page.NextCursor
				page, e = source.Fetch(ctx, req)
				requests++
				if e != nil {
					t.Fatalf("official PubMed history fetch: %v (no retries or bypass)", e)
				}
				if !page.Complete {
					cursor, e := decodePubmedCursor(page.NextCursor)
					if e != nil || cursor.Offset == 0 {
						t.Fatal("PubMed history restart/subdivision is not successful EFetch coverage")
					}
				}
			}
			if source.Name() == "pubmed" && requests == 2 && len(page.Records) == 0 {
				t.Fatal("bounded PubMed EFetch must retain at least one authoritative ESearch match")
			}
			t.Logf("official source=%s HTTP calls=%d normalized records=%d sample-page complete=%v further cursor=%v; this is NOT a full-source crawl (pageSize1 where supported; bio/med30 and OAI provider-sized pages)", source.Name(), requests, len(page.Records), page.Complete, page.NextCursor != "")
			for _, r := range page.Records {
				if r.IsMock || r.Source != source.Name() || r.SourceID == "" || r.Title == "" {
					t.Fatal("invalid real-source normalization")
				}
			}
		})
		// bioRxiv and medRxiv share a provider; keep a single connection and >=3s between provider requests.
		time.Sleep(3 * time.Second)
	}
}
