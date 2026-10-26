package monitor

import "sort"

// compact 只清理目录已确认持久化的候选；未审核、未投影、失败或退避积压一律保留。
func (s *Service) compact(state *monitorState) {
	if !s.config.CompactPublished {
		return
	}
	limit := s.config.PublishedCacheLimit
	if limit <= 0 {
		limit = 2000
	}
	historyLimit := s.config.CompletedRunLimit
	if historyLimit <= 0 {
		historyLimit = 100
	}
	for i := range state.Runs {
		for _, progress := range state.Runs[i].Sources {
			remaining := make([]string, 0, len(progress.CandidateKeys))
			for _, key := range progress.CandidateKeys {
				candidate, ok := state.Records[key]
				if ok && (!candidate.Published || candidate.Review == nil) {
					remaining = append(remaining, key)
				}
			}
			progress.CandidateKeys = remaining
		}
	}
	completed := make([]*Candidate, 0)
	for _, candidate := range state.Records {
		if candidate.Quarantine != nil || !candidate.Published || candidate.Review == nil {
			continue
		}
		// 关系目录已经保存全部来源观察、原始证据和正式事件；监控只缓存去重与审核回执。
		candidate.Record.Raw = nil
		candidate.Record.Abstract = ""
		candidate.Record.Title = ""
		candidate.Record.URL = ""
		candidate.Record.PublicationEvents = nil
		candidate.Record.Authors = nil
		candidate.Record.ISSNs = nil
		candidate.Record.Journal = ""
		candidate.Compacted = true
		completed = append(completed, candidate)
	}
	sort.Slice(completed, func(i, j int) bool {
		if completed[i].Record.AcquiredAt.Equal(completed[j].Record.AcquiredAt) {
			return completed[i].ID > completed[j].ID
		}
		return completed[i].Record.AcquiredAt.After(completed[j].Record.AcquiredAt)
	})
	for _, candidate := range completed[min(limit, len(completed)):] {
		delete(state.Records, candidate.Key)
	}
	keep := make([]Run, 0, min(len(state.Runs), historyLimit+1))
	completedCount := 0
	for i := len(state.Runs) - 1; i >= 0; i-- {
		run := state.Runs[i]
		if !run.FinishedAt.IsZero() {
			if completedCount >= historyLimit {
				continue
			}
			completedCount++
		}
		keep = append(keep, run)
	}
	for i, j := 0, len(keep)-1; i < j; i, j = i+1, j-1 {
		keep[i], keep[j] = keep[j], keep[i]
	}
	state.Runs = keep
}
