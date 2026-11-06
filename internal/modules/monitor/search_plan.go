package monitor

import (
	"fmt"
	"strings"
	"time"

	"github.com/teamsillybees/glean/internal/retrieval"
)

// QueryProgress 保存单一来源查询的冻结语义、分区进度和覆盖证据。
type QueryProgress struct {
	CoverageWindow      Window    `json:"coverageWindow"`
	AdvanceCheckpoint   bool      `json:"advanceCheckpoint"`
	QueryID             string    `json:"queryId"`
	QueryVersion        string    `json:"queryVersion"`
	Topic               string    `json:"topic"`
	Source              string    `json:"source"`
	Mode                string    `json:"mode"`
	Query               string    `json:"query"`
	Queries             []string  `json:"queries,omitempty"`
	Warnings            []string  `json:"warnings,omitempty"`
	Window              Window    `json:"window"`
	TargetEnd           time.Time `json:"targetEnd"`
	Status              string    `json:"status"`
	Requests            int       `json:"requests"`
	Pages               int       `json:"pages"`
	Observed            int       `json:"observed"`
	Quarantined         int       `json:"quarantined"`
	NewCandidates       int       `json:"newCandidates"`
	CompletedPartitions int       `json:"completedPartitions"`
	CompletedAt         time.Time `json:"completedAt,omitempty"`
	// LastError 只保存安全错误分类，不保存上游正文。
	LastError string `json:"lastError,omitempty"`
}

func sourceTemplates(templates []SearchTemplate, core bool, source string) []SearchTemplate {
	var ordinary, cores []SearchTemplate
	for _, t := range templates {
		if len(t.Routes) == 0 {
			v := t
			v.Core = false
			v.QueryID = t.ID
			v.QueryVersion = hashJSON(struct {
				Topic, Source string
				Groups        [][]string
			}{t.Topic, source, t.TermGroups})
			ordinary = append(ordinary, v)
		} else {
			for _, route := range t.Routes {
				selected := false
				for _, name := range route.Sources {
					if name == source {
						selected = true
					}
				}
				if !selected {
					continue
				}
				v := t
				v.Core = false
				v.Routes = nil
				v.ID = t.ID + "/" + route.ID
				v.QueryID = v.ID
				v.TermGroups = route.TermGroups
				v.FieldMode = route.FieldMode
				if v.FieldMode == "" {
					v.FieldMode = "default"
				}
				v.BackfillFrom = route.BackfillFrom
				compiled, err := retrieval.Compile(source, route.TermGroups, v.FieldMode)
				if err != nil {
					continue
				} // freezeTemplates 已校验所有 route。
				v.Query = compiled.Query
				v.Terms = nil
				for _, group := range v.TermGroups {
					v.Terms = append(v.Terms, group...)
				}
				v.Terms = uniqueStrings(v.Terms)
				v.QueryVersion = hashJSON(struct {
					Source, FieldMode string
					TermGroups        [][]string
					BackfillFrom      *time.Time
				}{source, v.FieldMode, route.TermGroups, route.BackfillFrom})
				ordinary = append(ordinary, v)
			}
		}
		if core && t.Core {
			v := t
			v.Routes = nil
			v.FieldMode = ""
			v.QueryID = t.ID + "#core"
			v.QueryVersion = hashJSON(struct{ Topic, Source, Mode string }{t.Topic, source, "core"})
			cores = append(cores, v)
		}
	}
	return append(ordinary, cores...)
}

func queryKey(t SearchTemplate) string { return t.QueryID + "@" + t.QueryVersion }

