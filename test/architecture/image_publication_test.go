package architecture_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestImagePublicationBoundary 保证镜像发布独立于生产开关、凭据和服务器，且验证来自真实 registry 拉取。
func TestImagePublicationBoundary(t *testing.T) {
	workflow := readReleaseWorkflow(t, "publish-images.yml")
	require.Equal(t, map[string]string{"contents": "read"}, workflow.Permissions)
	require.Len(t, workflow.On, 1)
	require.Contains(t, workflow.On, "workflow_dispatch")
	require.Equal(t, "glean-image-publication", workflow.Concurrency.Group)
	require.False(t, workflow.Concurrency.Cancel)
	require.Len(t, workflow.Jobs, 3)
	require.Contains(t, workflow.Jobs["verify"].If, "github.repository == 'TeamSillyBees/glean'")
	require.Contains(t, workflow.Jobs["verify"].If, "github.ref == 'refs/heads/main'")
	require.Contains(t, workflow.Jobs["verify"].Steps[0].Run, `[[ "$DEPLOY_SHA" == "$GITHUB_SHA" ]]`)
	require.Equal(t, "verify", workflow.Jobs["publish"].Needs)
	require.Equal(t, map[string]string{"contents": "read", "packages": "write"}, workflow.Jobs["publish"].Permissions)
	validate := workflow.Jobs["validate"]
	require.ElementsMatch(t, []any{"verify", "publish"}, validate.Needs)
	require.Equal(t, map[string]string{"contents": "read", "packages": "read"}, validate.Permissions)
	for _, job := range workflow.Jobs {
		require.Empty(t, job.Environment)
		for _, step := range job.Steps {
			require.NotContains(t, step.Run, "${{")
			for _, value := range step.Env {
				require.NotContains(t, value, "secrets.")
				require.NotContains(t, value, "PRODUCTION_")
			}
			for _, forbidden := range []string{"ssh ", "scp ", "PRODUCTION_", ":latest", "--build-arg", "--password "} {
				require.NotContains(t, step.Run, forbidden)
			}
			if step.Uses != "" {
				require.Regexp(t, `^actions/(checkout|upload-artifact)@[0-9a-f]{40}$`, step.Uses)
			}
		}
	}
	require.Contains(t, validate.Steps[1].Run, "docker pull --platform linux/amd64")
	require.Contains(t, validate.Steps[1].Run, "org.opencontainers.image.revision")
	require.Contains(t, validate.Steps[1].Run, "docker logout ghcr.io")
	require.Equal(t, "bash scripts/ci/container-smoke.sh", validate.Steps[2].Run)
	require.Contains(t, validate.Steps[4].Uses, "actions/upload-artifact@")
	require.Equal(t, "glean-images-${{ github.run_attempt }}", validate.Steps[4].With["name"])
	// 常规 CI 和发布后验证执行同一组 HTTP 断言，避免发布路径退化为只检查 docker push 的退出码。
	ci := readWorkflowBoundary(t, "ci.yml")
	require.Contains(t, ci.Jobs["containers"].Steps[len(ci.Jobs["containers"].Steps)-1].Run, "scripts/ci/container-smoke.sh")
}

