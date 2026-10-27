package monitor

import (
	"context"
	"time"

	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// RunFinite 在全局请求预算内推进显式窗口，只等待正常限速，不等待或绕过上游错误退避。
func (s *Service) RunFinite(ctx context.Context, window Window, maxRequests int) (Snapshot, error) {
	window.Start = window.Start.UTC()
	window.End = window.End.UTC()
	if window.Start.IsZero() || !window.Start.Before(window.End) || window.End.Sub(window.Start) > 90*24*time.Hour || (s.config.RealFetchEnabled && window.End.After(s.clock().UTC())) {
		return Snapshot{}, bizerrors.BadRequest("invalid scan window")
	}
	return s.runFinite(ctx, &window, maxRequests, 0)
}

// ResumeFinite 在请求预算内恢复当前运行；无待办时遵循持久化 72 小时计划与首次三十日窗口。
func (s *Service) ResumeFinite(ctx context.Context, maxRequests int) (Snapshot, error) {
	return s.runFinite(ctx, nil, maxRequests, 0)
}

func (s *Service) runFinite(ctx context.Context, window *Window, maxRequests int, target idgen.ID) (Snapshot, error) {
	if maxRequests < 1 || maxRequests > 200 {
		return Snapshot{}, bizerrors.BadRequest("max-pages must be between 1 and 200")
	}
	latest, err := s.Snapshot(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	if target != 0 {
		found := false
		for _, run := range latest.Runs {
			if run.ID == target {
				found = true
				if !run.FinishedAt.IsZero() {
					return latest, nil
				}
			}
		}
		if !found {
			return latest, bizerrors.BadRequest("retry target is not retained")
		}
		if active := activeSnapshotRun(latest); active == nil || active.ID != target {
			return latest, bizerrors.BadRequest("another run is active")
		}
	}
	initialRequests := 0
	key := ""
	if window != nil {
		key = s.windowKey(*window)
		if active := activeSnapshotRun(latest); active != nil {
			key = s.windowKey(*window, active.TemplateVersion)
		}
		if active := activeSnapshotRun(latest); active != nil && active.RequestKey != key {
			return latest, bizerrors.BadRequest("another window is active; resume its original window first")
		}
		for _, run := range latest.Runs {
			if run.RequestKey == s.windowKey(*window, run.TemplateVersion) {
				initialRequests = run.Requests
				if !run.FinishedAt.IsZero() {
					return latest, nil
				}
			}
		}
	} else if run := activeSnapshotRun(latest); run != nil {
		initialRequests = run.Requests
	}
	for {
		used := 0
		if run := activeSnapshotRun(latest); run != nil {
			used = run.Requests - initialRequests
		}
		if used >= maxRequests {
			return latest, nil
		}
		copy := *s
		copy.config = s.config
		copy.config.MaxPagesPerCall = maxRequests - used
		before := progressWeight(latest)
		if window != nil {
			latest, err = copy.RunWindow(ctx, *window)
		} else {
			latest, err = copy.runOnce(ctx, s.clock().UTC(), nil, target)
		}
		if err != nil {
			return latest, err
		}
		run := activeSnapshotRun(latest)
		if run == nil {
			return latest, nil
		}
		if run.Requests-initialRequests >= maxRequests {
			return latest, nil
		}
		next, err := s.NextWakeAt(ctx)
		if err != nil {
			return latest, err
		}
		delay := next.Sub(s.clock().UTC())
		if delay > s.config.SourceInterval+time.Second {
			return latest, nil
		}
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return latest, ctx.Err()
			case <-timer.C:
			}
		} else if progressWeight(latest) == before {
			return latest, nil
		}
	}
}
func activeSnapshotRun(snapshot Snapshot) *Run {
	if len(snapshot.Runs) == 0 {
		return nil
	}
	last := snapshot.Runs[len(snapshot.Runs)-1]
	if last.FinishedAt.IsZero() {
		return &last
	}
	return nil
}
func progressWeight(snapshot Snapshot) int {
	weight := 0
	for _, run := range snapshot.Runs {
		weight += run.Requests
		for _, p := range run.Sources {
			weight += p.TemplateIndex*2 + p.Observed + p.ReviewedCount + p.Projected
			if p.Acquired {
				weight++
			}
			if p.Reviewed {
				weight++
			}
			if p.Published {
				weight++
			}
		}
	}
	for _, candidate := range snapshot.Records {
		if candidate.Review != nil {
			weight++
		}
		if candidate.Published {
			weight++
		}
	}
	return weight
}
