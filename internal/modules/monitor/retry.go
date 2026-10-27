package monitor

import (
	"context"
	"encoding/json"
	"time"

	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/entx"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// RetryFailedFinite 显式恢复已分类的暂时失败；同一原运行只创建一个恢复运行，不循环重置预算。
func (s *Service) RetryFailedFinite(ctx context.Context, runID idgen.ID, maxRequests int) (Snapshot, error) {
	if runID <= 0 || maxRequests < 1 || maxRequests > 200 {
		return Snapshot{}, bizerrors.BadRequest("retry requires a positive run ID and max-pages between 1 and 200")
	}
	target, snapshot, err := s.prepareRetry(ctx, runID)
	if err != nil {
		return snapshot, err
	}
	return s.runFinite(ctx, nil, maxRequests, target)
}

func (s *Service) prepareRetry(ctx context.Context, runID idgen.ID) (idgen.ID, Snapshot, error) {
	if s.requireOperator {
		if err := s.validateOperator(ctx); err != nil {
			return 0, Snapshot{}, err
		}
	}
	if s.operatorID > 0 {
		ctx = entx.WithOperatorID(ctx, s.operatorID)
	}
	state, err := s.acquire(ctx)
	if err != nil {
		return 0, Snapshot{}, err
	}
	defer s.release(ctx, state)
	index := -1
	for i := range state.Runs {
		if state.Runs[i].ID == runID {
			index = i
			break
		}
	}
	if index < 0 {
		return 0, state.Snapshot, bizerrors.BadRequest("retry run not found in retained history")
	}
	original := state.Runs[index]
	if original.TemplateVersion != TemplateVersion {
		return 0, state.Snapshot, bizerrors.BadRequest("retry requires original template version")
	}
	if len(original.Sources) != len(s.sources) {
		return 0, state.Snapshot, bizerrors.BadRequest("retry requires the exact original source set")
	}
	for _, source := range s.sources {
		if original.Sources[source.Name()] == nil {
			return 0, state.Snapshot, bizerrors.BadRequest("retry requires the exact original source set")
		}
	}
	if original.RetriedBy != 0 {
		for _, run := range state.Runs {
			if run.ID == original.RetriedBy {
				return run.ID, state.Snapshot, nil
			}
		}
		return 0, state.Snapshot, bizerrors.BadRequest("retry already created but no longer retained; never recreate it")
	}
	if activeRun(state) != nil {
		return 0, state.Snapshot, bizerrors.BadRequest("another run is active; resume it first")
	}
	if original.Status != "completed_with_errors" || original.FinishedAt.IsZero() {
		return 0, state.Snapshot, bizerrors.BadRequest("only completed failed runs can be explicitly retried")
	}
	now := s.clock().UTC()
	eligible := 0
	for name, progress := range original.Sources {
		if !progress.Failed || !progress.RetryableFailure {
			continue
		}
		checkpoint := state.Sources[name]
		if checkpoint == nil {
			return 0, state.Snapshot, bizerrors.BadRequest("retry checkpoint missing")
		}
		s.shareProviderCooldown(state, name, checkpoint)
		if checkpoint.Blocked {
			return 0, state.Snapshot, bizerrors.BadRequest("source is blocked; retry cannot override access restrictions")
		}
		if now.Before(checkpoint.NextRetryAt) || now.Before(progress.RetryNotBefore) {
			return 0, state.Snapshot, bizerrors.BadRequest("retry cooldown has not expired")
		}
		for _, key := range progress.CandidateKeys {
			if state.Records[key] == nil {
				return 0, state.Snapshot, bizerrors.BadRequest("retry candidate evidence is missing")
			}
		}
		eligible++
	}
	if eligible == 0 {
		return 0, state.Snapshot, bizerrors.BadRequest("no classified transient failure to retry; permanent or legacy unknown errors require inspection")
	}
	// 深复制保留窗口、检索版本、期刊集合、分页游标与阶段进度；旧运行作为审计证据不被重写。
	raw, err := json.Marshal(original)
	if err != nil {
		return 0, state.Snapshot, err
	}
	var retry Run
	if err := json.Unmarshal(raw, &retry); err != nil {
		return 0, state.Snapshot, err
	}
	retry.ID = s.ids.NextID()
	retry.RetryOf = original.ID
	retry.RetriedBy = 0
	retry.Number = original.Number
	retry.StartedAt = now
	retry.FinishedAt = time.Time{}
	retry.Status = "pending"
	retry.Requests = 0
	retry.RetryCandidateKeys = map[string][]string{}
	for name, progress := range retry.Sources {
		if !progress.Failed || !progress.RetryableFailure {
			continue
		}
		retry.RetryCandidateKeys[name] = append([]string(nil), progress.CandidateKeys...)
		progress.Failed = false
		progress.Attempts = 0
		progress.LastError = ""
		progress.RetryableFailure = false
		progress.RetryNotBefore = time.Time{}
		checkpoint := state.Sources[name]
		checkpoint.ConsecutiveFailures = 0
		checkpoint.LastError = ""
		checkpoint.LastStatusCode = 0
	}
	state.Runs[index].RetriedBy = retry.ID
	state.Runs = append(state.Runs, retry)
	if err := s.save(ctx, state); err != nil {
		return 0, Snapshot{}, err
	}
	return retry.ID, state.Snapshot, nil
}
