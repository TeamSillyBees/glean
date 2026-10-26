package architecture_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/require"
)

type exportedCommentFinding struct {
	path    string
	line    int
	symbol  string
	comment string
}

// TestExportedDeclarationsHaveCanonicalComments 约束生产 Go 源码的导出声明使用以标识符开头的文档注释。
func TestExportedDeclarationsHaveCanonicalComments(t *testing.T) {
	findings, err := findExportedCommentIssues(repoRoot(t))
	require.NoError(t, err)

	for _, finding := range findings {
		t.Errorf(
			"%s:%d: 导出声明 %s 的注释必须以标识符开头，当前注释为 %q",
			finding.path,
			finding.line,
			finding.symbol,
			finding.comment,
		)
	}
}

// TestPlatformDoesNotImportApplication 固定基础能力不能反向依赖业务或项目生成的 Ent。
func TestPlatformDoesNotImportApplication(t *testing.T) {
	for _, pkg := range goList(t, "./internal/platform/...") {
		for _, imported := range pkg.Imports {
			if strings.HasPrefix(imported, "github.com/teamsillybees/glean/") {
				require.Truef(t, strings.HasPrefix(imported, "github.com/teamsillybees/glean/internal/platform/"), "%s imports application package %s", pkg.ImportPath, imported)
			}
			require.Falsef(
				t,
				strings.Contains(imported, "/internal/data/ent"),
				"%s imports generated Ent package %s",
				pkg.ImportPath,
				imported,
			)
		}
	}
}

// TestLowLevelPackagesDoNotImportWeb 固定底层公共包不能依赖 Web 装配层。
func TestLowLevelPackagesDoNotImportWeb(t *testing.T) {
	for _, pattern := range []string{"./internal/platform/errors", "./internal/platform/requestctx"} {
		for _, pkg := range goList(t, pattern) {
			for _, imported := range pkg.Imports {
				require.NotEqualf(t, "github.com/teamsillybees/glean/internal/platform/server", imported, "%s imports internal/platform/server", pkg.ImportPath)
			}
		}
	}
}

// TestRedisConstructionGoesThroughRedisx 固定 Redis client 和 Lua 脚本必须通过 internal/platform/redisx 统一封装。
func TestRedisConstructionGoesThroughRedisx(t *testing.T) {
	disallowed := []string{
		"redis.NewClient(",
		"redis.NewFailoverClient(",
		"redis.NewUniversalClient(",
		"redis.NewScript(",
	}
	allowedDirs := []string{
		filepath.Join("internal", "platform", "redisx"),
	}

	root := repoRoot(t)
	for _, dir := range []string{"internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, err error) error {
			require.NoError(t, err)
			if entry.IsDir() {
				rel, relErr := filepath.Rel(root, path)
				require.NoError(t, relErr)
				if isPathUnderAny(rel, allowedDirs) {
					return filepath.SkipDir
				}
				return nil
			}
			if filepath.Ext(path) != ".go" {
				return nil
			}

			content, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			for _, token := range disallowed {
				require.NotContainsf(t, string(content), token, "%s must use internal/platform/redisx instead of %s", path, token)
			}
			return nil
		})
		require.NoError(t, err)
	}
}

func findExportedCommentIssues(root string) ([]exportedCommentFinding, error) {
	sourceRoots := []string{"cmd", "internal", "tools"}
	findings := make([]exportedCommentFinding, 0)
	for _, sourceRoot := range sourceRoots {
		path := filepath.Join(root, sourceRoot)
		err := filepath.WalkDir(path, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == "vendor" || entry.Name() == "tmp" || entry.Name() == "var" {
					return filepath.SkipDir
				}
				if isGeneratedEntDirectory(root, path) {
					return filepath.SkipDir
				}
				return nil
			}
			if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}

			fileFindings, err := exportedCommentIssuesInFile(root, path)
			if err != nil {
				return err
			}
			findings = append(findings, fileFindings...)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].path != findings[j].path {
			return findings[i].path < findings[j].path
		}
		if findings[i].line != findings[j].line {
			return findings[i].line < findings[j].line
		}
		return findings[i].symbol < findings[j].symbol
	})
	return findings, nil
}

