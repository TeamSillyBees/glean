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
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// Config 限制一次执行的工作量；真实采集默认关闭，调度由调用方显式触发。
type Config struct {
	StateKey         string
	AnchorUTC        time.Time
	RealFetchEnabled bool
	MaxPagesPerCall  int
	RetryBudget      int
	BaseBackoff      time.Duration
	MaxBackoff       time.Duration
	LeaseDuration    time.Duration
	RequestTimeout   time.Duration
	SourceInterval   time.Duration
}

// DefaultConfig 返回不会启动外部采集的保守配置。
func DefaultConfig() Config {
	return Config{StateKey: "glean-monitor-mock-v1", AnchorUTC: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), MaxPagesPerCall: 12, RetryBudget: 5, BaseBackoff: time.Minute, MaxBackoff: 24 * time.Hour, LeaseDuration: 5 * time.Minute, RequestTimeout: 45 * time.Second, SourceInterval: 3 * time.Second}
}

// Service 协调采集、审核和目录投影，数据库状态通过修订号 CAS 防止并发覆盖。
type Service struct {
	client    *ent.Client
	memory    *memoryState
	ids       *idgen.Generator
	config    Config
	sources   []Source
	reviewer  Reviewer
	publisher Publisher
	clock     func() time.Time
}

// NewService 创建使用 Ent 的持久化监控服务；不自行启动后台任务。
func NewService(client *ent.Client, ids *idgen.Generator, cfg Config, sources []Source, reviewer Reviewer, publisher Publisher) (*Service, error) {
	if client == nil {
		return nil, bizerrors.BadRequest("monitor database is required")
	}
	return newService(client, nil, ids, cfg, sources, reviewer, publisher)
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
	if cfg.MaxPagesPerCall < 1 || cfg.RetryBudget < 1 || cfg.BaseBackoff < time.Second || cfg.MaxBackoff < cfg.BaseBackoff || cfg.RequestTimeout <= 0 || cfg.LeaseDuration < 2*cfg.RequestTimeout || cfg.SourceInterval < 0 {
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
		publisher = MockPublisher{}
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
	return state.Snapshot, nil
}

// RunOnce 执行一个有预算的工作片段，退避未到期时立即返回而不忙循环。
// 未完成的运行恢复同一固定 End；来源分页全部成功前不推进采集水位。
func (s *Service) RunOnce(ctx context.Context, now time.Time) (Snapshot, error) {
	now = now.UTC()
	state, err := s.acquire(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	defer s.release(state)
	run := activeRun(state)
	if run != nil {
		if run.TemplateVersion != TemplateVersion {
			return state.Snapshot, bizerrors.BadRequest("active monitor run requires its original template version")
		}
		if len(run.Sources) != len(s.sources) {
			return state.Snapshot, bizerrors.BadRequest("active monitor run requires its original source set")
		}
		for _, source := range s.sources {
			if _, ok := run.Sources[source.Name()]; !ok {
				return state.Snapshot, bizerrors.BadRequest("active monitor source set changed")
			}
		}
	}
	if run == nil {
		if now.Before(state.Schedule.NextRunUTC) {
			return state.Snapshot, nil
		}
		run = s.startRun(state, now)
		if err = s.save(ctx, state); err != nil {
			return Snapshot{}, err
		}
	}
	run.Status = "running"
	pages := 0
	templates := SearchTemplates()
	for _, source := range s.sources {
		progress, ok := run.Sources[source.Name()]
		if !ok {
			continue
		}
		checkpoint := state.Sources[source.Name()]
		if progress.Failed || progress.Published || now.Before(checkpoint.NextRetryAt) {
			continue
		}
		if !progress.Acquired && pages >= s.config.MaxPagesPerCall {
			continue
		}
		checkpoint.LastError = ""
		checkpoint.NextRetryAt = time.Time{}
		for !progress.Acquired && pages < s.config.MaxPagesPerCall {
			if err = ctx.Err(); err != nil {
				return s.cancelRun(state, run, err)
			}
			template := templates[progress.TemplateIndex]
			fetchCtx, cancel := context.WithTimeout(ctx, s.config.RequestTimeout)
			page, fetchErr := source.Fetch(fetchCtx, FetchRequest{Window: progress.Window, Cursor: progress.Cursor, Template: template})
			cancel()
			pages++
			if fetchErr != nil {
				if ctx.Err() != nil {
					return s.cancelRun(state, run, ctx.Err())
				}
				s.fail(progress, checkpoint, fetchErr, now)
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
				if (len(record.Raw) > 0 && !json.Valid(record.Raw)) || strings.TrimSpace(record.Title) == "" {
					s.fail(progress, checkpoint, &SourceError{Kind: "invalid_record", Message: "record metadata is malformed"}, now)
					break
				}
				record.Source = source.Name()
				record.AcquiredAt = now
				record.Topics = uniqueStrings(append(record.Topics, template.Topic))
				if strings.TrimSpace(record.SourceID) == "" && strings.TrimSpace(record.DOI) == "" {
					s.fail(progress, checkpoint, &SourceError{Kind: "identity", Message: "record lacks source identity", Retryable: false}, now)
					break
				}
				key, identity := recordIdentity(record)
				candidate, exists := state.Records[key]
				if !exists {
					candidate = &Candidate{ID: s.ids.NextID(), Key: key, Identity: identity, Record: record}
					state.Records[key] = candidate
				} else {
					topics := uniqueStrings(append(candidate.Record.Topics, record.Topics...))
					if len(topics) != len(candidate.Record.Topics) {
						candidate.Review = nil
						candidate.Published = false
					}
					candidate.Record.Topics = topics
				}
				progress.CandidateKeys = appendUnique(progress.CandidateKeys, key)
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
			checkpoint.NextRetryAt = time.Time{}
			if page.Complete {
				progress.TemplateIndex++
				progress.Cursor = ""
				if progress.TemplateIndex == len(templates) {
					progress.Acquired = true
					checkpoint.AcquiredThrough = run.FixedEnd
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
		if progress.Acquired && !progress.Reviewed && !progress.Failed {
			for _, key := range progress.CandidateKeys {
				candidate := state.Records[key]
				if candidate.Review != nil {
					continue
				}
				reviewCtx, cancel := context.WithTimeout(ctx, s.config.RequestTimeout)
				review, reviewErr := s.reviewer.Review(reviewCtx, candidate.Record)
				cancel()
				if reviewErr != nil {
					if ctx.Err() != nil {
						return s.cancelRun(state, run, ctx.Err())
					}
					s.fail(progress, checkpoint, reviewErr, now)
					break
				}
				if review.IsMock {
					review.ScientificReviewed = false
					review.InclusionStatus = "pending"
				}
				review.ReviewedAt = now
				candidate.Review = &review
				if err = s.save(ctx, state); err != nil {
					return Snapshot{}, err
				}
			}
			if checkpoint.LastError == "" {
				progress.Reviewed = true
				progress.LastError = ""
				checkpoint.ConsecutiveFailures = 0
				checkpoint.ReviewedThrough = run.FixedEnd
				if err = s.save(ctx, state); err != nil {
					return Snapshot{}, err
				}
			}
		}
		if progress.Reviewed && !progress.Published && !progress.Failed && checkpoint.LastError == "" {
			publications := make([]Publication, 0, len(progress.CandidateKeys))
			for _, key := range progress.CandidateKeys {
				candidate := state.Records[key]
				if !candidate.Published {
					publications = append(publications, Publication{Key: key, Record: candidate.Record, Review: *candidate.Review})
				}
			}
			publishCtx, cancel := context.WithTimeout(ctx, s.config.RequestTimeout)
			publishErr := s.publisher.Publish(publishCtx, run.ID, publications)
			cancel()
			if publishErr != nil {
				if ctx.Err() != nil {
					return s.cancelRun(state, run, ctx.Err())
				}
				s.fail(progress, checkpoint, publishErr, now)
			} else {
				for _, publication := range publications {
					state.Records[publication.Key].Published = true
				}
				progress.Published = true
				progress.LastError = ""
				checkpoint.ConsecutiveFailures = 0
				checkpoint.PublishedThrough = run.FixedEnd
			}
			if err = s.save(ctx, state); err != nil {
				return Snapshot{}, err
			}
		}
	}
	terminal, failed := true, false
	for _, progress := range run.Sources {
		terminal = terminal && (progress.Published || progress.Failed)
		failed = failed || progress.Failed
	}
	if terminal {
		run.Status = "completed"
		if failed {
			run.Status = "completed_with_errors"
		}
		run.FinishedAt = now
		state.Schedule.CompletedRuns++
		state.Schedule.NextRunUTC = nextAnchor(state.Schedule.AnchorUTC, run.FixedEnd)
	} else {
		run.Status = "pending"
		for _, checkpoint := range state.Sources {
			if checkpoint.NextRetryAt.After(now) {
				run.Status = "retry_wait"
				break
			}
		}
	}
	if err = s.save(ctx, state); err != nil {
		return Snapshot{}, err
	}
	return state.Snapshot, nil
}

func (s *Service) cancelRun(state *monitorState, run *Run, cause error) (Snapshot, error) {
	run.Status = "canceled"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
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

func (s *Service) startRun(state *monitorState, now time.Time) *Run {
	number := state.Schedule.CompletedRuns + 1
	mode := "mock"
	for _, source := range s.sources {
		if !isMockSource(source) {
			mode = "live"
		}
	}
	run := Run{ID: s.ids.NextID(), Number: number, StartedAt: now, FixedEnd: now, Status: "running", Mode: mode, TemplateVersion: TemplateVersion, Sources: map[string]*SourceProgress{}}
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
		if number%10 == 0 && now.Add(-90*24*time.Hour).Before(start) {
			start = now.Add(-90 * 24 * time.Hour)
		}
		// A delayed trigger starts at the old checkpoint, preserving every missed interval.
		keys := []string{}
		for key, candidate := range state.Records {
			if candidate.Record.Source == source.Name() && (!candidate.Published || candidate.Review == nil) {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		run.Sources[source.Name()] = &SourceProgress{Window: Window{Start: start, End: now}, CandidateKeys: keys}
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
