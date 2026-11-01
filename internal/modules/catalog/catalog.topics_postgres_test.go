package catalog

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/data/ent/gleanpaperreview"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// TestCatalogTopicCountsPostgres 验证成员行计数、原始历史状态及按主题生效的精确审核规则。
func TestCatalogTopicCountsPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client, _, _ := newCatalogPostgresTest(t, ctx)
	service := NewService(client)
	for i, slug := range []string{"biomed-ai", "empty-custom", "active-custom"} {
		_, err := client.GleanTopic.Create().SetSlug(slug).SetName("Configured " + slug).SetSearchEnabled(false).SetRule("fixture rule").SetTermGroups([][]string{{"fixture"}}).SetCreatedAt(time.Date(2026, 1, 1, 0, 0, i, 0, time.UTC)).Save(ctx)
		require.NoError(t, err)
	}
	ids := map[int]idgen.ID{}
	for i := 1; i <= 8; i++ {
		p, err := client.GleanPaper.Create().SetIdentityKey(fmt.Sprintf("topic-fixture-%d", i)).SetTitle("Topic fixture").Save(ctx)
		require.NoError(t, err)
		ids[i] = p.ID
	}
	for i, row := range []struct {
		paper                     int
		topic, status, key, value string
	}{
		{1, "biomed-ai", "historical", "collection", "正式纳入"},
		{1, "biomed-ai", "historical", "collection", "正式纳入"}, // 同一论文的两个原始编号仍是两个成员。
		{2, "biomed-ai", "historical", "collection", "正式纳入"},
		{3, "biomed-ai", "historical", "collection", "正式纳入"},
		{4, "biomed-ai", "pending-review", "collection", "正式纳入"},
		{5, "biomed-ai", "pending-review", "collection", "正式纳入"},
		{6, "biomed-ai", "pending-review", "collection", "正式纳入"},
		{7, "biomed-ai", "historical", "display_collection", "正式纳入"},
		{8, "biomed-ai", "historical", "collection", "非正式纳入"},
		{1, "llm-agent", "historical", "collection", "正式纳入"}, // 错主题的键不得自动精选。
		{2, "llm-agent", "historical", "display_collection", "正式纳入"},
		{3, "biomed-worldmodel", "historical", "eligibility_status", "正式纳入"},
		{1, "active-custom", "historical", "collection", "正式纳入"},
		{4, "active-custom", "pending-review", "collection", "正式纳入"},
	} {
		_, err := client.GleanMembership.Create().SetPaperID(ids[row.paper]).SetTopic(row.topic).SetOriginalID(fmt.Sprintf("original-%d", i)).SetInclusionStatus(row.status).SetDimensions(map[string][]string{row.key: {row.value}}).Save(ctx)
		require.NoError(t, err)
	}
	for _, row := range []struct {
		paper  int
		topic  string
		status gleanpaperreview.Status
	}{
		{2, "biomed-ai", gleanpaperreview.StatusExcluded},
		{3, "biomed-ai", gleanpaperreview.StatusPending},
		{4, "biomed-ai", gleanpaperreview.StatusIncluded},
		{5, "biomed-ai", gleanpaperreview.StatusExcluded},
		{4, "active-custom", gleanpaperreview.StatusIncluded},
	} {
		_, err := client.GleanPaperReview.Create().SetPaperID(ids[row.paper]).SetTopic(row.topic).SetStatus(row.status).Save(ctx)
		require.NoError(t, err)
	}
	configs, err := service.topicConfigs(ctx)
	require.NoError(t, err)
	got, err := service.Topics(ctx)
	require.NoError(t, err)
	require.Len(t, got, 5)
	want := map[string][4]int{
		"biomed-ai": {9, 6, 2, 3}, "llm-agent": {2, 2, 0, 1}, "biomed-worldmodel": {1, 1, 0, 1},
		"empty-custom": {0, 0, 0, 0}, "active-custom": {2, 1, 0, 1},
	}
	for i, item := range got {
		require.Equal(t, want[item.Slug], [4]int{item.MembershipCount, item.HistoricalCount, item.CandidateCount, item.CuratedCount}, item.Slug)
		item.MembershipCount, item.HistoricalCount, item.CandidateCount, item.CuratedCount = 0, 0, 0, 0
		require.Equal(t, configs[i], item, "configuration and ordering must be unchanged")
	}
	require.Equal(t, "empty-custom", got[3].Slug)
	require.Equal(t, "active-custom", got[4].Slug)
}
