// setup 在当前模板项目内完成改名和本地凭据初始化，不访问数据库。
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/mod/modfile"
	xmodule "golang.org/x/mod/module"
	"gopkg.in/yaml.v3"
)

type options struct{ module, name, slug string }

// fileChange 保存回滚所需原文；新增凭据仅在所有文件成功写入后对用户可见。
type fileChange struct {
	path          string
	before, after []byte
	mode          fs.FileMode
}

func main() {
	var opts options
	flag.StringVar(&opts.module, "module", "", "新的 Go module path；留空保留现值")
	flag.StringVar(&opts.name, "name", "", "应用展示名称；留空保留现值")
	flag.StringVar(&opts.slug, "slug", "", "应用标识；默认由 name 派生")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "setup 不接受位置参数")
		os.Exit(1)
	}
	if err := run(".", opts, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "初始化失败:", err)
		os.Exit(1)
	}
}

func run(root string, opts options, output io.Writer) error {
	changes, password, err := plan(root, opts)
	if err != nil {
		return err
	}
	for i, change := range changes {
		if err := writeChange(change); err != nil {
			// 恢复此前成功的改动，避免源码和 go.mod 只完成一半改名。
			for j := i - 1; j >= 0; j-- {
				previous := changes[j]
				if previous.before == nil {
					err = errors.Join(err, os.Remove(previous.path))
				} else {
					err = errors.Join(err, writeChange(fileChange{path: previous.path, before: previous.after, after: previous.before, mode: previous.mode}))
				}
			}
			return err
		}
	}
	if _, err := fmt.Fprintln(output, "本地配置与 seed 已准备完成；已有文件和凭据会保留。数据库迁移与 seed 需单独执行。"); err != nil {
		return err
	}
	if password != "" {
		_, err = fmt.Fprintf(output, "管理员: admin\n初始密码（仅本次显示）: %s\n", password)
	}
	return err
}

func plan(root string, opts options) ([]fileChange, string, error) {
	modBytes, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil, "", fmt.Errorf("请在项目根目录执行: %w", err)
	}
	mod, err := modfile.Parse("go.mod", modBytes, nil)
	if err != nil || mod.Module == nil {
		return nil, "", fmt.Errorf("go.mod 缺少有效 module 声明")
	}
	oldModule := mod.Module.Mod.Path
	if opts.module == "" {
		opts.module = oldModule
	}
	if err := xmodule.CheckPath(opts.module); err != nil {
		return nil, "", fmt.Errorf("module path 无效: %w", err)
	}
	baseBytes, err := os.ReadFile(filepath.Join(root, "configs", "config.yaml"))
	if err != nil {
		return nil, "", err
	}
	var base yaml.Node
	if err := yaml.Unmarshal(baseBytes, &base); err != nil {
		return nil, "", err
	}
	oldSlug := yamlString(&base, "app", "slug")
	oldName := yamlString(&base, "app", "name")
	if opts.name == "" {
		opts.name = oldName
	}
	if opts.slug == "" {
		opts.slug = oldSlug
		if opts.name != oldName {
			opts.slug = normalizeSlug(opts.name)
		}
	}
	if opts.slug == "" || len(opts.slug) > 48 || normalizeSlug(opts.slug) != opts.slug {
		return nil, "", fmt.Errorf("slug 必须是以字母开头、最长 48 字符的小写字母、数字或中划线；可通过 -slug 指定")
	}
	var changes []fileChange
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "tmp" || entry.Name() == "var" || entry.Name() == "vendor" || entry.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		ext := filepath.Ext(path)
		if entry.Name() != "go.mod" && ext != ".go" && ext != ".md" && ext != ".yaml" && ext != ".yml" {
			return nil
		}
		before, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		after := before
		// module 引用只出现在源码和文档中；配置值可能包含同样前缀的凭据或外部地址。
		if ext == ".go" || ext == ".md" {
			after = bytes.ReplaceAll(before, []byte(oldModule+"/"), []byte(opts.module+"/"))
		}
		if entry.Name() == "go.mod" && filepath.Dir(path) == filepath.Clean(root) {
			if err := mod.AddModuleStmt(opts.module); err != nil {
				return err
			}
			after, err = mod.Format()
			if err != nil {
				return err
			}
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if (opts.slug != oldSlug || opts.name != oldName) && (strings.HasPrefix(relative, "configs/") && ext == ".yaml" || relative == "docker-compose.yml") {
			var document yaml.Node
			if err := yaml.Unmarshal(after, &document); err != nil {
				return err
			}
			if relative == "docker-compose.yml" {
				if err := renameValue(&document, oldSlug, opts.slug, "name"); err != nil {
					return err
				}
				if err := renameValue(&document, oldSlug, opts.slug, "services", "postgres", "environment", "POSTGRES_DB"); err != nil {
					return err
				}
			} else {
				if err := renameConfig(&document, oldSlug, opts.slug); err != nil {
					return err
				}
			}
			if relative == "configs/config.yaml" {
				if err := yamlSet(&document, opts.name, "app", "name"); err != nil {
					return err
				}
			}
			after, err = yaml.Marshal(&document)
			if err != nil {
				return err
			}
		}
		if relative == "README.md" && opts.name != oldName {
			after = bytes.Replace(after, []byte("# "+oldName+"\n"), []byte("# "+opts.name+"\n"), 1)
		}
		if !bytes.Equal(before, after) {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			changes = append(changes, fileChange{path: path, before: before, after: after, mode: info.Mode().Perm()})
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	localPath := filepath.Join(root, "configs", "config.local.yaml")
	if info, err := os.Stat(localPath); errors.Is(err, os.ErrNotExist) {
		content, err := os.ReadFile(filepath.Join(root, "configs", "config.local.example.yaml"))
		if err != nil {
			return nil, "", err
		}
		var local yaml.Node
		if err := yaml.Unmarshal(content, &local); err != nil {
			return nil, "", err
		}
		secret, err := randomValue(32)
		if err != nil {
			return nil, "", err
		}
		if err := renameConfig(&local, oldSlug, opts.slug); err != nil {
			return nil, "", err
		}
		if err := yamlSet(&local, secret, "auth", "jwt", "secret"); err != nil {
			return nil, "", err
		}
		content, err = yaml.Marshal(&local)
		if err != nil {
			return nil, "", err
		}
		changes = append(changes, fileChange{path: localPath, after: content, mode: 0o600})
	} else if err != nil {
		return nil, "", err
	} else if !info.Mode().IsRegular() {
		return nil, "", fmt.Errorf("本地配置路径必须是普通文件: %s", localPath)
	}
	seedPath := filepath.Join(root, "db", "seeds", "001_seed_admin.local.sql")
	var password string
	if info, err := os.Stat(seedPath); errors.Is(err, os.ErrNotExist) {
		seed, err := os.ReadFile(filepath.Join(root, "db", "seeds", "001_seed_admin.sql"))
		if err != nil {
			return nil, "", err
		}
		const placeholder = "__INITRA_ADMIN_PASSWORD_HASH__"
		if bytes.Count(seed, []byte(placeholder)) != 1 {
			return nil, "", fmt.Errorf("seed 必须包含一个密码哈希占位符")
		}
		password, err = randomValue(20)
		if err != nil {
			return nil, "", err
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return nil, "", err
		}
		changes = append(changes, fileChange{path: seedPath, after: bytes.Replace(seed, []byte(placeholder), hash, 1), mode: 0o600})
	} else if err != nil {
		return nil, "", err
	} else if !info.Mode().IsRegular() {
		return nil, "", fmt.Errorf("本地 seed 路径必须是普通文件: %s", seedPath)
	}
	return changes, password, nil
}

func writeChange(change fileChange) error {
	if change.before == nil {
		file, err := os.OpenFile(change.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, change.mode)
		if err != nil {
			return err
		}
		_, writeErr := file.Write(change.after)
		if err := errors.Join(writeErr, file.Close()); err != nil {
			return errors.Join(err, os.Remove(change.path))
		}
		return nil
	}
	file, err := os.CreateTemp(filepath.Dir(change.path), ".setup-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(change.mode); err != nil {
		_ = file.Close()
		return err
	}
	_, writeErr := file.Write(change.after)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), change.path)
}

