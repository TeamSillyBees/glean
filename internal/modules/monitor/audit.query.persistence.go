package monitor

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/gleansearchoccurrence"
	"github.com/teamsillybees/glean/internal/data/ent/gleansearchrequest"
	"github.com/teamsillybees/glean/internal/data/ent/gleansearchrun"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/retrieval"
)

// CatalogAuditReader 由启动层连接目录，监控模块不持有目录 service。
type CatalogAuditReader interface {
	RetrievalExportPage(context.Context, idgen.ID, idgen.ID, int) ([]retrieval.CatalogExportRecord, error)
}

func (s *Service) auditRun(ctx context.Context, id idgen.ID) (Run, error) {
	var raw []byte
	if s.memory != nil {
		s.memory.mu.Lock()
		raw = append([]byte(nil), s.memory.auditRuns[id]...)
		s.memory.mu.Unlock()
	} else {
		row, err := s.client.GleanSearchRun.Query().Where(gleansearchrun.ID(id), gleansearchrun.StateKey(s.config.StateKey)).Only(ctx)
		if err != nil && !ent.IsNotFound(err) {
			return Run{}, err
		}
		if row != nil {
			raw = row.Payload
		}
	}
	if len(raw) == 0 {
		state, err := s.load(ctx)
		if err != nil {
			return Run{}, err
		}
		for _, run := range state.Runs {
			if run.ID == id {
				return run, nil
			}
		}
		return Run{}, bizerrors.BadRequest("run archive not found")
	}
	var run Run
	if err := json.Unmarshal(raw, &run); err != nil {
		return Run{}, err
	}
	return run, nil
}

func (s *Service) auditRequests(ctx context.Context, runID, after idgen.ID, limit int) ([]RequestAudit, error) {
	result := []RequestAudit{}
	if s.memory != nil {
		s.memory.mu.Lock()
		defer s.memory.mu.Unlock()
		for _, r := range s.memory.auditRequests {
			if r.RunID == runID && r.RequestID > after {
				result = append(result, r)
			}
		}
		sort.Slice(result, func(i, j int) bool { return result[i].RequestID < result[j].RequestID })
		return result[:min(limit, len(result))], nil
	}
	rows, err := s.client.GleanSearchRequest.Query().Where(gleansearchrequest.RunID(runID), gleansearchrequest.IDGT(after)).Order(ent.Asc(gleansearchrequest.FieldID)).Limit(limit).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		var r RequestAudit
		if err = json.Unmarshal(row.Payload, &r); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, nil
}

func (s *Service) auditOccurrences(ctx context.Context, runID, after idgen.ID, limit int) ([]OccurrenceAudit, error) {
	result := []OccurrenceAudit{}
	if s.memory != nil {
		s.memory.mu.Lock()
		defer s.memory.mu.Unlock()
		for _, r := range s.memory.auditOccurrences {
			if r.RunID == runID && r.ID > after {
				result = append(result, r)
			}
		}
		sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
		return result[:min(limit, len(result))], nil
	}
	rows, err := s.client.GleanSearchOccurrence.Query().Where(gleansearchoccurrence.RunID(runID), gleansearchoccurrence.IDGT(after)).Order(ent.Asc(gleansearchoccurrence.FieldID)).Limit(limit).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		var r OccurrenceAudit
		if err = json.Unmarshal(row.Payload, &r); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, nil
}

func publicRun(run Run) Run {
	raw, _ := json.Marshal(run)
	var result Run
	_ = json.Unmarshal(raw, &result)
	for _, p := range result.Sources {
		p.Cursor = safeAuditCursor(p.Cursor)
	}
	return result
}

// archivedRunsPage 保存长期运行分页结果。
type archivedRunsPage struct {
	Items      []Run     `json:"items"`
	NextBefore *idgen.ID `json:"nextBefore"`
}

func (s *Service) archivedRunPage(ctx context.Context, before idgen.ID, limit int) (*archivedRunsPage, error) {
	if before < 0 {
		return nil, bizerrors.BadRequest("invalid cursor")
	}
	items := []Run{}
	if s.memory != nil {
		s.memory.mu.Lock()
		for id, raw := range s.memory.auditRuns {
			if before == 0 || id < before {
				var r Run
				if json.Unmarshal(raw, &r) == nil {
					items = append(items, r)
				}
			}
		}
		s.memory.mu.Unlock()
		sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
		items = items[:min(len(items), limit+1)]
	} else {
		q := s.client.GleanSearchRun.Query().Where(gleansearchrun.StateKey(s.config.StateKey)).Order(ent.Desc(gleansearchrun.FieldID)).Limit(limit + 1)
		if before > 0 {
			q.Where(gleansearchrun.IDLT(before))
		}
		rows, e := q.All(ctx)
		if e != nil {
			return nil, e
		}
		for _, row := range rows {
			var r Run
			if e = json.Unmarshal(row.Payload, &r); e != nil {
				return nil, e
			}
			items = append(items, r)
		}
	}
	var next *idgen.ID
	if len(items) > limit {
		items = items[:limit]
		v := items[len(items)-1].ID
		next = &v
	}
	for i := range items {
		items[i] = publicRun(items[i])
	}
	return &archivedRunsPage{Items: items, NextBefore: next}, nil
}
