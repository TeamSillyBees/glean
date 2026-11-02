package catalog

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/entx"
)

func pipelineArchiveFixture() ObservationInput {
	input := observationFixture()
	input.Abstract = "Complete original abstract retained for retry"
	input.Review.Pipeline = json.RawMessage(`{"candidateId":"321","key":"record-key","runId":"7001","status":"needs_human_review","templates":[{"topic":"biomed-ai","version":"v1"}],"history":[{"status":"completed"}]}`)
	input.Review.Summary = "latest model summary"
	return input
}

// TestPipelineArchiveReadsFullSourceAndLatestReview 验证归档恢复不经过材料瘦身且采用最新review和追加主题命中。
func TestPipelineArchiveReadsFullSourceAndLatestReview(t *testing.T) {
	for _, byKey := range []bool{false, true} {
		t.Run(map[bool]string{false: "candidate-id", true: "source-key"}[byKey], func(t *testing.T) {
			service, mock := catalogMock(t)
			input := pipelineArchiveFixture()
			metadata, _ := json.Marshal(input)
			review, _ := json.Marshal(input.Review)
			query := mock.ExpectQuery(`SELECT .*FROM "glean_observation".*"review".*ORDER BY.*"updated_at" DESC.*LIMIT 1`)
			if byKey {
				query.WithArgs(input.Source, input.SourceID, "record-key")
			} else {
				query.WithArgs("321")
			}
			query.WillReturnRows(sqlmock.NewRows([]string{"id", "metadata", "review"}).AddRow(12, metadata, review))
			mock.ExpectQuery(`SELECT .*FROM "glean_observation_match"`).WithArgs(int64(12)).WillReturnRows(sqlmock.NewRows([]string{"id", "observation_id", "topic"}).AddRow(13, 12, "llm-agent"))
			var got ObservationInput
			var err error
			if byKey {
				var found bool
				got, found, err = service.FindPipelineObservation(context.Background(), input.Source, input.SourceID, "record-key")
				require.True(t, found)
			} else {
				got, err = service.LoadPipelineObservation(context.Background(), 321)
			}
			require.NoError(t, err)
			require.Equal(t, input.Abstract, got.Abstract)
			require.JSONEq(t, string(input.Raw), string(got.Raw))
			require.JSONEq(t, string(input.Review.Pipeline), string(got.Review.Pipeline))
			require.JSONEq(t, string(got.Pipeline), string(got.Review.Pipeline))
			require.Equal(t, []string{"biomed-ai", "llm-agent"}, got.Topics)
		})
	}
}

// TestPipelineArchiveMissingRecordIsNotAFalseHit 验证来源key不命中返回false；数据库错误不能误判为未处理并重新付费。
func TestPipelineArchiveMissingRecordIsNotAFalseHit(t *testing.T) {
	service, mock := catalogMock(t)
	mock.ExpectQuery(`SELECT .*FROM "glean_observation"`).WithArgs("crossref", "x", "different-version").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	_, found, err := service.FindPipelineObservation(context.Background(), "crossref", "x", "different-version")
	require.NoError(t, err)
	require.False(t, found)
	mock.ExpectQuery(`SELECT .*FROM "glean_observation"`).WillReturnError(context.DeadlineExceeded)
	_, found, err = service.FindPipelineObservation(context.Background(), "crossref", "x", "same-version")
	require.Error(t, err)
	require.False(t, found)
}

// TestPipelineArchivePostgres 验证真实归档跨重投影保留原材料和人工审核，热状态之外仍可按id/key恢复。
func TestPipelineArchivePostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client, _, _ := newCatalogPostgresTest(t, ctx)
	_, err := client.SysUser.Create().SetID(7).SetUsername("archive-test").SetPasswordHash("test-only").Save(ctx)
	require.NoError(t, err)
	ctx = entx.WithOperatorID(ctx, 7)
	service := NewService(client)
	input := pipelineArchiveFixture()
	first, err := service.IngestObservation(ctx, input)
	require.NoError(t, err)
	manual, err := client.GleanPaperReview.Create().SetPaperID(first.PaperID).SetTopic("biomed-ai").SetStatus("included").SetNote("human evidence must remain").Save(ctx)
	require.NoError(t, err)
	original, err := client.GleanObservation.Get(ctx, first.ObservationID)
	require.NoError(t, err)
	input.Topics = append(input.Topics, "llm-agent")
	input.Review.Summary = "updated independently reviewed summary"
	second, err := service.IngestObservation(ctx, input)
	require.NoError(t, err)
	require.Equal(t, first.ObservationID, second.ObservationID)
	got, err := service.LoadPipelineObservation(ctx, 321)
	require.NoError(t, err)
	require.Equal(t, input.Abstract, got.Abstract)
	require.JSONEq(t, string(input.Raw), string(got.Raw))
	require.Equal(t, input.Review.Summary, got.Review.Summary)
	require.Equal(t, []string{"biomed-ai", "llm-agent"}, got.Topics)
	foundInput, found, err := service.FindPipelineObservation(ctx, input.Source, input.SourceID, "record-key")
	require.NoError(t, err)
	require.True(t, found)
	require.JSONEq(t, string(got.Pipeline), string(foundInput.Pipeline))
	_, found, err = service.FindPipelineObservation(ctx, input.Source, "other-source-id", "record-key")
	require.NoError(t, err)
	require.False(t, found)
	saved, err := client.GleanObservation.Get(ctx, first.ObservationID)
	require.NoError(t, err)
	require.JSONEq(t, string(original.Metadata), string(saved.Metadata))
	untouched, err := client.GleanPaperReview.Get(ctx, manual.ID)
	require.NoError(t, err)
	require.Equal(t, manual.Status, untouched.Status)
	require.Equal(t, manual.Note, untouched.Note)
}

// TestPipelineArchiveDoesNotReviveStaleMetadataReview 验证最新review无档案时不能复活metadata内过期模型结果。
func TestPipelineArchiveDoesNotReviveStaleMetadataReview(t *testing.T) {
	service, mock := catalogMock(t)
	metadata, _ := json.Marshal(pipelineArchiveFixture())
	mock.ExpectQuery(`SELECT .*FROM "glean_observation"`).WillReturnRows(sqlmock.NewRows([]string{"id", "metadata", "review"}).AddRow(12, metadata, `{"summary":"no current pipeline"}`))
	_, err := service.LoadPipelineObservation(context.Background(), 321)
	require.ErrorContains(t, err, "论文处理归档无效")
}
