package monitor

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teamsillybees/glean/internal/data"
	"github.com/teamsillybees/glean/internal/data/ent"
	"github.com/teamsillybees/glean/internal/data/ent/gleanmonitorstate"
	"github.com/teamsillybees/glean/internal/data/ent/gleansearchrun"
	"github.com/teamsillybees/glean/internal/modules/bizerrors"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// saveAuditedState 将 CAS 水位和变更的长期运行账本放在同一事务，过期 worker 不能改写归档。
func (s *Service) saveAuditedState(ctx context.Context, state *monitorState) error {
	hashes := map[string]string{}
	changed := map[idgen.ID]json.RawMessage{}
	for _, run := range state.Runs {
		h := hashJSON(run)
		hashes[run.ID.String()] = h
		if state.archiveHashes[run.ID.String()] != h {
			b, e := json.Marshal(run)
			if e != nil {
				return e
			}
			changed[run.ID] = b
		}
	}
	raw, err := json.Marshal(persistedState{View: state.Snapshot, LeaseOwner: state.leaseOwner, LeaseUntil: state.leaseUntil, ArchiveHashes: hashes})
	if err != nil {
		return err
	}
	nextID := state.id
	nextRevision := state.revision + 1
	err = data.WithinTx(ctx, s.client, func(ctx context.Context, c *ent.Client) error {
		if state.id == 0 {
			row, e := c.GleanMonitorState.Create().SetID(s.ids.NextID()).SetKey(s.config.StateKey).SetSnapshot(raw).SetRevision(1).Save(ctx)
			if e != nil {
				return e
			}
			nextID = row.ID
			nextRevision = 1
		} else {
			n, e := c.GleanMonitorState.Update().Where(gleanmonitorstate.ID(state.id), gleanmonitorstate.Revision(state.revision)).SetSnapshot(raw).AddRevision(1).Save(ctx)
			if e != nil {
				return e
			}
			if n != 1 {
				return bizerrors.BadRequest("monitor state changed; lease lost")
			}
		}
		for id, payload := range changed {
			row, e := c.GleanSearchRun.Query().Where(gleansearchrun.ID(id)).ForUpdate().Only(ctx)
			if ent.IsNotFound(e) {
				e = c.GleanSearchRun.Create().SetID(id).SetStateKey(s.config.StateKey).SetPayload(payload).Exec(ctx)
			} else if e == nil {
				if row.StateKey != s.config.StateKey {
					return fmt.Errorf("run belongs to a different monitor state")
				}
				e = c.GleanSearchRun.UpdateOneID(id).SetPayload(payload).Exec(ctx)
			}
			if e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	state.id = nextID
	state.revision = nextRevision
	state.archiveHashes = hashes
	return nil
}
