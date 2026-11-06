package monitor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/retrieval"
)

// routeSource 记录真实执行意图，以相同来源记录验证多路线去重与分页恢复。
type routeSource struct {
	requests []FetchRequest
	twoPages bool
	failure  error
}

func (s *routeSource) Name() string { return "pubmed" }
func (s *routeSource) IsMock() bool { return true }
func (s *routeSource) Fetch(_ context.Context, r FetchRequest) (Page, error) {
	s.requests = append(s.requests, r)
	if s.failure != nil {
		return Page{}, s.failure
	}
	record := Record{SourceID: "same-paper", Title: "Shared discovery", Version: "1"}
	if s.twoPages && r.Cursor == "" {
		return Page{Records: []Record{record}, NextCursor: "next"}, nil
	}
	return Page{Records: []Record{record}, Complete: true}, nil
}
func routeTemplate(routes ...retrieval.Route) SearchTemplate {
	t := TopicSearchTemplate("route-topic", nil, 1)
	t.Routes = routes
	return t
}
func testRoute(id, term string) retrieval.Route {
	return retrieval.Route{ID: id, Sources: []string{"pubmed"}, FieldMode: "title_abstract", TermGroups: [][]string{{term}}}
}
func routeService(t *testing.T, source *routeSource, routes ...retrieval.Route) (*Service, *mutableTemplates) {
	s := offline(t, []Source{source}, nil, nil)
	s.clock = func() time.Time { return scenarioTime }
	provider := &mutableTemplates{items: []SearchTemplate{routeTemplate(routes...)}}
	s.templates = provider
	return s, provider
}

