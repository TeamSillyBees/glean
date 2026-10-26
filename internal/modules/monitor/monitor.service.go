package monitor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/sysuser"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/entx"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// Config 限制一次执行的工作量；正常装配采用 RuntimeConfig，调度由调用方显式触发。
type Config struct {
	StateKey            string
	AnchorUTC           time.Time
	RealFetchEnabled    bool
	MaxPagesPerCall     int
	PageSize            int
	MaxReviewPerCall    int
	CompactPublished    bool
	PublishedCacheLimit int
	CompletedRunLimit   int
	RetryBudget         int
	BaseBackoff         time.Duration
	MaxBackoff          time.Duration
	LeaseDuration       time.Duration
	RequestTimeout      time.Duration
	SourceInterval      time.Duration
}

// DefaultConfig 返回不会启动外部采集的保守配置。
func DefaultConfig() Config {
	return Config{StateKey: "glean-monitor-mock-v1", AnchorUTC: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), MaxPagesPerCall: 12, MaxReviewPerCall: 200, RetryBudget: 5, BaseBackoff: time.Minute, MaxBackoff: 24 * time.Hour, LeaseDuration: 5 * time.Minute, RequestTimeout: 45 * time.Second, SourceInterval: 3 * time.Second}
}

// Service 协调采集、审核和目录投影，数据库状态通过修订号 CAS 防止并发覆盖。
type Service struct {
	client           *ent.Client
	memory           *memoryState
	ids              *idgen.Generator
	config           Config
	sources          []Source
	reviewer         Reviewer
	publisher        Publisher
	clock            func() time.Time
	journals         JournalProvider
	schedulerEnabled bool
	operatorID       idgen.ID
	requireOperator  bool
}

// NewService 创建使用 Ent 的持久化监控服务；不自行启动后台任务。
func NewService(client *ent.Client, ids *idgen.Generator, cfg Config, sources []Source, reviewer Reviewer, publisher Publisher, journals ...JournalProvider) (*Service, error) {
	if client == nil {
		return nil, bizerrors.BadRequest("monitor database is required")
	}
	if len(journals) > 1 {
		return nil, bizerrors.BadRequest("at most one journal registry provider is allowed")
	}
	service, err := newService(client, nil, ids, cfg, sources, reviewer, publisher)
	if err == nil && len(journals) == 1 {
		service.journals = journals[0]
	}
	return service, err
}

// NewOfflineService 创建仅用于演示和测试的内存服务，不用于生产持久化。
func NewOfflineService(ids *idgen.Generator, cfg Config, sources []Source, reviewer Reviewer, publisher Publisher) (*Service, error) {
	return newService(nil, &memoryState{}, ids, cfg, sources, reviewer, publisher)
}

func newService(client *ent.Client, memory *memoryState, ids *idgen.Generator, cfg Config, sources []Source, reviewer Reviewer, publisher Publisher) (*Service, error) {
	defaults := DefaultConfig()
	if ids == nil {
		return nil, bizerrors.BadRequest("monitor ID generator is required")
	}
	if cfg.StateKey == "" {
		cfg.StateKey = defaults.StateKey
	}
	if cfg.AnchorUTC.IsZero() {
		cfg.AnchorUTC = defaults.AnchorUTC
	}
	cfg.AnchorUTC = cfg.AnchorUTC.UTC()
	if cfg.MaxPagesPerCall == 0 {
		cfg.MaxPagesPerCall = defaults.MaxPagesPerCall
	}
	if cfg.MaxReviewPerCall == 0 {
		cfg.MaxReviewPerCall = defaults.MaxReviewPerCall
	}
	if cfg.RetryBudget == 0 {
		cfg.RetryBudget = defaults.RetryBudget
	}
	if cfg.BaseBackoff == 0 {
		cfg.BaseBackoff = defaults.BaseBackoff
	}
	if cfg.MaxBackoff == 0 {
		cfg.MaxBackoff = defaults.MaxBackoff
	}
	if cfg.LeaseDuration == 0 {
		cfg.LeaseDuration = defaults.LeaseDuration
	}
	if cfg.RequestTimeout == 0 {
		cfg.RequestTimeout = defaults.RequestTimeout
	}
	if cfg.SourceInterval == 0 {
		cfg.SourceInterval = defaults.SourceInterval
	}
	if cfg.PageSize < 0 || cfg.PageSize > 1000 || cfg.MaxReviewPerCall < 1 || cfg.MaxPagesPerCall < 1 || cfg.RetryBudget < 1 || cfg.BaseBackoff < time.Second || cfg.MaxBackoff < cfg.BaseBackoff || cfg.RequestTimeout <= 0 || cfg.LeaseDuration < 2*cfg.RequestTimeout || cfg.SourceInterval < 0 {
		return nil, bizerrors.BadRequest("invalid monitor budgets")
	}
	names := map[string]bool{}
	liveSources := false
	for _, source := range sources {
		if source == nil || source.Name() == "" || names[source.Name()] {
			return nil, bizerrors.BadRequest("monitor sources must have unique names")
		}
		names[source.Name()] = true
		if !isMockSource(source) {
			liveSources = true
		}
		if !isMockSource(source) && !cfg.RealFetchEnabled {
			return nil, bizerrors.BadRequest("real source fetch is disabled")
		}
	}
	if liveSources && cfg.StateKey == defaults.StateKey {
		cfg.StateKey = "glean-monitor-live-v1"
	}
	if len(sources) == 0 {
		return nil, bizerrors.BadRequest("at least one monitor source is required")
	}
	if reviewer == nil {
		reviewer = MockReviewer{}
	}
	if publisher == nil {
		if liveSources {
			return nil, bizerrors.BadRequest("real sources require a real catalog publisher")
		}
		publisher = MockPublisher{}
	}
	if _, mock := publisher.(MockPublisher); mock && liveSources {
		return nil, bizerrors.BadRequest("real sources cannot use a mock catalog publisher")
	}
	return &Service{client: client, memory: memory, ids: ids, config: cfg, sources: sources, reviewer: reviewer, publisher: publisher, clock: time.Now}, nil
}

