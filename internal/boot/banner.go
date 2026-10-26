package boot

import (
	"strconv"

	"github.com/teamsillybees/glean/internal/platform/startup"
)

// newStartupInfo 根据应用配置构造启动提示所需的脱敏摘要。
func newStartupInfo(cfg *Config, addr string) startup.Info {
	if cfg == nil {
		return startup.Info{}
	}
	baseURL := startup.LocalURL(addr)
	info := startup.Info{
		AppName:    cfg.App.Name,
		Env:        cfg.App.Env,
		Version:    cfg.App.Version,
		InstanceID: cfg.App.InstanceID,
		Addr:       addr,
		Port:       startup.ListenPort(addr),
		URL:        baseURL,
		Database: startup.SQLDatabaseSummary(startup.SQLDatabase{
			Driver:       "postgres",
			Host:         cfg.Database.Host,
			Port:         cfg.Database.Port,
			User:         cfg.Database.User,
			DBName:       cfg.Database.DBName,
			MaxIdleConns: cfg.Database.MaxIdleConns,
			MaxOpenConns: cfg.Database.MaxOpenConns,
		}),
		Redis:       startup.RedisSummary(cfg.Redis),
		Task:        startup.TaskSummary(cfg.Task),
		Storage:     startup.EnabledSummary(cfg.Storage.Enabled, string(cfg.Storage.Provider)),
		HTTPClient:  startup.EnabledSummary(cfg.HTTPClient.Enabled, "services="+strconv.Itoa(len(cfg.HTTPClient.Services))),
		ShutdownTTL: cfg.Server.ShutdownTimeout.String(),
	}
	if baseURL == "" {
		return info
	}
	if cfg.Server.Docs.Enabled {
		info.DocsURL = baseURL + "/docs"
	} else {
		info.DocsURL = "disabled"
	}
	if cfg.Observability.Health.Enabled {
		info.Health = baseURL + "/health"
	} else {
		info.Health = "disabled"
	}
	return info
}