func TestSearchRoutesUnionPreservesOneCandidate(t *testing.T) {
	source := &routeSource{}
	s, _ := routeService(t, source, testRoute("broad", "alpha"), testRoute("precise", "beta"))
	got, err := s.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	require.Equal(t, "completed", got.Runs[0].Status)
	require.Len(t, got.Records, 1)
	for _, candidate := range got.Records {
		require.Len(t, candidate.Record.Discoveries, 2)
		require.True(t, candidate.Published)
	}
	require.Len(t, source.requests, 2)
	require.Contains(t, source.requests[0].Template.Query, "[tiab]")
	for _, q := range got.Runs[0].Sources["pubmed"].Queries {
		require.Equal(t, "completed", q.Status)
		require.Equal(t, 1, q.Requests)
		require.Equal(t, 1, q.Observed)
	}
}
func TestSearchRouteHistoryPartitionsResumeFrozen(t *testing.T) {
	source := &routeSource{twoPages: true}
	route := testRoute("history", "alpha")
	from := scenarioTime.Add(-200 * 24 * time.Hour)
	route.BackfillFrom = &from
	s, provider := routeService(t, source, route)
	first, err := s.ResumeFinite(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, "next", first.Runs[0].Sources["pubmed"].Cursor)
	old := first.Runs[0]
	provider.err = errors.New("configuration unavailable during frozen resume")
	done, err := s.ResumeFinite(context.Background(), 20)
	require.NoError(t, err)
	require.Equal(t, old.ID, done.Runs[0].ID)
	require.Equal(t, "completed", done.Runs[0].Status)
	require.Len(t, source.requests, 6)
	q := done.Runs[0].Sources["pubmed"].Queries["route-topic-configured/history"]
	require.Equal(t, 3, q.CompletedPartitions)
	require.Equal(t, from, q.CoverageWindow.Start)
	require.Equal(t, scenarioTime, q.CoverageWindow.End)
	require.Equal(t, from.Add(180*24*time.Hour), q.Window.Start)
	require.Equal(t, source.requests[0].Window, source.requests[1].Window)
	require.Equal(t, source.requests[1].Window.End, source.requests[2].Window.Start)
	require.Equal(t, source.requests[3].Window.End, source.requests[4].Window.Start)
	require.Equal(t, "next", source.requests[1].Cursor)
	require.Equal(t, 1, provider.calls)
}
func TestSearchRouteNewVersionHasIndependentBackfill(t *testing.T) {
	source := &routeSource{}
	route := testRoute("broad", "alpha")
	from := scenarioTime.Add(-60 * 24 * time.Hour)
	route.BackfillFrom = &from
	s, provider := routeService(t, source, route)
	first, err := s.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	oldQ := first.Runs[0].Sources["pubmed"].Queries["route-topic-configured/broad"]
	provider.items[0].Routes[0].TermGroups = [][]string{{"beta"}}
	now := scenarioTime.Add(72 * time.Hour)
	s.clock = func() time.Time { return now }
	next, err := s.RunOnce(context.Background(), now)
	require.NoError(t, err)
	q := next.Runs[1].Sources["pubmed"].Queries[oldQ.QueryID]
	require.NotEqual(t, oldQ.QueryVersion, q.QueryVersion)
	require.Equal(t, from, q.CoverageWindow.Start)
	require.Len(t, next.Sources["pubmed"].QueryAcquiredThrough, 2)
}
func TestSearchPlanNewWindowDoesNotReuseOldPlanReceipt(t *testing.T) {
	source := &routeSource{}
	s, provider := routeService(t, source, testRoute("broad", "alpha"))
	window := Window{Start: scenarioTime.Add(-24 * time.Hour), End: scenarioTime}
	first, err := s.RunFinite(context.Background(), window, 20)
	require.NoError(t, err)
	provider.items[0].Routes[0].TermGroups = [][]string{{"beta"}}
	same, err := s.RunFinite(context.Background(), window, 20)
	require.NoError(t, err)
	require.Len(t, same.Runs, 1)
	next, err := s.RunPlanWindowFinite(context.Background(), window, 20)
	require.NoError(t, err)
	require.Len(t, next.Runs, 2)
	require.NotEqual(t, first.Runs[0].TemplateVersion, next.Runs[1].TemplateVersion)
	again, err := s.RunPlanWindowFinite(context.Background(), window, 20)
	require.NoError(t, err)
	require.Len(t, again.Runs, 2)
}
func TestNewPlanPendingResumePrefersActiveRun(t *testing.T) {
	source := &routeSource{}
	s, provider := routeService(t, source, testRoute("broad", "alpha"))
	window := Window{Start: scenarioTime.Add(-24 * time.Hour), End: scenarioTime}
	_, err := s.RunFinite(context.Background(), window, 20)
	require.NoError(t, err)
	provider.items[0].Routes = append(provider.items[0].Routes, testRoute("extra", "beta"))
	pending, err := s.RunPlanWindowFinite(context.Background(), window, 1)
	require.NoError(t, err)
	require.Len(t, pending.Runs, 2)
	require.True(t, pending.Runs[1].FinishedAt.IsZero())
	_, err = s.RunPlanWindowFinite(context.Background(), window, 20)
	require.Error(t, err)
	provider.err = errors.New("resume must use frozen plan")
	done, err := s.RunFinite(context.Background(), window, 20)
	require.NoError(t, err)
	require.Equal(t, pending.Runs[1].ID, done.Runs[1].ID)
	require.Equal(t, "completed", done.Runs[1].Status)
	require.Len(t, source.requests, 3)
}
func TestNarrowExplicitRouteWindowDoesNotSkipHistoricalGap(t *testing.T) {
	source := &routeSource{}
	route := testRoute("broad", "alpha")
	from := scenarioTime.Add(-60 * 24 * time.Hour)
	route.BackfillFrom = &from
	s, _ := routeService(t, source, route)
	window := Window{Start: scenarioTime.Add(-24 * time.Hour), End: scenarioTime}
	first, err := s.RunFinite(context.Background(), window, 20)
	require.NoError(t, err)
	q := first.Runs[0].Sources["pubmed"].Queries["route-topic-configured/broad"]
	require.False(t, q.AdvanceCheckpoint)
	require.Empty(t, first.Sources["pubmed"].QueryAcquiredThrough)
	now := scenarioTime.Add(72 * time.Hour)
	s.clock = func() time.Time { return now }
	next, err := s.RunOnce(context.Background(), now)
	require.NoError(t, err)
	require.Equal(t, from, next.Runs[1].Sources["pubmed"].Queries[q.QueryID].CoverageWindow.Start)
}
func TestRouteStartAppliesToExplicitWindow(t *testing.T) {
	from := scenarioTime.Add(-time.Hour)
	route := testRoute("broad", "alpha")
	route.BackfillFrom = &from
	source := &routeSource{}
	s, _ := routeService(t, source, route)
	_, err := s.RunFinite(context.Background(), Window{Start: scenarioTime.Add(-24 * time.Hour), End: scenarioTime}, 20)
	require.NoError(t, err)
	require.Equal(t, from, source.requests[0].Window.Start)
	source2 := &routeSource{}
	s2, _ := routeService(t, source2, route)
	_, err = s2.RunFinite(context.Background(), Window{Start: scenarioTime.Add(-24 * time.Hour), End: scenarioTime.Add(-2 * time.Hour)}, 20)
	require.Error(t, err)
	require.Empty(t, source2.requests)
}
func TestCosmeticRouteChangeDoesNotResetQueryIdentity(t *testing.T) {
	route := testRoute("broad", "alpha")
	a := sourceTemplates([]SearchTemplate{routeTemplate(route)}, false, "pubmed")[0]
	route.Name = "Cosmetic name"
	route.Sources = append(route.Sources, "europepmc")
	b := sourceTemplates([]SearchTemplate{routeTemplate(route)}, false, "pubmed")[0]
	require.Equal(t, a.QueryVersion, b.QueryVersion)
}
func TestSearchPlanMissingSourceRemainsVisible(t *testing.T) {
	route := testRoute("broad", "alpha")
	route.Sources = append(route.Sources, "europepmc")
	s, _ := routeService(t, &routeSource{}, route)
	got, err := s.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	require.Equal(t, "completed_with_gaps", got.Runs[0].Status)
	require.Len(t, got.Runs[0].UnavailableQueries, 1)
	q := got.Runs[0].UnavailableQueries[0]
	require.Equal(t, "europepmc", q.Source)
	require.Equal(t, "unavailable", q.Status)
	require.NotEmpty(t, q.QueryVersion)
}
func TestSearchPlanFailureRetainsPerQueryProgress(t *testing.T) {
	source := &routeSource{failure: &SourceError{Kind: "pagination", Retryable: false}}
	s, _ := routeService(t, source, testRoute("first", "alpha"), testRoute("second", "beta"))
	got, err := s.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	queries := got.Runs[0].Sources["pubmed"].Queries
	require.Equal(t, "failed", queries["route-topic-configured/first"].Status)
	require.Equal(t, 1, queries["route-topic-configured/first"].Requests)
	require.Equal(t, "blocked", queries["route-topic-configured/second"].Status)
	require.Empty(t, got.Sources["pubmed"].QueryAcquiredThrough)
}
func TestSearchPlanPreviewIsOfflineAndSourceSpecific(t *testing.T) {
	a := testRoute("fields", "alpha")
	a.Sources = append(a.Sources, "europepmc")
	b := testRoute("seeds", "alpha")
	b.Sources = []string{"crossref", "arxiv"}
	b.FieldMode = "default"
	source := &routeSource{}
	s, _ := routeService(t, source, a, b)
	response, err := NewHandler(s).templates(context.Background(), &templatesRequest{})
	require.NoError(t, err)
	raw := hashJSON(response)
	require.NotEmpty(t, raw)
	require.Empty(t, source.requests)
	templates, _, err := s.currentTemplates(context.Background())
	require.NoError(t, err)
	previews := queryPreviews(templates, s.sources)
	require.Len(t, previews, 4)
	modes := map[string]string{}
	for _, p := range previews {
		modes[p.Source] = p.Mode
		if p.Source == "pubmed" {
			require.True(t, p.Enabled)
			require.Contains(t, p.Query, "[tiab]")
		}
	}
	require.Equal(t, "seed_union", modes["crossref"])
	require.Equal(t, "full_window", modes["arxiv"])
}
