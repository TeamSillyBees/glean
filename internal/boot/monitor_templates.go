package boot

import (
	"context"
	"fmt"
	"sort"

	"github.com/teamsillybees/glean/internal/modules/catalog"
	"github.com/teamsillybees/glean/internal/modules/monitor"
)

type catalogSearchTopics interface {
	SearchTopicConfigs(context.Context) ([]catalog.SearchTopicConfig, error)
}
type monitorTemplateProvider struct{ catalog catalogSearchTopics }

// NewMonitorTemplateProvider 把目录配置转换为监控检索意图，保持模块依赖单向。
func NewMonitorTemplateProvider(service *catalog.Service) monitor.TemplateProvider {
	if service == nil {
		return &monitorTemplateProvider{}
	}
	return &monitorTemplateProvider{catalog: service}
}

// Templates 读取启用主题，内置默认保持原语义；自定义主题仅发现，不自动核定纳入资格。
func (p *monitorTemplateProvider) Templates(ctx context.Context) ([]monitor.SearchTemplate, error) {
	if p.catalog == nil {
		return nil, fmt.Errorf("catalog topic provider is required")
	}
	configs, err := p.catalog.SearchTopicConfigs(ctx)
	if err != nil {
		return nil, err
	}
	configs = append([]catalog.SearchTopicConfig(nil), configs...)
	defaults := monitor.SearchTemplates()
	builtin := map[string]monitor.SearchTemplate{}
	rank := map[string]int{}
	for i, t := range defaults {
		builtin[t.Topic] = t
		rank[t.Topic] = i + 1
	}
	sort.Slice(configs, func(i, j int) bool {
		a, b := rank[configs[i].Slug], rank[configs[j].Slug]
		if a == 0 {
			a = 100
		}
		if b == 0 {
			b = 100
		}
		if a != b {
			return a < b
		}
		return configs[i].Slug < configs[j].Slug
	})
	result := []monitor.SearchTemplate{}
	seen := map[string]bool{}
	for _, config := range configs {
		if seen[config.Slug] {
			return nil, fmt.Errorf("duplicate topic configuration")
		}
		seen[config.Slug] = true
		if !config.SearchEnabled {
			continue
		}
		t, known := builtin[config.Slug]
		if known && len(config.TermGroups) == 0 {
			result = append(result, t)
			continue
		}
		configured := monitor.TopicSearchTemplate(config.Slug, config.TermGroups, config.Version)
		if known {
			configured.Core = t.Core
			configured.Policy = t.Policy
		}
		result = append(result, configured)
	}
	return result, nil
}
