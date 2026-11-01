package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/teamsillybees/glean/internal/platform/httpclient"
)

func rxivPage(offset, total int, ids ...string) *httpclient.Response {
	records := make([]map[string]string, 0, len(ids))
	for _, id := range ids {
		records = append(records, map[string]string{"doi": "10.5555/" + id, "title": "language agents " + id, "date": "2026-10-20", "version": "1"})
	}
	body, _ := json.Marshal(map[string]any{"messages": []map[string]any{{"status": "ok", "cursor": offset, "total": total, "count": len(ids)}}, "collection": records})
	return &httpclient.Response{StatusCode: 200, Body: body}
}

func TestRxivTotalShrinkCannotCompleteSkippedRecord(t *testing.T) {
	for _, name := range []string{"biorxiv", "medrxiv"} {
		t.Run(name, func(t *testing.T) {
			f := &adapterExecutor{responses: []*httpclient.Response{rxivPage(0, 4, "a", "b"), rxivPage(2, 3, "d")}}
			source := &remoteSource{name: name, executor: f}
			req := adapterWindow()
			first, err := source.Fetch(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			req.Cursor = first.NextCursor
			second, err := source.Fetch(context.Background(), req)
			if err != nil || second.Complete || second.NextCursor == "" || len(second.Records) != 0 {
				t.Fatalf("changed total must replay, not complete while skipping c: %+v %v", second, err)
			}
		})
	}
}

func rxivOffset(t *testing.T, value string) int {
	t.Helper()
	c, err := decodeRxivCursor(value)
	if err != nil {
		t.Fatal(err)
	}
	return c.Offset
}

func TestRxivPaginationDriftReplayAndStableCompletion(t *testing.T) {
	for _, name := range []string{"biorxiv", "medrxiv"} {
		for _, total := range []int{0, 1, 3, 5} {
			t.Run(fmt.Sprintf("%s/total%d", name, total), func(t *testing.T) {
				changed := rxivPage(2, total)
				if total >= 3 {
					changed = rxivPage(2, total, "d")
				}
				f := &adapterExecutor{responses: []*httpclient.Response{rxivPage(0, 4, "a", "b"), changed, rxivPage(0, 3, "b", "c"), rxivPage(2, 3, "d")}}
				source := &remoteSource{name: name, executor: f}
				req := adapterWindow()
				for step := 0; step < 4; step++ {
					page, err := source.Fetch(context.Background(), req)
					if err != nil {
						t.Fatal(err)
					}
					if page.Complete != (step == 3) {
						t.Fatalf("step%d premature/incomplete: %+v", step, page)
					}
					if step == 1 {
						c, e := decodeRxivCursor(page.NextCursor)
						if e != nil || c.Offset != 0 || c.Total != -1 || c.Restarts != 1 || len(page.Records) != 0 {
							t.Fatalf("bad replay: %+v %v", c, e)
						}
					}
					if step == 2 && (len(page.Records) != 2 || page.Records[1].SourceID != "10.5555/c") {
						t.Fatalf("replay must recover the skipped record c: %+v", page.Records)
					}
					req.Cursor = page.NextCursor
				}
				for i, offset := range []int{0, 2, 0, 2} {
					if got, want := f.requests[i].path, fmt.Sprintf("/details/%s/2026-10-19/2026-10-25/%d/json", name, offset); got != want {
						t.Fatalf("internal cursor leaked: %s != %s", got, want)
					}
				}
			})
		}
	}
}

func TestRxivRestartBudgetAndLegacyCursor(t *testing.T) {
	for _, name := range []string{"biorxiv", "medrxiv"} {
		t.Run(name, func(t *testing.T) {
			f := &adapterExecutor{}
			source := &remoteSource{name: name, executor: f}
			req := adapterWindow()
			req.Cursor = "2"
			page, err := source.Fetch(context.Background(), req)
			if err != nil || page.Complete || len(f.requests) != 0 {
				t.Fatalf("legacy offset must replay without trusting old prefix: %+v %v", page, err)
			}
			c, err := decodeRxivCursor(page.NextCursor)
			if err != nil || c.Offset != 0 || c.Total != -1 || c.Restarts != 1 {
				t.Fatalf("bad legacy replay %+v %v", c, err)
			}
			req.Cursor = page.NextCursor
			for restart := 1; restart <= 3; restart++ {
				f.responses = append(f.responses, rxivPage(0, 4, "a", "b"), rxivPage(2, 3, "d"))
				page, err = source.Fetch(context.Background(), req)
				if err != nil || page.Complete {
					t.Fatalf("stable first page %+v %v", page, err)
				}
				c, err = decodeRxivCursor(page.NextCursor)
				if err != nil || c.Restarts != restart {
					t.Fatalf("budget reset %+v %v", c, err)
				}
				req.Cursor = page.NextCursor
				page, err = source.Fetch(context.Background(), req)
				if restart == 3 {
					if err == nil || page.Complete || page.NextCursor != "" {
						t.Fatalf("exhaustion must fail closed %+v %v", page, err)
					}
				} else {
					if err != nil || page.Complete {
						t.Fatalf("restart %+v %v", page, err)
					}
					req.Cursor = page.NextCursor
				}
			}
		})
	}
}

func TestRxivMalformedCursorsNeverRequestOrComplete(t *testing.T) {
	invalid := []string{"-1", "+1", " 1", "1.5", "99999999999999999999", "rx2:abc", "rx1:!", "rx1:e30", "rx1:bnVsbA"}
	for _, c := range []rxivCursor{{Offset: -1, Total: 4}, {Offset: 2, Total: -1}, {Offset: 0, Total: -2}, {Offset: 0, Total: 0}, {Offset: 0, Total: 4}, {Offset: 4, Total: 4}, {Offset: 2, Total: 4, Restarts: -1}, {Offset: 2, Total: 4, Restarts: 4}} {
		invalid = append(invalid, c.encode())
	}
	for _, name := range []string{"biorxiv", "medrxiv"} {
		for _, value := range invalid {
			t.Run(name+"/"+value, func(t *testing.T) {
				f := &adapterExecutor{}
				req := adapterWindow()
				req.Cursor = value
				page, err := (&remoteSource{name: name, executor: f}).Fetch(context.Background(), req)
				if err == nil || page.Complete || len(f.requests) != 0 {
					t.Fatalf("malformed cursor requested/completed: %+v %v", page, err)
				}
			})
		}
	}
}

func TestRxivExplicitEmptyContinuationReplaysBeforeCompletion(t *testing.T) {
	for _, name := range []string{"biorxiv", "medrxiv"} {
		t.Run(name, func(t *testing.T) {
			empty := &httpclient.Response{StatusCode: 200, Body: []byte(`{"messages":[{"status":"no posts found"}],"collection":[]}`)}
			f := &adapterExecutor{responses: []*httpclient.Response{empty, empty}}
			source := &remoteSource{name: name, executor: f}
			req := adapterWindow()
			req.Cursor = (rxivCursor{Offset: 2, Total: 4}).encode()
			page, err := source.Fetch(context.Background(), req)
			if err != nil || page.Complete || page.NextCursor == "" {
				t.Fatalf("empty continuation silently completed: %+v %v", page, err)
			}
			req.Cursor = page.NextCursor
			page, err = source.Fetch(context.Background(), req)
			if err != nil || !page.Complete || page.NextCursor != "" || len(page.Records) != 0 {
				t.Fatalf("empty replay must complete at offset0: %+v %v", page, err)
			}
		})
	}
}
