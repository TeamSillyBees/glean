package architecture_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestCIEfficiencyPreservesValidation 约束去重、缓存和并行优化不得改变完整验收或发布边界。
func TestCIEfficiencyPreservesValidation(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/ci.yml"))
	require.NoError(t, err)
	var workflow struct {
		On map[string]struct {
			Branches    []string `yaml:"branches"`
			Paths       []string `yaml:"paths"`
			PathsIgnore []string `yaml:"paths-ignore"`
		} `yaml:"on"`
		Concurrency struct {
			Group  string `yaml:"group"`
			Cancel string `yaml:"cancel-in-progress"`
		} `yaml:"concurrency"`
	}
	require.NoError(t, yaml.Unmarshal(body, &workflow))
	require.Equal(t, []string{"main"}, workflow.On["push"].Branches)
	for _, event := range []string{"push", "pull_request", "workflow_dispatch"} {
		require.Contains(t, workflow.On, event)
		require.Empty(t, workflow.On[event].Paths)
		require.Empty(t, workflow.On[event].PathsIgnore)
	}
	require.Equal(t, "glean-ci-${{ github.workflow }}-${{ github.event.pull_request.number || github.run_id }}", workflow.Concurrency.Group)
	require.Equal(t, "${{ github.event_name == 'pull_request' }}", workflow.Concurrency.Cancel)
	ci := readWorkflowBoundary(t, "ci.yml")
	require.Len(t, ci.Jobs, 7)
	for _, name := range []string{"verify", "recovery", "frontend", "containers", "browser"} {
		job := ci.Jobs[name]
		require.Equal(t, "needs.changes.outputs.docs_only != 'true'", job.If, name)
		require.Equal(t, "changes", job.Needs, name)
		require.Nil(t, job.ContinueOnError, name)
	}
	require.Equal(t, "always()", ci.Jobs["result"].If)
	require.Equal(t, []any{"changes", "verify", "recovery", "frontend", "containers", "browser"}, ci.Jobs["result"].Needs)
	require.Contains(t, ci.Jobs["result"].Steps[1].Run, "scripts/ci/ci_gate.py")
	for _, name := range []string{"verify", "browser"} {
		foundModule, foundBuild := false, false
		for _, step := range ci.Jobs[name].Steps {
			if step.Name == "Set up Go" {
				require.Equal(t, "false", step.With["cache"])
			}
			if step.With["path"] == "~/go/pkg/mod" {
				foundModule = true
				require.Contains(t, step.With["key"], "hashFiles('go.sum')")
				require.NotContains(t, step.With["key"], "github.sha")
			}
			if step.With["path"] == "~/.cache/go-build" {
				foundBuild = true
				for _, value := range []string{"steps.go.outputs.go-version", "github.job", "hashFiles('go.sum')", "github.sha"} {
					require.Contains(t, step.With["key"], value)
				}
				require.Contains(t, step.With["restore-keys"], "github.job")
			}
		}
		require.True(t, foundModule && foundBuild, name)
	}
	cachedImages := 0
	for _, step := range ci.Jobs["containers"].Steps {
		if step.With["cache-to"] != "" {
			cachedImages++
			require.Equal(t, "true", step.With["load"])
			require.Equal(t, "false", step.With["push"])
			require.Contains(t, step.With["cache-from"], "type=gha,scope=glean-ci-")
		}
	}
	require.Equal(t, 2, cachedImages)
	commands := string(body)
	for _, prohibited := range []string{"docker push", "docker login", "packages: write", "production/deploy.sh", "continue-on-error:"} {
		require.NotContains(t, commands, prohibited)
	}
	wait := strings.Index(commands, "wait \"$frontend_pid\"\n          trap - EXIT")
	start := strings.Index(commands, "- name: Start actual Go HTTP API")
	require.GreaterOrEqual(t, wait, 0)
	require.Greater(t, start, wait)
}