// TestDeploymentImageManifestGuard 实际执行校验脚本，拒绝错误发布来源、失败执行和篡改的镜像清单。
func TestDeploymentImageManifestGuard(t *testing.T) {
	for _, tool := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " unavailable")
		}
	}
	const sha = "1111111111111111111111111111111111111111"
	backend := "ghcr.io/teamsillybees/glean-backend@sha256:" + strings.Repeat("b", 64)
	web := "ghcr.io/teamsillybees/glean-web@sha256:" + strings.Repeat("c", 64)
	cases := []struct {
		name     string
		run      map[string]any
		manifest map[string]any
		ok       bool
	}{
		{name: "verified publication", ok: true},
		{name: "wrong run", run: map[string]any{"id": 43}},
		{name: "wrong workflow", run: map[string]any{"path": ".github/workflows/ci.yml"}},
		{name: "wrong commit", run: map[string]any{"head_sha": strings.Repeat("2", 40)}},
		{name: "wrong branch", run: map[string]any{"head_branch": "feature"}},
		{name: "PR run", run: map[string]any{"event": "pull_request"}},
		{name: "fork", run: map[string]any{"head_repository": map[string]any{"full_name": "other/glean"}}},
		{name: "failed smoke", run: map[string]any{"conclusion": "failure"}},
		{name: "running", run: map[string]any{"status": "in_progress"}},
		{name: "rerun after approval", run: map[string]any{"run_attempt": 2}},
		{name: "wrong manifest version", manifest: map[string]any{"schema": 2}},
		{name: "wrong manifest commit", manifest: map[string]any{"commit": strings.Repeat("2", 40)}},
		{name: "wrong manifest run", manifest: map[string]any{"run_id": "43"}},
		{name: "wrong manifest attempt", manifest: map[string]any{"run_attempt": "2"}},
		{name: "mutable tag", manifest: map[string]any{"backend": "ghcr.io/teamsillybees/glean-backend:latest"}},
		{name: "other image", manifest: map[string]any{"backend": strings.Replace(backend, "teamsillybees", "other", 1)}},
		{name: "swapped components", manifest: map[string]any{"backend": web, "web": backend}},
		{name: "missing digest", manifest: map[string]any{"web": ""}},
		{name: "output injection", manifest: map[string]any{"backend": backend + "\nweb=malicious"}},
	}
	workflow := readDeploymentWorkflow(t)
	guards := []string{workflow.Jobs["verify"].Steps[1].Run, workflow.Jobs["deploy"].Steps[1].Run}
	for phase, guard := range guards {
		for _, tc := range cases {
			if phase == 1 && tc.manifest != nil {
				continue // 审批后重新校验 run/attempt，清单输出已由前置 job 固定。
			}
			t.Run(string(rune('A'+phase))+"/"+tc.name, func(t *testing.T) {
				dir := t.TempDir()
				run := map[string]any{"id": 42, "run_attempt": 1, "path": ".github/workflows/publish-images.yml", "head_sha": sha, "head_branch": "main", "event": "workflow_dispatch", "head_repository": map[string]any{"full_name": "TeamSillyBees/glean"}, "status": "completed", "conclusion": "success"}
				manifest := map[string]any{"schema": 1, "commit": sha, "run_id": "42", "run_attempt": "1", "backend": backend, "web": web}
				for key, value := range tc.run {
					run[key] = value
				}
				for key, value := range tc.manifest {
					manifest[key] = value
				}
				for name, fixture := range map[string]any{"run.json": run, "manifest.json": manifest} {
					data, err := json.Marshal(fixture)
					require.NoError(t, err)
					require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0600))
				}
				// fake 只提供外部 API/下载结果；真正的 jq、格式校验和 shell 退出路径保持执行。
				fakeGH := "#!/bin/bash\nset -eu\ncase \"$1\" in\napi) cat \"$TEST_DIR/run.json\" ;;\nrun) [[ \"$*\" == *'--name glean-images-1 '* ]]; cp \"$TEST_DIR/manifest.json\" \"${!#}/images.json\" ;;\n*) exit 90 ;;\nesac\n"
				require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(fakeGH), 0700))
				command := exec.Command("bash", "-c", guard)
				command.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "DEPLOY_SHA="+sha,
					"GITHUB_REPOSITORY=TeamSillyBees/glean", "IMAGE_RUN_ID=42", "IMAGE_ATTEMPT=1", "RUNNER_TEMP="+dir,
					"GITHUB_OUTPUT="+filepath.Join(dir, "output"), "TEST_DIR="+dir)
				output, err := command.CombinedOutput()
				if tc.ok {
					require.NoError(t, err, "%s", output)
					if phase == 0 {
						data, err := os.ReadFile(filepath.Join(dir, "output"))
						require.NoError(t, err)
						require.Equal(t, "backend="+backend+"\nweb="+web+"\nimage_attempt=1\n", string(data))
					}
				} else {
					require.Error(t, err, "%s", output)
					require.NoFileExists(t, filepath.Join(dir, "output"), "拒绝的清单不得产生可部署的 digest 输出")
				}
			})
		}
	}
}
