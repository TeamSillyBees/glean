package monitor

// compileRegistry 只按明确主题路由真实发现目标，不把发现资格等同于指标核验。
func compileRegistry(entries []JournalEntry, selected ...[]SearchTemplate) ([]string, map[string][]string) {
	topics := []string{}
	byTopic := map[string][]string{}
	templates := SearchTemplates()
	if len(selected) > 0 {
		templates = selected[0]
	}
	for _, template := range templates {
		topics = append(topics, template.Topic)
		byTopic[template.Topic] = []string{}
	}
	union := []string{}
	for _, entry := range entries {
		if entry.IsMock {
			continue
		}
		targets := entry.DiscoveryTopics
		// 兼容旧测试/注入提供者；生产目录提供者始终明确返回每条记录的 discoveryTopics。
		if targets == nil {
			targets = topics
		}
		for _, target := range targets {
			if _, known := byTopic[target]; !known {
				continue
			}
			for _, raw := range entry.ISSNs {
				if value, err := NormalizeISSN(raw); err == nil {
					byTopic[target] = append(byTopic[target], value)
					union = append(union, value)
				}
			}
		}
	}
	for topic, values := range byTopic {
		byTopic[topic] = uniqueStrings(values)
	}
	return uniqueStrings(union), byTopic
}
