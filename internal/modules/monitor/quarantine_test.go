package monitor

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

type rejectingPublisher struct {
	good     map[string]bool
	badCalls int
}

func (p *rejectingPublisher) Publish(_ context.Context, _ idgen.ID, publications []Publication) error {
	for _, publication := range publications {
		if publication.Record.SourceID == "bad-conflict" {
			p.badCalls++
			return &RejectedRecordError{Kind: "identity_conflict"}
		}
		p.good[publication.Key] = true
	}
	return nil
}

// TestQuarantinePreservesBadRecordsAndProjectsGood 验证永久坏记录不阻塞分页或良好记录，隔离证据不会被保留策略删除。
func TestQuarantinePreservesBadRecordsAndProjectsGood(t *testing.T) {
	records := []Record{}
	for _, name := range []string{"good-one", "bad-conflict", "good-two", "bad-json"} {
		raw := json.RawMessage(`{"fixture":true}`)
		if name == "bad-json" {
			raw = json.RawMessage(`not-json`)
		}
		records = append(records, Record{SourceID: name, Title: name, Topics: []string{"biomed-ai"}, SourceUpdatedAt: scenarioTime.Add(-time.Hour), Raw: raw})
	}
	source := &FixtureSource{SourceName: "crossref", PageSize: 10, Records: records}
	publisher := &rejectingPublisher{good: map[string]bool{}}
	service := offline(t, []Source{source}, MockReviewer{}, publisher)
	service.config.CompactPublished = true
	service.config.PublishedCacheLimit = 1
	snapshot, err := service.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	require.Equal(t, "completed_with_quarantine", snapshot.Runs[0].Status)
	require.Len(t, publisher.good, 2)
	require.Equal(t, 1, publisher.badCalls)
	require.Equal(t, 2, snapshot.Sources["crossref"].Quarantined)
	require.Equal(t, scenarioTime, snapshot.Sources["crossref"].AcquiredThrough)
	require.Equal(t, scenarioTime, snapshot.Sources["crossref"].HandledThrough)
	require.True(t, snapshot.Sources["crossref"].PublishedThrough.IsZero())
	rawKept := false
	for _, candidate := range snapshot.Records {
		if candidate.Record.SourceID == "bad-json" {
			require.Equal(t, []byte("not-json"), candidate.Quarantine.RawEvidence)
			rawKept = true
		}
	}
	require.True(t, rawKept)
	require.Len(t, snapshot.Records, 3, "one published cache receipt and two permanent quarantines")
	second, err := service.RunOnce(context.Background(), scenarioTime.Add(72*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, publisher.badCalls, "overlap must not reprobe an unchanged permanent conflict")
	require.Equal(t, 2, second.Sources["crossref"].Quarantined)
	require.Equal(t, "completed_with_quarantine", second.Runs[1].Status)
}

// TestAdapterRejectedItemNeverReachesReviewer 验证解析器隔离的单条记录保留原文和安全原因，不进入审核或目录。
func TestAdapterRejectedItemNeverReachesReviewer(t *testing.T) {
	source := &FixtureSource{SourceName: "crossref", Records: []Record{{SourceID: "raw-sha256-example", Title: "Invalid dated source item", ValidationError: "invalid_source_record", Topics: []string{"biomed-ai"}, SourceUpdatedAt: scenarioTime.Add(-time.Hour), Raw: json.RawMessage(`{"date":"not-a-date","id":"fixture"}`)}}}
	reviewer := &flakyReviewer{}
	publisher := &rejectingPublisher{good: map[string]bool{}}
	service := offline(t, []Source{source}, reviewer, publisher)
	snapshot, err := service.RunOnce(context.Background(), scenarioTime)
	require.NoError(t, err)
	require.Zero(t, reviewer.calls)
	require.Empty(t, publisher.good)
	require.Equal(t, 1, snapshot.Sources["crossref"].Quarantined)
	for _, candidate := range snapshot.Records {
		require.Equal(t, "invalid_source_record", candidate.Quarantine.Reason)
		require.JSONEq(t, `{"date":"not-a-date","id":"fixture"}`, string(candidate.Record.Raw))
		require.Nil(t, candidate.Review)
		require.False(t, candidate.Published)
	}
}