func isMockSource(s Source) bool { v, ok := s.(interface{ IsMock() bool }); return ok && v.IsMock() }

// Snapshot 读取脱离持久化对象的快照；只读查询不会创建运行或触发网络。
func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	state, err := s.load(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	s.decorateSnapshot(&state.Snapshot)
	return state.Snapshot, nil
}

// RunOnce 执行一个有预算的工作片段，退避未到期时立即返回而不忙循环。
// 未完成的运行恢复同一固定 End；来源分页全部成功前不推进采集水位。
func (s *Service) RunOnce(ctx context.Context, now time.Time) (Snapshot, error) {
	return s.runOnce(ctx, now, nil)
}

// RunWindow 执行显式有界真实扫描窗口，重试保持同一窗口与运行 ID，不跨越未覆盖的来源缺口。
func (s *Service) RunWindow(ctx context.Context, window Window) (Snapshot, error) {
	window.Start = window.Start.UTC()
	window.End = window.End.UTC()
	if window.Start.IsZero() || !window.Start.Before(window.End) || window.End.Sub(window.Start) > 90*24*time.Hour || (s.config.RealFetchEnabled && window.End.After(s.clock().UTC())) {
		return Snapshot{}, bizerrors.BadRequest("scan window must be positive and at most 90 days")
	}
	return s.runOnce(ctx, s.clock().UTC(), &window)
}

