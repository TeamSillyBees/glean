package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/teamsillybees/glean/internal/boot"
	"github.com/teamsillybees/glean/internal/platform/logx"
	"github.com/teamsillybees/glean/internal/platform/observability"
)

// 构建元信息变量会在发布构建时被覆盖，默认值服务于本地运行。
var (
	// version 由构建脚本通过 -ldflags 注入，未注入时使用 dev 便于本地开发识别。
	version = "dev"
	// commit 记录当前构建关联的 Git 提交号，用于 /version 接口和问题追踪。
	commit = "none"
	// buildTime 记录构建发生时间，和 version / commit 一起构成最小构建元信息。
	buildTime = "unknown"
)

// main 是 HTTP 服务入口，负责组装启动上下文、注入构建信息并运行应用。
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	app, err := boot.Bootstrap(boot.Options{
		Env: os.Getenv("APP_ENV"),
		BuildInfo: observability.BuildInfoVO{
			Version:   version,
			Commit:    commit,
			BuildTime: buildTime,
		},
	})
	if err != nil {
		fatal(ctx, "bootstrap app failed", err, nil)
	}

	if err := app.Run(ctx); err != nil {
		fatal(ctx, "run app failed", err, app.Logger)
	}
}

func fatal(ctx context.Context, msg string, err error, logger *logx.Logger) {
	if logger == nil {
		logger = fallbackLogger()
	}
	logger.Error(ctx, msg, err)
	_ = logger.Sync()
	os.Exit(1)
}

func fallbackLogger() *logx.Logger {
	logger, err := logx.NewLogger(logx.Config{
		Level: "error",
		Console: logx.ConsoleConfig{
			Enabled: true,
			Level:   "error",
			Stack:   logx.StackShort,
			Output:  "stderr",
		},
		Redact: logx.RedactConfig{Enabled: true},
	})
	if err != nil {
		return logx.NewNop()
	}
	return logger
}
