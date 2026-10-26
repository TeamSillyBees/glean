package monitor

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/teamsillybees/glean/internal/platform/httpclient"
)

// syntheticRegistryISSNs 生成校验位正确的合成 ISSN，仅测试查询长度与游标，不代表真实期刊。
func syntheticRegistryISSNs(n int) []string {
	out := make([]string, n)
	for i := range out {
		head := fmt.Sprintf("%07d", 1000000+i)
		sum := 0
		for j := 0; j < 7; j++ {
			sum += int(head[j]-'0') * (8 - j)
		}
		check := (11 - sum%11) % 11
		tail := fmt.Sprint(check)
		if check == 10 {
			tail = "X"
		}
		out[i] = head[:4] + "-" + head[4:] + tail
	}
	return out
}

func TestCoreISSNFeedsBoundGETAndFinishOnlyAfterAllBatches(t *testing.T) {
	for _, source := range []string{"pubmed", "europepmc"} {
		t.Run(source, func(t *testing.T) {
			req := adapterWindow()
			req.Template.Core = true
			req.RegistryISSNs = syntheticRegistryISSNs(125)
			want := append([]string(nil), req.RegistryISSNs...)
			sort.Sort(sort.Reverse(sort.StringSlice(req.RegistryISSNs)))
			req.RegistryISSNs = append(req.RegistryISSNs, req.RegistryISSNs[0])
			body, param, origin := `{"esearchresult":{"count":"0"}}`, "term", "https://eutils.ncbi.nlm.nih.gov"
			if source == "europepmc" {
				body, param, origin = `{"hitCount":0,"resultList":{"result":[]}}`, "query", "https://www.ebi.ac.uk"
			}
			f := &adapterExecutor{}
			for i := 0; i < 4; i++ {
				f.responses = append(f.responses, jsonResponse(body))
			}
			s := &remoteSource{name: source, executor: f}
			seen := map[string]int{}
			maxBytes := 0
			for feed := 0; feed < 4; feed++ {
				page, err := s.Fetch(context.Background(), req)
				if err != nil || page.Complete != (feed == 3) || (page.NextCursor == "") != (feed == 3) {
					t.Fatalf("feed %d completed too early/late: %+v %v", feed, page, err)
				}
				request := f.requests[feed]
				query := request.options.QueryParams.Get(param)
				ids := regexp.MustCompile(`[0-9]{4}-[0-9]{3}[0-9X]`).FindAllString(query, -1)
				wantSize := coreISSNBatchSize
				if feed == 3 {
					wantSize = 5
				}
				if len(ids) != wantSize || strings.Contains(query, req.Template.Query) {
					t.Fatalf("bad batch query: %s", query)
				}
				for _, id := range ids {
					seen[id]++
				}
				uri := origin + request.path + "?" + request.options.QueryParams.Encode()
				if len(uri) > 4096 {
					t.Fatalf("core request exceeds conservative 4KB bound: %d", len(uri))
				}
				if len(uri) > maxBytes {
					maxBytes = len(uri)
				}
				if source == "pubmed" && !strings.Contains(query, "2026/10/19:2026/10/25") {
					t.Fatal("batch lost original fixed window")
				}
				if source == "europepmc" && !strings.Contains(query, "[2026-10-19 TO 2026-10-25]") {
					t.Fatal("batch lost original fixed window")
				}
				req.Cursor = page.NextCursor
			}
			if len(seen) != len(want) {
				t.Fatal("registry coverage lost")
			}
			for _, id := range want {
				if seen[id] != 1 {
					t.Fatalf("ISSN %s queried %d times across feeds", id, seen[id])
				}
			}
			t.Logf("%d unique synthetic ISSNs across four feeds, max encoded GET URL %d bytes", len(want), maxBytes)
		})
	}
}

func TestCorePubMedFeedSurvivesExpiredHistory(t *testing.T) {
	req := adapterWindow()
	req.Template.Core, req.RegistryISSNs = true, syntheticRegistryISSNs(41)
	f := &adapterExecutor{responses: []*httpclient.Response{
		jsonResponse(`{"esearchresult":{"count":"0"}}`),
		jsonResponse(`{"esearchresult":{"count":"1","webenv":"expired","querykey":"1"}}`),
		jsonResponse(`<PubmedArticleSet><ERROR>expired</ERROR></PubmedArticleSet>`),
		jsonResponse(`{"esearchresult":{"count":"1","webenv":"fresh","querykey":"1"}}`),
		adapterFixture(t, "pubmed-fetch.xml"),
	}}
	for step := 0; step < len(f.responses)+len(f.requests); step++ {
		page, err := NewPubMedSource(f).Fetch(context.Background(), req)
		if err != nil || page.Complete != (step == 4) {
			t.Fatalf("step %d %+v %v", step, page, err)
		}
		if !page.Complete {
			c, err := decodePubmedCursor(page.NextCursor)
			if err != nil || c.CoreFeed != 1 || c.CoreBatchSize != 40 || c.CoreRegistry == "" {
				t.Fatalf("feed scope lost on restart: %+v %v", c, err)
			}
		}
		req.Cursor = page.NextCursor
	}
	if f.requests[1].options.QueryParams.Get("term") != f.requests[3].options.QueryParams.Get("term") {
		t.Fatal("history restart changed the current ISSN feed")
	}
}

