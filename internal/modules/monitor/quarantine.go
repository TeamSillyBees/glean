package monitor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// quarantineRecord 保留单条坏记录，不让它阻塞同页有效记录或推进“已发布”水位。
func (s *Service) quarantineRecord(state *monitorState, progress *SourceProgress, checkpoint *Checkpoint, record Record, reason string, at time.Time) {
	raw := append([]byte(nil), record.Raw...)
	sum := sha256.Sum256([]byte(record.Source + "\x00" + record.SourceID + "\x00" + record.Title + "\x00" + string(raw)))
	key := "quarantine:" + hex.EncodeToString(sum[:])
	if _, exists := state.Records[key]; !exists {
		q := &Quarantine{Reason: reason, At: at}
		if len(raw) > 0 && !json.Valid(raw) {
			q.RawEvidence = raw
			record.Raw = nil
		}
		state.Records[key] = &Candidate{ID: s.ids.NextID(), Key: key, Identity: record.Source + ":" + record.SourceID, Record: record, Quarantine: q}
		checkpoint.Quarantined++
	}
	before := len(progress.CandidateKeys)
	progress.CandidateKeys = appendUnique(progress.CandidateKeys, key)
	if len(progress.CandidateKeys) > before {
		progress.Quarantined++
	}
}

func safeRejectionKind(kind string) string {
	switch kind {
	case "identity_conflict", "invalid_source_record", "invalid_record", "missing_identity", "unsupported_record_type", "mock_source_record":
		return kind
	}
	return "record_rejected"
}
