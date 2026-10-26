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
	"github.com/teamsillybees/glean/internal/data/ent/gleanobservation"
	"github.com/teamsillybees/glean/internal/platform/entx"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
)

// TestHistoricalAbstractEnrichmentPostgres 验证真实 PostgreSQL 上的受限补录、幂等性、历史保护和实时新鲜度隔离。
func TestHistoricalAbstractEnrichmentPostgres(t *testing.T) {
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
	schema := "glean_enrichment_test_" + strings.ReplaceAll(time.Now().Format("20060102150405.000000000"), ".", "_")
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
	_, err = idgen.ConfigureDefault(53)
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(&catalogTestDriver{Driver: entsql.NewDriver(dialect.Postgres, entsql.Conn{ExecQuerier: tx}), tx: tx}))
	client.Use(entx.RejectDeleteHook())
	_, err = client.SysUser.Create().SetID(7).SetUsername("enricher").SetPasswordHash("test-only").Save(ctx)
	require.NoError(t, err)
	service := NewService(client)
	operator := entx.WithOperatorID(ctx, 7)
	viewer := requestctx.WithUserID(ctx, "7")
	at := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	records := verifiedAbstractFixtures(t)
	// 即使来源内容正确，缺失历史目标也不允许创建新论文或成员。
	_, err = service.EnrichHistoricalAbstract(operator, records[0], 7001, at)
	require.Error(t, err)
	kind, rejected := ObservationRejectionKind(err)
	require.True(t, rejected)
	require.Equal(t, "identity_conflict", kind)
	require.Zero(t, client.GleanPaper.Query().CountX(ctx))
	require.Zero(t, client.GleanMembership.Query().CountX(ctx))
	ids := []idgen.ID{}
	for _, r := range records {
		imported, err := service.Import(ctx, ImportRecord{Topic: r.Topic, OriginalID: r.OriginalID, DOI: r.DOI, Title: r.Title, ReadStatus: "read", Raw: map[string]any{"abstract_read": false, "method_checked": false, "method_summary": "preserved method note", "collection": "正式纳入"}})
		require.NoError(t, err)
		ids = append(ids, imported.PaperID)
		beforePaper := client.GleanPaper.GetX(ctx, imported.PaperID)
		beforeMember := client.GleanMembership.GetX(ctx, imported.MembershipID)
		preciseAt := at.Add(123456789 * time.Nanosecond)
		first, err := service.EnrichHistoricalAbstract(operator, r, 7001, preciseAt)
		require.NoError(t, err)
		require.True(t, first.Created)
		require.Zero(t, first.MembershipsAdded)
		again, err := service.EnrichHistoricalAbstract(operator, r, 7002, at.Add(time.Hour))
		require.NoError(t, err)
		require.False(t, again.Created)
		require.Equal(t, first.ObservationID, again.ObservationID)
		// 比较持久化全部公开字段，包括原始科学标记与审计时间，确保无暗中覆盖。
		before, _ := json.Marshal(beforePaper)
		after, _ := json.Marshal(client.GleanPaper.GetX(ctx, imported.PaperID))
		require.JSONEq(t, string(before), string(after))
		before, _ = json.Marshal(beforeMember)
		after, _ = json.Marshal(client.GleanMembership.GetX(ctx, imported.MembershipID))
		require.JSONEq(t, string(before), string(after))
		view, err := service.Paper(viewer, imported.PaperID)
		require.NoError(t, err)
		require.Empty(t, view.Abstract)
		require.NotNil(t, view.AbstractEnrichment)
		require.Equal(t, r.Abstract, view.AbstractEnrichment.Abstract)
		require.True(t, preciseAt.Truncate(time.Microsecond).Equal(view.AbstractEnrichment.EnrichedAt))
		require.Nil(t, view.LastObservedAt)
		require.Nil(t, view.LatestSourceUpdatedAt)
		require.Nil(t, view.Review)
		require.Nil(t, view.FirstSeenAt)
		require.Equal(t, "unread", view.ReadStatus)
		require.Equal(t, "read", view.Memberships[0].ReadStatus)
		require.Equal(t, false, view.Memberships[0].Raw["abstract_read"])
		require.Equal(t, 1, view.ObservationCount)
	}
	require.Equal(t, 3, client.GleanPaper.Query().CountX(ctx))
	require.Equal(t, 3, client.GleanMembership.Query().CountX(ctx))
	require.Equal(t, 3, client.GleanObservation.Query().CountX(ctx))
	// 模拟已有来源层的冲突记录：即使 canonical DOI 正确，也不能忽略已知冲突追加正文。
	existing := client.GleanObservation.Query().Where(gleanobservation.PaperID(ids[0])).OnlyX(ctx)
	var conflicting map[string]any
	require.NoError(t, json.Unmarshal(existing.Metadata, &conflicting))
	conflicting["doi"] = "10.9999/observed-conflict"
	wrongMetadata, err := json.Marshal(conflicting)
	require.NoError(t, err)
	client.GleanObservation.UpdateOneID(existing.ID).SetMetadata(wrongMetadata).ExecX(ctx)
	_, err = service.EnrichHistoricalAbstract(operator, records[0], 7003, at)
	require.Error(t, err)
	client.GleanObservation.UpdateOneID(existing.ID).SetMetadata(existing.Metadata).ExecX(ctx)
	require.Equal(t, 3, client.GleanObservation.Query().CountX(ctx))
	// 故障注入：不能仅凭旧 content_hash 对已损坏存储正文报告幂等成功。
	conflicting["doi"] = records[0].DOI
	conflicting["abstract"] = "tampered stored body"
	wrongMetadata, err = json.Marshal(conflicting)
	require.NoError(t, err)
	client.GleanObservation.UpdateOneID(existing.ID).SetMetadata(wrongMetadata).ExecX(ctx)
	_, err = service.EnrichHistoricalAbstract(operator, records[0], 7003, at)
	require.Error(t, err)
	client.GleanObservation.UpdateOneID(existing.ID).SetMetadata(existing.Metadata).ExecX(ctx)
	require.Equal(t, 3, client.GleanObservation.Query().CountX(ctx))
	// 错误 DOI 不会被补录“修复”；事务失败后也没有新观察。
	client.GleanPaper.UpdateOneID(ids[0]).SetDoi("10.9999/conflict").ExecX(ctx)
	_, err = service.EnrichHistoricalAbstract(operator, records[0], 7003, at)
	require.Error(t, err)
	client.GleanPaper.UpdateOneID(ids[0]).SetDoi(records[0].DOI).ExecX(ctx)
	require.Equal(t, 3, client.GleanObservation.Query().CountX(ctx))
	// 补录摘要仍可通过现有来源全文检索找到，跨版本不会重复结果。
	page, err := service.Papers(viewer, PaperQuery{Q: records[0].Abstract[:70], CollectionScope: "all"})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	require.Equal(t, ids[0], page.Items[0].ID)
	require.Nil(t, page.Items[0].LastObservedAt)
	require.Nil(t, page.Items[0].Review)
	// 后写入的缓存补录不覆盖实际来源观察的时间/模型结果，也不算论文版本更新。
	for i := 0; i < 2; i++ {
		r := records[i]
		live := observationFixture()
		live.Source, live.SourceID, live.DOI, live.Title = r.Source, r.SourceID, r.DOI, r.Title
		live.Topics = []string{r.Topic}
		live.ObservedAt = at.Add(-time.Duration(48-i*24) * time.Hour)
		updated := live.ObservedAt.Add(-time.Hour)
		live.SourceUpdatedAt = &updated
		result, err := service.IngestObservation(operator, live)
		require.NoError(t, err)
		require.Zero(t, result.MembershipsAdded)
		view, err := service.Paper(viewer, ids[i])
		require.NoError(t, err)
		require.True(t, live.ObservedAt.Equal(*view.LastObservedAt))
		require.True(t, updated.Equal(*view.LatestSourceUpdatedAt))
		require.True(t, view.Review.IsMock)
		require.NotNil(t, view.AbstractEnrichment)
		require.Equal(t, "source_observed", view.Observations[1].EventType)
	}
	for _, sort := range []string{"observed", "source-updated"} {
		page, err = service.Papers(viewer, PaperQuery{Sort: sort, CollectionScope: "all"})
		require.NoError(t, err)
		require.Equal(t, 3, page.Total)
		require.Equal(t, ids[1], page.Items[0].ID)
		require.True(t, page.Items[0].Review.IsMock)
		require.Equal(t, 2, page.Items[0].ObservationCount)
	}
}
