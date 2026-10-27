package monitor

import (
	"fmt"
	"github.com/samber/do"
	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/platform/httpclient"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// ProviderOptions 定义真实来源服务装配；构造本身不会请求外部 API。
type ProviderOptions struct {
	Templates        TemplateProvider
	Config           Config
	Sources          []string
	SchedulerEnabled bool
	OperatorID       idgen.ID
}

// RuntimeOptions 是独立 CLI 与启动层共用的运行参数。
type RuntimeOptions = ProviderOptions

// RuntimeConfig 返回真实来源能力配置；是否持续运行仍由显式 scheduler_enabled 控制。
func RuntimeConfig() Config {
	cfg := DefaultConfig()
	cfg.StateKey = "glean-monitor-live-v1"
	cfg.RealFetchEnabled = true
	cfg.CompactPublished = true
	cfg.PublishedCacheLimit = 2000
	cfg.CompletedRunLimit = 100
	return cfg
}

// NewRuntimeService 创建真实采集与真实目录投影的持久化服务；只有 LLM Reviewer 使用明确 mock。
func NewRuntimeService(client *ent.Client, ids *idgen.Generator, factory *httpclient.Factory, options ProviderOptions, publisher Publisher, journals JournalProvider) (*Service, error) {
	if options.Templates == nil {
		return nil, fmt.Errorf("live monitor requires a topic template provider")
	}
	cfg := options.Config
	if cfg.StateKey == "" {
		cfg = RuntimeConfig()
	}
	sources, err := NewHTTPSourceSet(factory, options.Sources)
	if err != nil {
		return nil, err
	}
	service, err := NewService(client, ids, cfg, sources, MockReviewer{}, publisher, journals)
	if err != nil {
		return nil, err
	}
	service.templates = options.Templates
	service.operatorID = options.OperatorID
	service.requireOperator = true
	service.schedulerEnabled = options.SchedulerEnabled
	return service, nil
}

// Provide 显式装配真实来源、Ent 与目录发布器，绝不默认安装模拟来源或模拟发布器。
func Provide(injector *do.Injector, options ...ProviderOptions) {
	opts := ProviderOptions{Config: RuntimeConfig(), Sources: DefaultSourceNames()}
	if len(options) > 0 {
		opts = options[0]
	}
	ProvideWithFactory(injector, func(i *do.Injector) (*Service, error) {
		client, err := do.Invoke[*ent.Client](i)
		if err != nil {
			return nil, err
		}
		ids, err := do.Invoke[*idgen.Generator](i)
		if err != nil {
			return nil, err
		}
		factory, err := do.Invoke[*httpclient.Factory](i)
		if err != nil {
			return nil, err
		}
		publisher, err := do.Invoke[Publisher](i)
		if err != nil {
			return nil, err
		}
		journals, err := do.Invoke[JournalProvider](i)
		if err != nil {
			return nil, err
		}
		if opts.Templates == nil {
			opts.Templates, err = do.Invoke[TemplateProvider](i)
			if err != nil {
				return nil, err
			}
		}
		return NewRuntimeService(client, ids, factory, opts, publisher, journals)
	})
}

// ProvideWithFactory 允许启动层集中装配小接口，避免业务模块反向依赖 boot 或其他业务模块。
func ProvideWithFactory(injector *do.Injector, factory func(*do.Injector) (*Service, error)) {
	do.Provide(injector, factory)
	do.Provide(injector, func(i *do.Injector) (*Handler, error) {
		service, err := do.Invoke[*Service](i)
		if err != nil {
			return nil, err
		}
		return NewHandler(service), nil
	})
	do.Provide(injector, func(i *do.Injector) (*Module, error) {
		handler, err := do.Invoke[*Handler](i)
		if err != nil {
			return nil, err
		}
		return NewModule(handler), nil
	})
}
