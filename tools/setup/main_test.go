package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

// TestSetupRenamesSourcesAndPreservesCredentials 验证完整初始化、密码哈希及重复运行时的凭据保护。
func TestSetupRenamesSourcesAndPreservesCredentials(t *testing.T) {
	root := setupFixture(t)
	var output bytes.Buffer
	opts := options{module: "example.com/orders", name: "Orders API"}
	require.NoError(t, run(root, opts, &output))
	require.Contains(t, readFixture(t, root, "go.mod"), "module example.com/orders")
	require.Contains(t, readFixture(t, root, "internal/data/entgenerate/main.go"), "example.com/orders/internal/data/ent")
	require.Contains(t, readFixture(t, root, "internal/service.go"), "example.com/orders/internal/platform/auth")
	require.Contains(t, readFixture(t, root, "README.md"), "# Orders API\n")
	require.Contains(t, readFixture(t, root, "docker-compose.yml"), "orders-api")
	// 显式改名只调整身份字段，不把恰好等于旧标识的密码一起替换。
	var base, local yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(readFixture(t, root, "configs/config.yaml")), &base))
	require.Equal(t, "Orders API", yamlString(&base, "app", "name"))
	require.Equal(t, "orders-api", yamlString(&base, "database", "dbname"))
	require.Equal(t, "source", yamlString(&base, "database", "password"))
	localBefore := readFixture(t, root, "configs/config.local.yaml")
	seedBefore := readFixture(t, root, "db/seeds/001_seed_admin.local.sql")
	require.NoError(t, yaml.Unmarshal([]byte(localBefore), &local))
	secret := yamlString(&local, "auth", "jwt", "secret")
	require.GreaterOrEqual(t, len(secret), 32)
	require.Equal(t, "orders-api", yamlString(&local, "database", "dbname"))
	passwordMatch := regexp.MustCompile(`初始密码（仅本次显示）: ([A-Za-z0-9_-]+)`).FindStringSubmatch(output.String())
	require.Len(t, passwordMatch, 2)
	hash := regexp.MustCompile(`\$2[aby]\$\d\d\$[./A-Za-z0-9]{53}`).FindString(seedBefore)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(hash), []byte(passwordMatch[1])))
	require.NotContains(t, seedBefore, passwordMatch[1])
	require.NotContains(t, output.String(), secret)
	require.Equal(t, "example.com/source/internal/keep", readFixture(t, root, ".git/keep.go"))
	require.Equal(t, "example.com/source/internal/keep", readFixture(t, root, "tmp/keep.go"))
	output.Reset()
	require.NoError(t, run(root, opts, &output))
	require.Equal(t, localBefore, readFixture(t, root, "configs/config.local.yaml"))
	require.Equal(t, seedBefore, readFixture(t, root, "db/seeds/001_seed_admin.local.sql"))
	require.NotContains(t, output.String(), "初始密码")
}

// TestSetupModuleRenamePreservesConfiguration 验证 module 改名只影响源码引用，不改写已有配置值。
func TestSetupModuleRenamePreservesConfiguration(t *testing.T) {
	root := setupFixture(t)
	local := "database:\n  password: example.com/source/private-password\nauth:\n  jwt:\n    secret: example.com/source/keep-this-secret-0123456789abcdef\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "configs/config.local.yaml"), []byte(local), 0o600))
	base := readFixture(t, root, "configs/config.yaml")
	var output bytes.Buffer
	require.NoError(t, run(root, options{module: "example.com/orders"}, &output))
	require.Equal(t, local, readFixture(t, root, "configs/config.local.yaml"))
	require.Equal(t, base, readFixture(t, root, "configs/config.yaml"))
}

// TestSetupFillsNullJWTSection 验证常见的空 YAML 分组能正确填入随机密钥。
func TestSetupFillsNullJWTSection(t *testing.T) {
	root := setupFixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "configs/config.local.example.yaml"), []byte("auth:\n  jwt:\n"), 0o644))
	var output bytes.Buffer
	require.NoError(t, run(root, options{}, &output))
	var local yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(readFixture(t, root, "configs/config.local.yaml")), &local))
	require.GreaterOrEqual(t, len(yamlString(&local, "auth", "jwt", "secret")), 32)
}

// TestSetupPreflightDoesNotLeavePartialChanges 验证参数或 seed 错误发生时尚未修改任何项目文件。
func TestSetupPreflightDoesNotLeavePartialChanges(t *testing.T) {
	for _, scenario := range []string{"invalid-module", "invalid-seed", "invalid-config-target", "invalid-jwt-group"} {
		t.Run(scenario, func(t *testing.T) {
			root := setupFixture(t)
			opts := options{module: "example.com/orders", name: "Orders"}
			if scenario == "invalid-module" {
				opts.module = "../invalid"
			} else if scenario == "invalid-seed" {
				require.NoError(t, os.WriteFile(filepath.Join(root, "db/seeds/001_seed_admin.sql"), []byte("SELECT 1;"), 0o644))
			} else if scenario == "invalid-config-target" {
				require.NoError(t, os.Mkdir(filepath.Join(root, "configs/config.local.yaml"), 0o755))
			} else {
				require.NoError(t, os.WriteFile(filepath.Join(root, "configs/config.local.example.yaml"), []byte("auth:\n  jwt: invalid\n"), 0o644))
			}
			before := readFixture(t, root, "go.mod")
			var output bytes.Buffer
			require.Error(t, run(root, opts, &output))
			require.Empty(t, output.String(), "失败前不输出一次性凭据")
			require.Equal(t, before, readFixture(t, root, "go.mod"))
			if scenario != "invalid-config-target" {
				require.NoFileExists(t, filepath.Join(root, "configs/config.local.yaml"))
			}
			require.NoFileExists(t, filepath.Join(root, "db/seeds/001_seed_admin.local.sql"))
		})
	}
}

func setupFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod":                            "module example.com/source\n\ngo 1.26.0\n",
		"README.md":                         "# Source API\n",
		"internal/service.go":               "package internal\nimport _ \"example.com/source/internal/platform/auth\"\n",
		"internal/data/entgenerate/main.go": "package main\nconst target = \"example.com/source/internal/data/ent\"\n",
		"configs/config.yaml":               "app:\n  name: Source API\n  slug: source\ndatabase:\n  dbname: source\n  password: source\nauth:\n  jwt:\n    issuer: source\n",
		"configs/config.local.example.yaml": "database:\n  dbname: source\nauth:\n  jwt:\n    secret: \"\"\n",
		"docker-compose.yml":                "name: source\nservices:\n  postgres:\n    environment:\n      POSTGRES_DB: source\n",
		"db/seeds/001_seed_admin.sql":       "INSERT INTO sys_user(password_hash) VALUES ('__INITRA_ADMIN_PASSWORD_HASH__');\n",
		".git/keep.go":                      "example.com/source/internal/keep",
		"tmp/keep.go":                       "example.com/source/internal/keep",
	}
	for path, content := range files {
		absolute := filepath.Join(root, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(absolute), 0o755))
		require.NoError(t, os.WriteFile(absolute, []byte(content), 0o644))
	}
	return root
}

func readFixture(t *testing.T, root, path string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	require.NoError(t, err)
	return string(content)
}
