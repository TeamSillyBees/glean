package catalog

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/entx"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
)

// TestCatalogFacetBatchEquivalence 验证批大小不改变完整筛选项、排序及共享/个人覆盖层。
func TestCatalogFacetBatchEquivalence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client, _, _ := newCatalogPostgresTest(t, ctx)
	for _, uid := range []idgen.ID{7, 8} {
		_, err := client.SysUser.Create().SetID(uid).SetUsername("facet-user-" + uid.String()).SetPasswordHash("test-only").SetIsSuperAdmin(uid == 7).Save(ctx)
		require.NoError(t, err)
	}
	service := NewService(client)
	manager := platformauth.WithPrincipal(ctx, platformauth.Principal{UserID: 7, IsSuperAdmin: true})
	users := []context.Context{requestctx.WithUserID(ctx, "7"), requestctx.WithUserID(ctx, "8")}
	ids := []idgen.ID{}
	for i := 0; i < 3; i++ {
		record := ImportRecord{Topic: "biomed-ai", OriginalID: fmt.Sprintf("facet-%d", i), DOI: fmt.Sprintf("10.1234/facet-%d", i), Title: fmt.Sprintf("Facet paper %d", i), Year: 2020 + i, Venue: "Original venue", PublicationType: "article", Tier: "historical", PrimaryCategory: "Oncology", Categories: []string{"Oncology", "Clinical"}, Dimensions: map[string][]string{"collection": {"正式纳入"}, "technical_tasks": {"planning"}, "ai_methods": {"transformer"}, "llm_role": {"assistant"}, "relation_direction": {"forward"}}}
		result, err := service.Import(ctx, record)
		require.NoError(t, err)
		ids = append(ids, result.PaperID)
		record.Topic = "llm-agent"
		record.Tier = "secondary"
		record.Dimensions = map[string][]string{"tasks": {"retrieval"}, "roles": {"agent"}}
		_, err = service.Import(ctx, record)
		require.NoError(t, err)
	}
	for i, id := range ids {
		_, err := service.SaveReview(manager, id, "biomed-ai", ReviewBody{Status: []string{"included", "excluded", "pending"}[i], Note: "transaction fixture", Evidence: screeningFixture("biomed-ai", []string{"included", "excluded", "pending"}[i]), Labels: []string{"shared-label", fmt.Sprintf("label-%d", i)}})
		require.NoError(t, err)
	}
	year, venue := 2030, "Corrected venue"
	_, err := service.SaveCorrection(manager, ids[0], CorrectionBody{Fields: CorrectionFields{Year: &year, Venue: &venue}, Reason: "fixture verified"})
	require.NoError(t, err)
	// Repeated observations from one source must still count a paper only once.
	for i, source := range []string{"fixture-a", "fixture-a", "fixture-b"} {
		input := observationFixture()
		input.DOI = "10.1234/facet-0"
		input.Source, input.SourceID, input.Version = source, "facet-observation", fmt.Sprintf("v%d", i)
		input.Topics = []string{"biomed-ai", "llm-agent"}
		_, err := service.IngestObservation(entx.WithOperatorID(ctx, 7), input)
		require.NoError(t, err)
	}
	_, err = service.SavePaperState(users[0], ids[0], PaperStateVO{ReadStatus: "read", Hidden: true})
	require.NoError(t, err)
	_, err = service.SavePaperState(users[1], ids[0], PaperStateVO{ReadStatus: "reading"})
	require.NoError(t, err)
	_, err = service.SavePaperState(users[1], ids[1], PaperStateVO{ReadStatus: "read", Hidden: true})
	require.NoError(t, err)
	queries := []PaperQuery{
		{CollectionScope: "all"}, {CollectionScope: "all", Hidden: "include"},
		{Hidden: "only"}, {ReadStatus: "reading"},
		{Topic: "biomed-ai", CollectionScope: "curated"}, {CollectionScope: "excluded"}, {CollectionScope: "pending"},
		{Topic: "llm-agent", Tier: "secondary", Dimension: `{"tasks":"retrieval"}`},
		{Topic: "biomed-ai", Tier: "historical", Dimension: `{"technical_tasks":"planning"}`},
		{Dimension: `{"manual_labels":"label-0"}`, Hidden: "include"},
		{Year: 2030, Venue: venue, Source: "fixture-a", Hidden: "include"},
		{Year: 1999},
	}
	for u, user := range users {
		for i, query := range queries {
			t.Run(fmt.Sprintf("user-%d/query-%d", u, i), func(t *testing.T) {
				want, err := service.facetsWithBatchSize(user, query, 1000)
				require.NoError(t, err)
				// Tiny batches force boundaries across the rich fixture; the full-history diagnostic
				// separately crosses both real 1000/5000 boundaries using all 39,495 memberships.
				for _, size := range []int{1, 2, 5000, 0, 5001} {
					got, err := service.facetsWithBatchSize(user, query, size)
					require.NoError(t, err)
					require.Equal(t, want, got, "batch=%d", size)
				}
				got, err := service.Facets(user, query)
				require.NoError(t, err)
				require.Equal(t, want, got)
			})
		}
	}
	corrected, err := service.Facets(users[1], PaperQuery{Year: 2030, Hidden: "include"})
	require.NoError(t, err)
	require.Equal(t, []FacetVO{{Value: "2030", Count: 1}}, corrected.Years)
	require.Equal(t, []FacetVO{{Value: venue, Count: 1}}, corrected.Venues)
	require.Equal(t, []FacetVO{{Value: "fixture-a", Count: 1}, {Value: "fixture-b", Count: 1}}, corrected.Sources)
	require.Equal(t, []FacetVO{{Value: "reading", Count: 1}}, corrected.ReadStatuses)
	require.Equal(t, []FacetVO{{Value: "label-0", Count: 1}, {Value: "shared-label", Count: 1}}, corrected.Dimensions["manual_labels"])
	require.Equal(t, []FacetVO{{Value: "historical", Count: 1}, {Value: "secondary", Count: 1}}, corrected.Tiers)
}