func initializeQueryProgress(run *Run, progress *SourceProgress, cp *Checkpoint, templates []SearchTemplate, source string, explicit bool) error {
	progress.Queries = map[string]*QueryProgress{}
	for _, t := range templates {
		compiled, err := retrieval.Compile(source, t.TermGroups, t.FieldMode)
		if err != nil && t.FieldMode != "" {
			return err
		}
		if err != nil {
			compiled = legacyQueryPreview(t, source)
		}
		if t.Core {
			compiled = retrieval.Compiled{Source: source, Mode: "core_journal", Query: "ISSN registry full journal feeds", Warnings: []string{"核心期刊全刊发现不表示主题相关；登记表并非期刊全集。"}}
		}
		window := progress.Window
		if !t.Core && !progress.IncrementalWindow.Start.IsZero() {
			window = progress.IncrementalWindow
		}
		if !explicit && !t.Core {
			last := cp.QueryAcquiredThrough[queryKey(t)]
			hasVersion := false
			for key := range cp.QueryAcquiredThrough {
				if strings.HasPrefix(key, t.QueryID+"@") {
					hasVersion = true
					break
				}
			}
			if last.IsZero() && !hasVersion && t.FieldMode == "" && !cp.TopicAcquiredThrough[t.Topic].IsZero() {
				last = cp.TopicAcquiredThrough[t.Topic]
			}
			if !last.IsZero() {
				window.Start = last.Add(-7 * 24 * time.Hour)
			} else if t.BackfillFrom != nil {
				window.Start = t.BackfillFrom.UTC()
			} else if t.FieldMode != "" || (len(cp.TopicAcquiredThrough) > 0 && cp.TopicAcquiredThrough[t.Topic].IsZero()) || hasVersion {
				window.Start = run.FixedEnd.Add(-30 * 24 * time.Hour)
			}
		}
		// 传统非期刊来源的每十轮90日补查继续保持旧行为。
		if t.FieldMode == "" && !explicit && !progress.CoreBackfill && progress.Window.Start.Before(window.Start) {
			window.Start = progress.Window.Start
		}
		if t.BackfillFrom != nil && window.Start.Before(*t.BackfillFrom) {
			window.Start = t.BackfillFrom.UTC()
		}
		if !window.Start.Before(run.FixedEnd) {
			return fmt.Errorf("query history start must precede scan end")
		}
		window.End = run.FixedEnd
		coverageWindow := window
		if t.FieldMode != "" && window.End.Sub(window.Start) > 90*24*time.Hour {
			window.End = window.Start.Add(90 * 24 * time.Hour)
		}
		advance := true
		if explicit && t.FieldMode != "" {
			previous := cp.QueryAcquiredThrough[queryKey(t)]
			requiredStart := run.FixedEnd.Add(-30 * 24 * time.Hour)
			if t.BackfillFrom != nil {
				requiredStart = t.BackfillFrom.UTC()
			}
			if previous.IsZero() {
				advance = !window.Start.After(requiredStart)
			} else {
				advance = !window.Start.After(previous)
			}
		}
		if !advance {
			progress.AdvanceCheckpoint = false
		}
		progress.Queries[t.QueryID] = &QueryProgress{CoverageWindow: coverageWindow, AdvanceCheckpoint: advance, QueryID: t.QueryID, QueryVersion: t.QueryVersion, Topic: t.Topic, Source: source, Mode: compiled.Mode, Query: compiled.Query, Queries: compiled.Queries, Warnings: compiled.Warnings, Window: window, TargetEnd: run.FixedEnd, Status: "not_started"}
	}
	if len(templates) == 0 {
		progress.Acquired = true
		progress.Reviewed = true
		progress.Published = true
		progress.Handled = true
	}
	return nil
}

func queryFor(progress *SourceProgress, t SearchTemplate) *QueryProgress {
	return progress.Queries[t.QueryID]
}

func (s *Service) updateQueryStatuses(run *Run, state *monitorState) {
	for source, p := range run.Sources {
		cp := state.Sources[source]
		for _, q := range p.Queries {
			if q.Status == "completed" || q.Status == "completed_empty" {
				continue
			}
			if p.Failed {
				q.Status = "failed"
				if q.Requests == 0 || cp.Blocked {
					q.Status = "blocked"
				}
				q.LastError = p.LastError
			} else if !cp.NextRetryAt.IsZero() && cp.LastError != "" {
				q.Status = "retry_wait"
				q.LastError = cp.LastError
			} else if q.Requests > 0 {
				q.Status = "partial_budget"
			}
		}
	}
}

