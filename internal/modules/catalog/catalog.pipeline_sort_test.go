package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	sql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/entx"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
)

// TestModelPrioritySQLLimitsBeforeScoring 验证先锁定最新版本再评分且主题参数绑定，分页应用于服务端排序之后。
func TestModelPrioritySQLLimitsBeforeScoring(t *testing.T) {
	table := sql.Table("glean_paper")
	selector := sql.Dialect(dialect.Postgres).Select(table.C("id")).From(table)
	attack := "topic' OR TRUE--"
	modelPriorityOrder(attack)(selector)
	selector.OrderBy(sql.Desc(table.C("id"))).Limit(20).Offset(20)
	statement, args := selector.Query()
	require.Contains(t, statement, "ORDER BY")
	require.Contains(t, statement, `"observed_at" DESC`)
	require.Contains(t, statement, "LIMIT 1) latest_pipeline")
	require.Contains(t, statement, "stage->>'status' = 'completed'")
	require.Contains(t, statement, "BETWEEN 0 AND 1")
	require.Contains(t, statement, ") DESC NULLS LAST")
	require.NotContains(t, statement, attack)
	require.Contains(t, args, attack)
	q := PaperQuery{Sort: "model-priority"}
	require.NoError(t, validateQuery(&q))
}

// TestModelPriorityPostgres 验证跨分页主题分排序、未知后置与新版本不回退旧评分。
func TestModelPriorityPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client, _, _ := newCatalogPostgresTest(t, ctx)
	_, err := client.SysUser.Create().SetID(7).SetUsername("priority-test").SetPasswordHash("test-only").Save(ctx)
	require.NoError(t, err)
	service := NewService(client)
	ctx = entx.WithOperatorID(requestctx.WithUserID(ctx, "7"), 7)
	for i, score := range []float64{0.3, 0.9, 0.6} {
		input := observationFixture()
		input.SourceID = fmt.Sprintf("priority-%d", i)
		input.DOI = ""
		input.Title = input.SourceID
		input.Topics = []string{"biomed-ai", "llm-agent"}
		input.Review.Pipeline = json.RawMessage(fmt.Sprintf(`{"stages":[{"name":"classification","status":"completed","result":{"topics":[{"topic":"biomed-ai","score":%v},{"topic":"llm-agent","score":%v}]}}]}`, score, 1-score))
		_, err = service.IngestObservation(ctx, input)
		require.NoError(t, err)
		if i == 1 {
			input.Version = "v2"
			input.ObservedAt = input.ObservedAt.Add(time.Hour)
			input.Pipeline = nil
			input.Review.Pipeline = json.RawMessage(`{"stages":[{"name":"classification","status":"pending"}]}`)
			_, err = service.IngestObservation(ctx, input)
			require.NoError(t, err)
		}
	}
	for page, want := range []string{"priority-2", "priority-0", "priority-1"} {
		got, err := service.Papers(ctx, PaperQuery{Topic: "biomed-ai", Sort: "model-priority", Page: page + 1, PageSize: 1})
		require.NoError(t, err)
		require.Equal(t, 3, got.Total)
		require.Equal(t, want, got.Items[0].Title)
	}
	got, err := service.Papers(ctx, PaperQuery{Topic: "llm-agent", Sort: "model-priority", Page: 1, PageSize: 1})
	require.NoError(t, err)
	require.Equal(t, "priority-0", got.Items[0].Title)
}
