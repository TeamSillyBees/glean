package monitor_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	atlas "ariga.io/atlas/sql/migrate"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/boot"
	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/modules/catalog"
	"github.com/teamsillybees/glean/internal/modules/monitor"
	"github.com/teamsillybees/glean/internal/platform/entx"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/platform/requestctx"
)

// postgresPaperSource 仅替代文献网络返回；IsMock保持false以经过真实目录入库边界。
type postgresPaperSource struct {
	record monitor.Record
	calls  int
}

func (s *postgresPaperSource) Name() string { return "crossref" }
func (s *postgresPaperSource) Fetch(_ context.Context, in monitor.FetchRequest) (monitor.Page, error) {
	s.calls++
	if in.Template.Topic != "biomed-ai" {
		return monitor.Page{Complete: true}, nil
	}
	return monitor.Page{Complete: true, Records: []monitor.Record{s.record}}, nil
}

// postgresPaperProcessor 只替代模型调用，不替代目录发布、数据库或JSONB持久化。
type postgresPaperProcessor struct {
	missing, failSummary bool
	calls                []string
	beforeCall           func(context.Context)
}

func (p *postgresPaperProcessor) Ready(string) bool { return !p.missing }
func (p *postgresPaperProcessor) Metadata(stage string) monitor.StageMetadata {
	return monitor.StageMetadata{Provider: "postgres-fixture", Model: "fixture-model-v1", PromptVersion: stage + "-fixture-v1"}
}
func (p *postgresPaperProcessor) Process(ctx context.Context, stage string, in monitor.PipelineInput) (monitor.PipelineResult, error) {
	if p.beforeCall != nil {
		p.beforeCall(ctx)
	}
	p.calls = append(p.calls, stage)
	if p.missing {
		return monitor.PipelineResult{}, &monitor.PipelineError{Kind: "awaiting_configuration"}
	}
	if stage == "summary" && p.failSummary {
		return monitor.PipelineResult{}, &monitor.PipelineError{Kind: "transport", Retryable: true}
	}
	switch stage {
	case "classification":
		out := monitor.PipelineResult{}
		for _, topic := range in.Record.Topics {
			out.Topics = append(out.Topics, monitor.TopicDecision{Topic: topic, Relevant: true, Category: "candidate", Score: .8})
		}
		return out, nil
	case "summary":
		for _, block := range in.Material.Blocks {
			if block.Kind == "abstract" {
				return monitor.PipelineResult{Summary: "原文报告了研究样本。", Claims: []monitor.PipelineClaim{{Text: "The study included 12 participants.", Quote: block.Text, EvidenceBlockID: block.ID, Numbers: []string{"12"}, Citations: []string{"PMID:123"}}}}, nil
			}
		}
		return monitor.PipelineResult{}, fmt.Errorf("fixture requires retained abstract")
	default:
		yes := true
		return monitor.PipelineResult{Supported: &yes}, nil
	}
}