func mergeDiscoveries(existing, incoming []retrieval.Hit) ([]retrieval.Hit, bool) {
	result := append([]retrieval.Hit(nil), existing...)
	changed := false
	for _, hit := range incoming {
		found := false
		for _, old := range result {
			if old.RunID == hit.RunID && old.QueryID == hit.QueryID && old.QueryVersion == hit.QueryVersion {
				found = true
				break
			}
		}
		if !found {
			result = append(result, hit)
			changed = true
		}
	}
	return result, changed
}

func recordUnavailableQueries(run *Run, templates []SearchTemplate, sources []Source) {
	enabled := map[string]bool{}
	for _, source := range sources {
		enabled[source.Name()] = true
	}
	for _, t := range templates {
		for _, route := range t.Routes {
			for _, source := range route.Sources {
				if enabled[source] {
					continue
				}
				expanded := sourceTemplates([]SearchTemplate{t}, false, source)
				for _, q := range expanded {
					if q.ID != t.ID+"/"+route.ID {
						continue
					}
					compiled, _ := retrieval.Compile(source, q.TermGroups, q.FieldMode)
					run.UnavailableQueries = append(run.UnavailableQueries, QueryProgress{QueryID: q.QueryID, QueryVersion: q.QueryVersion, Topic: t.Topic, Source: source, Mode: compiled.Mode, Query: compiled.Query, Queries: compiled.Queries, Status: "unavailable", Warnings: append(compiled.Warnings, "计划要求此来源，但本次执行来源集合未启用；未执行不代表无结果。")})
				}
			}
		}
	}
}

// QueryPreview 是无网络副作用的来源编译预览，Enabled 仅表示装配，不表示已执行。
type QueryPreview struct {
	retrieval.Compiled
	QueryID      string     `json:"queryId"`
	QueryVersion string     `json:"queryVersion"`
	Topic        string     `json:"topic"`
	Enabled      bool       `json:"enabled"`
	BackfillFrom *time.Time `json:"backfillFrom,omitempty"`
}

func queryPreviews(templates []SearchTemplate, sources []Source) []QueryPreview {
	enabled := map[string]bool{}
	for _, source := range sources {
		enabled[source.Name()] = true
	}
	var result []QueryPreview
	for _, source := range DefaultSourceNames() {
		for _, t := range sourceTemplates(templates, false, source) {
			compiled, err := retrieval.Compile(source, t.TermGroups, t.FieldMode)
			if err != nil {
				compiled = legacyQueryPreview(t, source)
			}
			result = append(result, QueryPreview{Compiled: compiled, QueryID: t.QueryID, QueryVersion: t.QueryVersion, Topic: t.Topic, Enabled: enabled[source], BackfillFrom: t.BackfillFrom})
		}
	}
	return result
}

func legacyQueryPreview(t SearchTemplate, source string) retrieval.Compiled {
	compiled := retrieval.Compiled{Source: source, Mode: "boolean", Query: t.Query, Warnings: []string{"传统词组查询预览；检索命中仅为待审核候选。"}}
	switch source {
	case "crossref":
		compiled.Mode = "seed_union"
		if len(t.TermGroups) > 0 {
			compiled.Queries = append([]string(nil), t.TermGroups[0]...)
		}
		compiled.Query = "Seed union (separate bibliographic requests): " + strings.Join(compiled.Queries, " | ")
		compiled.Warnings = append(compiled.Warnings, "Crossref逐种子并集不强制完整Boolean式；相关性需后续审核。")
	case "arxiv", "biorxiv", "medrxiv":
		compiled.Mode = "full_window"
		compiled.Query = ""
		compiled.Warnings = append(compiled.Warnings, "日期feed不执行主题关键词；保留候选供后续相关性审核。")
	}
	return compiled
}
