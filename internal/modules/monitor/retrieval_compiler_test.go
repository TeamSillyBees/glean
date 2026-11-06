package monitor

import (
	"context"
	"github.com/teamsillybees/glean/internal/platform/httpclient"
	"strings"
	"testing"
)

// TestRouteCompilerUsedByAdapters 验证预览与实际来源字段一致，且不支持的模式在网络前拒绝。
func TestRouteCompilerUsedByAdapters(t *testing.T) {
	for _, tt := range []struct{ source, key, expected string }{
		{"pubmed", "term", `("AI"[tiab]) AND ("medicine"[tiab])`},
		{"europepmc", "query", `((TITLE:"AI" OR ABSTRACT:"AI")) AND ((TITLE:"medicine" OR ABSTRACT:"medicine"))`},
	} {
		t.Run(tt.source, func(t *testing.T) {
			response := `{"esearchresult":{"count":"0"}}`
			if tt.source == "europepmc" {
				response = `{"hitCount":0,"resultList":{"result":[]}}`
			}
			executor := &adapterExecutor{responses: []*httpclient.Response{{StatusCode: 200, Body: []byte(response)}}}
			req := adapterWindow()
			req.Template.TermGroups = [][]string{{"AI"}, {"medicine"}}
			req.Template.FieldMode = "title_abstract"
			req.Template.Query = "wrong legacy query"
			if _, err := (&remoteSource{name: tt.source, executor: executor}).Fetch(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			if len(executor.requests) != 1 || !strings.Contains(executor.requests[0].options.QueryParams.Get(tt.key), tt.expected) {
				t.Fatalf("wrong upstream query: %+v", executor.requests)
			}
		})
	}
	for _, name := range []string{"crossref", "arxiv", "biorxiv", "medrxiv"} {
		t.Run(name, func(t *testing.T) {
			executor := &adapterExecutor{}
			req := adapterWindow()
			req.Template.TermGroups = [][]string{{"AI"}}
			req.Template.FieldMode = "title_abstract"
			if _, err := (&remoteSource{name: name, executor: executor}).Fetch(context.Background(), req); err == nil {
				t.Fatal("unsupported field accepted")
			}
			if len(executor.requests) != 0 {
				t.Fatal("invalid query sent HTTP")
			}
		})
	}
}
func TestLegacyAdapterQueryPreserved(t *testing.T) {
	executor := &adapterExecutor{responses: []*httpclient.Response{{StatusCode: 200, Body: []byte(`{"esearchresult":{"count":"0"}}`)}}}
	req := adapterWindow()
	req.Template.Query = `legacy-special-query[all]`
	if _, err := NewPubMedSource(executor).Fetch(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(executor.requests[0].options.QueryParams.Get("term"), "("+req.Template.Query+") AND ") {
		t.Fatal("legacy query changed")
	}
}
func TestCrossrefRouteUsesSeedUnion(t *testing.T) {
	executor := &adapterExecutor{responses: []*httpclient.Response{{StatusCode: 200, Body: []byte(`{"status":"ok","message":{"items":[],"total-results":0}}`)}, {StatusCode: 200, Body: []byte(`{"status":"ok","message":{"items":[],"total-results":0}}`)}}}
	req := adapterWindow()
	req.Template.FieldMode = "default"
	req.Template.TermGroups = [][]string{{"first", "second"}, {"medicine"}}
	source := NewCrossrefSource(executor)
	page, err := source.Fetch(context.Background(), req)
	if err != nil || page.Complete || page.NextCursor == "" {
		t.Fatalf("first seed: %+v %v", page, err)
	}
	req.Cursor = page.NextCursor
	page, err = source.Fetch(context.Background(), req)
	if err != nil || !page.Complete {
		t.Fatalf("second seed: %+v %v", page, err)
	}
	for i, seed := range []string{"first", "second"} {
		if executor.requests[i].options.QueryParams.Get("query.bibliographic") != seed {
			t.Fatal("incorrect seed union")
		}
	}
}
