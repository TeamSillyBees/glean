package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/logx"
	"github.com/teamsillybees/glean/internal/platform/server"
)

func seedQuarantineView(t *testing.T, ctx context.Context, s *Service) {
	t.Helper()
	state, err := s.load(ctx)
	require.NoError(t, err)
	state.Records["bad"] = &Candidate{ID: 101, Record: Record{Source: "crossref", SourceID: "https://user:secret@example.com/?token=leak", Title: "<script>alert(1)</script>", DOI: "10.1234/example", Raw: json.RawMessage(`{"title":"ok","nested":{"Authorization":"Bearer raw-secret"}}`), AcquiredAt: scenarioTime}, Quarantine: &Quarantine{Reason: "password=reason-secret", At: scenarioTime}}
	state.Records["bad2"] = &Candidate{ID: 102, Record: Record{Source: "pubmed", Title: strings.Repeat("文", 3000)}, Quarantine: &Quarantine{Reason: "invalid_source_record", At: scenarioTime.Add(time.Hour), RawEvidence: []byte("<xml password='invalid-raw-secret'>")}}
	state.Records["published"] = &Candidate{ID: 103, Published: true, Record: Record{Title: "not quarantined"}}
	state.Runs = append(state.Runs, Run{ID: 200, Sources: map[string]*SourceProgress{"crossref": {CandidateKeys: []string{"bad"}}}})
	require.NoError(t, s.save(ctx, state))
}
func TestQuarantineReadOnlySafeProjection(t *testing.T) {
	ctx := context.Background()
	s := offline(t, DemoSources(scenarioTime, false), nil, nil)
	seedQuarantineView(t, ctx, s)
	before := append([]byte(nil), s.memory.data...)
	h := NewHandler(s)
	list, err := h.quarantine(ctx, &quarantineRequest{Page: 1, PageSize: 1})
	require.NoError(t, err)
	require.Equal(t, 2, list.Body.Data.Total)
	require.Equal(t, idgen.ID(102), list.Body.Data.Items[0].ID)
	list, err = h.quarantine(ctx, &quarantineRequest{Page: 2, PageSize: 1})
	require.NoError(t, err)
	require.Equal(t, idgen.ID(101), list.Body.Data.Items[0].ID)
	list, err = h.quarantine(ctx, &quarantineRequest{Page: 999999, PageSize: 100})
	require.NoError(t, err)
	require.Empty(t, list.Body.Data.Items)
	list, err = h.quarantine(ctx, &quarantineRequest{Page: 1, PageSize: 20, Source: "crossref"})
	require.NoError(t, err)
	require.Equal(t, 1, list.Body.Data.Total)
	for _, req := range []*quarantineRequest{{Page: 0, PageSize: 20}, {Page: 1, PageSize: 101}, {Page: 1, PageSize: 20, Source: "http://127.0.0.1"}} {
		_, err = h.quarantine(ctx, req)
		require.Error(t, err)
	}
	detail, err := h.quarantineDetail(ctx, &quarantineDetailRequest{ID: 101})
	require.NoError(t, err)
	require.Equal(t, []idgen.ID{200}, detail.Body.Data.RunIDs)
	require.Equal(t, "record_rejected", detail.Body.Data.Reason)
	require.Empty(t, detail.Body.Data.SourceID)
	require.Equal(t, "json", detail.Body.Data.Evidence.Format)
	require.Len(t, detail.Body.Data.Evidence.SHA256, 64)
	require.True(t, detail.Body.Data.Evidence.Redacted)
	output, err := json.Marshal(detail)
	require.NoError(t, err)
	for _, secret := range []string{"raw-secret", "reason-secret", "user:secret", "Authorization", "<script>"} {
		require.NotContains(t, string(output), secret)
	}
	detail, err = h.quarantineDetail(ctx, &quarantineDetailRequest{ID: 102})
	require.NoError(t, err)
	require.Equal(t, "text", detail.Body.Data.Evidence.Format)
	require.True(t, detail.Body.Data.Evidence.Truncated)
	require.Less(t, len(detail.Body.Data.Evidence.Preview), 4096)
	for _, id := range []idgen.ID{103, 999} {
		_, err = h.quarantineDetail(ctx, &quarantineDetailRequest{ID: id})
		require.Error(t, err)
	}
	require.Equal(t, before, s.memory.data, "inspection must not rewrite raw evidence or scientific state")
}
func TestQuarantineHTTPAuthenticationAndBounds(t *testing.T) {
	ctx := context.Background()
	s := offline(t, DemoSources(scenarioTime, false), nil, nil)
	seedQuarantineView(t, ctx, s)
	tokens, err := platformauth.NewJWTManager(platformauth.JWTConfig{Store: platformauth.NewMemoryTokenStore(), Issuer: "test", Secret: "quarantine-test-secret-longer-than-32-bytes", AccessTokenTTL: time.Hour, RefreshTokenTTL: 24 * time.Hour})
	require.NoError(t, err)
	version := int64(1)
	active := true
	app, err := server.NewApp(server.Options{Title: "Glean", Version: "test", Env: "test"}, logx.NewNop(), tokens, platformauth.IdentityResolverFunc(func(_ context.Context, id idgen.ID) (platformauth.Principal, bool, error) {
		return platformauth.Principal{UserID: id, SessionVersion: version}, active, nil
	}), nil)
	require.NoError(t, err)
	NewModule(NewHandler(s)).Register(app.API, app.Registry)
	pair, err := tokens.IssueTokenPair(ctx, platformauth.Principal{UserID: 7, SessionVersion: 1})
	require.NoError(t, err)
	request := func(path, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("Accept", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		app.Engine.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/api/v1/monitor/quarantine", "/api/v1/monitor/quarantine/101"} {
		require.Equal(t, 401, request(path, "").Code)
		r := request(path, pair.AccessToken)
		require.Equal(t, 200, r.Code, r.Body.String())
	}
	for _, query := range []string{"?page=0", "?pageSize=101", "?pageSize=foo", "?source=unknown"} {
		r := request("/api/v1/monitor/quarantine"+query, pair.AccessToken)
		require.GreaterOrEqual(t, r.Code, 400)
		require.Less(t, r.Code, 500)
	}
	require.Equal(t, 404, request("/api/v1/monitor/quarantine/103", pair.AccessToken).Code)
	version = 2
	require.Equal(t, 401, request("/api/v1/monitor/quarantine/101", pair.AccessToken).Code)
	version = 1
	active = false
	require.Equal(t, 401, request("/api/v1/monitor/quarantine", pair.AccessToken).Code)
}

func TestQuarantineMetadataBoundsAndFallback(t *testing.T) {
	c := &Candidate{ID: 9, Record: Record{Source: "https://private/?token=hidden", SourceID: "Bearer hidden", Title: "", DOI: "password-hidden", Topics: []string{"Authorization: hidden"}}, Quarantine: &Quarantine{Reason: "missing_identity"}}
	item := quarantineItem(c, nil)
	require.Equal(t, "unknown", item.Source)
	require.NotEmpty(t, item.Title)
	require.Empty(t, item.DOI)
	require.Empty(t, item.SourceID)
	require.Equal(t, []string{"[redacted]"}, item.Topics)
	require.Equal(t, "missing_identity", item.Reason)
	require.Empty(t, item.RunIDs)
	require.Equal(t, "[redacted]", quarantineText("https://host/path?api_key=private", 512))
	require.LessOrEqual(t, len(quarantineText(strings.Repeat("<", 10000), 512)), 512)
}

func quarantineLargeSnapshot(count int) Snapshot {
	snapshot := Snapshot{Records: map[string]*Candidate{}, Runs: []Run{{ID: 300, Sources: map[string]*SourceProgress{"crossref": {}}}}}
	for i := 0; i < count; i++ {
		key := fmt.Sprint(i)
		snapshot.Records[key] = &Candidate{ID: idgen.ID(i + 1), Record: Record{Source: "crossref", Title: "evidence"}, Quarantine: &Quarantine{At: scenarioTime}}
		snapshot.Runs[0].Sources["crossref"].CandidateKeys = append(snapshot.Runs[0].Sources["crossref"].CandidateKeys, key)
	}
	return snapshot
}
func TestQuarantinePageOnlyHydratesSelectedRecords(t *testing.T) {
	snapshot := quarantineLargeSnapshot(10000)
	selected := []quarantineReference{{key: "9999", candidate: snapshot.Records["9999"]}}
	snapshot.Runs = append(snapshot.Runs, Run{ID: 400, RetryCandidateKeys: map[string][]string{"crossref": {"9999", "9999"}}, Sources: map[string]*SourceProgress{"crossref": {CandidateKeys: []string{"9999"}}}})
	index := quarantineRunAssociations(snapshot.Runs, selected)
	require.Len(t, index, 1, "association memory must be bounded by selected page, not all candidates")
	require.Equal(t, []idgen.ID{400, 300}, index["9999"], "deduplicate associations within one retained run")
	page := quarantinePage(snapshot, &quarantineRequest{Page: 1, PageSize: 20})
	require.Equal(t, 10000, page.Total)
	require.Len(t, page.Items, 20)
	require.Equal(t, idgen.ID(10000), page.Items[0].ID)
	require.Equal(t, []idgen.ID{400, 300}, page.Items[0].RunIDs)
	require.Equal(t, idgen.ID(9981), page.Items[19].ID)
	require.Empty(t, quarantineRunAssociations(snapshot.Runs, nil))
}

// BenchmarkQuarantinePage 使用同一运行的大量隔离项，防止每项重复扫描全部运行键的平方级退化。
func BenchmarkQuarantinePage(b *testing.B) {
	for _, count := range []int{1000, 10000, 100000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			snapshot := quarantineLargeSnapshot(count)
			req := &quarantineRequest{Page: 1, PageSize: 20}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				page := quarantinePage(snapshot, req)
				if len(page.Items) != 20 {
					b.Fatal("incorrect page")
				}
			}
		})
	}
}
