package architecture_test

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// goPackage 承接 go list 输出的包路径和直接依赖列表。
type goPackage struct {
	ImportPath string
	Imports    []string
}

// TestApplicationUsesLocalSource 验证应用不依赖旧的外部框架或示例路径。
func TestApplicationUsesLocalSource(t *testing.T) {
	packages := goList(t, "./...")
	// 这是旧框架的固定标识，分开保存以免项目改名时将禁用目标也改成派生项目。
	const legacyModule = "github.com/teamsillybees/" + "initra"

	for _, pkg := range packages {
		for _, imported := range pkg.Imports {
			for _, legacy := range []string{legacyModule + "/pkg/", legacyModule + "/examples/"} {
				require.Falsef(t, strings.HasPrefix(imported, legacy), "%s imports obsolete package %s", pkg.ImportPath, imported)
			}
		}
	}
}

// TestTemplateUsesAPIFoundationLayout 固定源码模板包含基础业务和数据文件。
func TestTemplateUsesAPIFoundationLayout(t *testing.T) {
	root := repoRoot(t)

	require.DirExists(t, filepath.Join(root, "internal", "modules"))
	for _, moduleName := range []string{"auth", "user", "rbac", "file", "httpdemo", "taskdemo"} {
		moduleDir := filepath.Join(root, "internal", "modules", moduleName)
		require.DirExists(t, moduleDir)
		for _, suffix := range []string{"handler", "service", "dto", "routes"} {
			require.FileExists(t, filepath.Join(moduleDir, moduleName+"."+suffix+".go"))
		}
		require.FileExists(t, filepath.Join(moduleDir, "providers.go"))
		requireNoTransportTypes(t, filepath.Join(moduleDir, moduleName+".handler.go"))
		requireHTTPNaming(t, filepath.Join(moduleDir, moduleName+".dto.go"))
	}
	require.FileExists(t, filepath.Join(root, "db", "seeds", "001_seed_admin.sql"))
	require.NoDirExists(t, filepath.Join(root, "internal", "gen", "jet"))
	require.NoDirExists(t, filepath.Join(root, "tools", "jetgen"))
	require.NoDirExists(t, filepath.Join(root, "scripts"))
	require.DirExists(t, filepath.Join(root, "internal", "data", "schema"))
	require.FileExists(t, filepath.Join(root, "internal", "data", "ent", "client.go"))
	require.FileExists(t, filepath.Join(root, "internal", "data", "tx.go"))
	requireEntImportsStayInPersistenceFiles(t, root)
	_, err := os.Stat(filepath.Join(root, "internal", "app"))
	require.True(t, errors.Is(err, os.ErrNotExist), "业务模块应位于 internal/modules")
}

// TestBusinessModulesUseBizerrorsFacade 固定业务模块只能通过 bizerrors 使用统一错误门面。
func TestBusinessModulesUseBizerrorsFacade(t *testing.T) {
	moduleRoot := filepath.Join(repoRoot(t), "internal", "modules")
	require.NoError(t, filepath.WalkDir(moduleRoot, func(path string, entry os.DirEntry, err error) error {
		require.NoError(t, err)
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		content, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		text := string(content)
		require.NotContainsf(t, text, "github.com/samber/oops", "%s must not import oops directly", path)
		if !isBizerrorsPath(path) {
			require.NotContainsf(t, text, "github.com/teamsillybees/glean/internal/platform/errors", "%s must use internal/modules/bizerrors", path)
		}
		return nil
	}))
}

