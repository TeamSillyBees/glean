package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"entgo.io/ent/entc"
	"entgo.io/ent/entc/gen"
	"github.com/teamsillybees/glean/internal/platform/logx"
)

const entPackage = "github.com/teamsillybees/glean/internal/data/ent"

// main 生成 Ent 代码，并把手写 schema 与生成代码分开放置。
func main() {
	root, err := dataDir()
	if err != nil {
		fatal("resolve entgenerate source path", err)
	}
	target := filepath.Join(root, "ent")
	if err := os.MkdirAll(target, 0o755); err != nil {
		fatal("create ent target dir", err)
	}
	err = entc.Generate(
		filepath.Join(root, "schema"),
		&gen.Config{
			Target:  target,
			Package: entPackage,
		},
		entc.FeatureNames("sql/versioned-migration", "sql/lock"),
	)
	if err != nil {
		fatal("generate ent code", err)
	}
}

// dataDir 返回 internal/data 目录。
func dataDir() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime caller unavailable")
	}
	return filepath.Dir(filepath.Dir(file)), nil
}

func fatal(msg string, err error) {
	logger, createErr := logx.NewLogger(logx.Config{
		Level: "error",
		Console: logx.ConsoleConfig{
			Enabled: true,
			Level:   "error",
			Stack:   logx.StackShort,
			Output:  "stderr",
		},
		Redact: logx.RedactConfig{Enabled: true},
	})
	if createErr == nil {
		logger.Error(context.Background(), msg, err)
		_ = logger.Sync()
	}
	os.Exit(1)
}
