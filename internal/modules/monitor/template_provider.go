package monitor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// TemplateProvider 在新运行开始时读取主题配置；恢复运行不再次读取。
type TemplateProvider interface {
	Templates(context.Context) ([]SearchTemplate, error)
}

type defaultTemplateProvider struct{}

// Templates 返回离线测试与旧固定模板的兼容默认值。
func (defaultTemplateProvider) Templates(context.Context) ([]SearchTemplate, error) {
	return SearchTemplates(), nil
}

// TopicSearchTemplate 从关键词组建立检索意图，不把配置文字伪装为科学纳入规则。
func TopicSearchTemplate(slug string, groups [][]string, version int) SearchTemplate {
	return SearchTemplate{ID: slug + "-configured", Topic: slug, Version: fmt.Sprintf("topic-%d", version), TermGroups: groups}
}

var topicSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,79}$`)

func freezeTemplates(input []SearchTemplate) ([]SearchTemplate, string, error) {
	if len(input) == 0 {
		return nil, "", fmt.Errorf("search requires at least one enabled template")
	}
	// JSON 深拷贝，提供者后续修改不能改变正在运行的游标语义。
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, "", err
	}
	var result []SearchTemplate
	if err = json.Unmarshal(raw, &result); err != nil {
		return nil, "", err
	}
	topics := map[string]bool{}
	ids := map[string]bool{}
	for i := range result {
		t := &result[i]
		if !topicSlugPattern.MatchString(t.Topic) || len(t.Topic) > 80 || t.ID == "" || topics[t.Topic] || ids[t.ID] || len(t.TermGroups) == 0 || len(t.TermGroups) > 10 {
			return nil, "", fmt.Errorf("invalid search template configuration")
		}
		topics[t.Topic] = true
		ids[t.ID] = true
		groups := []string{}
		terms := []string{}
		for _, group := range t.TermGroups {
			if len(group) == 0 || len(group) > 50 {
				return nil, "", fmt.Errorf("invalid search term group")
			}
			quoted := []string{}
			for _, term := range group {
				if strings.TrimSpace(term) != term || term == "" || len(term) > 300 || strings.ContainsAny(term, "\r\n\x00") {
					return nil, "", fmt.Errorf("invalid search term")
				}
				quoted = append(quoted, strconv.Quote(term))
				terms = append(terms, term)
			}
			groups = append(groups, "("+strings.Join(quoted, " OR ")+")")
		}
		t.Query = strings.Join(groups, " AND ")
		t.Terms = uniqueStrings(terms)
	}
	raw, _ = json.Marshal(result)
	defaults, _ := json.Marshal(SearchTemplates())
	if string(raw) == string(defaults) {
		return result, TemplateVersion, nil
	}
	hash := sha256.Sum256(raw)
	return result, "snapshot-" + hex.EncodeToString(hash[:]), nil
}

func (s *Service) currentTemplates(ctx context.Context) ([]SearchTemplate, string, error) {
	if s.templates == nil {
		return nil, "", fmt.Errorf("search template provider is required")
	}
	templates, err := s.templates.Templates(ctx)
	if err != nil {
		return nil, "", err
	}
	return freezeTemplates(templates)
}

func runTemplates(run *Run) ([]SearchTemplate, error) {
	if run.SearchTemplates == nil {
		if run.TemplateVersion != TemplateVersion {
			return nil, fmt.Errorf("run requires its original template snapshot")
		}
		return SearchTemplates(), nil
	}
	templates, version, err := freezeTemplates(run.SearchTemplates)
	if err != nil {
		return nil, err
	}
	if version != run.TemplateVersion {
		return nil, fmt.Errorf("run template snapshot version mismatch")
	}
	return templates, nil
}

func executionTemplates(templates []SearchTemplate, core bool) []SearchTemplate {
	result := append([]SearchTemplate(nil), templates...)
	for i := range result {
		result[i].Core = false
	}
	if core {
		for _, t := range templates {
			if t.Core {
				result = append(result, t)
			}
		}
	}
	return result
}
