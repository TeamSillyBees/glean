package baseline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDecodePackedPreservesValuesAndRejectsBadIndex(t *testing.T) {
	body := []byte(`{"packed":{"strings":["P1","Evidence", "待核"],"columns":["id","title","status","negative","flags"],"rows":[[-1,-2,-3,{"$number":-2},[false,null,0]]]}}`)
	papers, _, err := DecodeCatalog(body)
	if err != nil {
		t.Fatal(err)
	}
	p := papers[0]
	if p["id"] != "P1" || p["status"] != "待核" || p["negative"] != json.Number("-2") {
		t.Fatalf("lost packed values: %#v", p)
	}
	if _, _, err := DecodeCatalog([]byte(`{"packed":{"strings":[],"columns":["id"],"rows":[[-1]]}}`)); err == nil {
		t.Fatal("invalid dictionary reference accepted")
	}
}
func TestCatalogJSONDoesNotExecuteHTML(t *testing.T) {
	html := []byte(`<script>throw new Error('not executed')</script><script type="application/json" id='catalog-data'>{"papers":[]}</script>`)
	got, err := CatalogJSON(html)
	if err != nil || string(got) != `{"papers":[]}` {
		t.Fatalf("got %s, %v", got, err)
	}
	if _, err := CatalogJSON(append(html, html...)); err == nil {
		t.Fatal("multiple catalogs accepted")
	}
}
func TestNormalizePreservesPendingAndTypedDates(t *testing.T) {
	raw := map[string]any{"id": "P1", "title_en": "A study", "doi": "https://doi.org/10.1234/ABC", "category": "待核", "eligibility_status": "待核实", "paper_type": "research", "authors": []any{"An Author"}, "date": "2026-01-02", "date_online": "2026-03-11", "date_online_precision": "day", "date_online_verified": true, "date_issue": "2026-04", "date_issue_precision": "month", "date_issue_verified": true, "abstract_read": false, "fulltext_accessed": false, "roles": []any{"A", "待核"}}
	r, n, err := Normalize("biomed-worldmodel", raw)
	if err != nil || n != 0 {
		t.Fatalf("%v %d", err, n)
	}
	if r.DOI != "10.1234/abc" || r.Title != "A study" || r.Tier != "待核实" || r.PublicationType != "research" {
		t.Fatalf("mapping incorrect: %#v", r)
	}
	if r.FormalOnlineDate == nil || r.FormalOnlineDate.Format("2006-01-02") != "2026-03-11" || r.IssueDate != nil {
		t.Fatal("typed date precision/verification lost")
	}
	if r.ReadStatus != "unread" || r.IsMock || r.Raw["abstract_read"] != false || r.Raw["fulltext_accessed"] != false {
		t.Fatal("review/read truth changed")
	}
	if !reflect.DeepEqual(r.Dimensions["roles"], []string{"A", "待核"}) {
		t.Fatal("roles not preserved")
	}
	if !reflect.DeepEqual(raw, r.Raw) {
		t.Fatal("scientific raw record changed")
	}
}
func TestNormalizeNeverPromotesAcceptanceOrAmbiguousDates(t *testing.T) {
	raw := map[string]any{"id": "P2", "title": "Unknown date", "date": "2026-01-02", "year": json.Number("2026"), "publication_events": []any{map[string]any{"event": "accepted", "date": "2026-01-02", "precision": "day", "identity_verified": true, "qualifying_date_type": true}, map[string]any{"event": "online", "date": "2026-01-02", "precision": "day", "identity_verified": false, "qualifying_date_type": true}}}
	r, _, err := Normalize("llm-agent", raw)
	if err != nil {
		t.Fatal(err)
	}
	if r.FormalOnlineDate != nil || r.IssueDate != nil {
		t.Fatal("unqualified date promoted")
	}
}
func TestRedactOnlyPrivateProvenance(t *testing.T) {
	raw := map[string]any{"id": "P1", "title": "Exact title", "evidence_urls": []any{"https://doi.org/10.1234/paper", "https://chatgpt.com/c/private-session"}, "cache_file": "/home/researcher/private/raw.json", "relative_cache": "cache/raw.json", "api_key": "synthetic-private-value", "fulltext_accessed": false}
	r, n, err := Normalize("llm-agent", raw)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("redactions=%d", n)
	}
	if r.Raw["relative_cache"] != "cache/raw.json" || r.Raw["fulltext_accessed"] != false || r.Raw["title"] != "Exact title" {
		t.Fatal("unrelated evidence modified")
	}
	body, _ := json.Marshal(r)
	if bytes.Contains(body, []byte("synthetic-private-value")) || bytes.Contains(body, []byte("private-session")) {
		t.Fatal("private provenance leaked")
	}
	if raw["cache_file"] != "/home/researcher/private/raw.json" {
		t.Fatal("source mutated")
	}
}
func TestIdentityNeverMergesByTitle(t *testing.T) {
	x := newIdentityIndex()
	x.add(Record{Topic: "llm-agent", OriginalID: "a", Title: "Same"})
	x.add(Record{Topic: "llm-agent", OriginalID: "b", Title: "Same"})
	x.add(Record{DOI: "10.1234/x"})
	x.add(Record{DOI: "10.1234/x"})
	if n, _ := x.counts(); n != 3 {
		t.Fatalf("unique=%d", n)
	}
	if NormalizeArxiv("https://arxiv.org/abs/2501.12345v2") != "2501.12345" {
		t.Fatal("arxiv normalization")
	}
}
func TestExtractVerifyAndImportIdempotentQuarantine(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.html")
	html := `<script id="catalog-data">{"papers":[{"id":"P1","title":"One","abstract_read":false},{"id":"P2","title":"Two","category":"待核"},{"id":"P3","title":"Three"}]}</script>`
	if err := os.WriteFile(source, []byte(html), 0600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "snapshots")
	m, err := Extract([]SourceSpec{{Topic: "llm-agent", Path: source, SHA256: digest([]byte(html)), Memberships: 3}}, ExtractOptions{OutputDir: dir, ArchiveDir: filepath.Join(root, "archive"), RecordsPerShard: 2, MaxShardBytes: 2000})
	if err != nil {
		t.Fatal(err)
	}
	if m.Memberships != 3 || len(m.Shards) != 2 {
		t.Fatalf("bad manifest: %#v", m)
	}
	if err := Verify(dir, m); err != nil {
		t.Fatal(err)
	}
	archive, err := readCompressed(filepath.Join(root, "archive", "llm-agent.catalog.json.gz"))
	if err != nil {
		t.Fatal(err)
	}
	original, _ := CatalogJSON([]byte(html))
	if !bytes.Equal(archive, original) {
		t.Fatal("archive not byte-exact")
	}
	seen := map[string]bool{}
	fail := true
	apply := func(_ context.Context, line json.RawMessage) (bool, error) {
		var r Record
		_ = json.Unmarshal(line, &r)
		if fail && r.OriginalID == "P2" {
			return false, errors.New("synthetic database failure")
		}
		key := r.Topic + ":" + r.OriginalID
		if seen[key] {
			return false, nil
		}
		seen[key] = true
		return true, nil
	}
	var q bytes.Buffer
	report, err := Import(context.Background(), dir, m, apply, &q)
	if !errors.Is(err, ErrQuarantined) || report.Created != 2 || report.Quarantined != 1 || report.Attempted != 3 || report.Complete {
		t.Fatalf("lost failure: %#v %v", report, err)
	}
	if !strings.Contains(q.String(), `"originalId":"P2"`) || strings.Contains(q.String(), "synthetic database failure") {
		t.Fatal("quarantine identity missing or private DB error exposed")
	}
	fail = false
	q.Reset()
	report, err = Import(context.Background(), dir, m, apply, &q)
	if err != nil || report.Created != 1 || report.Existing != 2 || !report.Complete {
		t.Fatalf("retry not idempotent: %#v %v", report, err)
	}
	report, err = Import(context.Background(), dir, m, apply, &q)
	if err != nil || report.Existing != 3 || report.Created != 0 {
		t.Fatal("repeat import created duplicates")
	}
	bad := m
	bad.Shards = append([]Shard(nil), m.Shards...)
	bad.Shards[0].SHA256 = strings.Repeat("0", 64)
	if err := Verify(dir, bad); err == nil {
		t.Fatal("tampering accepted")
	}
}
func TestExtractRefusesWrongSourceHashAndDuplicateID(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source.html")
	body := []byte(`<script id="catalog-data">{"papers":[{"id":"P1","title":"One"},{"id":"P1","title":"Two"}]}</script>`)
	_ = os.WriteFile(path, body, 0600)
	opts := ExtractOptions{OutputDir: filepath.Join(root, "out"), ArchiveDir: filepath.Join(root, "archive")}
	if _, err := Extract([]SourceSpec{{Topic: "llm-agent", Path: path, SHA256: "incorrect"}}, opts); err == nil {
		t.Fatal("wrong source hash accepted")
	}
	if _, err := Extract([]SourceSpec{{Topic: "llm-agent", Path: path}}, opts); err == nil {
		t.Fatal("duplicate ID accepted")
	}
	if _, err := os.Stat(filepath.Join(opts.OutputDir, "manifest.json")); !os.IsNotExist(err) {
		t.Fatal("failed extraction wrote completion manifest")
	}
}

