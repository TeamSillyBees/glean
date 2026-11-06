package monitor

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	platformauth "github.com/teamsillybees/glean/internal/platform/auth"
	"github.com/teamsillybees/glean/internal/platform/httpclient"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/retrieval"
)

func budgetTemplate(t *testing.T) SearchTemplate {
	t.Helper()
	raw, err := os.ReadFile("../../retrieval/testdata/hydrogen-plan.json")
	require.NoError(t, err)
	preview, err := retrieval.ImportProtocol(raw, time.Now().UTC())
	require.NoError(t, err)
	p := preview.Protocol
	p.Mode = "execute"
	p.DateRange.From = scenarioTime.AddDate(0, 0, -1).Format("2006-01-02")
	p.DateRange.To = scenarioTime.Format("2006-01-02")
	p.DateRange.Timezone = "UTC"
	p.Budget = retrieval.Budget{MaxRequests: 1, MaxPages: 10, MaxRecords: 100, MaxQueue: 100, MaxHistoryDays: 30}
	p.Sources = []string{"europepmc"}
	p.SourceMatrix = []retrieval.SourcePlan{{Source: "europepmc", Reason: "测试实际适配器", License: "离线构造题录", Risk: "非真实文献", AccessStatus: "untested"}}
	p.ExternalQueries = nil
	p.Variants = p.Variants[:1]
	p.Variants[0].Sources = []string{"europepmc"}
	preview, err = retrieval.PreviewProtocol(p, time.Now().UTC())
	require.NoError(t, err)
	return SearchTemplate{ID: "custom-configured", Topic: "custom", Version: "test-1", Routes: preview.SearchRoutes, Protocol: &p}
}

// TestProtocolBudgetIncreaseResumesFrozenCursor 检查重建服务后仍受累计额度约束，授权扩容只续游标、不改查询。
func TestProtocolBudgetIncreaseResumesFrozenCursor(t *testing.T) {
	ctx := context.Background()
	executor := &adapterExecutor{responses: []*httpclient.Response{
		jsonResponse(`{"hitCount":2,"nextCursorMark":"next","resultList":{"result":[{"id":"1","source":"MED","title":"Synthetic first"}]}}`),
		jsonResponse(`{"hitCount":2,"resultList":{"result":[{"id":"2","source":"MED","title":"Synthetic second"}]}}`),
	}}
	template := budgetTemplate(t)
	memory := &memoryState{}
	now := scenarioTime
	base := offline(t, DemoSources(scenarioTime, false), nil, nil)
	build := func() *Service {
		s, err := NewOfflineService(base.ids, RuntimeConfig(), []Source{NewEuropePMCSource(executor)}, MockReviewer{}, &flakyPublisher{})
		require.NoError(t, err)
		s.memory = memory
		s.clock = func() time.Time { return now }
		s.templates = &mutableTemplates{items: []SearchTemplate{template}}
		return s
	}
	first, err := build().RunOnce(ctx, now)
	require.NoError(t, err)
	run := first.Runs[0]
	cursor := run.Sources["europepmc"].Cursor
	require.NotEmpty(t, cursor)
	now = now.Add(time.Minute)
	paused, err := build().RunOnce(ctx, now)
	require.NoError(t, err)
	// 既有分段流程先排空已取得页，再尝试下一页；两个阶段都不能重新取得调用额度。
	paused, err = build().RunOnce(ctx, now)
	require.NoError(t, err)
	require.Len(t, executor.requests, 1, "restarting a process must not restore spent quota")
	require.Equal(t, "protocol_request_budget", paused.Runs[0].Sources["europepmc"].LastError)
	body := BudgetIncreaseBody{Topic: "custom", Limits: template.Protocol.Budget, Reason: "测试续查第二页"}
	body.Limits.MaxRequests = 2
	_, err = build().IncreaseProtocolBudget(ctx, run.ID, body)
	require.Error(t, err, "authenticated administrator is required")
	admin := platformauth.WithPrincipal(ctx, platformauth.Principal{UserID: 7, IsSuperAdmin: true})
	changed, err := build().IncreaseProtocolBudget(admin, run.ID, body)
	require.NoError(t, err)
	require.Equal(t, run.SearchTemplates, changed.SearchTemplates)
	require.Equal(t, run.TemplateVersion, changed.TemplateVersion)
	require.Equal(t, cursor, changed.Sources["europepmc"].Cursor)
	require.Len(t, changed.BudgetChanges, 1)
	_, err = build().IncreaseProtocolBudget(admin, run.ID, body)
	require.ErrorContains(t, err, "版本")
	done, err := build().RunOnce(ctx, now)
	require.NoError(t, err)
	done, err = build().RunOnce(ctx, now)
	require.NoError(t, err)
	require.Equal(t, "completed", done.Runs[0].Status)
	require.Len(t, executor.requests, 2)
	require.Equal(t, "next", executor.requests[1].options.QueryParams.Get("cursorMark"))
	_, err = build().IncreaseProtocolBudget(admin, run.ID, body)
	require.ErrorContains(t, err, "已结束")
	assertAuditZIP(t, build(), run.ID, 2, 2)
}

func assertAuditZIP(t *testing.T, s *Service, id idgen.ID, requests, occurrences int) {
	t.Helper()
	content, err := s.ExportAuditZIP(context.Background(), id)
	require.NoError(t, err)
	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	require.NoError(t, err)
	files := map[string][]byte{}
	for _, f := range zr.File {
		r, err := f.Open()
		require.NoError(t, err)
		b, err := io.ReadAll(r)
		require.NoError(t, err, "CRC must validate")
		require.NoError(t, r.Close())
		files[f.Name] = b
	}
	var manifest struct {
		Complete bool         `json:"complete_export"`
		Files    []exportFile `json:"files"`
	}
	require.NoError(t, json.Unmarshal(files["manifest.json"], &manifest))
	require.True(t, manifest.Complete)
	require.Len(t, manifest.Files, 10)
	for _, entry := range manifest.Files {
		b, ok := files[entry.Name]
		require.True(t, ok)
		h := sha256.Sum256(b)
		require.Equal(t, entry.Bytes, len(b))
		require.Equal(t, entry.SHA256, hex.EncodeToString(h[:]))
	}
	var coverage struct {
		Requests    int             `json:"request_entries"`
		Occurrences int             `json:"fetched_occurrences"`
		Queries     []auditCoverage `json:"queries"`
	}
	require.NoError(t, json.Unmarshal(files["coverage.json"], &coverage))
	require.Equal(t, requests, coverage.Requests)
	require.Equal(t, occurrences, coverage.Occurrences)
	require.Equal(t, requests, bytes.Count(files["requests.jsonl"], []byte{'\n'}))
	require.Equal(t, occurrences, bytes.Count(files["occurrences.jsonl"], []byte{'\n'}))
	for _, q := range coverage.Queries {
		require.Equal(t, "complete", q.Status)
		require.False(t, q.HasMore)
		require.NotEmpty(t, q.TerminalEvidence)
	}
}
