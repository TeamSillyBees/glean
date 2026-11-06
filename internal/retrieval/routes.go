// Package retrieval 定义有限、可审计且不发送网络请求的检索计划。
package retrieval

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Route 描述一条召回路线；路线取并集，组间 AND、组内 OR。
type Route struct {
	ID           string     `json:"id"`
	Name         string     `json:"name,omitempty"`
	Sources      []string   `json:"sources"`
	TermGroups   [][]string `json:"termGroups"`
	FieldMode    string     `json:"fieldMode,omitempty"`
	BackfillFrom *time.Time `json:"backfillFrom,omitempty"`
}

// Compiled 表示来源实际支持的查询方式，不代表单次精确 HTTP 请求。
type Compiled struct {
	Source   string   `json:"source"`
	Mode     string   `json:"mode"`
	Query    string   `json:"query,omitempty"`
	Queries  []string `json:"queries,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

var routeID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,79}$`)

// NormalizeRoutes 深复制并规范来源顺序、词项空白；调用方须先校验原输入。
func NormalizeRoutes(routes []Route) []Route {
	if routes == nil {
		return nil
	}
	out := make([]Route, len(routes))
	for i, r := range routes {
		out[i] = r
		out[i].Name = strings.TrimSpace(r.Name)
		out[i].Sources = unique(r.Sources)
		sort.Strings(out[i].Sources)
		out[i].TermGroups = make([][]string, len(r.TermGroups))
		for j, g := range r.TermGroups {
			out[i].TermGroups[j] = unique(g)
		}
		if r.BackfillFrom != nil {
			v := r.BackfillFrom.UTC()
			out[i].BackfillFrom = &v
		}
	}
	return out
}
func unique(values []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, s := range values {
		s = strings.TrimSpace(s)
		if !seen[s] {
			out = append(out, s)
			seen[s] = true
		}
	}
	return out
}
func invalidText(s string) bool {
	return !utf8.ValidString(s) || strings.ContainsFunc(s, unicode.IsControl)
}

// ValidateRoutes 校验声明式路线；空路线继续使用旧主题配置。
func ValidateRoutes(routes []Route, now time.Time) error {
	if len(routes) > 20 {
		return fmt.Errorf("at most 20 search routes are allowed")
	}
	seen := map[string]bool{}
	for _, r := range routes {
		if !routeID.MatchString(r.ID) || seen[r.ID] {
			return fmt.Errorf("search route IDs must be unique safe slugs (1-80 characters)")
		}
		seen[r.ID] = true
		if len(r.Name) > 300 || invalidText(r.Name) {
			return fmt.Errorf("route %s has an invalid name", r.ID)
		}
		if len(r.Sources) == 0 || len(r.Sources) > 6 {
			return fmt.Errorf("route %s requires 1-6 known sources", r.ID)
		}
		sources := map[string]bool{}
		for _, source := range r.Sources {
			if sources[source] {
				return fmt.Errorf("route %s has duplicate sources", r.ID)
			}
			sources[source] = true
			if _, err := Compile(source, r.TermGroups, r.FieldMode); err != nil {
				return fmt.Errorf("route %s: %w", r.ID, err)
			}
		}
		if r.BackfillFrom != nil && (r.BackfillFrom.IsZero() || r.BackfillFrom.Year() < 1000 || r.BackfillFrom.After(now)) {
			return fmt.Errorf("route %s backfill date must be a nonfuture date", r.ID)
		}
	}
	return nil
}
func validateGroups(groups [][]string) error {
	if len(groups) == 0 || len(groups) > 10 {
		return fmt.Errorf("search requires 1-10 term groups")
	}
	total := 0
	for _, g := range groups {
		if len(g) == 0 {
			return fmt.Errorf("search term groups cannot be empty")
		}
		total += len(g)
		for _, term := range g {
			if strings.TrimSpace(term) == "" || len(term) > 300 || invalidText(term) {
				return fmt.Errorf("search terms must be nonempty, at most 300 bytes and contain no control characters")
			}
		}
	}
	if total > 50 {
		return fmt.Errorf("search routes allow at most 50 terms")
	}
	return nil
}

// Compile 只编译受支持的来源语义；不接收任意 URL，不发送网络请求。
func Compile(source string, groups [][]string, fieldMode string) (Compiled, error) {
	result := Compiled{Source: source}
	switch source {
	case "crossref", "pubmed", "europepmc", "arxiv", "biorxiv", "medrxiv":
	default:
		return result, fmt.Errorf("unknown search source %q", source)
	}
	if fieldMode != "" && fieldMode != "default" && fieldMode != "title_abstract" {
		return result, fmt.Errorf("unsupported search field mode %q", fieldMode)
	}
	if err := validateGroups(groups); err != nil {
		return result, err
	}
	if fieldMode == "title_abstract" && source != "pubmed" && source != "europepmc" {
		return result, fmt.Errorf("source %s does not support title_abstract search", source)
	}
	switch source {
	case "crossref":
		result.Mode = "seed_union"
		result.Queries = unique(groups[0])
		seeds := make([]string, 0, len(result.Queries))
		for _, seed := range result.Queries {
			seeds = append(seeds, strconv.Quote(seed))
		}
		result.Query = "Seed union (separate bibliographic requests): " + strings.Join(seeds, " | ")
		result.Warnings = []string{"Crossref uses one bibliographic seed query per first-group term and unions the results; it does not enforce Boolean groups or title/abstract fields. All returned candidates are retained for downstream topic-relevance review. This readable route expression is not a single exact HTTP request."}
		return result, nil
	case "arxiv", "biorxiv", "medrxiv":
		result.Mode = "full_window"
		result.Warnings = []string{"This source harvests the full date window without server-side keyword filtering; all returned candidates are retained for downstream topic-relevance review and are not confirmed keyword matches."}
		return result, nil
	}
	result.Mode = "boolean"
	compiled := []string{}
	for _, g := range groups {
		terms := []string{}
		for _, term := range g {
			q := strconv.Quote(strings.TrimSpace(term))
			if fieldMode == "title_abstract" {
				if source == "pubmed" {
					q += "[tiab]"
				} else {
					q = "(TITLE:" + q + " OR ABSTRACT:" + q + ")"
				}
			}
			terms = append(terms, q)
		}
		compiled = append(compiled, "("+strings.Join(terms, " OR ")+")")
	}
	result.Query = strings.Join(compiled, " AND ")
	return result, nil
}