func TestDisplayMappingsAreImportedNotGuessed(t *testing.T) {
	html := []byte(`const UI_TAXONOMY={"version":"test","maps":{"scale":{"分子":["分子、蛋白质与基因调控"]},"tasks":{"exact legacy task":["已有分组"]},"roles":{"A":["原职责"]},"type":{"研究论文":["原创研究"]}}}; throw new Error('must not execute')`)
	taxonomy, _, err := ReadDisplayTaxonomy(html)
	if err != nil {
		t.Fatal(err)
	}
	r, _, err := Normalize("biomed-worldmodel", map[string]any{"id": "BW1", "title_en": "Title", "paper_type": "research", "scale": []any{"分子"}, "tasks": []any{"exact legacy task"}, "roles": []any{"A"}, "abstract_read": false, "abstract_fetched": true, "jif": json.Number("10")})
	if err != nil {
		t.Fatal(err)
	}
	applyDisplay(&r, taxonomy)
	if r.Dimensions["display_tasks"][0] != "已有分组" || r.Dimensions["display_type"][0] != "原创研究" {
		t.Fatal("original taxonomy not applied")
	}
	if r.Dimensions["display_scope"][0] != "期刊 · 影响因子4–10" {
		t.Fatal("JIF boundary changed")
	}
	if r.Dimensions["display_review_status"][0] != "摘要已获取，待阅读" || r.ReadStatus != "unread" {
		t.Fatal("acquisition confused with reading")
	}
}
func TestVenueKeepsPendingWithoutInventingQualification(t *testing.T) {
	r := Record{Topic: "biomed-ai", Venue: "A Journal", Raw: map[string]any{"jif": json.Number("10"), "jif_year": "官网未标注指标年", "metric_url": "https://publisher.example/metrics"}}
	v, ok := VenueFromRecord(r)
	if !ok || v.JIF == nil || *v.JIF != 10 || v.MetricYear != 0 || v.QualificationStatus != "pending" || len(v.EligibleTopics) != 0 {
		t.Fatalf("invented registry fact: %#v", v)
	}
	v2, _ := VenueFromRecord(r)
	if v.RegistryVersion != v2.RegistryVersion {
		t.Fatal("registry version is nondeterministic")
	}
	r.Topic = "llm-agent"
	v3, _ := VenueFromRecord(r)
	if v3.RegistryVersion == v.RegistryVersion {
		t.Fatal("topic evidence collided")
	}
}

