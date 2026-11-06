package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	atlas "ariga.io/atlas/sql/migrate"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/gleanmembership"

	"github.com/teamsillybees/glean/internal/platform/entx"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
	"github.com/teamsillybees/glean/internal/retrieval"
)

// TestCatalogLiveObservationPostgres 验证真实来源事件持久化、版本重试、历史保护及 mock 审核隔离。
func TestCatalogLiveObservationPostgres(t *testing.T) {
	dsn := os.Getenv("INITRA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("INITRA_TEST_DATABASE_URL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	schema := "glean_observation_test_" + strings.ReplaceAll(time.Now().Format("20060102150405.000000000"), ".", "_")
	_, err = tx.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `SET LOCAL search_path TO "`+schema+`"`)
	require.NoError(t, err)
	dir, err := atlas.NewLocalDir(filepath.Join("..", "..", "..", "db", "migrations"))
	require.NoError(t, err)
	files, err := dir.Files()
	require.NoError(t, err)
	for _, file := range files {
		_, err = tx.ExecContext(ctx, string(file.Bytes()))
		require.NoError(t, err)
	}
	_, err = idgen.ConfigureDefault(51)
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(&catalogTestDriver{Driver: entsql.NewDriver(dialect.Postgres, entsql.Conn{ExecQuerier: tx}), tx: tx}))
	client.Use(entx.RejectDeleteHook())
	_, err = client.SysUser.Create().SetID(7).SetUsername("observer").SetPasswordHash("test-only").Save(ctx)
	require.NoError(t, err)
	service := NewService(client)
	old, err := service.Import(ctx, ImportRecord{Topic: "biomed-ai", OriginalID: "historical-1", Title: "Historical conclusion remains", DOI: "10.9999/source", ReadStatus: "read", Dimensions: map[string][]string{"collection": {"正式纳入"}}, Raw: map[string]any{"conclusion": "preserve"}})
	require.NoError(t, err)
	input := observationFixture()
	input.Topics = []string{"biomed-ai", "llm-agent"}
	input.PublicationEvents = []PublicationEventVO{{Kind: "accepted", Date: input.ObservedAt, Evidence: "not a formal date"}}
	operator := entx.WithOperatorID(ctx, 7)
	first, err := service.IngestObservation(operator, input)
	require.NoError(t, err)
	require.True(t, first.Created)
	require.Equal(t, old.PaperID, first.PaperID)
	require.Equal(t, 1, first.MembershipsAdded)
	input.RunID++
	input.ObservedAt = input.ObservedAt.Add(time.Hour)
	second, err := service.IngestObservation(operator, input)
	require.NoError(t, err)
	require.False(t, second.Created)
	require.Equal(t, first.ObservationID, second.ObservationID)

	input.Discoveries = []retrieval.Hit{{RunID: input.RunID, QueryID: "main", QueryVersion: "v1", PlanVersion: "plan1", Source: input.Source, Query: "alpha", Mode: "boolean", DiscoveredAt: input.ObservedAt}}
	for i := 0; i < 2; i++ {
		replay, err := service.IngestObservation(operator, input)
		require.NoError(t, err)
		require.False(t, replay.Created)
		require.Equal(t, first.ObservationID, replay.ObservationID)
	}
	hits, err := client.GleanSearchHit.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	require.Equal(t, first.ObservationID, hits[0].ObservationID)
	require.Equal(t, "alpha", hits[0].QueryText)
	input.Discoveries[0].QueryID = "alternative"
	_, err = service.IngestObservation(operator, input)
	require.NoError(t, err)
	hitCount, err := client.GleanSearchHit.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, hitCount)
	savedObservation, err := client.GleanObservation.Get(ctx, first.ObservationID)
	require.NoError(t, err)
	require.NotContains(t, string(savedObservation.Metadata), "discoveries")
	discoveryView, err := service.Paper(requestctx.WithUserID(ctx, "7"), first.PaperID)
	require.NoError(t, err)
	foundHits := false
	for _, observation := range discoveryView.Observations {
		if observation.ID == first.ObservationID {
			require.Len(t, observation.Discoveries, 2)
			require.False(t, observation.DiscoveriesHasMore)
			foundHits = true
		}
	}
	require.True(t, foundHits)
	input.Discoveries = nil
	input.Version = "v2"
	input.Title = "New corrected title from source"
	input.Raw = json.RawMessage(`{"title":"New corrected title from source","version":2}`)
	third, err := service.IngestObservation(operator, input)
	require.NoError(t, err)
	require.True(t, third.Created)
	require.Equal(t, first.PaperID, third.PaperID)
	viewed, err := service.Paper(requestctx.WithUserID(ctx, "7"), first.PaperID)
	require.NoError(t, err)
	require.False(t, viewed.IsMock)
	require.Equal(t, "Historical conclusion remains", viewed.Title)
	require.Nil(t, viewed.FormalOnlineDate)
	require.Equal(t, "unread", viewed.ReadStatus)
	require.Len(t, viewed.Observations, 2)
	require.Equal(t, 2, viewed.ObservationCount)
	require.True(t, viewed.Review.IsMock)
	require.False(t, viewed.Review.ScientificReviewed)
	require.Equal(t, "pending-review", viewed.Review.InclusionStatus)
	require.Equal(t, "New corrected title from source", viewed.Observations[0].Title)
	require.Equal(t, "metadata_updated", viewed.Observations[0].EventType)
	require.Equal(t, "historical", viewed.Origin)
	require.NotNil(t, viewed.ImportedAt)
	require.Nil(t, viewed.FirstSeenAt)
	require.Equal(t, "historical", viewed.Memberships[0].InclusionStatus)
	require.Equal(t, "read", viewed.Memberships[0].ReadStatus)
	require.Equal(t, "pending-review", viewed.Memberships[1].InclusionStatus)
	count, err := client.GleanMembership.Query().Where(gleanmembership.PaperID(first.PaperID)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, count)
	observed, err := service.Papers(requestctx.WithUserID(ctx, "7"), PaperQuery{Topic: "llm-agent", InclusionStatus: "pending-review", Source: "crossref", Sort: "observed"})
	require.NoError(t, err)
	require.Equal(t, 1, observed.Total)
	require.Equal(t, 2, observed.Items[0].ObservationCount)
	facets, err := service.Facets(requestctx.WithUserID(ctx, "7"), PaperQuery{Topic: "llm-agent"})
	require.NoError(t, err)
	require.Equal(t, []FacetVO{{Value: "crossref", Count: 1}}, facets.Sources)
	require.Equal(t, []FacetVO{{Value: "pending-review", Count: 1}}, facets.InclusionStatuses)
	curated, err := service.Papers(requestctx.WithUserID(ctx, "7"), PaperQuery{Topic: "biomed-ai", CollectionScope: "curated"})
	require.NoError(t, err)
	require.Equal(t, 1, curated.Total)
	pending, err := service.Papers(requestctx.WithUserID(ctx, "7"), PaperQuery{Topic: "llm-agent", CollectionScope: "pending"})
	require.NoError(t, err)
	require.Equal(t, 1, pending.Total)
	llmCurated, err := service.Papers(requestctx.WithUserID(ctx, "7"), PaperQuery{Topic: "llm-agent", CollectionScope: "curated"})
	require.NoError(t, err)
	require.Zero(t, llmCurated.Total, "mock-reviewed query hits cannot enter curated historical collection")
	topicCounts, err := service.Topics(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, topicCounts[0].CuratedCount)
	require.Equal(t, 1, topicCounts[1].CandidateCount)
	input.SourceID = "10.9999/new"
	input.DOI = "10.9999/new"
	input.Topics = []string{"biomed-worldmodel"}
	input.PublicationEvents = []PublicationEventVO{{Kind: "online", Date: input.ObservedAt, Evidence: "publisher first"}, {Kind: "online", Date: input.ObservedAt.Add(-24 * time.Hour), Evidence: "publisher second"}}
	fresh, err := service.IngestObservation(operator, input)
	require.NoError(t, err)
	newPaper, err := client.GleanPaper.Get(ctx, fresh.PaperID)
	require.NoError(t, err)
	require.False(t, newPaper.IsMock)
	require.Equal(t, "live", newPaper.Origin)
	require.Nil(t, newPaper.FormalOnlineDate)
	require.Zero(t, newPaper.Year)
	require.Error(t, client.GleanObservation.DeleteOneID(first.ObservationID).Exec(ctx))
	jif := 12.0
	require.NoError(t, service.RegisterVenue(ctx, VenueVO{Name: "Verified historical journal", JIF: &jif, ISSNs: []string{"0028-0836"}, MetricYear: 2025, RegistryVersion: "history-v1", Source: "https://example.org/original-metric-evidence", QualificationStatus: "historical-verified"}))
	core, err := service.CoreJournals(ctx)
	require.NoError(t, err)
	require.Len(t, core, 1)
	require.Contains(t, core[0].EligibleTopics, "biomed-ai")
	unknownYearJIF := 14.0
	require.NoError(t, service.RegisterVenue(ctx, VenueVO{Name: "Medical Image Analysis", Kind: "journal", JIF: &unknownYearJIF, ISSNs: []string{"1361-8415", "1361-8423"}, MetricYear: 0, RegistryVersion: "historical-discovery-v1", Source: "committed curated source evidence", QualificationStatus: "historical-curated-discovery", Evidence: map[string]any{"discoveryEnabled": true, "discoveryTopics": []string{"biomed-ai"}, "metricVerified": false}}))
	require.NoError(t, service.RegisterVenue(ctx, VenueVO{Name: "Conference series fixture", Kind: "conference", JIF: &jif, ISSNs: []string{"0018-9219"}, MetricYear: 2025, RegistryVersion: "not-a-journal-v1", Source: "explicit conference fixture", QualificationStatus: "historical-verified"}))
	core, err = service.CoreJournals(ctx)
	require.NoError(t, err)
	require.Len(t, core, 2, "conference series is never a journal target")
	for _, target := range core {
		if target.Name == "Medical Image Analysis" {
			require.Zero(t, target.MetricYear)
			require.False(t, target.QualificationVerified)
			require.Empty(t, target.EligibleTopics)
			require.Equal(t, []string{"biomed-ai"}, target.DiscoveryTopics)
		}
	}
	// 同一个真实 PMID 在 PubMed 与 Europe PMC 无 DOI 时也只创建一个全局论文。
	medical := observationFixture()
	medical.Source, medical.SourceID, medical.PMID, medical.DOI = "pubmed", "12345678", "12345678", ""
	medical.SourceURL = "https://pubmed.ncbi.nlm.nih.gov/12345678/"
	pubmed, err := service.IngestObservation(operator, medical)
	require.NoError(t, err)
	medical.Source, medical.SourceID = "europepmc", "MED:12345678"
	medical.DOI = "10.9999/medical-enrichment"
	medical.SourceURL = "https://europepmc.org/article/MED/12345678"
	europepmc, err := service.IngestObservation(operator, medical)
	require.NoError(t, err)
	require.Equal(t, pubmed.PaperID, europepmc.PaperID)
	require.NotEqual(t, pubmed.ObservationID, europepmc.ObservationID)
	medicalDetail, err := service.Paper(requestctx.WithUserID(ctx, "7"), pubmed.PaperID)
	require.NoError(t, err)
	require.Len(t, medicalDetail.Observations, 2)
	require.Equal(t, "12345678", medicalDetail.Observations[0].PMID)
	require.Equal(t, medical.SourceURL, medicalDetail.Observations[0].SourceURL)
	medical.Source, medical.SourceID, medical.PMID = "crossref", medical.DOI, ""
	doiOnly, err := service.IngestObservation(operator, medical)
	require.NoError(t, err)
	require.Equal(t, pubmed.PaperID, doiOnly.PaperID)
	preserved, err := client.GleanPaper.Get(ctx, pubmed.PaperID)
	require.NoError(t, err)
	require.Nil(t, preserved.Doi, "observation enrichment must not overwrite canonical metadata")
	for _, query := range []string{"10.9999/medical-enrichment", "REAL SOURCE TITLE"} {
		searched, err := service.Papers(requestctx.WithUserID(ctx, "7"), PaperQuery{Q: query, CollectionScope: "pending"})
		require.NoError(t, err)
		if query == "10.9999/medical-enrichment" {
			require.Equal(t, 1, searched.Total, "multiple source observations must not duplicate paper results")
			require.Equal(t, pubmed.PaperID, searched.Items[0].ID)
		} else {
			require.GreaterOrEqual(t, searched.Total, 1)
		}
	}
	alias := medical
	alias.Title, alias.Abstract = "Distinctive later official title", "Official enrichment abstract not present in canonical paper"
	_, err = service.IngestObservation(operator, alias)
	require.NoError(t, err)
	for _, query := range []string{"distinctive later official", "enrichment abstract"} {
		searched, err := service.Papers(requestctx.WithUserID(ctx, "7"), PaperQuery{Q: query})
		require.NoError(t, err)
		require.Equal(t, 1, searched.Total)
		require.Equal(t, pubmed.PaperID, searched.Items[0].ID)
	}
	medical.DOI = "10.9999/different-medical-identity"
	_, err = service.IngestObservation(operator, medical)
	require.ErrorContains(t, err, "contradicts an observed")
	kind, rejected := ObservationRejectionKind(err)
	require.True(t, rejected)
	require.Equal(t, "identity_conflict", kind)
	// 同来源忽然声称不同 DOI 时隔离冲突，而不是把不同论文静默合并。
	conflict := observationFixture()
	conflict.DOI = "10.9999/contradicting-id"
	_, err = service.IngestObservation(operator, conflict)
	require.ErrorContains(t, err, "contradicts an established")
}
