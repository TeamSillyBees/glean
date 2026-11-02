package catalog

import (
	"context"
	"encoding/json"
	"entgo.io/ent/dialect"
	sql "entgo.io/ent/dialect/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/entx"
)

func observationFixture() ObservationInput {
	return ObservationInput{RunID: 7001, Source: "crossref", SourceID: "10.9999/source", Title: "Real source title", DOI: "10.9999/source", ObservedAt: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), Topics: []string{"biomed-ai"}, Raw: json.RawMessage(`{"DOI":"10.9999/source","title":["Real source title"]}`), Review: ReviewVO{Provider: "mock", IsMock: true, ScientificReviewed: true, InclusionStatus: "included"}}
}

// TestObservationIdentityIgnoresRetries 验证重复采集、重复主题和模拟输出不会制造新的来源内容版本。
func TestObservationIdentityIgnoresRetries(t *testing.T) {
	first := observationFixture()
	hash, err := normalizeObservation(&first)
	require.NoError(t, err)
	require.False(t, first.Review.ScientificReviewed)
	require.Equal(t, "pending-review", first.Review.InclusionStatus)
	second := observationFixture()
	second.RunID++
	second.ObservedAt = second.ObservedAt.Add(time.Hour)
	second.Topics = []string{"llm-agent", "biomed-ai"}
	second.CoreDiscovery = true
	second.Review.Summary = "different mock text"
	second.Raw = json.RawMessage(`{ "title": ["Real source title"], "DOI": "10.9999/source" }`)
	hash2, err := normalizeObservation(&second)
	require.NoError(t, err)
	require.Equal(t, hash, hash2)
	second.Version = "v2"
	hash2, err = normalizeObservation(&second)
	require.NoError(t, err)
	require.NotEqual(t, hash, hash2)
}

// TestFormalDatesDoNotResolveConflicts 验证接收日期和冲突正式日期不会被擅自折算为单一日期。
func TestFormalDatesDoNotResolveConflicts(t *testing.T) {
	a := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b := a.AddDate(0, 1, 0)
	events := []PublicationEventVO{{Kind: "accepted", Date: a, Evidence: "publisher"}, {Kind: "online", Date: a, Evidence: "publisher"}, {Kind: "online", Date: b, Evidence: "publisher correction"}}
	require.Nil(t, exactFormalDate(events, "online"))
	require.Nil(t, exactFormalDate(events, "issue"))
	require.NotNil(t, exactFormalDate(events[:2], "online"))
}

// TestObservationRequiresActiveAuditOperator 验证来源投影没有匿名审计后门。
func TestObservationRequiresActiveAuditOperator(t *testing.T) {
	service, mock := catalogMock(t)
	_, err := service.IngestObservation(context.Background(), observationFixture())
	require.ErrorContains(t, err, "explicit monitor audit operator")
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "sys_user".* FOR SHARE`).WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()
	_, err = service.IngestObservation(entx.WithOperatorID(context.Background(), 7), observationFixture())
	require.Error(t, err)
}

// TestISSNChecksumValidation 验证别名必须是真实格式且校验和正确的 ISSN。
func TestISSNChecksumValidation(t *testing.T) {
	require.Equal(t, []string{"0028-0836", "1476-4687"}, normalizeISSNs([]string{"00280836", "1476-4687", "0028-0836", "1234-0000", "not-an-issn"}))
}

// TestSourceMetadataIsNotPaperVersion 验证索引或引用元数据变化不伪装成论文新版本。
func TestSourceMetadataIsNotPaperVersion(t *testing.T) {
	require.Equal(t, "metadata_updated", sourceChangeType("crossref", "1", "2"))
	require.Equal(t, "metadata_updated", sourceChangeType("arxiv", "2026-10-25", "2026-10-26"))
	require.Equal(t, "version_updated", sourceChangeType("arxiv", "v1", "v2"))
	require.Equal(t, "metadata_updated", sourceChangeType("biorxiv", "2", "1"))
}

// TestObservationPMIDNormalization 验证跨来源标识严格规范化而不允许非数字伪标识。
func TestObservationPMIDNormalization(t *testing.T) {
	input := observationFixture()
	input.PMID = " 0012345678 "
	_, err := normalizeObservation(&input)
	require.NoError(t, err)
	require.Equal(t, "12345678", input.PMID)
	for _, invalid := range []string{"0", "MED:123", "-1", "1.2", "12' OR 1=1"} {
		input.PMID = invalid
		_, err = normalizeObservation(&input)
		require.Error(t, err)
	}
}

// TestObservationPermanentRejectionClassification 验证只有单条坏数据隔离，不能把审计配置或数据库失败吞为成功。
func TestObservationPermanentRejectionClassification(t *testing.T) {
	service, _ := catalogMock(t)
	invalid := observationFixture()
	invalid.Title = ""
	_, err := service.IngestObservation(context.Background(), invalid)
	kind, rejected := ObservationRejectionKind(err)
	require.True(t, rejected)
	require.Equal(t, "invalid_source_record", kind)
	_, err = service.IngestObservation(context.Background(), observationFixture())
	_, rejected = ObservationRejectionKind(err)
	require.False(t, rejected, "missing audit operator is not a record rejection")
	_, rejected = ObservationRejectionKind(errors.New("database unavailable"))
	require.False(t, rejected)
}

// TestObservationSearchBindsLiteralText 验证来源别名查询使用固定字段与绑定字面量，并以 EXISTS 避免重复版本计数。
func TestObservationSearchBindsLiteralText(t *testing.T) {
	attack := "Needle' OR TRUE --%_"
	table := sql.Table("glean_paper")
	selector := sql.Dialect(dialect.Postgres).Select(table.C("id")).From(table)
	observedTextPredicate(attack)(selector)
	statement, args := selector.Query()
	require.NotContains(t, statement, attack)
	require.Contains(t, statement, "EXISTS")
	require.Contains(t, statement, "->>'title' ILIKE $1")
	require.Contains(t, fmt.Sprint(args), `needle' or true --\%\_`)
	require.Len(t, args, 6)
	require.NotContains(t, statement, "->>'raw'")
	require.NotContains(t, statement, "->>'review'")
}