func TestIdentityAuditsConflictingAliases(t *testing.T) {
	x := newIdentityIndex()
	x.add(Record{DOI: "10.1234/x", ArxivID: "2501.00001"})
	x.add(Record{DOI: "10.1234/x", ArxivID: "2501.00002"})
	if _, conflicts := x.counts(); conflicts != 1 {
		t.Fatal("same DOI with distinct arxiv was not flagged")
	}
}
func TestCommittedHistoricalManifest(t *testing.T) {
	dir := filepath.Join("..", "..", "data", "baselines")
	m, err := ReadManifest(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Memberships != 39495 || len(m.Sources) != 3 {
		t.Fatalf("historical baseline replaced by sample: %d", m.Memberships)
	}
	want := map[string]int{"llm-agent": 4059, "biomed-ai": 31399, "biomed-worldmodel": 4037}
	for _, s := range m.Sources {
		if s.Memberships != want[s.Topic] || s.TypedOnlineDates == 0 {
			t.Fatalf("historical source count/date evidence lost: %#v", s)
		}
	}
	if err := Verify(dir, m); err != nil {
		t.Fatal(err)
	}
}

func TestConflictingVerifiedEventDaysRemainUntyped(t *testing.T) {
	raw := map[string]any{"id": "conflict", "title": "Conflicting online date", "publication_events": []any{map[string]any{"event": "online", "date": "2025-08-19", "precision": "day", "identity_verified": true, "qualifying_date_type": true}, map[string]any{"event": "online", "date": "2025-08-20", "precision": "day", "identity_verified": true, "qualifying_date_type": true}}}
	r, _, err := Normalize("llm-agent", raw)
	if err != nil {
		t.Fatal(err)
	}
	if r.FormalOnlineDate != nil || len(r.Evidence) != 2 || !strings.Contains(r.DateProvenance, "conflicting verified online") {
		t.Fatal("ambiguous date arbitrated or evidence lost")
	}
}
