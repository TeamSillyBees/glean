package catalog

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// TestVenuePageSQL 验证只加载当前页，按指标降序且缺失指标后置，排序末尾使用稳定 ID。
func TestVenuePageSQL(t *testing.T) {
	service, mock := catalogMock(t)
	mock.ExpectQuery(`SELECT COUNT\(.* FROM "glean_venue"`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(45))
	mock.ExpectQuery(`SELECT .* FROM "glean_venue" ORDER BY "glean_venue"\."jif" DESC NULLS LAST, "glean_venue"\."name" ASC, "glean_venue"\."metric_year" DESC, "glean_venue"\."registry_version" DESC, "glean_venue"\."id" DESC LIMIT 20 OFFSET 20`).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "jif"}).AddRow(22, "Page two journal", 12.0))
	page, err := service.Venues(context.Background(), VenueQuery{Page: 2})
	require.NoError(t, err)
	require.Equal(t, 45, page.Total)
	require.Equal(t, 20, page.PageSize)
	require.Equal(t, idgen.ID(22), page.Items[0].ID)
}

// TestVenuePageRejectsUnboundedInput 防止绕过 HTTP 的调用请求无限页长或不受支持的排序。
func TestVenuePageRejectsUnboundedInput(t *testing.T) {
	for _, q := range []VenueQuery{{Page: -1}, {Page: 100001}, {PageSize: 101}, {PageSize: -1}, {Sort: "jif;drop table"}, {Kind: "unknown"}} {
		service, _ := catalogMock(t)
		_, err := service.Venues(context.Background(), q)
		require.Error(t, err)
	}
}

// TestVenuePaginationPostgres 验证多页无重叠、同分稳定、空值后置以及筛选后的真实总数。
func TestVenuePaginationPostgres(t *testing.T) {
	ctx := context.Background()
	client, _, _ := newCatalogPostgresTest(t, ctx)
	service := NewService(client)
	for i, score := range []*float64{ptrJIF(15), ptrJIF(8), nil, ptrJIF(15), ptrJIF(2)} {
		kind := "journal"
		if score == nil {
			kind = "conference"
		}
		_, err := client.GleanVenue.Create().SetID(idgen.ID(100 + i)).SetName("Pagination venue").SetKind(kind).SetNillableJif(score).SetMetricYear(2026 - i).SetRegistryVersion(idgen.ID(100 + i).String()).SetSource("isolated fixture").SetEligibleTopics([]string{"biomed-ai"}).Save(ctx)
		require.NoError(t, err)
	}
	got := []idgen.ID{}
	for page := 1; page <= 3; page++ {
		result, err := service.Venues(ctx, VenueQuery{Topic: "biomed-ai", Page: page, PageSize: 2})
		require.NoError(t, err)
		require.Equal(t, 5, result.Total)
		for _, row := range result.Items {
			got = append(got, row.ID)
		}
	}
	require.Equal(t, []idgen.ID{100, 103, 101, 104, 102}, got, "metric ties use year; unknown impact factors stay last")
	filtered, err := service.Venues(ctx, VenueQuery{Kind: "conference", Q: "Pagination", Sort: "jif_asc"})
	require.NoError(t, err)
	require.Equal(t, 1, filtered.Total)
	require.Nil(t, filtered.Items[0].JIF)
	ascending, err := service.Venues(ctx, VenueQuery{Sort: "jif_asc", PageSize: 100})
	require.NoError(t, err)
	require.Equal(t, idgen.ID(104), ascending.Items[0].ID)
	require.Nil(t, ascending.Items[4].JIF)
	empty, err := service.Venues(ctx, VenueQuery{Q: "absent venue"})
	require.NoError(t, err)
	require.Zero(t, empty.Total)
	require.Empty(t, empty.Items)
}

func ptrJIF(value float64) *float64 { return &value }
