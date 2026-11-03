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
	Needs       any               `yaml:"needs"`
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
	return readReleaseWorkflow(t, "deploy.yml")
}

func readReleaseWorkflow(t *testing.T, name string) deploymentWorkflow {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", name))
	require.NoError(t, err)
	var workflow deploymentWorkflow
	require.NoError(t, yaml.Unmarshal(content, &workflow))
	return workflow
}

// TestDeploymentWorkflowSecurity 固定手动审批、不可变版本、最小权限和 SSH 认证与显式服务器身份校验策略。
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
	require.Len(t, verify.Steps, 2)
	require.Empty(t, verify.Steps[0].Uses)
	deploy := workflow.Jobs["deploy"]
	require.Equal(t, "verify", deploy.Needs)
	require.Equal(t, "production", deploy.Environment)
	require.Equal(t, map[string]string{"contents": "read", "actions": "read"}, deploy.Permissions)
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
	for _, required := range []string{"StrictHostKeyChecking=no", "UserKnownHostsFile=/dev/null", "GlobalKnownHostsFile=/dev/null", "IdentitiesOnly=yes", "BatchMode=yes", "deploy/REVISION", `git archive --format=tar "$DEPLOY_SHA"`, "@sha256:", `PRODUCTION_BASE_DIR='$PRODUCTION_BASE_DIR'`} {
		require.Contains(t, sshScript, required)
	}
	for _, forbidden := range []string{"ssh-keyscan", "ssh-keygen -F", "SSH_KNOWN_HOSTS", "StrictHostKeyChecking=yes", "set -x", "pull_request_target", ":latest", "--build-arg", "--password "} {
		require.NotContains(t, shellScripts, forbidden)
	}
	for _, forbidden := range []string{"docker build", "docker push", "docker login"} {
		require.NotContains(t, shellScripts, forbidden, "部署只能消费已经验证的镜像，不再接触 registry 写入权限")
	}
	require.Contains(t, verify.Steps[1].Run, "glean-images-$attempt")
	require.Contains(t, verify.Steps[1].Run, ".github/workflows/publish-images.yml")
}

// TestDeploymentShellSyntax 在无网络、无凭据的情况下解析所有内联 shell。
func TestDeploymentShellSyntax(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash unavailable")
	}
	for _, name := range []string{"deploy.yml", "publish-images.yml"} {
		for jobName, job := range readReleaseWorkflow(t, name).Jobs {
			for _, step := range job.Steps {
				if step.Run == "" {
					continue
				}
				t.Run(name+"/"+jobName+"/"+step.Name, func(t *testing.T) {
					command := exec.Command("bash", "-n")
					command.Stdin = strings.NewReader(step.Run)
					output, err := command.CombinedOutput()
					require.NoError(t, err, "%s", output)
				})
			}
		}
	}
	command := exec.Command("bash", "-n", filepath.Join(repoRoot(t), "scripts/ci/container-smoke.sh"))
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
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
	guards := []string{workflow.Jobs["verify"].Steps[0].Run, workflow.Jobs["deploy"].Steps[0].Run, readReleaseWorkflow(t, "publish-images.yml").Jobs["verify"].Steps[0].Run}
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
				command.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "DEPLOY_SHA="+tc.input, "GITHUB_SHA="+sha, "DEPLOY_ENABLED=true", "PRODUCTION_BASE_DIR=/srv/glean", "GITHUB_REPOSITORY=TeamSillyBees/glean", "GITHUB_OUTPUT="+filepath.Join(dir, "output"), "TEST_MAIN="+tc.main, "TEST_RUNS="+filepath.Join(dir, "runs.json"))
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
			command.Env = append(os.Environ(), "DEPLOY_ENABLED="+enabled, "PRODUCTION_BASE_DIR=/srv/glean")
			output, err := command.CombinedOutput()
			require.Error(t, err)
			require.Contains(t, string(output), "Production deployment is not enabled")
		})
	}
}

