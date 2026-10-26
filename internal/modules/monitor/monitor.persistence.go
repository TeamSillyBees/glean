package monitor

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/gleanmonitorstate"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

type persistedState struct {
	View       Snapshot  `json:"view"`
	LeaseOwner string    `json:"leaseOwner"`
	LeaseUntil time.Time `json:"leaseUntil"`
}
type monitorState struct {
	Snapshot
	id         idgen.ID
	revision   int
	leaseOwner string
	leaseUntil time.Time
}

// memoryState 仅用于明确的离线演示和测试；生产 NewService 始终使用 Ent。
type memoryState struct {
	mu       sync.Mutex
	data     []byte
	revision int
	id       idgen.ID
}

func (s *Service) emptyState() *monitorState {
	state := &monitorState{Snapshot: Snapshot{FormatVersion: 1, Schedule: Schedule{AnchorUTC: s.config.AnchorUTC, NextRunUTC: s.config.AnchorUTC, EveryHours: 72, RealFetchEnabled: s.config.RealFetchEnabled}, Runs: []Run{}, Sources: map[string]*Checkpoint{}, Records: map[string]*Candidate{}}}
	for _, source := range s.sources {
		state.Sources[source.Name()] = &Checkpoint{Source: source.Name()}
	}
	s.decorateSnapshot(&state.Snapshot)
	return state
}

func (s *Service) load(ctx context.Context) (*monitorState, error) {
	var raw []byte
	var id idgen.ID
	var revision int
	if s.memory != nil {
		s.memory.mu.Lock()
		raw = append([]byte(nil), s.memory.data...)
		id = s.memory.id
		revision = s.memory.revision
		s.memory.mu.Unlock()
	} else {
		row, err := s.client.GleanMonitorState.Query().Where(gleanmonitorstate.KeyEQ(s.config.StateKey)).Only(ctx)
		if ent.IsNotFound(err) {
			return s.emptyState(), nil
		}
		if err != nil {
			return nil, bizerrors.WrapDBContext(ctx, err, "load monitor state failed")
		}
		raw = row.Snapshot
		id = row.ID
		revision = row.Revision
	}
	if len(raw) == 0 {
		return s.emptyState(), nil
	}
	var persisted persistedState
	if err := json.Unmarshal(raw, &persisted); err != nil {
		return nil, bizerrors.WrapInternalContext(ctx, err, "invalid monitor state")
	}
	if persisted.View.Schedule.RealFetchEnabled != s.config.RealFetchEnabled {
		return nil, bizerrors.BadRequest("monitor mock/live configuration changed; use a separate state key")
	}
	if err := ValidateSnapshot(persisted.View); err != nil {
		return nil, bizerrors.WrapInternalContext(ctx, err, "unsupported monitor state")
	}
	return &monitorState{Snapshot: persisted.View, id: id, revision: revision, leaseOwner: persisted.LeaseOwner, leaseUntil: persisted.LeaseUntil}, nil
}

func (s *Service) save(ctx context.Context, state *monitorState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if state.leaseOwner != "" {
		state.leaseUntil = s.clock().UTC().Add(s.config.LeaseDuration)
	}
	raw, err := json.Marshal(persistedState{View: state.Snapshot, LeaseOwner: state.leaseOwner, LeaseUntil: state.leaseUntil})
	if err != nil {
		return bizerrors.WrapInternalContext(ctx, err, "encode monitor state failed")
	}
	if s.memory != nil {
		s.memory.mu.Lock()
		defer s.memory.mu.Unlock()
		if state.id != s.memory.id || state.revision != s.memory.revision {
			return bizerrors.BadRequest("monitor state changed; lease lost")
		}
		if state.id == 0 {
			state.id = s.ids.NextID()
		}
		state.revision++
		s.memory.id = state.id
		s.memory.revision = state.revision
		s.memory.data = raw
		return nil
	}
	if state.id == 0 {
		row, createErr := s.client.GleanMonitorState.Create().SetID(s.ids.NextID()).SetKey(s.config.StateKey).SetSnapshot(raw).SetRevision(1).Save(ctx)
		if createErr != nil {
			return bizerrors.WrapDBContext(ctx, createErr, "initialize monitor state failed; another worker may own it")
		}
		state.id = row.ID
		state.revision = row.Revision
		return nil
	}
	affected, updateErr := s.client.GleanMonitorState.Update().Where(gleanmonitorstate.IDEQ(state.id), gleanmonitorstate.RevisionEQ(state.revision)).SetSnapshot(raw).AddRevision(1).Save(ctx)
	if updateErr != nil {
		return bizerrors.WrapDBContext(ctx, updateErr, "save monitor state failed")
	}
	if affected != 1 {
		return bizerrors.BadRequest("monitor state changed; lease lost")
	}
	state.revision++
	return nil
}

func (s *Service) acquire(ctx context.Context) (*monitorState, error) {
	state, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	if state.leaseOwner != "" && state.leaseUntil.After(s.clock().UTC()) {
		return nil, bizerrors.BadRequest("a monitor run is already active")
	}
	state.leaseOwner = s.ids.NextID().String()
	if err = s.save(ctx, state); err != nil {
		return nil, err
	}
	return state, nil
}

func (s *Service) release(parent context.Context, state *monitorState) {
	// Revision CAS fences an expired worker: release cannot overwrite a newer owner.
	state.leaseOwner = ""
	state.leaseUntil = time.Time{}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 3*time.Second)
	defer cancel()
	_ = s.save(ctx, state)
}
