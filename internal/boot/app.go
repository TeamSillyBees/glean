package boot

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/samber/do"
	"github.com/teamsillybees/glean/internal/accesscontrol"
	"github.com/teamsillybees/glean/internal/modules/monitor"
	platformdatabase "github.com/teamsillybees/glean/internal/platform/database"
	"github.com/teamsillybees/glean/internal/platform/logx"
	"github.com/teamsillybees/glean/internal/platform/observability"
	"github.com/teamsillybees/glean/internal/platform/redisx"
	"github.com/teamsillybees/glean/internal/platform/server"
	"github.com/teamsillybees/glean/internal/platform/task"
)

// Options 描述应用启动时的外部输入。
type Options struct {
	Env       string
	ConfigDir string
	BuildInfo observability.BuildInfoVO
}

// Application 是启动完成后的应用聚合根。
type Application struct {
	Container     *do.Injector
	Config        *Config
	Logger        *logx.Logger
	Web           *server.App
	Server        *http.Server
	DB            *sql.DB
	Redis         redisx.UniversalClient
	Publisher     task.Publisher
	Worker        task.Worker
	Scheduler     task.Scheduler
	AccessControl *accesscontrol.Control

	workerStarted    bool
	schedulerStarted bool
}

const dependencyReadinessTimeout = 2 * time.Second

// Bootstrap 完成配置加载、依赖注入、模块注册与 HTTP Server 组装。
func Bootstrap(options Options) (*Application, error) {
	cfg, err := LoadConfig(options.Env, options.ConfigDir)
	if err != nil {
		return nil, err
	}

	injector := do.New()
	do.ProvideValue(injector, cfg)

	registerProviders(injector, cfg, options.BuildInfo)
	registerModules(injector, cfg)

	logger := do.MustInvoke[*logx.Logger](injector)
	db, err := do.Invoke[*sql.DB](injector)
	if err != nil {
		return nil, fmt.Errorf("resolve database client: %w", err)
	}
	var redisClient redisx.UniversalClient
	if cfg.Redis.Enabled {
		redisClient, err = do.Invoke[redisx.UniversalClient](injector)
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("resolve redis client: %w", err)
		}
	}
	webApp := do.MustInvoke[*server.App](injector)
	accessControl := do.MustInvoke[*accesscontrol.Control](injector)
	enforcer := do.MustInvoke[*casbin.SyncedEnforcer](injector)
	accessControl.BindEnforcer(enforcer)
	publisher, err := do.Invoke[task.Publisher](injector)
	if err != nil {
		closeBootstrapResources(db, redisClient, nil)
		return nil, fmt.Errorf("resolve task publisher: %w", err)
	}
	if err := registerMonitorTasks(injector, cfg); err != nil {
		closeBootstrapResources(db, redisClient, publisher)
		return nil, err
	}
	worker, scheduler, err := resolveTaskRunners(injector, cfg.Task)
	if err != nil {
		closeBootstrapResources(db, redisClient, publisher)
		return nil, err
	}
	readiness, err := newReadinessRegistry(cfg, db, redisClient, publisher)
	if err != nil {
		closeBootstrapResources(db, redisClient, publisher)
		return nil, err
	}

	registerRoutes(injector, webApp, options.BuildInfo, readiness)

	s := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           webApp.Engine,
		ReadTimeout:       cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
		ReadHeaderTimeout: 3 * time.Second,
	}

	return &Application{
		Container:     injector,
		Config:        cfg,
		Logger:        logger,
		Web:           webApp,
		Server:        s,
		DB:            db,
		Redis:         redisClient,
		Publisher:     publisher,
		Worker:        worker,
		Scheduler:     scheduler,
		AccessControl: accessControl,
	}, nil
}

// registerMonitorTasks 在 Worker 构建处理器快照前完成显式真实来源调度装配。
func registerMonitorTasks(injector *do.Injector, cfg *Config) error {
	if !cfg.Monitor.SchedulerEnabled {
		return nil
	}
	if !cfg.Task.Enabled || !cfg.Task.Worker.Enabled || !cfg.Task.Scheduler.Enabled {
		return fmt.Errorf("monitor scheduler requires enabled task worker and scheduler")
	}
	service, err := do.Invoke[*monitor.Service](injector)
	if err != nil {
		return fmt.Errorf("resolve monitor service: %w", err)
	}
	registry, err := do.Invoke[task.Registry](injector)
	if err != nil {
		return fmt.Errorf("resolve monitor task registry: %w", err)
	}
	if err := service.RegisterTasks(registry); err != nil {
		return fmt.Errorf("register monitor tasks: %w", err)
	}
	scheduler, err := do.Invoke[task.Scheduler](injector)
	if err != nil {
		return fmt.Errorf("resolve monitor task scheduler: %w", err)
	}
	if err := service.RegisterSchedule(scheduler); err != nil {
		return fmt.Errorf("register monitor schedule: %w", err)
	}
	return nil
}

func resolveTaskRunners(injector *do.Injector, cfg task.Config) (task.Worker, task.Scheduler, error) {
	var worker task.Worker
	var scheduler task.Scheduler
	var err error
	if cfg.Enabled && cfg.Worker.Enabled {
		worker, err = do.Invoke[task.Worker](injector)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve task worker: %w", err)
		}
	}
	if cfg.Enabled && cfg.Scheduler.Enabled {
		scheduler, err = do.Invoke[task.Scheduler](injector)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve task scheduler: %w", err)
		}
	}
	return worker, scheduler, nil
}

func newReadinessRegistry(
	cfg *Config,
	db *sql.DB,
	redisClient redisx.UniversalClient,
	publisher task.Publisher,
) (*observability.ReadinessRegistry, error) {
	readiness := observability.NewReadinessRegistry()
	if err := readiness.Register("database", dependencyReadinessTimeout, observability.ReadinessCheckFunc(func(ctx context.Context) error {
		return platformdatabase.Ping(ctx, db)
	})); err != nil {
		return nil, err
	}
	if cfg.Redis.Enabled {
		if err := readiness.Register("redis", dependencyReadinessTimeout, observability.ReadinessCheckFunc(func(ctx context.Context) error {
			return redisx.Ping(ctx, redisClient)
		})); err != nil {
			return nil, err
		}
	}
	if cfg.Task.Enabled {
		checker, ok := publisher.(observability.ReadinessChecker)
		if !ok {
			return nil, fmt.Errorf("task publisher does not implement readiness checker")
		}
		if err := readiness.Register("task", dependencyReadinessTimeout, checker); err != nil {
			return nil, err
		}
	}
	return readiness, nil
}

func closeBootstrapResources(db *sql.DB, redisClient redisx.UniversalClient, publisher task.Publisher) {
	if publisher != nil {
		_ = publisher.Close()
	}
	if redisClient != nil {
		_ = redisClient.Close()
	}
	if db != nil {
		_ = db.Close()
	}
}
