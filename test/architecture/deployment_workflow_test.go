package architecture_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type deploymentStep struct {
	Name string            `yaml:"name"`
	ID   string            `yaml:"id"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	Env  map[string]string `yaml:"env"`
	With map[string]any    `yaml:"with"`
}

type deploymentJob struct {
	If          string            `yaml:"if"`
	Needs       string            `yaml:"needs"`
	Environment string            `yaml:"environment"`
	Permissions map[string]string `yaml:"permissions"`
	Steps       []deploymentStep  `yaml:"steps"`
}

type deploymentWorkflow struct {
	On          map[string]any    `yaml:"on"`
	Permissions map[string]string `yaml:"permissions"`
	Concurrency struct {
		Group  string `yaml:"group"`
		Cancel bool   `yaml:"cancel-in-progress"`
	} `yaml:"concurrency"`
	Jobs map[string]deploymentJob `yaml:"jobs"`
}

func readDeploymentWorkflow(t *testing.T) deploymentWorkflow {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "deploy.yml"))
	require.NoError(t, err)
	var workflow deploymentWorkflow
	require.NoError(t, yaml.Unmarshal(content, &workflow))
	return workflow
}

// TestDeploymentWorkflowSecurity 固定手动审批、不可变版本、最小权限和 SSH 信任边界。
func TestDeploymentWorkflowSecurity(t *testing.T) {
	workflow := readDeploymentWorkflow(t)
	require.Len(t, workflow.On, 1)
	require.Contains(t, workflow.On, "workflow_dispatch")
	require.Equal(t, map[string]string{"contents": "read"}, workflow.Permissions)
	require.Equal(t, "glean-production-deployment", workflow.Concurrency.Group)
	require.False(t, workflow.Concurrency.Cancel)
	require.Len(t, workflow.Jobs, 2)
	verify := workflow.Jobs["verify"]
	require.Contains(t, verify.If, "github.ref == 'refs/heads/main'")
	require.Contains(t, verify.If, "github.repository == 'TeamSillyBees/glean'")
	require.Equal(t, map[string]string{"contents": "read", "actions": "read"}, verify.Permissions)
	require.Len(t, verify.Steps, 1)
	require.Empty(t, verify.Steps[0].Uses)
	deploy := workflow.Jobs["deploy"]
	require.Equal(t, "verify", deploy.Needs)
	require.Equal(t, "production", deploy.Environment)
	require.Equal(t, map[string]string{"contents": "read", "actions": "read", "packages": "write"}, deploy.Permissions)
	require.Contains(t, deploy.Steps[0].Run, `[[ "$DEPLOY_ENABLED" == true ]]`)
	require.Contains(t, deploy.Steps[0].Run, ".conclusion == \"success\"")

	var shellScripts, sshScript string
	checkoutCount, secretSteps := 0, 0
	for _, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if step.Uses != "" {
				require.Regexp(t, `^actions/checkout@[0-9a-f]{40}$`, step.Uses)
				require.Equal(t, false, step.With["persist-credentials"])
				require.Equal(t, "${{ needs.verify.outputs.commit }}", step.With["ref"])
				checkoutCount++
			}
			// 表达式只能通过 env / with 传值，不能拼接成 shell 源码。
			require.NotContains(t, step.Run, "${{")
			shellScripts += step.Run
			containsSecret := false
			for _, value := range step.Env {
				containsSecret = containsSecret || strings.Contains(value, "secrets.")
			}
			if containsSecret {
				secretSteps++
				sshScript = step.Run
				require.Contains(t, step.Run, "scripts/production/deploy.sh")
				require.NotContains(t, step.Run, "docker build")
			}
		}
	}
	require.Equal(t, 1, checkoutCount)
	require.Equal(t, 1, secretSteps)
	for _, required := range []string{"StrictHostKeyChecking=yes", "UserKnownHostsFile=", "GlobalKnownHostsFile=/dev/null", "IdentitiesOnly=yes", "BatchMode=yes", "ssh-keygen -F", "deploy/REVISION", `git archive --format=tar "$DEPLOY_SHA"`, "@sha256:", "GLEAN_OPS_ENV=/etc/glean/ops.env"} {
		require.Contains(t, sshScript, required)
	}
	for _, forbidden := range []string{"ssh-keyscan", "StrictHostKeyChecking=no", "set -x", "pull_request_target", ":latest", "--build-arg", "--password "} {
		require.NotContains(t, shellScripts, forbidden)
	}
	require.Contains(t, shellScripts, "--password-stdin")
	require.Contains(t, shellScripts, "docker logout ghcr.io")
	require.Contains(t, shellScripts, `rm -rf "$DOCKER_CONFIG"`)
}

// TestDeploymentShellSyntax 在无网络、无凭据的情况下解析所有内联 shell。
func TestDeploymentShellSyntax(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash unavailable")
	}
	for jobName, job := range readDeploymentWorkflow(t).Jobs {
		for _, step := range job.Steps {
			if step.Run == "" {
				continue
			}
			t.Run(jobName+"/"+step.Name, func(t *testing.T) {
				command := exec.Command("bash", "-n")
				command.Stdin = strings.NewReader(step.Run)
				output, err := command.CombinedOutput()
				require.NoError(t, err, "%s", output)
			})
		}
	}
}

// TestDeploymentReleaseGuard 拒绝错误 SHA、PR 成功和被失败的新执行覆盖的旧成功。
func TestDeploymentReleaseGuard(t *testing.T) {
	for _, tool := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " unavailable")
		}
	}
	const sha = "1111111111111111111111111111111111111111"
	const other = "2222222222222222222222222222222222222222"
	run := func(number int, commit, branch, event, repository, status, conclusion string) map[string]any {
		return map[string]any{"run_number": number, "head_sha": commit, "head_branch": branch, "event": event, "head_repository": map[string]string{"full_name": repository}, "status": status, "conclusion": conclusion}
	}
	success := run(1, sha, "main", "push", "TeamSillyBees/glean", "completed", "success")
	cases := []struct {
		name, input, main string
		runs              []map[string]any
		ok                bool
	}{
		{"success", sha, sha, []map[string]any{success}, true},
		{"short SHA", "1111111", sha, []map[string]any{success}, false},
		{"shell injection", "$(touch /tmp/never-deploy)", sha, []map[string]any{success}, false},
		{"not current main", other, sha, []map[string]any{success}, false},
		{"no CI", sha, sha, nil, false},
		{"wrong CI SHA", sha, sha, []map[string]any{run(2, other, "main", "push", "TeamSillyBees/glean", "completed", "success")}, false},
		{"PR CI", sha, sha, []map[string]any{run(2, sha, "main", "pull_request", "TeamSillyBees/glean", "completed", "success")}, false},
		{"other branch CI", sha, sha, []map[string]any{run(2, sha, "feature", "push", "TeamSillyBees/glean", "completed", "success")}, false},
		{"fork CI", sha, sha, []map[string]any{run(2, sha, "main", "push", "other/glean", "completed", "success")}, false},
		{"latest failed", sha, sha, []map[string]any{run(2, sha, "main", "push", "TeamSillyBees/glean", "completed", "failure"), success}, false},
		{"latest pending", sha, sha, []map[string]any{success, run(2, sha, "main", "push", "TeamSillyBees/glean", "in_progress", "")}, false},
	}
	workflow := readDeploymentWorkflow(t)
	guards := []string{workflow.Jobs["verify"].Steps[0].Run, workflow.Jobs["deploy"].Steps[0].Run}
	for index, guard := range guards {
		for _, tc := range cases {
			t.Run(string(rune('A'+index))+"/"+tc.name, func(t *testing.T) {
				dir := t.TempDir()
				payload, err := json.Marshal(map[string]any{"workflow_runs": tc.runs})
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(dir, "runs.json"), payload, 0600))
				fakeGH := "#!/bin/bash\nset -eu\ncase \"$2\" in\n*/git/ref/heads/main) printf '%s\\n' \"$TEST_MAIN\" ;;\n*/actions/workflows/ci.yml/runs?*) cat \"$TEST_RUNS\" ;;\n*) exit 90 ;;\nesac\n"
				require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(fakeGH), 0700))
				command := exec.Command("bash", "-c", guard)
				command.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "DEPLOY_SHA="+tc.input, "DEPLOY_ENABLED=true", "GITHUB_REPOSITORY=TeamSillyBees/glean", "GITHUB_OUTPUT="+filepath.Join(dir, "output"), "TEST_MAIN="+tc.main, "TEST_RUNS="+filepath.Join(dir, "runs.json"))
				output, err := command.CombinedOutput()
				if tc.ok {
					require.NoError(t, err, "%s", output)
				} else {
					require.Error(t, err, "%s", output)
				}
			})
		}
	}
	for _, enabled := range []string{"", "false", "TRUE"} {
		t.Run("production opt-in rejected/"+enabled, func(t *testing.T) {
			command := exec.Command("bash", "-c", guards[1])
			command.Env = append(os.Environ(), "DEPLOY_ENABLED="+enabled)
			output, err := command.CombinedOutput()
			require.Error(t, err)
			require.Contains(t, string(output), "Production deployment is not enabled")
		})
	}
}
