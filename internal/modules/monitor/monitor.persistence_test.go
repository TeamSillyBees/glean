package monitor

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/data"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

type snapshotArgument struct {
	expectedRecords int
	expectedLease   string
}

// Match 检查 Ent 实际发送的 JSON，避免仅验证内存假实现。
func (a snapshotArgument) Match(value driver.Value) bool {
	var raw []byte
	switch v := value.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return false
	}
	var persisted persistedState
	if json.Unmarshal(raw, &persisted) != nil {
		return false
	}
	return len(persisted.View.Records) == a.expectedRecords && persisted.LeaseOwner == a.expectedLease && persisted.View.FormatVersion == 1
}

func persistenceService(t *testing.T) (*Service, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ids, err := idgen.NewGenerator(9)
	require.NoError(t, err)
	client := data.NewEntClientFromDB(db, ids)
	service, err := NewService(client, ids, DefaultConfig(), DemoSources(scenarioTime, false)[:1], nil, nil)
	require.NoError(t, err)
	return service, mock
}

// TestEntReadRestoresLeaseAndCheckpoints 验证进程重建后读取持久化水位、游标与租约，不靠内存状态。
func TestEntReadRestoresLeaseAndCheckpoints(t *testing.T) {
	service, mock := persistenceService(t)
	state := service.emptyState()
	state.leaseOwner = "worker-123"
	state.leaseUntil = scenarioTime
	state.Sources["crossref"].AcquiredThrough = scenarioTime
	state.Runs = []Run{{ID: 99, Status: "retry_wait", FixedEnd: scenarioTime, Sources: map[string]*SourceProgress{"crossref": {Cursor: "opaque-cursor"}}}}
	raw, err := json.Marshal(persistedState{View: state.Snapshot, LeaseOwner: state.leaseOwner, LeaseUntil: state.leaseUntil})
	require.NoError(t, err)
	mock.ExpectQuery(`SELECT .* FROM "glean_monitor_state" WHERE "glean_monitor_state"\."key" = \$1 LIMIT 2`).WithArgs(service.config.StateKey).WillReturnRows(sqlmock.NewRows([]string{"id", "key", "snapshot", "revision", "created_at", "updated_at", "created_by", "updated_by"}).AddRow(int64(15), service.config.StateKey, raw, 4, scenarioTime, scenarioTime, nil, nil))
	restored, err := service.load(context.Background())
	require.NoError(t, err)
	require.Equal(t, idgen.ID(15), restored.id)
	require.Equal(t, 4, restored.revision)
	require.Equal(t, "worker-123", restored.leaseOwner)
	require.Equal(t, scenarioTime, restored.Sources["crossref"].AcquiredThrough)
	require.Equal(t, "opaque-cursor", restored.Runs[0].Sources["crossref"].Cursor)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestEntStateCASFencesConcurrentWriter 验证更新 WHERE 同时带 ID 和 revision，旧进程不能覆盖新进度。
func TestEntStateCASFencesConcurrentWriter(t *testing.T) {
	service, mock := persistenceService(t)
	state := service.emptyState()
	state.id = 15
	state.revision = 4
	mock.ExpectExec(`UPDATE "glean_monitor_state" SET "snapshot" = \$1, "updated_at" = \$2, "revision" = COALESCE\("glean_monitor_state"\."revision", 0\) \+ \$3 WHERE "glean_monitor_state"\."id" = \$4 AND "glean_monitor_state"\."revision" = \$5`).WithArgs(snapshotArgument{expectedRecords: 0}, sqlmock.AnyArg(), 1, int64(15), 4).WillReturnResult(sqlmock.NewResult(0, 0))
	err := service.save(context.Background(), state)
	require.ErrorContains(t, err, "lease lost")
	require.Equal(t, 4, state.revision)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestEntStateSuccessfulCASIncrementsRevision 验证成功持久化后才推进本地 fencing 修订号。
func TestEntStateSuccessfulCASIncrementsRevision(t *testing.T) {
	service, mock := persistenceService(t)
	state := service.emptyState()
	state.id = 15
	state.revision = 4
	mock.ExpectExec(`UPDATE "glean_monitor_state" SET .* WHERE "glean_monitor_state"\."id" = \$4 AND "glean_monitor_state"\."revision" = \$5`).WithArgs(snapshotArgument{expectedRecords: 0}, sqlmock.AnyArg(), 1, int64(15), 4).WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, service.save(context.Background(), state))
	require.Equal(t, 5, state.revision)
	require.NoError(t, mock.ExpectationsWereMet())
}