// TestDockerBaselineKeepsRuntimeImageMinimalAndLocalServicesPrivate 固定企业部署基线的最小权限边界。
func TestDockerBaselineKeepsRuntimeImageMinimalAndLocalServicesPrivate(t *testing.T) {
	root := repoRoot(t)
	dockerfile, err := os.ReadFile(filepath.Join(root, "Dockerfile"))
	require.NoError(t, err)
	dockerText := string(dockerfile)
	require.Contains(t, dockerText, "static-debian12:nonroot")
	require.Contains(t, dockerText, "USER 65532:65532")
	require.Contains(t, dockerText, "--chown=65532:65532 /out/var /app/var")
	require.NotContains(t, dockerText, "/app/db")
	require.NotContains(t, dockerText, "/app/configs/config")
	require.FileExists(t, filepath.Join(root, ".dockerignore"))

	compose, err := os.ReadFile(filepath.Join(root, "docker-compose.yml"))
	require.NoError(t, err)
	composeText := string(compose)
	require.Contains(t, composeText, "127.0.0.1:5432:5432")
	require.Contains(t, composeText, "127.0.0.1:6379:6379")
	require.NotContains(t, composeText, "container_name:")
}

// requireNoTransportTypes 确认 handler 文件只承载 HTTP 适配逻辑，不再混放类型定义。
func requireNoTransportTypes(t *testing.T, path string) {
	t.Helper()

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	transportType := regexp.MustCompile(`(?m)^type\s+\w+(Input|Output|Request|Response|Query|Body|VO|DTO|Params)\s+`)
	exportedHandlerMethod := regexp.MustCompile(`(?m)^func\s+\(h\s+\*Handler\)\s+[A-Z]\w*\s*\(`)
	require.Falsef(t, transportType.Match(content), "%s should keep type definitions outside handler files", path)
	require.Falsef(t, exportedHandlerMethod.Match(content), "%s should keep Huma handler methods unexported", path)
}

// requireHTTPNaming 固定 HTTP 边界类型命名：Huma 包装类型仅内部使用，JSON 出参使用 VO。
func requireHTTPNaming(t *testing.T, path string) {
	t.Helper()

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	legacyType := regexp.MustCompile(`(?m)^type\s+\w+(Input|Output|Params)\s+`)
	exportedWrapper := regexp.MustCompile(`(?m)^type\s+[A-Z]\w*(Request|Response)\s+`)
	require.Falsef(t, legacyType.Match(content), "%s should not use Input/Output/Params suffixes", path)
	require.Falsef(t, exportedWrapper.Match(content), "%s should keep Huma Request/Response wrappers unexported", path)
}

// requireEntImportsStayInPersistenceFiles 确认 Ent 只出现在同包持久化与映射边界，不泄漏到 handler/dto/usecase。
func requireEntImportsStayInPersistenceFiles(t *testing.T, root string) {
	t.Helper()

	moduleRoot := filepath.Join(root, "internal", "modules")
	require.NoError(t, filepath.WalkDir(moduleRoot, func(path string, entry os.DirEntry, err error) error {
		require.NoError(t, err)
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || allowsEntImport(path) {
			return nil
		}
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NotContainsf(t, string(content), "/internal/data/ent", "%s should not import internal/data/ent", path)
		return nil
	}))
}

func allowsEntImport(path string) bool {
	if strings.HasSuffix(path, "_test.go") || filepath.Base(path) == "providers.go" {
		return true
	}
	for _, suffix := range []string{".service.go", ".persistence.go", ".roles.go", ".mapper.go"} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

func isBizerrorsPath(path string) bool {
	clean := filepath.Clean(path)
	segment := string(filepath.Separator) + "bizerrors" + string(filepath.Separator)
	return strings.Contains(clean, segment)
}

// goList 调用 go list 并解析指定包模式的依赖元信息。
func goList(t *testing.T, pattern string) []goPackage {
	t.Helper()

	cmd := exec.Command("go", "list", "-json", pattern)
	cmd.Dir = repoRoot(t)
	output, err := cmd.Output()
	require.NoError(t, err)

	decoder := json.NewDecoder(strings.NewReader(string(output)))
	packages := make([]goPackage, 0)
	// go list -json 会连续输出多个 JSON 对象，而不是 JSON 数组；
	// 这里按流式对象逐个解码，避免目录数量变化时测试不稳定。
	for {
		var pkg goPackage
		err := decoder.Decode(&pkg)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		packages = append(packages, pkg)
	}
	return packages
}

// repoRoot 从当前测试文件位置推导仓库根目录。
func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
