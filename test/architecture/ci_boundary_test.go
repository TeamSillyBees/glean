package architecture_test

import (
	"go/build"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type workflowBoundary struct {
	On          map[string]any    `yaml:"on"`
	Permissions map[string]string `yaml:"permissions"`
	Jobs        map[string]struct {
		If              string            `yaml:"if"`
		Needs           any               `yaml:"needs"`
		Permissions     any               `yaml:"permissions"`
		ContinueOnError any               `yaml:"continue-on-error"`
		Services        any               `yaml:"services"`
		Env             map[string]string `yaml:"env"`
		Steps           []struct {
			Name            string            `yaml:"name"`
			ID              string            `yaml:"id"`
			If              string            `yaml:"if"`
			With            map[string]string `yaml:"with"`
			Run             string            `yaml:"run"`
			Env             map[string]string `yaml:"env"`
			ContinueOnError any               `yaml:"continue-on-error"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func readWorkflowBoundary(t *testing.T, name string) workflowBoundary {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", name))
	require.NoError(t, err)
	var workflow workflowBoundary
	require.NoError(t, yaml.Unmarshal(body, &workflow))
	return workflow
}

// TestNormalCIExcludesExternalSourceAcceptance 防止供应商实时可用性再次阻塞常规构建。
func TestNormalCIExcludesExternalSourceAcceptance(t *testing.T) {
	workflow := readWorkflowBoundary(t, "ci.yml")
	require.Contains(t, workflow.On, "push")
	require.Contains(t, workflow.On, "pull_request")
	for _, name := range []string{"verify", "frontend", "containers", "browser"} {
		require.Contains(t, workflow.Jobs, name)
	}
	commands := ""
	for name, job := range workflow.Jobs {
		require.NotEqual(t, "live", job.Env["GLEAN_E2E_PHASE"], name)
		require.NotEqual(t, "1", job.Env["GLEAN_LIVE_SOURCE_CANARY"], name)
		for _, step := range job.Steps {
			require.NotEqual(t, "live", step.Env["GLEAN_E2E_PHASE"], step.Name)
			require.NotEqual(t, "1", step.Env["GLEAN_LIVE_SOURCE_CANARY"], step.Name)
			require.Nil(t, step.ContinueOnError, "do not hide failures in %s", step.Name)
			commands += step.Run + "\n"
		}
	}
	for _, prohibited := range []string{"--require-observation", "--sources ", "GLEAN_E2E_LIVE_REPORT", "GLEAN_LIVE_SOURCE_CANARY=1", "-tags=live", "europepmc/webservices", "api.crossref.org", "eutils.ncbi.nlm.nih.gov"} {
		require.NotContains(t, commands, prohibited)
	}
	for _, required := range []string{"go test -json ./... -count=1", "TestMonitorRealPipelinePostgres", "TestHistoricalAbstractEnrichmentPostgres", `report["attempted"] == report["created"] == 39495`, "npm run test:e2e"} {
		require.Contains(t, commands, required)
	}
	require.Equal(t, "historical", workflow.Jobs["browser"].Env["GLEAN_E2E_PHASE"])
}

// TestLiveAcceptanceRequiresManualDispatch 保留独立真实验收，但禁止自动触发或吞掉失败。
func TestLiveAcceptanceRequiresManualDispatch(t *testing.T) {
	workflow := readWorkflowBoundary(t, "live-source-acceptance.yml")
	require.Len(t, workflow.On, 1)
	require.Contains(t, workflow.On, "workflow_dispatch")
	require.Contains(t, workflow.Jobs, "live-source")
	commands := ""
	liveBrowser := false
	for _, job := range workflow.Jobs {
		for _, step := range job.Steps {
			require.Nil(t, step.ContinueOnError, "manual acceptance must expose failures")
			commands += step.Run + "\n"
			liveBrowser = liveBrowser || step.Env["GLEAN_E2E_PHASE"] == "live" && strings.Contains(step.Run, "npm run test:e2e")
		}
	}
	require.Contains(t, commands, "--sources europepmc")
	require.Contains(t, commands, "--max-pages 1 --page-size 1 --require-observation")
	require.Contains(t, commands, "GLEAN_E2E_LIVE_REPORT")
	require.True(t, liveBrowser)
}

// TestExternalGoCanaryIsExcludedFromDefaultSuite 默认测试不编译任何外部实时 canary。
func TestExternalGoCanaryIsExcludedFromDefaultSuite(t *testing.T) {
	dir := filepath.Join(repoRoot(t), "internal", "modules", "monitor")
	ctx := build.Default
	ctx.BuildTags = nil
	included, err := ctx.MatchFile(dir, "source_live_test.go")
	require.NoError(t, err)
	require.False(t, included)
	ctx.BuildTags = []string{"live"}
	included, err = ctx.MatchFile(dir, "source_live_test.go")
	require.NoError(t, err)
	require.True(t, included)
}

// TestHistoricalQueryDiagnosticsAreRequired 仅允许保守选择器关闭性能诊断，语义验证始终运行。
func TestHistoricalQueryDiagnosticsAreRequired(t *testing.T) {
	workflow := readWorkflowBoundary(t, "ci.yml")
	require.Contains(t, workflow.On, "workflow_dispatch")
	verify := workflow.Jobs["verify"]
	require.Empty(t, verify.If)
	found, selected, tested := false, false, false
	for _, step := range verify.Steps {
		require.Nil(t, step.ContinueOnError)
		switch step.Name {
		case "Checkout repository":
			require.Equal(t, "0", step.With["fetch-depth"])
			require.Equal(t, "false", step.With["persist-credentials"])
		case "Test conservative query diagnostic selection":
			require.Empty(t, step.If)
			require.Contains(t, step.Run, "python3 -m unittest discover -s scripts/ci/tests -v")
			tested = true
		case "Select historical query diagnostics":
			require.True(t, tested)
			require.Empty(t, step.If)
			require.Equal(t, "catalog_diagnostics", step.ID)
			require.Equal(t, "python3 scripts/ci/catalog_diagnostics.py", strings.TrimSpace(step.Run))
			selected = true
		case "Collect historical query diagnostics", "Preserve diagnostic samples (not a latency gate)":
			require.Equal(t, "always() && steps.catalog_diagnostics.outputs.enabled != '0'", step.If)
		default:
			require.Empty(t, step.If, "only the diagnostic artifact may be conditional")
		}
		if strings.Contains(step.Run, "go test -json ./... -count=1") {
			found = true
			require.True(t, selected)
			require.Equal(t, "${{ steps.catalog_diagnostics.outputs.enabled == '0' && '0' || '1' }}", step.Env["GLEAN_CATALOG_QUERY_DIAGNOSTICS"])
			require.Contains(t, step.Run, `enabled = os.environ.get("GLEAN_CATALOG_QUERY_DIAGNOSTICS") != "0"`)
			require.Contains(t, step.Run, "if enabled:\n    required.add(diagnostic)")
			require.Contains(t, step.Run, "if not enabled and diagnostic not in skipped:")
			require.Contains(t, step.Run, "TestCatalogHistoricalQueryDiagnostics")
			require.Contains(t, step.Run, "missing = required - passed")
			for _, name := range []string{
				"TestCatalogPostgresEndToEnd", "TestCatalogFacetBatchEquivalence", "TestCatalogTopicCountsPostgres",
				"TestCatalogLiveObservationPostgres", "TestHistoricalAbstractEnrichmentPostgres", "TestCatalogManagementPostgres",
				"TestUserAdministrationPostgres", "TestMonitorPostgresDurableMockRun",
				"TestMonitorPostgresDurableMockRun/CancellationPartialFailureFrozenTemplates", "TestMonitorPostgresProcessRestart",
				"TestMonitorRealPipelinePostgres", "TestMigrationsAndSeedOnPostgres", "TestCommittedHistoricalManifest",
				"TestHistoricalJournalRegistryPreservesEvidence",
			} {
				require.Contains(t, strings.Split(step.Run, "diagnostic =")[0], name)
			}
		}
	}
	require.True(t, found)
	for _, name := range []string{"browser", "recovery"} {
		require.Empty(t, workflow.Jobs[name].If)
		for _, step := range workflow.Jobs[name].Steps {
			require.NotContains(t, step.If, "catalog_diagnostics")
		}
	}
}

// TestSixSourceManualProbeBoundary 固定手动探针预算和安全报告，不能扩展为自动采集或隐藏失败。
func TestSixSourceManualProbeBoundary(t *testing.T) {
	workflow := readWorkflowBoundary(t, "live-source-acceptance.yml")
	require.Equal(t, map[string]string{"contents": "read"}, workflow.Permissions)
	require.Len(t, workflow.Jobs, 2)
	for _, job := range workflow.Jobs {
		require.Nil(t, job.Needs, "live jobs must remain independent")
		require.Empty(t, job.If)
		require.Nil(t, job.Permissions)
		require.Nil(t, job.ContinueOnError)
	}
	job, ok := workflow.Jobs["six-source-canary"]
	require.True(t, ok)
	require.Empty(t, job.Env)
	require.Nil(t, job.Services)
	commands := ""
	for _, step := range job.Steps {
		commands += step.Run + "\n"
		require.Nil(t, step.ContinueOnError)
		if strings.Contains(step.Run, "go test") {
			require.Empty(t, step.If)
			require.Equal(t, map[string]string{"GLEAN_LIVE_SOURCE_CANARY": "1", "GLEAN_LIVE_SOURCE": ""}, step.Env)
			require.Contains(t, step.Run, "go test -json -tags=live ./internal/modules/monitor -run '^TestLiveSourceCanary$' -count=1 -timeout=10m")
			require.Contains(t, step.Run, `status=$?`)
			require.Contains(t, step.Run, `exit "$status"`)
		}
		if strings.Contains(step.Run, "live_canary_report.py") {
			require.Equal(t, "always()", step.If)
			require.Contains(t, step.Run, `"$GITHUB_STEP_SUMMARY"`)
		}
		if step.With["path"] != "" {
			require.Equal(t, "${{ runner.temp }}/glean-canary-safe/summary.md", step.With["path"])
			require.Equal(t, "always()", step.If)
			require.Equal(t, "false", step.With["include-hidden-files"])
		}
	}
	for _, forbidden := range []string{"secrets.", "--retry", "-failfast", "tee ", "cat ", "--demo", "psql", "curl", "npm", "CONTACT", "API_KEY"} {
		require.NotContains(t, commands, forbidden)
	}
	body, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/live-source-acceptance.yml"))
	require.NoError(t, err)
	require.NotContains(t, string(body), "secrets.")
	source, err := os.ReadFile(filepath.Join(repoRoot(t), "internal/modules/monitor/source_live_test.go"))
	require.NoError(t, err)
	code := string(source)
	require.Equal(t, 2, strings.Count(code, "source.Fetch(ctx, req)"))
	require.Contains(t, code, `if source.Name() == "pubmed" && !page.Complete {`)
	require.Contains(t, code, "names := DefaultSourceNames()")
	require.Contains(t, code, "PageSize: 1")
	require.Contains(t, code, "Start: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)")
	require.Contains(t, code, "time.NewTimer(3 * time.Second)")
	require.Contains(t, code, "time.Sleep(3 * time.Second)")
	require.NotContains(t, code, "t.Parallel()")
	require.Equal(t, 2, strings.Count(code, "for "), "only sources and normalized records loops, no request retries")
	require.Contains(t, code, `Services: SourceHTTPServices("")`)
	require.Contains(t, code, "SourceHTTPFactoryOptions()...")
	transport, err := os.ReadFile(filepath.Join(repoRoot(t), "internal/modules/monitor/source_http.go"))
	require.NoError(t, err)
	require.Contains(t, string(transport), `return []string{"crossref", "pubmed", "arxiv", "biorxiv", "europepmc", "medrxiv"}`)
	require.Contains(t, string(transport), "Retry: httpclient.RetryConfig{Enabled: false, Count: 0}")
	require.Contains(t, string(transport), "request.Response != nil")

}