// TestDeploymentBaseTransfer 执行实际传输脚本但替换外部命令，验证配置路径与符号链接边界。
func TestDeploymentBaseTransfer(t *testing.T) {
	for _, tool := range []string{"bash", "python3", "tar", "gzip"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " unavailable")
		}
	}
	var script string
	for _, step := range readDeploymentWorkflow(t).Jobs["deploy"].Steps {
		if strings.Contains(step.Run, "scp ") {
			script = step.Run
			require.Equal(t, "${{ secrets.PRODUCTION_SSH_KEY }}", step.Env["SSH_PRIVATE_KEY"])
			require.NotContains(t, step.Env, "SSH_KNOWN_HOSTS")
		}
	}
	require.NotEmpty(t, script)
	for _, kind := range []string{"valid", "missing", "relative", "root", "dot", "parent", "trailing", "space", "quote", "substitution", "newline", "symlink-base", "symlink-releases", "public-base"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			base := filepath.Join(dir, "chosen-base")
			require.NoError(t, os.MkdirAll(filepath.Join(base, "releases"), 0700))
			switch kind {
			case "missing":
				base = ""
			case "relative":
				base = "relative"
			case "root":
				base = "/"
			case "dot":
				base += "/./nested"
			case "parent":
				base += "/../nested"
			case "trailing":
				base += "/"
			case "space":
				base += " with-space"
			case "quote":
				base += "'quoted"
			case "substitution":
				base += "$(touch INJECTION)"
			case "newline":
				base += "\nINJECTION"
			case "symlink-base":
				alias := filepath.Join(dir, "alias")
				require.NoError(t, os.Symlink(base, alias))
				base = alias
			case "symlink-releases":
				require.NoError(t, os.Remove(filepath.Join(base, "releases")))
				require.NoError(t, os.Symlink(dir, filepath.Join(base, "releases")))
			case "public-base":
				require.NoError(t, os.Chmod(base, 0755))
			}
			bin := filepath.Join(dir, "bin")
			require.NoError(t, os.Mkdir(bin, 0700))
			for name, body := range map[string]string{
				"git": "#!/bin/bash\nexec tar -cf - --files-from /dev/null\n",
				"ssh": "#!/bin/bash\nset -eu\nprintf '%s\\n' \"$*\" >> \"$TEST_LOG\"\ncommand=${!#}\nif [[ $command = 'python3 - '* ]]; then exec bash -c \"$command\"; fi\n",
				"scp": "#!/bin/bash\nprintf 'scp %s\\n' \"$*\" >> \"$TEST_LOG\"\n",
			} {
				require.NoError(t, os.WriteFile(filepath.Join(bin, name), []byte(body), 0700))
			}
			logPath := filepath.Join(dir, "commands")
			command := exec.Command("bash", "-c", script)
			command.Dir = dir
			command.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "PRODUCTION_BASE_DIR="+base,
				"DEPLOY_HOST=example.invalid", "DEPLOY_USER=deploy", "DEPLOY_PORT=22", "SSH_PRIVATE_KEY=synthetic-not-a-key",
				"DEPLOY_SHA="+strings.Repeat("a", 40), "GITHUB_RUN_ID=12", "GITHUB_RUN_ATTEMPT=3", "RUNNER_TEMP="+dir,
				"BACKEND_IMAGE=ghcr.io/example/backend@sha256:"+strings.Repeat("b", 64),
				"WEB_IMAGE=ghcr.io/example/web@sha256:"+strings.Repeat("c", 64), "TEST_LOG="+logPath)
			output, err := command.CombinedOutput()
			log, _ := os.ReadFile(logPath)
			if kind == "valid" {
				require.NoError(t, err, "%s", output)
				release := filepath.Join(base, "releases", strings.Repeat("a", 40)+"-12-3")
				require.DirExists(t, release)
				require.Contains(t, string(log), "deploy@example.invalid:"+release+"/release.tar.gz")
				require.Contains(t, string(log), "PRODUCTION_BASE_DIR='"+base+"'")
				require.Contains(t, string(log), "StrictHostKeyChecking=no")
			} else {
				require.Error(t, err, "%s", output)
				require.NotContains(t, string(log), "scp ")
				require.NotContains(t, string(log), "scripts/production/deploy.sh")
			}
			require.NoFileExists(t, filepath.Join(dir, "INJECTION"))
			require.NotContains(t, string(log)+string(output), "synthetic-not-a-key")
		})
	}
}
