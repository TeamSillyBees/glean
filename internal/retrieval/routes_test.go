package retrieval

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCompileSourceSemantics(t *testing.T) {
	groups := [][]string{{"AI", "machine learning"}, {"medicine"}}
	tests := []struct{ source, field, mode, query string }{
		{"pubmed", "default", "boolean", `("AI" OR "machine learning") AND ("medicine")`},
		{"europepmc", "", "boolean", `("AI" OR "machine learning") AND ("medicine")`},
		{"pubmed", "title_abstract", "boolean", `("AI"[tiab] OR "machine learning"[tiab]) AND ("medicine"[tiab])`},
		{"europepmc", "title_abstract", "boolean", `((TITLE:"AI" OR ABSTRACT:"AI") OR (TITLE:"machine learning" OR ABSTRACT:"machine learning")) AND ((TITLE:"medicine" OR ABSTRACT:"medicine"))`},
		{"crossref", "default", "seed_union", `Seed union (separate bibliographic requests): "AI" | "machine learning"`},
		{"arxiv", "default", "full_window", ""}, {"biorxiv", "default", "full_window", ""}, {"medrxiv", "default", "full_window", ""},
	}
	for _, tt := range tests {
		t.Run(tt.source+tt.field, func(t *testing.T) {
			result, err := Compile(tt.source, groups, tt.field)
			if err != nil || result.Mode != tt.mode || result.Query != tt.query {
				t.Fatalf("unexpected compile: %+v %v", result, err)
			}
			again, _ := Compile(tt.source, groups, tt.field)
			if !reflect.DeepEqual(result, again) {
				t.Fatal("nondeterministic compiler")
			}
			if tt.mode != "boolean" && len(result.Warnings) == 0 {
				t.Fatal("missing capability warning")
			}
			if tt.source == "crossref" && !reflect.DeepEqual(result.Queries, groups[0]) {
				t.Fatal("incorrect seed union")
			}
		})
	}
	for _, source := range []string{"crossref", "arxiv", "biorxiv", "medrxiv", "https://attacker.example"} {
		if _, err := Compile(source, groups, "title_abstract"); err == nil {
			t.Fatalf("unsupported source/mode accepted: %s", source)
		}
	}
	if _, err := Compile("pubmed", groups, "title"); err == nil {
		t.Fatal("unknown mode accepted")
	}
}
func TestCompileQuotesCannotBecomeRawSyntax(t *testing.T) {
	result, err := Compile("europepmc", [][]string{{`a" OR TITLE:*`, `C:\path`}}, "title_abstract")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Query, `TITLE:"a\" OR TITLE:*"`) || !strings.Contains(result.Query, `TITLE:"C:\\path"`) {
		t.Fatalf("unquoted terms: %s", result.Query)
	}
}
func TestRouteValidationAndNormalization(t *testing.T) {
	now := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	valid := func() Route {
		return Route{ID: "route-1", Sources: []string{"pubmed", "europepmc"}, TermGroups: [][]string{{"AI", "AI"}, {"medicine"}}, BackfillFrom: &now}
	}
	if err := ValidateRoutes([]Route{valid()}, now); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*Route)
	}{
		{"unsafe ID", func(r *Route) { r.ID = "../x" }},
		{"empty sources", func(r *Route) { r.Sources = nil }},
		{"unknown source", func(r *Route) { r.Sources = []string{"future"} }},
		{"duplicate source", func(r *Route) { r.Sources = []string{"pubmed", "pubmed"} }},
		{"empty groups", func(r *Route) { r.TermGroups = nil }},
		{"empty group", func(r *Route) { r.TermGroups = [][]string{{}} }},
		{"blank term", func(r *Route) { r.TermGroups = [][]string{{" "}} }},
		{"long term", func(r *Route) { r.TermGroups = [][]string{{strings.Repeat("x", 301)}} }},
		{"control", func(r *Route) { r.TermGroups = [][]string{{"x\ty"}} }},
		{"unicode control", func(r *Route) { r.TermGroups = [][]string{{"x\u0085y"}} }},
		{"invalid utf8", func(r *Route) { r.TermGroups = [][]string{{string([]byte{255})}} }},
		{"many groups", func(r *Route) { r.TermGroups = make([][]string, 11) }},
		{"many terms", func(r *Route) {
			r.TermGroups = [][]string{make([]string, 51)}
			for i := range r.TermGroups[0] {
				r.TermGroups[0][i] = "term"
			}
		}},
		{"future", func(r *Route) { v := now.Add(time.Second); r.BackfillFrom = &v }},
		{"zero date", func(r *Route) { r.BackfillFrom = &time.Time{} }},
		{"unsupported fields", func(r *Route) { r.Sources = []string{"crossref"}; r.FieldMode = "title_abstract" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := valid()
			tt.mutate(&r)
			if ValidateRoutes([]Route{r}, now) == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
	if ValidateRoutes([]Route{valid(), valid()}, now) == nil {
		t.Fatal("duplicate IDs accepted")
	}
	if ValidateRoutes(make([]Route, 21), now) == nil {
		t.Fatal("many routes accepted")
	}
	if ValidateRoutes(nil, now) != nil {
		t.Fatal("legacy route absence rejected")
	}
	input := []Route{valid()}
	normalized := NormalizeRoutes(input)
	if normalized[0].Sources[0] != "europepmc" || len(normalized[0].TermGroups[0]) != 1 {
		t.Fatal("normalization mismatch")
	}
	normalized[0].Sources[0] = "changed"
	normalized[0].TermGroups[0][0] = "changed"
	*normalized[0].BackfillFrom = time.Time{}
	if input[0].Sources[0] != "pubmed" || input[0].TermGroups[0][0] != "AI" || input[0].BackfillFrom.IsZero() {
		t.Fatal("aliased input")
	}
}

// TestRouteLimitsAcceptBoundary 验证按 UTF-8 字节限制并覆盖字段展开后的证据长度预算。
func TestRouteLimitsAcceptBoundary(t *testing.T) {
	groups := make([][]string, 10)
	for i := range groups {
		groups[i] = make([]string, 5)
		for j := range groups[i] {
			groups[i][j] = strings.Repeat("\\", 300)
		}
	}
	compiled, err := Compile("europepmc", groups, "title_abstract")
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Query) <= 20000 || len(compiled.Query) > 128*1024 {
		t.Fatalf("compiled text budget mismatch: %d", len(compiled.Query))
	}
	for _, term := range []string{strings.Repeat("字", 100), strings.Repeat("x", 300)} {
		if _, err := Compile("pubmed", [][]string{{term}}, "default"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Compile("pubmed", [][]string{{strings.Repeat("字", 101)}}, "default"); err == nil {
		t.Fatal("byte limit treated as character count")
	}
}