func (s *Service) runOnce(ctx context.Context, now time.Time, requested *Window) (Snapshot, error) {
	if s.requireOperator && s.operatorID <= 0 {
		return Snapshot{}, bizerrors.BadRequest("real scan requires an explicit audited operator")
	}
	if s.requireOperator {
		if err := s.validateOperator(ctx); err != nil {
			return Snapshot{}, err
		}
	}
	if s.operatorID > 0 {
		ctx = entx.WithOperatorID(ctx, s.operatorID)
	}
	now = now.UTC()
	state, err := s.acquire(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	defer s.release(ctx, state)
	run := activeRun(state)
	if run != nil {
		if run.TemplateVersion != TemplateVersion {
			return state.Snapshot, bizerrors.BadRequest("active monitor run requires its original template version")
		}
		names := map[string]bool{}
		for _, source := range s.sources {
			names[source.Name()] = true
		}
		for name := range run.Sources {
			if !names[name] {
				return state.Snapshot, bizerrors.BadRequest("active run needs its original sources; resume with those sources")
			}
		}
		if requested != nil && run.RequestKey != s.windowKey(*requested) {
			return state.Snapshot, bizerrors.BadRequest("another window is active; resume its original window first")
		}
	}
	if run == nil {
		if requested != nil {
			key := s.windowKey(*requested)
			for _, previous := range state.Runs {
				if previous.RequestKey == key {
					return state.Snapshot, nil
				}
			}
		} else if now.Before(state.Schedule.NextRunUTC) && s.knownSources(state) {
			return state.Snapshot, nil
		}
		registry := []string{}
		registryByTopic := map[string][]string{}
		if s.journals != nil {
			entries, registryErr := s.journals.CoreJournals(ctx)
			if registryErr != nil {
				return state.Snapshot, registryErr
			}
			registry, registryByTopic = compileRegistry(entries)
		}
		run = s.startRun(state, now, requested, registry, registryByTopic)
		if err = s.save(ctx, state); err != nil {
			return Snapshot{}, err
		}
	}
	run.Status = "running"
	pages := 0
	reviewsUsed := 0
	templates := SearchTemplates()
	offset := run.Requests % len(s.sources)
	ordered := append(append([]Source{}, s.sources[offset:]...), s.sources[:offset]...)
	for _, source := range ordered {
		progress, ok := run.Sources[source.Name()]
		if !ok {
			continue
		}
		checkpoint := state.Sources[source.Name()]
		if !isMockSource(source) {
			s.shareProviderCooldown(state, source.Name(), checkpoint)
			if checkpoint.Blocked {
				progress.Failed = true
				progress.LastError = "provider_forbidden"
			}
		}
		if progress.Failed || progress.Handled || progress.Published || now.Before(checkpoint.NextRetryAt) {
			continue
		}
		if !progress.Acquired && pages >= s.config.MaxPagesPerCall && !pendingCandidates(state, progress) {
			continue
		}
		checkpoint.LastError = ""
		checkpoint.NextRetryAt = time.Time{}
		drainFirst := pendingCandidates(state, progress)
		for !progress.Acquired && pages < s.config.MaxPagesPerCall && !drainFirst {
			if err = ctx.Err(); err != nil {
				return s.cancelRun(ctx, state, run, err)
			}
			template := templates[progress.TemplateIndex%len(templates)]
			template.Core = progress.CoreBackfill && progress.TemplateIndex >= len(templates)
			fetchWindow := progress.Window
			if progress.CoreBackfill && !template.Core && !progress.IncrementalWindow.Start.IsZero() {
				fetchWindow = progress.IncrementalWindow
			}
			registryISSNs := progress.RegistryISSNs
			if template.Core && len(progress.RegistryByTopic) > 0 {
				registryISSNs = progress.RegistryByTopic[template.Topic]
			}
			if template.Core && len(registryISSNs) == 0 {
				s.fail(progress, checkpoint, &SourceError{Kind: "missing_registry", Message: "core journal registry has no registered ISSNs for this topic"}, now)
				if err = s.save(ctx, state); err != nil {
					return Snapshot{}, err
				}
				break
			}
			fetchCtx, cancel := context.WithTimeout(ctx, s.config.RequestTimeout)
			page, fetchErr := source.Fetch(fetchCtx, FetchRequest{PageSize: s.config.PageSize, Window: fetchWindow, Cursor: progress.Cursor, Template: template, RegistryISSNs: registryISSNs})
			cancel()
			pages++
			run.Requests++
			if fetchErr != nil {
				if ctx.Err() != nil {
					return s.cancelRun(ctx, state, run, ctx.Err())
				}
				failureAt := now
				if !isMockSource(source) {
					failureAt = s.clock().UTC()
				}
				s.fail(progress, checkpoint, fetchErr, failureAt)
				if err = s.save(ctx, state); err != nil {
					return Snapshot{}, err
				}
				break
			}
			if !page.Complete && (page.NextCursor == "" || page.NextCursor == progress.Cursor) {
				s.fail(progress, checkpoint, &SourceError{Kind: "pagination", Message: "source did not advance cursor", Retryable: false}, now)
				if err = s.save(ctx, state); err != nil {
					return Snapshot{}, err
				}
				break
			}
			for _, record := range page.Records {
				progress.Observed++
				record.IsMock = isMockSource(source)
				record.CoreDiscovery = template.Core
				record.Source = source.Name()
				record.AcquiredAt = now
				if !isMockSource(source) {
					record.AcquiredAt = s.clock().UTC()
				}
				record.Topics = uniqueStrings(append(record.Topics, template.Topic))
				reason := ""
				if record.ValidationError != "" {
					reason = safeRejectionKind(record.ValidationError)
					record.ValidationError = reason
				}
				if reason == "" && ((len(record.Raw) > 0 && !json.Valid(record.Raw)) || strings.TrimSpace(record.Title) == "") {
					reason = "invalid_record"
				}
				if reason == "" && strings.TrimSpace(record.SourceID) == "" && strings.TrimSpace(record.DOI) == "" {
					reason = "missing_identity"
				}
				if reason != "" {
					s.quarantineRecord(state, progress, checkpoint, record, reason, now)
					continue
				}
				key, identity := recordIdentity(record)
				candidate, exists := state.Records[key]
				if !exists {
					candidate = &Candidate{ID: s.ids.NextID(), Key: key, Identity: identity, Record: record}
					state.Records[key] = candidate
				} else {
					topics := uniqueStrings(append(candidate.Record.Topics, record.Topics...))
					topicsChanged := len(topics) != len(candidate.Record.Topics)
					if candidate.Compacted && topicsChanged {
						candidate.Record = record
						candidate.Compacted = false
					}
					if topicsChanged {
						candidate.Review = nil
						candidate.Published = false
					}
					candidate.Record.Topics = topics
				}
				beforeKeys := len(progress.CandidateKeys)
				progress.CandidateKeys = appendUnique(progress.CandidateKeys, key)
				if candidate.Quarantine != nil && len(progress.CandidateKeys) > beforeKeys {
					progress.Quarantined++
				}
			}
			if progress.Failed {
				if err = s.save(ctx, state); err != nil {
					return Snapshot{}, err
				}
				break
			}
			progress.Pages++
			progress.Cursor = page.NextCursor
			checkpoint.LastError = ""
			checkpoint.ConsecutiveFailures = 0
			checkpoint.LastStatusCode = 0
			checkpoint.NextRetryAt = time.Time{}
			if page.Complete {
				if checkpoint.TopicAcquiredThrough == nil {
					checkpoint.TopicAcquiredThrough = map[string]time.Time{}
				}
				if (!progress.CoreBackfill || template.Core) && (progress.AdvanceCheckpoint || run.RequestKey == "") {
					checkpoint.TopicAcquiredThrough[template.Topic] = later(checkpoint.TopicAcquiredThrough[template.Topic], run.FixedEnd)
				}
				progress.TemplateIndex++
				progress.Cursor = ""
				templateCount := len(templates)
				if progress.CoreBackfill {
					templateCount *= 2
				}
				if progress.TemplateIndex == templateCount {
					progress.Acquired = true
					if progress.AdvanceCheckpoint || run.RequestKey == "" {
						checkpoint.AcquiredThrough = later(checkpoint.AcquiredThrough, run.FixedEnd)
					}
					if progress.CoreWide {
						checkpoint.CoreBackfillPending = false
					}
				}
			}
			if err = s.save(ctx, state); err != nil {
				return Snapshot{}, err
			}
			// Real sources are paced by durable next-at, not sleeping inside a worker.
			if !isMockSource(source) && !progress.Acquired {
				checkpoint.NextRetryAt = now.Add(s.config.SourceInterval)
				if err = s.save(ctx, state); err != nil {
					return Snapshot{}, err
				}
				break
			}
		}
		if !progress.Reviewed && !progress.Failed && checkpoint.LastError == "" {
			sourceReviews := 0
			quota := max(1, s.config.MaxReviewPerCall/len(run.Sources))
			for _, key := range progress.CandidateKeys {
				candidate := state.Records[key]
				if candidate.Review != nil || candidate.Quarantine != nil {
					continue
				}
				if reviewsUsed >= s.config.MaxReviewPerCall || sourceReviews >= quota {
					break
				}
				reviewsUsed++
				sourceReviews++
				reviewCtx, cancel := context.WithTimeout(ctx, s.config.RequestTimeout)
				review, reviewErr := s.reviewer.Review(reviewCtx, candidate.Record)
				cancel()
				if reviewErr != nil {
					if ctx.Err() != nil {
						return s.cancelRun(ctx, state, run, ctx.Err())
					}
					s.fail(progress, checkpoint, reviewErr, now)
					break
				}
				if review.IsMock {
					review.ScientificReviewed = false
					review.InclusionStatus = "pending"
				}
				review.ReviewedAt = now
				if !isMockSource(source) {
					review.ReviewedAt = s.clock().UTC()
				}
				candidate.Review = &review
				progress.ReviewedCount++
			}
			if err = s.save(ctx, state); err != nil {
				return Snapshot{}, err
			}
			allReviewed := true
			for _, key := range progress.CandidateKeys {
				allReviewed = allReviewed && (state.Records[key].Review != nil || state.Records[key].Quarantine != nil)
			}
			if checkpoint.LastError == "" && progress.Acquired && allReviewed {
				progress.Reviewed = true
				progress.LastError = ""
				checkpoint.ConsecutiveFailures = 0
				if checkpoint.Quarantined == 0 && (progress.AdvanceCheckpoint || run.RequestKey == "") {
					checkpoint.ReviewedThrough = later(checkpoint.ReviewedThrough, run.FixedEnd)
				}
				if err = s.save(ctx, state); err != nil {
					return Snapshot{}, err
				}
			}
		}
		if !progress.Published && !progress.Failed && checkpoint.LastError == "" {
			publications := make([]Publication, 0, len(progress.CandidateKeys))
			for _, key := range progress.CandidateKeys {
				candidate := state.Records[key]
				if !candidate.Published && candidate.Quarantine == nil && candidate.Review != nil {
					publications = append(publications, Publication{Key: key, Record: candidate.Record, Review: *candidate.Review})
				}
			}
			var publishErr error
			for _, publication := range publications {
				publishCtx, cancel := context.WithTimeout(ctx, s.config.RequestTimeout)
				publishErr = s.publisher.Publish(publishCtx, run.ID, []Publication{publication})
				cancel()
				if publishErr != nil {
					var rejected *RejectedRecordError
					if errors.As(publishErr, &rejected) {
						state.Records[publication.Key].Quarantine = &Quarantine{Reason: safeRejectionKind(rejected.Kind), At: s.clock().UTC()}
						progress.Quarantined++
						checkpoint.Quarantined++
						publishErr = nil
						continue
					}
					break
				}
				state.Records[publication.Key].Published = true
				progress.Projected++
			}
			if publishErr != nil {
				if ctx.Err() != nil {
					return s.cancelRun(ctx, state, run, ctx.Err())
				}
				s.fail(progress, checkpoint, publishErr, now)
			} else {
				progress.Handled = progress.Acquired && !pendingCandidates(state, progress)
				progress.Published = progress.Handled && progress.Quarantined == 0
				progress.LastError = ""
				checkpoint.ConsecutiveFailures = 0
				if progress.Handled && (progress.AdvanceCheckpoint || run.RequestKey == "") {
					checkpoint.HandledThrough = later(checkpoint.HandledThrough, run.FixedEnd)
					if checkpoint.Quarantined == 0 {
						checkpoint.PublishedThrough = later(checkpoint.PublishedThrough, run.FixedEnd)
					}
				}
			}
			if err = s.save(ctx, state); err != nil {
				return Snapshot{}, err
			}
		}
	}
	terminal, failed, quarantined := true, false, false
	for _, progress := range run.Sources {
		terminal = terminal && (progress.Handled || progress.Published || progress.Failed)
		quarantined = quarantined || progress.Quarantined > 0
		failed = failed || progress.Failed
	}
	if terminal {
		run.Status = "completed"
		if quarantined {
			run.Status = "completed_with_quarantine"
		}
		if failed {
			run.Status = "completed_with_errors"
		}
		run.FinishedAt = now
		state.Schedule.CompletedRuns++
		state.Schedule.NextRunUTC = later(state.Schedule.NextRunUTC, nextAnchor(state.Schedule.AnchorUTC, run.FixedEnd))
	} else {
		run.Status = "pending"
		for _, checkpoint := range state.Sources {
			if checkpoint.NextRetryAt.After(now) {
				run.Status = "retry_wait"
				break
			}
		}
	}
	s.compact(state)
	if err = s.save(ctx, state); err != nil {
		return Snapshot{}, err
	}
	s.decorateSnapshot(&state.Snapshot)
	return state.Snapshot, nil
}

func (s *Service) cancelRun(parent context.Context, state *monitorState, run *Run, cause error) (Snapshot, error) {
	run.Status = "canceled"
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 3*time.Second)
	defer cancel()
	if err := s.save(ctx, state); err != nil {
		return state.Snapshot, err
	}
	return state.Snapshot, cause
}