func randomValue(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func normalizeSlug(value string) string {
	var builder strings.Builder
	separator := false
	for _, char := range strings.ToLower(strings.TrimSpace(value)) {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			if separator && builder.Len() > 0 {
				builder.WriteByte('-')
			}
			builder.WriteRune(char)
			separator = false
		} else if unicode.IsSpace(char) || char == '-' || char == '_' || char == '.' {
			separator = true
		}
	}
	slug := builder.String()
	if len(slug) > 0 && slug[0] >= '0' && slug[0] <= '9' {
		slug = "app-" + slug
	}
	return slug
}

func yamlSet(node *yaml.Node, value string, path ...string) error {
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			node.Content = append(node.Content, &yaml.Node{})
		}
		node = node.Content[0]
	}
	if len(path) == 0 {
		node.Kind, node.Tag, node.Value = yaml.ScalarNode, "!!str", value
		node.Content = nil
		return nil
	}
	// 空 YAML 分组解析为 null scalar，需要转为 mapping 才能真正写入子字段。
	if node.Kind == 0 || node.Tag == "!!null" {
		node.Kind, node.Tag, node.Value = yaml.MappingNode, "!!map", ""
		node.Content = nil
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("配置字段 %s 的上级必须是对象", strings.Join(path, "."))
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == path[0] {
			return yamlSet(node.Content[i+1], value, path[1:]...)
		}
	}
	child := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: path[0]}, child)
	return yamlSet(child, value, path[1:]...)
}

func yamlString(node *yaml.Node, path ...string) string {
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	if len(path) == 0 {
		return node.Value
	}
	if node.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == path[0] {
			return yamlString(node.Content[i+1], path[1:]...)
		}
	}
	return ""
}

// renameConfig 只修改已知身份字段，保留连接密码等与项目标识同值的自定义配置。
func renameConfig(node *yaml.Node, oldSlug, newSlug string) error {
	for _, path := range [][]string{{"app", "slug"}, {"database", "dbname"}, {"database", "application_name"}, {"auth", "jwt", "issuer"}} {
		for _, suffix := range []string{"", "_dev", "_test"} {
			if err := renameValue(node, oldSlug+suffix, newSlug+suffix, path...); err != nil {
				return err
			}
		}
	}
	return nil
}

func renameValue(node *yaml.Node, oldValue, newValue string, path ...string) error {
	if yamlString(node, path...) == oldValue {
		return yamlSet(node, newValue, path...)
	}
	return nil
}