// TestPaperPipelinePostgresRestartAndCatalogProjection 使用真实迁移表验证先入库、缺配置、模型失败后跨Service重启和完整引用档案回写。
func TestPaperPipelinePostgresRestartAndCatalogProjection(t *testing.T) {
	dsn := os.Getenv("INITRA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("INITRA_TEST_DATABASE_URL required for real PostgreSQL staged paper pipeline test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	schema := fmt.Sprintf("glean_paper_stages_%d", time.Now().UnixNano())
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
		require.NoError(t, err, file.Name())
	}
	ids, err := idgen.ConfigureDefault(62)
	require.NoError(t, err)
	makeClient := func() *ent.Client {
		driver := &livePipelineDriver{Driver: entsql.NewDriver(dialect.Postgres, entsql.Conn{ExecQuerier: tx}), tx: tx}
		client := ent.NewClient(ent.Driver(driver))
		client.Use(entx.AuditHook(entx.AuditHookOptions{}))
		client.Use(entx.RejectDeleteHook())
		return client
	}
	client := makeClient()
	_, err = client.SysUser.Create().SetID(7).SetUsername("paper-pipeline-postgres-operator").SetPasswordHash("fixture-not-a-login-credential").SetIsSuperAdmin(true).Save(ctx)
	require.NoError(t, err)
	at := time.Now().UTC().Truncate(time.Minute)
	source := &postgresPaperSource{record: monitor.Record{Source: "crossref", SourceID: "10.0000/staged-pg", DOI: "10.0000/staged-pg", Title: "PostgreSQL staged pipeline integration fixture", Abstract: "The study included 12 participants. PMID:123", SourceURL: "https://api.crossref.org/works/10.0000/staged-pg", URL: "https://doi.org/10.0000/staged-pg", Version: "1", Topics: []string{"biomed-ai"}, SourceUpdatedAt: at.Add(-time.Hour), Raw: json.RawMessage(`{"integration_fixture":true}`)}}
	cfg := monitor.RuntimeConfig()
	cfg.StateKey = "paper-pipeline-postgres-v1"
	cfg.MaxPagesPerCall = 100
	cfg.BaseBackoff = time.Second
	cfg.SourceInterval = time.Nanosecond
	actorCtx := entx.WithOperatorID(ctx, 7)
	makeService := func(processor *postgresPaperProcessor) *monitor.Service {
		fresh := makeClient()
		publisher := boot.NewCatalogMonitorPublisher(catalog.NewService(fresh), 7)
		service, e := monitor.NewService(fresh, ids, cfg, []monitor.Source{source}, nil, publisher, pipelineRegistryFixture{})
		require.NoError(t, e)
		service.SetPipelineProcessor(processor)
		return service
	}
	missing := &postgresPaperProcessor{missing: true}
	first := makeService(missing)
	pending, err := first.RunOnce(actorCtx, at)
	require.NoError(t, err)
	require.Len(t, pending.Records, 1)
	require.Empty(t, missing.calls)
	var candidateID idgen.ID
	for _, c := range pending.Records {
		candidateID = c.ID
		require.True(t, c.Published)
		require.False(t, c.Record.IsMock)
		require.Equal(t, "awaiting_configuration", c.Pipeline.Status)
	}
	// executionTemplates先执行全部regular，再按CoreBackfill追加Core模板；不能只按冻结模板数估算。
	executionCount := len(pending.Runs[0].SearchTemplates)
	if pending.Runs[0].Sources["crossref"].CoreBackfill {
		for _, template := range pending.Runs[0].SearchTemplates {
			if template.Core {
				executionCount++
			}
		}
	}
	fixedRunID := pending.Runs[0].ID
	remaining := executionCount - pending.Runs[0].Sources["crossref"].TemplateIndex
	require.GreaterOrEqual(t, remaining, 0)
	// 此fixture每次返回Complete，真实源每调用最多推进一个模板；严格限制到剩余模板数。
	// 来源退避使用RunOnce的now，因此直接推进到已持久化截止时间；模型退避仍按下面的wallclock等待。
	for i := 0; i < remaining; i++ {
		previousIndex := pending.Runs[0].Sources["crossref"].TemplateIndex
		nextAt := pending.Sources["crossref"].NextRetryAt
		require.False(t, nextAt.IsZero(), "incomplete real-source template must retain its pacing deadline")
		require.False(t, nextAt.Before(at))
		pending, err = first.RunOnce(actorCtx, nextAt)
		require.NoError(t, err)
		require.Len(t, pending.Runs, 1, "draining must resume the original fixed window")
		require.Equal(t, fixedRunID, pending.Runs[0].ID)
		require.Equal(t, at, pending.Runs[0].FixedEnd)
		require.Equal(t, previousIndex+1, pending.Runs[0].Sources["crossref"].TemplateIndex, "complete fixture page must advance exactly one template")
	}
	require.Equal(t, executionCount, pending.Runs[0].Sources["crossref"].TemplateIndex)
	require.Equal(t, executionCount, source.calls, "all regular and core templates execute exactly once")
	require.True(t, pending.Runs[0].Sources["crossref"].Acquired)
	require.Equal(t, "completed", pending.Runs[0].Status)
	acquired := pending.Sources["crossref"].AcquiredThrough
	require.Equal(t, at, acquired)
	require.Empty(t, pending.Sources["crossref"].LastError)
	paper, err := client.GleanPaper.Query().Only(ctx)
	require.NoError(t, err)
	observation, err := client.GleanObservation.Query().Only(ctx)
	require.NoError(t, err)
	require.Equal(t, paper.ID, observation.PaperID)
	require.Equal(t, "PostgreSQL staged pipeline integration fixture", paper.Title)
	// 新Ent client和Service从JSONB恢复，不复用任何Service.memory。
	failedModel := &postgresPaperProcessor{failSummary: true, beforeCall: func(callCtx context.Context) {
		count, e := makeClient().GleanPaper.Query().Count(callCtx)
		require.NoError(t, e)
		require.Equal(t, 1, count, "source must already be in catalog before any model call")
	}}
	second := makeService(failedModel)
	restored, err := second.Snapshot(ctx)
	require.NoError(t, err)
	for _, c := range restored.Records {
		require.Equal(t, candidateID, c.ID)
		require.Equal(t, "awaiting_configuration", c.Pipeline.Status)
	}
	failed, err := second.RunOnce(actorCtx, at)
	require.NoError(t, err)
	var retryAt time.Time
	for _, c := range failed.Records {
		require.Equal(t, "retry_wait", c.Pipeline.Status)
		require.Equal(t, "completed", c.Pipeline.Stages[1].Status)
		require.Equal(t, "fixture-model-v1", c.Pipeline.Stages[1].Model)
		require.NotEmpty(t, c.Pipeline.Stages[1].InputHash)
		retryAt = c.Pipeline.Stages[2].NextRetryAt
	}
	require.Equal(t, acquired, failed.Sources["crossref"].AcquiredThrough)
	require.Empty(t, failed.Sources["crossref"].LastError)
	require.True(t, failed.Sources["crossref"].ReviewedThrough.IsZero())
	sourceCalls := source.calls
	successModel := &postgresPaperProcessor{}
	third := makeService(successModel)
	persisted, err := third.Snapshot(ctx)
	require.NoError(t, err)
	for _, c := range persisted.Records {
		require.Equal(t, "completed", c.Pipeline.Stages[1].Status)
		require.Equal(t, 1, c.Pipeline.Stages[1].Attempts)
	}
	// 完整阶段输出保存在真实目录，热状态可能已主动压缩，不将热缓存当数据库档案。
	partialDetail, e := catalog.NewService(makeClient()).Paper(requestctx.WithUserID(ctx, "7"), paper.ID)
	require.NoError(t, e)
	require.NotNil(t, partialDetail.Review)
	var partialArchive monitor.PaperPipeline
	require.NoError(t, json.Unmarshal(partialDetail.Review.Pipeline, &partialArchive))
	require.NotNil(t, partialArchive.Stages[1].Result)
	require.Equal(t, "completed", partialArchive.Stages[1].Status)
	require.Equal(t, "retry_wait", partialArchive.Stages[2].Status)
	// 使用持久化的退避截止时间，不绕过生产重试规则；此fixture最多等待配置的一秒。
	if delay := time.Until(retryAt); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	done, err := third.RunOnce(actorCtx, at)
	require.NoError(t, err)
	require.NotContains(t, successModel.calls, "classification", "completed paid stage must not rerun after restart")
	require.Equal(t, sourceCalls, source.calls, "model retries must not refetch source")
	require.Equal(t, acquired, done.Sources["crossref"].AcquiredThrough)
	require.Equal(t, acquired, done.Sources["crossref"].ReviewedThrough, "model watermark advances only after all stages finish")
	require.Empty(t, done.Sources["crossref"].LastError)
	// 从真实目录查询读取最终归档；热缓存可按生产规则压缩，不能据缓存假称持久化成功。
	detail, err := catalog.NewService(makeClient()).Paper(requestctx.WithUserID(ctx, "7"), paper.ID)
	require.NoError(t, err)
	require.False(t, detail.IsMock)
	require.NotNil(t, detail.Review)
	require.False(t, detail.Review.ScientificReviewed)
	var archive monitor.PaperPipeline
	require.NoError(t, json.Unmarshal(detail.Review.Pipeline, &archive))
	require.Equal(t, candidateID, archive.CandidateID)
	require.Equal(t, "needs_human_review", archive.Status)
	require.Len(t, archive.Stages, 6)
	for _, stage := range archive.Stages {
		require.Equal(t, "completed", stage.Status, stage.Name)
		require.NotEmpty(t, stage.InputHash, stage.Name)
		require.NotNil(t, stage.Result, stage.Name)
	}
	require.Equal(t, "fixture-model-v1", archive.Stages[2].Model)
	require.Equal(t, "summary-fixture-v1", archive.Stages[2].PromptVersion)
	require.Equal(t, 2, archive.Stages[2].Attempts)
	claim := archive.Stages[2].Result.Claims[0]
	require.Equal(t, []string{"PMID:123"}, claim.Citations)
	require.Equal(t, "abstract-0001", claim.EvidenceBlockID)
	require.Contains(t, claim.Quote, "12 participants")
	require.True(t, *archive.Stages[4].Result.Supported)
	require.Empty(t, archive.Stages[4].Result.Issues)
	count, err := makeClient().GleanObservation.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count, "pipeline projection updates the same source observation")
}