func activeRun(state *monitorState) *Run {
	if len(state.Runs) == 0 {
		return nil
	}
	run := &state.Runs[len(state.Runs)-1]
	if run.FinishedAt.IsZero() {
		return run
	}
	return nil
}

func (s *Service) startRun(state *monitorState, now time.Time, requested *Window, registry []string, registryByTopic map[string][]string) *Run {
	number := state.Schedule.CompletedRuns + 1
	mode := "mock"
	for _, source := range s.sources {
		if !isMockSource(source) {
			mode = "live"
		}
	}
	run := Run{ID: s.ids.NextID(), Number: number, StartedAt: now, FixedEnd: now, Status: "running", Mode: mode, TemplateVersion: TemplateVersion, Sources: map[string]*SourceProgress{}}
	if requested != nil {
		run.FixedEnd = requested.End
		run.RequestKey = s.windowKey(*requested)
	}
	for _, source := range s.sources {
		checkpoint := state.Sources[source.Name()]
		if checkpoint == nil {
			checkpoint = &Checkpoint{Source: source.Name()}
			state.Sources[source.Name()] = checkpoint
		}
		start := checkpoint.AcquiredThrough.Add(-7 * 24 * time.Hour)
		if checkpoint.AcquiredThrough.IsZero() {
			if checkpoint.BootstrapFrom.IsZero() {
				checkpoint.BootstrapFrom = now.Add(-30 * 24 * time.Hour)
			}
			start = checkpoint.BootstrapFrom
		}
		incrementalStart := start
		core := !isMockSource(source) && journalSource(source.Name())
		wideCore := core && (number%10 == 0 || checkpoint.CoreBackfillPending)
		if (number%10 == 0 || wideCore) && now.Add(-90*24*time.Hour).Before(start) {
			start = now.Add(-90 * 24 * time.Hour)
		}
		if wideCore {
			checkpoint.CoreBackfillPending = true
		}
		if requested != nil {
			start = requested.Start
			incrementalStart = start
			if checkpoint.AcquiredThrough.IsZero() {
				checkpoint.BootstrapFrom = start
			}
		}
		// A delayed trigger starts at the old checkpoint, preserving every missed interval.
		keys := []string{}
		for key, candidate := range state.Records {
			if candidate.Record.Source == source.Name() && candidate.Quarantine == nil && (!candidate.Published || candidate.Review == nil) {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		run.Sources[source.Name()] = &SourceProgress{RegistryByTopic: registryByTopic, CoreWide: wideCore && requested == nil, IncrementalWindow: Window{Start: incrementalStart, End: run.FixedEnd}, Window: Window{Start: start, End: run.FixedEnd}, CandidateKeys: keys, RegistryISSNs: append([]string(nil), registry...), CoreBackfill: core, AdvanceCheckpoint: checkpoint.AcquiredThrough.IsZero() || !start.After(checkpoint.AcquiredThrough), Failed: checkpoint.Blocked}
	}
	state.Runs = append(state.Runs, run)
	return &state.Runs[len(state.Runs)-1]
}

func nextAnchor(anchor, after time.Time) time.Time {
	interval := 72 * time.Hour
	if after.Before(anchor) {
		return anchor
	}
	return anchor.Add((after.Sub(anchor)/interval + 1) * interval)
}

func (s *Service) fail(progress *SourceProgress, checkpoint *Checkpoint, cause error, now time.Time) {
	progress.Attempts++
	checkpoint.ConsecutiveFailures++
	retryable := true
	delay := s.config.BaseBackoff
	for i := 1; i < checkpoint.ConsecutiveFailures && delay < s.config.MaxBackoff; i++ {
		if delay > s.config.MaxBackoff/2 {
			delay = s.config.MaxBackoff
		} else {
			delay *= 2
		}
	}
	if delay > s.config.MaxBackoff {
		delay = s.config.MaxBackoff
	}
	kind := "upstream_failure"
	var sourceErr *SourceError
	if errors.As(cause, &sourceErr) {
		retryable = sourceErr.Retryable
		checkpoint.LastStatusCode = sourceErr.StatusCode
		if sourceErr.StatusCode == 403 {
			if sourceErr.RetryAfter > 0 {
				retryable = true
			} else {
				checkpoint.Blocked = true
			}
		}
		kind = sourceErr.Kind
		if sourceErr.RetryAfter > delay {
			delay = sourceErr.RetryAfter
		}
	}
	// Deliberately do not persist raw error strings: URLs and upstream bodies may contain credentials.
	progress.LastError = kind
	checkpoint.LastError = kind
	if !retryable || progress.Attempts >= s.config.RetryBudget {
		progress.Failed = true
		checkpoint.NextRetryAt = time.Time{}
		if sourceErr != nil && sourceErr.RetryAfter > 0 {
			checkpoint.NextRetryAt = now.Add(delay)
		}
	} else {
		checkpoint.NextRetryAt = now.Add(delay)
	}
}

func recordIdentity(record Record) (string, string) {
	doi := strings.ToLower(strings.TrimSpace(record.DOI))
	doi = strings.TrimPrefix(doi, "https://doi.org/")
	doi = strings.TrimPrefix(doi, "http://doi.org/")
	doi = strings.TrimPrefix(doi, "doi:")
	identity := record.Source + ":" + record.SourceID
	if doi != "" {
		identity = "doi:" + doi
	}
	copy := record
	copy.DOI = doi
	copy.AcquiredAt = time.Time{}
	copy.Topics = nil
	copy.CoreDiscovery = false
	var rawEvidence any
	if json.Unmarshal(record.Raw, &rawEvidence) == nil {
		copy.Raw, _ = json.Marshal(rawEvidence)
	}
	data, _ := json.Marshal(copy)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), identity
}

func uniqueStrings(values []string) []string {
	set := map[string]bool{}
	for _, v := range values {
		if v != "" {
			set[v] = true
		}
	}
	result := make([]string, 0, len(set))
	for v := range set {
		result = append(result, v)
	}
	sort.Strings(result)
	return result
}
func appendUnique(values []string, value string) []string {
	for _, v := range values {
		if v == value {
			return values
		}
	}
	return append(values, value)
}

// ValidateSnapshot 可用于 CLI 或运维核验状态格式，不会触发采集。
func ValidateSnapshot(snapshot Snapshot) error {
	if snapshot.FormatVersion != 1 {
		return fmt.Errorf("unsupported monitor snapshot version %d", snapshot.FormatVersion)
	}
	if snapshot.Schedule.EveryHours != 72 {
		return fmt.Errorf("unexpected schedule interval")
	}
	return nil
}

// RunPersistentDemo 将显式模拟演练写入与服务查询相同的 Ent 状态，绝不创建目录论文或启用真实采集。
func RunPersistentDemo(ctx context.Context, client *ent.Client, ids *idgen.Generator, now time.Time, scenario string) (Snapshot, error) {
	if scenario != "baseline" && scenario != "incremental" && scenario != "failure" {
		return Snapshot{}, bizerrors.BadRequest("unknown mock scenario")
	}
	cfg := DefaultConfig()
	cfg.MaxPagesPerCall = 100
	sources := DemoSources(now, false)
	if scenario == "failure" {
		sources[1].(*FixtureSource).Failure = &SourceError{Kind: "invalid_response", Message: "MOCK malformed source fixture"}
	}
	service, err := NewService(client, ids, cfg, sources, MockReviewer{}, MockPublisher{})
	if err != nil {
		return Snapshot{}, err
	}
	return runDemoScenario(ctx, service, now, scenario)
}

func later(first, second time.Time) time.Time {
	if second.After(first) {
		return second
	}
	return first
}
func journalSource(name string) bool {
	return name == "crossref" || name == "pubmed" || name == "europepmc"
}
func (s *Service) windowKey(window Window) string {
	names := []string{}
	for _, source := range s.sources {
		names = append(names, source.Name())
	}
	sort.Strings(names)
	raw, _ := json.Marshal(struct {
		Window  Window
		Sources []string
		Version string
	}{window, names, TemplateVersion})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (s *Service) decorateSnapshot(snapshot *Snapshot) {
	snapshot.Schedule.SchedulerEnabled = s.schedulerEnabled
	snapshot.Schedule.AcquisitionMode = "demo"
	for _, source := range s.sources {
		if !isMockSource(source) {
			snapshot.Schedule.AcquisitionMode = "live"
		}
	}
	snapshot.Schedule.ReviewMode = "unknown"
	if _, ok := s.reviewer.(MockReviewer); ok {
		snapshot.Schedule.ReviewMode = "mock"
	}
	for _, checkpoint := range snapshot.Sources {
		checkpoint.Enabled = false
	}
	for _, source := range s.sources {
		checkpoint := snapshot.Sources[source.Name()]
		if checkpoint == nil {
			checkpoint = &Checkpoint{Source: source.Name()}
			snapshot.Sources[source.Name()] = checkpoint
		}
		checkpoint.Enabled = true
		checkpoint.AcquisitionMode = "live"
		if isMockSource(source) {
			checkpoint.AcquisitionMode = "demo"
		}
		if !checkpoint.AcquiredThrough.IsZero() {
			lag := int64(s.clock().UTC().Sub(checkpoint.AcquiredThrough).Seconds())
			if lag < 0 {
				lag = 0
			}
			checkpoint.LagSeconds = &lag
		}
	}
}

func pendingCandidates(state *monitorState, progress *SourceProgress) bool {
	for _, key := range progress.CandidateKeys {
		candidate := state.Records[key]
		if candidate.Quarantine == nil && (candidate.Review == nil || !candidate.Published) {
			return true
		}
	}
	return false
}

func (s *Service) validateOperator(ctx context.Context) error {
	if s.operatorID <= 0 {
		return bizerrors.BadRequest("configure an enabled monitor operator ID before scanning")
	}
	if s.client == nil {
		return bizerrors.BadRequest("real operator validation requires PostgreSQL")
	}
	exists, err := s.client.SysUser.Query().Where(sysuser.IDEQ(s.operatorID), sysuser.DeletedAtIsNil(), sysuser.IsEnableEQ(true)).Exist(ctx)
	if err != nil {
		return bizerrors.WrapDBContext(ctx, err, "monitor operator lookup failed")
	}
	if !exists {
		return bizerrors.BadRequest("monitor operator is missing or disabled")
	}
	return nil
}

// Doctor 检查真实运行配置和操作人，不请求任何外部论文 API。
func (s *Service) Doctor(ctx context.Context) ([]Diagnostic, error) {
	diagnostics := []Diagnostic{{Code: "SOURCES_CONFIGURED", Severity: "info", Message: fmt.Sprintf("%d sources configured; no network request made by this diagnostic", len(s.sources))}}
	if s.requireOperator {
		if s.operatorID <= 0 {
			diagnostics = append(diagnostics, Diagnostic{Code: "OPERATOR_REQUIRED", Severity: "error", Message: "Set monitor.operator_id to an existing enabled user; required for audited ingestion"})
		} else if err := s.validateOperator(ctx); err != nil {
			diagnostics = append(diagnostics, Diagnostic{Code: "OPERATOR_UNAVAILABLE", Severity: "error", Message: "Configured monitor operator is unavailable; verify database and enabled user"})
		} else {
			diagnostics = append(diagnostics, Diagnostic{Code: "OPERATOR_READY", Severity: "info", Message: "Existing enabled audit operator verified"})
		}
	}
	if s.journals != nil {
		entries, registryErr := s.journals.CoreJournals(ctx)
		valid := 0
		for _, entry := range entries {
			if entry.IsMock {
				continue
			}
			for _, issn := range entry.ISSNs {
				if _, e := NormalizeISSN(issn); e == nil {
					valid++
				}
			}
		}
		if registryErr != nil {
			diagnostics = append(diagnostics, Diagnostic{Code: "CORE_REGISTRY_UNAVAILABLE", Severity: "error", Message: "Core journal registry could not be read"})
		} else if valid > 0 {
			diagnostics = append(diagnostics, Diagnostic{Code: "CORE_REGISTRY_PARTIAL_SEED", Severity: "warning", Message: "Core journal coverage is an incomplete historical seed, not a complete journal universe; ordinary keyword discovery is not limited to that seed"})
		} else if valid == 0 {
			diagnostics = append(diagnostics, Diagnostic{Code: "CORE_REGISTRY_EMPTY", Severity: "warning", Message: "Ordinary discovery is available; each scheduled core journal scan requires verified registry ISSNs"})
		}
	}
	if !s.schedulerEnabled {
		diagnostics = append(diagnostics, Diagnostic{Code: "SCHEDULER_DISABLED", Severity: "warning", Message: "Scheduled scans are disabled; use the finite real scan CLI or explicitly enable monitor.scheduler_enabled"})
	}
	if _, mock := s.reviewer.(MockReviewer); mock {
		diagnostics = append(diagnostics, Diagnostic{Code: "LLM_MOCK", Severity: "info", Message: "LLM review is mocked; no scientific inclusion decision has been made"})
	}
	return diagnostics, nil
}

func (s *Service) knownSources(state *monitorState) bool {
	for _, source := range s.sources {
		if state.Sources[source.Name()] == nil {
			return false
		}
	}
	return true
}

func sourceProvider(name string) string {
	if name == "medrxiv" {
		return "biorxiv"
	}
	return name
}
func (s *Service) shareProviderCooldown(state *monitorState, name string, target *Checkpoint) {
	for other, checkpoint := range state.Sources {
		if other == name || sourceProvider(other) != sourceProvider(name) {
			continue
		}
		if checkpoint.Blocked {
			target.Blocked = true
			target.LastStatusCode = 403
			target.LastError = "provider_forbidden"
		}
		if (checkpoint.LastStatusCode == 429 || checkpoint.LastStatusCode == 503 || checkpoint.LastStatusCode == 403) && checkpoint.NextRetryAt.After(target.NextRetryAt) {
			target.NextRetryAt = checkpoint.NextRetryAt
			target.LastStatusCode = checkpoint.LastStatusCode
			target.LastError = "provider_backoff"
		}
	}
}