func exportedCommentIssuesInFile(root string, path string) ([]exportedCommentFinding, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("解析 Go 源码 %s 失败: %w", path, err)
	}
	if ast.IsGenerated(file) {
		return nil, nil
	}

	relative, err := filepath.Rel(root, path)
	if err != nil {
		return nil, err
	}
	relative = filepath.ToSlash(relative)
	findings := make([]exportedCommentFinding, 0)
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			if declaration.Name.IsExported() && !commentStartsWithIdentifier(declaration.Doc, declaration.Name.Name) {
				symbol := declaration.Name.Name
				if declaration.Recv != nil && len(declaration.Recv.List) > 0 {
					symbol = receiverTypeName(declaration.Recv.List[0].Type) + "." + declaration.Name.Name
				}
				findings = append(findings, newExportedCommentFinding(fset, relative, declaration.Name, symbol, declaration.Doc))
			}
		case *ast.GenDecl:
			if declaration.Tok != token.CONST && declaration.Tok != token.TYPE && declaration.Tok != token.VAR {
				continue
			}
			for _, specification := range declaration.Specs {
				switch specification := specification.(type) {
				case *ast.TypeSpec:
					doc := specification.Doc
					if doc == nil {
						doc = declaration.Doc
					}
					if specification.Name.IsExported() && !commentStartsWithIdentifier(doc, specification.Name.Name) {
						findings = append(findings, newExportedCommentFinding(fset, relative, specification.Name, specification.Name.Name, doc))
					}
				case *ast.ValueSpec:
					doc := specification.Doc
					if doc == nil {
						doc = declaration.Doc
					}
					for _, name := range specification.Names {
						if name.IsExported() && !commentStartsWithIdentifier(doc, name.Name) {
							findings = append(findings, newExportedCommentFinding(fset, relative, name, name.Name, doc))
						}
					}
				}
			}
		}
	}
	return findings, nil
}

func commentStartsWithIdentifier(doc *ast.CommentGroup, identifier string) bool {
	if doc == nil {
		return false
	}
	comment := strings.TrimSpace(doc.Text())
	if comment == identifier {
		return true
	}
	if !strings.HasPrefix(comment, identifier) {
		return false
	}
	for _, separator := range comment[len(identifier):] {
		return unicode.IsSpace(separator) || unicode.IsPunct(separator)
	}
	return false
}

func newExportedCommentFinding(fset *token.FileSet, path string, identifier *ast.Ident, symbol string, doc *ast.CommentGroup) exportedCommentFinding {
	comment := ""
	if doc != nil {
		comment = firstCommentLine(doc.Text())
	}
	return exportedCommentFinding{
		path:    path,
		line:    fset.Position(identifier.Pos()).Line,
		symbol:  symbol,
		comment: comment,
	}
}

func receiverTypeName(expression ast.Expr) string {
	switch expression := expression.(type) {
	case *ast.Ident:
		return expression.Name
	case *ast.StarExpr:
		return receiverTypeName(expression.X)
	case *ast.IndexExpr:
		return receiverTypeName(expression.X)
	case *ast.IndexListExpr:
		return receiverTypeName(expression.X)
	default:
		return "receiver"
	}
}

func firstCommentLine(comment string) string {
	comment = strings.TrimSpace(comment)
	if index := strings.IndexByte(comment, '\n'); index >= 0 {
		return comment[:index]
	}
	return comment
}

func isGeneratedEntDirectory(root string, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	relative = filepath.ToSlash(relative)
	return relative == "internal/data/ent" || strings.HasPrefix(relative, "internal/data/ent/")
}

func isPathUnderAny(path string, prefixes []string) bool {
	clean := filepath.Clean(path)
	for _, prefix := range prefixes {
		prefix = filepath.Clean(prefix)
		if clean == prefix || strings.HasPrefix(clean, prefix+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