// TestPipelineProgressDoesNotChangeSourceIdentity 验证后续阶段不会创建新来源版本或自动科学纳入。
func TestPipelineProgressDoesNotChangeSourceIdentity(t *testing.T) {
	first := observationFixture()
	hash, err := normalizeObservation(&first)
	require.NoError(t, err)
	second := observationFixture()
	second.Pipeline = json.RawMessage(`{"status":"ready_for_display","stages":[{"name":"summary","status":"completed"}]}`)
	second.Review.IsMock = false
	second.Review.ScientificReviewed = true
	nextHash, err := normalizeObservation(&second)
	require.NoError(t, err)
	require.Equal(t, hash, nextHash)
	require.False(t, second.Review.ScientificReviewed)
	require.Equal(t, "pending-review", second.Review.InclusionStatus)
	require.JSONEq(t, string(second.Pipeline), string(second.Review.Pipeline))
}

// TestPipelineReviewPreservesExistingScientificDecision 验证进度可更新但既有科学判定与扩展字段不能被覆写。
func TestPipelineReviewPreservesExistingScientificDecision(t *testing.T) {
	existing := map[string]any{"scientificReviewed": true, "inclusionStatus": "included", "manualNote": "keep", "summary": "old"}
	incoming := ReviewVO{Summary: "真实材料总结", InclusionStatus: "pending-review", Pipeline: json.RawMessage(`{"status":"needs_human_review"}`)}
	merged, err := mergePipelineReview(existing, incoming)
	require.NoError(t, err)
	require.Equal(t, true, merged["scientificReviewed"])
	require.Equal(t, "included", merged["inclusionStatus"])
	require.Equal(t, "keep", merged["manualNote"])
	require.Equal(t, "真实材料总结", merged["summary"])
	require.Equal(t, "old", existing["summary"], "输入map不应被就地改写")
	require.Equal(t, "needs_human_review", merged["pipeline"].(map[string]any)["status"])
}

// TestPipelineReprojectionUpdatesOnlyReview 验证已存在来源执行UPDATE而非丢弃后续模型结果，且不写主题/人工分类。
func TestPipelineReprojectionUpdatesOnlyReview(t *testing.T) {
	service, mock := catalogMock(t)
	input := observationFixture()
	input.DOI = ""
	input.Review.Pipeline = json.RawMessage(`{"status":"needs_human_review"}`)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "sys_user".* FOR SHARE`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
	mock.ExpectQuery(`SELECT .*FROM "glean_paper".* FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"id", "identity_key", "title"}).AddRow(11, "source:crossref:10.9999/source", "original"))
	mock.ExpectQuery(`SELECT .*FROM "glean_observation"`).WillReturnRows(sqlmock.NewRows([]string{"id", "paper_id", "review"}).AddRow(12, 11, `{"scientificReviewed":false,"summary":"pending"}`))
	mock.ExpectExec(`UPDATE "glean_observation" SET "review" = .*"updated_at" = .*WHERE`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT .*FROM "glean_observation"`).WillReturnRows(sqlmock.NewRows([]string{"id", "paper_id"}).AddRow(12, 11))
	mock.ExpectQuery(`SELECT .*FROM "glean_observation_match"`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(13))
	mock.ExpectQuery(`SELECT .*FROM "glean_membership"`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(14))
	mock.ExpectCommit()
	result, err := service.IngestObservation(entx.WithOperatorID(context.Background(), 7), input)
	require.NoError(t, err)
	require.False(t, result.Created)
	require.Equal(t, 0, result.MembershipsAdded)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestPipelineDTOUsesLatestReviewProjection 验证详情展示最新处理review而非首次不可变metadata里的pending快照。
func TestPipelineDTOUsesLatestReviewProjection(t *testing.T) {
	service, mock := catalogMock(t)
	pending := `{"pipeline":{"status":"processing"},"source":"crossref","sourceId":"x"}`
	completed := `{"pipeline":{"status":"needs_human_review"},"summary":"已阅读真实摘要","scientificReviewed":false}`
	mock.ExpectQuery(`SELECT .*FROM "glean_observation"`).WillReturnRows(sqlmock.NewRows([]string{"id", "paper_id", "observed_at", "metadata", "review"}).AddRow(12, 11, time.Now(), pending, completed))
	mock.ExpectQuery(`SELECT .*FROM "glean_observation_match"`).WillReturnRows(sqlmock.NewRows([]string{"id", "observation_id", "topic"}).AddRow(13, 12, "biomed-ai"))
	items := []PaperVO{{ID: 11}}
	require.NoError(t, service.attachObservations(context.Background(), items, true))
	require.JSONEq(t, `{"status":"needs_human_review"}`, string(items[0].Review.Pipeline))
	require.JSONEq(t, string(items[0].Review.Pipeline), string(items[0].Observations[0].Pipeline))
	require.Equal(t, "已阅读真实摘要", items[0].Review.Summary)
	require.NoError(t, mock.ExpectationsWereMet())
}
