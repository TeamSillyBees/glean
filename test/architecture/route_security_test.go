package architecture_test

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEveryAPIOperationRegistersRouteSecurity 防止新增 /api 路由时遗漏 fail-closed 安全元数据。
func TestEveryAPIOperationRegistersRouteSecurity(t *testing.T) {
	moduleRoot := filepath.Join(repoRoot(t), "internal", "modules")
	require.NoError(t, filepath.WalkDir(moduleRoot, func(path string, entry os.DirEntry, err error) error {
		require.NoError(t, err)
		if entry.IsDir() || !strings.HasSuffix(path, ".routes.go") {
			return nil
		}

		operations, registrations := routeKeys(t, path)
		for key := range operations {
			require.Containsf(t, registrations, key, "%s 中的 %s 缺少 RouteSecurity 注册", path, key)
		}
		for key := range registrations {
			require.Containsf(t, operations, key, "%s 中的 %s 没有对应 Huma operation", path, key)
		}
		return nil
	}))
}

func routeKeys(t *testing.T, path string) (map[string]struct{}, map[string]struct{}) {
	t.Helper()
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, nil, 0)
	require.NoError(t, err)
	operations := map[string]struct{}{}
	registrations := map[string]struct{}{}

	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		receiver, _ := selector.X.(*ast.Ident)
		switch {
		case receiver != nil && receiver.Name == "huma" && selector.Sel.Name == "Register" && len(call.Args) >= 2:
			if key, ok := operationRouteKey(fileSet, call.Args[1]); ok {
				operations[key] = struct{}{}
			}
		case receiver != nil && receiver.Name == "registry" && selector.Sel.Name == "Register" && len(call.Args) >= 2:
			if key, ok := routeKey(fileSet, call.Args[0], call.Args[1]); ok {
				registrations[key] = struct{}{}
			}
		}
		return true
	})
	return operations, registrations
}

func operationRouteKey(fileSet *token.FileSet, expression ast.Expr) (string, bool) {
	composite, ok := expression.(*ast.CompositeLit)
	if !ok {
		return "", false
	}
	var method ast.Expr
	var path ast.Expr
	for _, element := range composite.Elts {
		pair, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		name, _ := pair.Key.(*ast.Ident)
		if name == nil {
			continue
		}
		switch name.Name {
		case "Method":
			method = pair.Value
		case "Path":
			path = pair.Value
		}
	}
	return routeKey(fileSet, method, path)
}

func routeKey(fileSet *token.FileSet, method ast.Expr, path ast.Expr) (string, bool) {
	pathLiteral, ok := path.(*ast.BasicLit)
	if !ok || pathLiteral.Kind != token.STRING {
		return "", false
	}
	pathValue, err := strconv.Unquote(pathLiteral.Value)
	if err != nil || !strings.HasPrefix(pathValue, "/api/") {
		return "", false
	}
	var methodBuffer bytes.Buffer
	if method == nil || format.Node(&methodBuffer, fileSet, method) != nil {
		return "", false
	}
	return methodBuffer.String() + " " + pathValue, true
}