func TestCoreEuropePMCFeedSurvivesChangedCountRestart(t *testing.T) {
	req := adapterWindow()
	req.Template.Core, req.RegistryISSNs = true, syntheticRegistryISSNs(41)
	f := &adapterExecutor{responses: []*httpclient.Response{
		jsonResponse(`{"hitCount":0,"resultList":{"result":[]}}`),
		jsonResponse(`{"hitCount":2,"nextCursorMark":"next","resultList":{"result":[{"id":"synthetic-one","source":"MED","title":"Unrelated journal item"}]}}`),
		jsonResponse(`{"hitCount":3,"nextCursorMark":"changed","resultList":{"result":[{"id":"synthetic-two","source":"MED","title":"Unrelated journal item"}]}}`),
		jsonResponse(`{"hitCount":1,"resultList":{"result":[{"id":"synthetic-final","source":"MED","title":"Unrelated journal item"}]}}`),
	}}
	for step := 0; step < 4; step++ {
		page, err := NewEuropePMCSource(f).Fetch(context.Background(), req)
		if err != nil || page.Complete != (step == 3) {
			t.Fatalf("step %d %+v %v", step, page, err)
		}
		if !page.Complete {
			c, err := decodeEuropePMCCursor(page.NextCursor)
			if err != nil || c.CoreFeed != 1 || c.CoreBatchSize != 40 || c.CoreRegistry == "" {
				t.Fatalf("feed scope lost on restart: %+v %v", c, err)
			}
		}
		req.Cursor = page.NextCursor
	}
	for _, i := range []int{2, 3} {
		if f.requests[i].options.QueryParams.Get("query") != f.requests[1].options.QueryParams.Get("query") {
			t.Fatal("changed-count restart skipped current ISSN feed")
		}
	}
	if f.requests[3].options.QueryParams.Get("cursorMark") != "*" {
		t.Fatal("changed count must replay current feed")
	}
}

func TestCoreBatchRegistryValidationAndOldCursorReplay(t *testing.T) {
	for _, source := range []string{"pubmed", "europepmc"} {
		req := adapterWindow()
		req.Template.Core, req.RegistryISSNs = true, syntheticRegistryISSNs(101)
		body := `{"esearchresult":{"count":"0"}}`
		req.Cursor = (pubmedCursor{CoreFull: true, Count: 1, WebEnv: "old-unbatched", Key: "1", Pending: []pubmedPartition{{From: "2026-10-24", Until: "2026-10-25"}}}).encode()
		if source == "europepmc" {
			body = `{"hitCount":0,"resultList":{"result":[]}}`
			req.Cursor = (europePMCCursor{CoreFull: true, Mark: "old-unbatched", Seen: 1, Total: 2}).encode()
		}
		f := &adapterExecutor{responses: []*httpclient.Response{jsonResponse(body)}}
		page, err := (&remoteSource{name: source, executor: f}).Fetch(context.Background(), req)
		if err != nil || page.Complete || page.NextCursor == "" {
			t.Fatalf("old unbatched scope must restart, not skip feeds: %+v %v", page, err)
		}
		if source == "pubmed" && !strings.Contains(f.requests[0].options.QueryParams.Get("term"), "2026/10/19:2026/10/25") {
			t.Fatal("lost original lower bound")
		}
		if source == "europepmc" && f.requests[0].options.QueryParams.Get("cursorMark") != "*" {
			t.Fatal("reused old unbatched cursorMark")
		}
		req.Cursor = page.NextCursor
		req.RegistryISSNs = append(req.RegistryISSNs, "invalid-ISSN")
		f = &adapterExecutor{}
		page, err = (&remoteSource{name: source, executor: f}).Fetch(context.Background(), req)
		if err == nil || page.Complete || page.NextCursor != "" || len(f.requests) != 0 {
			t.Fatalf("invalid registry must fail before HTTP: %+v %v", page, err)
		}
	}
}
