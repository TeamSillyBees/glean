package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/baseline"
	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
)

// catalogQueryCounter 只计数、不记录 SQL/参数；这些诊断场景均为串行只读查询。
type catalogQueryCounter struct {
	dialect.Driver
	queries int
}

func (d *catalogQueryCounter) Query(ctx context.Context, query string, args, result any) error {
	d.queries++
	return d.Driver.Query(ctx, query, args, result)
}

// TestCatalogHistoricalQueryDiagnostics 在完整真实快照上记录有界查询样本，不把硬件耗时当作 SLO。
func TestCatalogHistoricalQueryDiagnostics(t *testing.T) {
	if os.Getenv("GLEAN_CATALOG_QUERY_DIAGNOSTICS") != "1" {
		t.Skip("set GLEAN_CATALOG_QUERY_DIAGNOSTICS=1 for full historical PostgreSQL diagnostics")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	client, tx, driver := newCatalogPostgresTest(t, ctx)
	_, err := client.SysUser.Create().SetID(7).SetUsername("catalog-diagnostics").SetPasswordHash("test-only-not-a-login-credential").Save(ctx)
	require.NoError(t, err)
	ctx = requestctx.WithUserID(ctx, "7")
	service := NewService(client)
	directory := filepath.Join("..", "..", "..", "data", "baselines")
	manifest, err := baseline.ReadManifest(filepath.Join(directory, "manifest.json"))
	require.NoError(t, err)
	require.NoError(t, baseline.Verify(directory, manifest))
	start := time.Now()
	report, err := baseline.Import(ctx, directory, manifest, func(ctx context.Context, raw json.RawMessage) (bool, error) {
		var record ImportRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			return false, err
		}
		result, err := service.Import(ctx, record)
		return result.Created, err
	}, io.Discard)
	importElapsed := time.Since(start)
	require.NoError(t, err, "full snapshot import failed; quarantined=%d", report.Quarantined)
	require.True(t, report.Complete)
	require.Equal(t, 39495, report.Created)
	require.Equal(t, map[string]int{"biomed-ai": 31399, "llm-agent": 4059, "biomed-worldmodel": 4037}, report.ByTopic)
	papers, err := client.GleanPaper.Query().Count(ctx)
	require.NoError(t, err)
	members, err := client.GleanMembership.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, report.Created, members)
	var version string
	require.NoError(t, tx.QueryRowContext(ctx, "SHOW server_version").Scan(&version))
	t.Logf("CATALOG_DIAGNOSTIC setup go=%s postgres=%s cpus=%d memberships=%d papers=%d import_ms=%.3f", runtime.Version(), version, runtime.NumCPU(), members, papers, float64(importElapsed.Microseconds())/1000)
	// Freshly imported tables have no autovacuum statistics within this open transaction.
	// Analyze only this isolated schema's tables, never the rest of the connected database.
	for _, table := range []string{"glean_paper", "glean_membership", "glean_observation", "glean_paper_state", "glean_paper_review", "glean_paper_correction"} {
		_, err := tx.ExecContext(ctx, "ANALYZE "+table)
		require.NoError(t, err)
	}
	counter := &catalogQueryCounter{Driver: driver}
	measured := NewService(ent.NewClient(ent.Driver(counter)))
	sample, err := service.Papers(ctx, PaperQuery{Topic: "biomed-ai", CollectionScope: "all", PageSize: 1})
	require.NoError(t, err)
	require.NotEmpty(t, sample.Items)
	selected := sample.Items[0]
	combined := PaperQuery{Topic: "biomed-ai", CollectionScope: "all", Year: selected.Year, Venue: selected.Venue, PageSize: 20}
	// Pick a deterministic, existing exact JSONB dimension without inventing labels.
	for _, membership := range selected.Memberships {
		if membership.Topic != "biomed-ai" {
			continue
		}
		keys := make([]string, 0, len(membership.Dimensions))
		for key, values := range membership.Dimensions {
			if len(values) > 0 {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		require.NotEmpty(t, keys)
		dimension, err := json.Marshal(map[string]string{keys[0]: membership.Dimensions[keys[0]][0]})
		require.NoError(t, err)
		combined.Dimension = string(dimension)
	}
	require.NotEmpty(t, combined.Dimension)

	type scenario struct {
		name string
		run  func(*testing.T) int
	}
	cases := []scenario{
		{"topics", func(t *testing.T) int {
			rows, err := measured.Topics(ctx)
			require.NoError(t, err)
			require.Len(t, rows, 3)
			return len(rows)
		}},
		{"detail", func(t *testing.T) int {
			row, err := measured.Paper(ctx, selected.ID)
			require.NoError(t, err)
			require.Equal(t, selected.ID, row.ID)
			return 1
		}},
	}
	lists := []struct {
		name  string
		query PaperQuery
	}{
		{"list/all", PaperQuery{CollectionScope: "all"}},
		{"list/curated", PaperQuery{CollectionScope: "curated"}},
		{"list/combined", combined},
		{"list/deep-page", PaperQuery{CollectionScope: "all", Page: 100, PageSize: 20}},
		{"list/search", PaperQuery{CollectionScope: "all", Q: "learning"}},
	}
	for _, sort := range []string{"newest", "oldest", "title", "observed", "source-updated"} {
		lists = append(lists, struct {
			name  string
			query PaperQuery
		}{"sort/" + sort, PaperQuery{Topic: "biomed-ai", CollectionScope: "all", Sort: sort}})
	}
	for _, item := range lists {
		cases = append(cases, scenario{item.name, func(t *testing.T) int {
			page, err := measured.Papers(ctx, item.query)
			require.NoError(t, err)
			require.NotEmpty(t, page.Items)
			if item.name == "list/all" {
				require.Equal(t, papers, page.Total)
			}
			return page.Total
		}})
	}
	for _, item := range []struct {
		name  string
		query PaperQuery
	}{
		{"facets/all", PaperQuery{CollectionScope: "all"}},
		{"facets/curated", PaperQuery{CollectionScope: "curated"}},
		{"facets/topic", PaperQuery{Topic: "biomed-ai", CollectionScope: "all"}},
		{"facets/combined", combined},
	} {
		// Both candidates share the same imported snapshot and compare every field and slice order.
		expected, err := measured.facetsWithBatchSize(ctx, item.query, 1000)
		require.NoError(t, err)
		for _, batchSize := range []int{1000, facetAssociationBatchSize} {
			cases = append(cases, scenario{fmt.Sprintf("%s/batch-%d", item.name, batchSize), func(t *testing.T) int {
				facets, err := measured.facetsWithBatchSize(ctx, item.query, batchSize)
				require.NoError(t, err)
				require.Equal(t, expected, facets, "complete facets, including ordering, must be identical")
				require.NotEmpty(t, facets.ReadStatuses)
				total := 0
				for _, facet := range facets.ReadStatuses {
					total += facet.Count
				}
				if item.name == "facets/all" {
					require.Equal(t, papers, total)
				}
				return total
			}})
		}
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			expected := item.run(t) // One unreported warmup; neither disk-cold nor production latency.
			for iteration := 1; iteration <= 3; iteration++ {
				counter.queries = 0
				started := time.Now()
				rows := item.run(t)
				elapsed := time.Since(started)
				require.Equal(t, expected, rows)
				require.Positive(t, counter.queries)
				t.Logf("CATALOG_DIAGNOSTIC scenario=%s sample=%d elapsed_ms=%.3f sql_queries=%d matched=%d", item.name, iteration, float64(elapsed.Microseconds())/1000, counter.queries, rows)
			}
		})
	}
}

// TestCatalogQueryCounter 验证实际 Ent Count 经由计数器并可逐样本复位，不需要数据库。
func TestCatalogQueryCounter(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	counter := &catalogQueryCounter{Driver: entsql.OpenDB(dialect.Postgres, db)}
	client := ent.NewClient(ent.Driver(counter))
	for iteration := 0; iteration < 2; iteration++ {
		counter.queries = 0
		mock.ExpectQuery(`SELECT COUNT.* FROM "glean_paper"`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(42))
		count, err := client.GleanPaper.Query().Count(context.Background())
		require.NoError(t, err)
		require.Equal(t, 42, count)
		require.Equal(t, 1, counter.queries)
	}
	require.NoError(t, mock.ExpectationsWereMet())
}
