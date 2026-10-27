package boot

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/modules/catalog"
	"github.com/teamsillybees/glean/internal/modules/monitor"
)

type searchTopicFixture struct {
	configs []catalog.SearchTopicConfig
	err     error
}

func (s searchTopicFixture) SearchTopicConfigs(context.Context) ([]catalog.SearchTopicConfig, error) {
	return s.configs, s.err
}

// TestMonitorTopicBridge 保留内置模板顺序与指标策略，禁用主题不参与采集，新主题无虚构指标门槛。
func TestMonitorTopicBridge(t *testing.T) {
	configs := []catalog.SearchTopicConfig{
		{Slug: "biomed-worldmodel", SearchEnabled: true}, {Slug: "llm-agent", SearchEnabled: true}, {Slug: "biomed-ai", SearchEnabled: true},
	}
	p := &monitorTemplateProvider{catalog: searchTopicFixture{configs: configs}}
	templates, err := p.Templates(context.Background())
	require.NoError(t, err)
	require.Equal(t, monitor.SearchTemplates(), templates)
	configs[1].SearchEnabled = false
	configs[2].TermGroups = [][]string{{"new biomedical keyword"}}
	configs[2].Version = 2
	configs = append(configs, catalog.SearchTopicConfig{Slug: "custom", SearchEnabled: true, Version: 1, TermGroups: [][]string{{"alpha", "beta"}, {"medical"}}})
	p.catalog = searchTopicFixture{configs: configs}
	templates, err = p.Templates(context.Background())
	require.NoError(t, err)
	require.Len(t, templates, 3)
	require.Equal(t, "biomed-ai", templates[0].Topic)
	require.Equal(t, configs[2].TermGroups, templates[0].TermGroups)
	require.Equal(t, monitor.SearchTemplates()[0].Policy, templates[0].Policy)
	require.Equal(t, "custom", templates[2].Topic)
	require.False(t, templates[2].Core)
	require.Equal(t, monitor.InclusionPolicy{}, templates[2].Policy)
}

// TestMonitorTopicBridgeFailsClosed 目录读取失败、缺失提供者或重复配置不能偷偷退回静态主题。
func TestMonitorTopicBridgeFailsClosed(t *testing.T) {
	_, err := NewMonitorTemplateProvider(nil).Templates(context.Background())
	require.Error(t, err)
	unavailable := errors.New("catalog unavailable")
	p := &monitorTemplateProvider{catalog: searchTopicFixture{err: unavailable}}
	_, err = p.Templates(context.Background())
	require.ErrorIs(t, err, unavailable)
	p.catalog = searchTopicFixture{configs: []catalog.SearchTopicConfig{{Slug: "same"}, {Slug: "same"}}}
	_, err = p.Templates(context.Background())
	require.ErrorContains(t, err, "duplicate")
}
