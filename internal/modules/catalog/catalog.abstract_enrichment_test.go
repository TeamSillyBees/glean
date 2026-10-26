package catalog

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	sql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/abstractenrichment"
	"github.com/teamsillybees/glean/internal/data/ent"
)

func verifiedAbstractFixtures(t *testing.T) []abstractenrichment.Record {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "abstractenrichment", "testdata", "verified_records.json"))
	require.NoError(t, err)
	var records []abstractenrichment.Record
	require.NoError(t, json.Unmarshal(data, &records))
	require.Len(t, records, 3)
	for _, record := range records {
		require.NoError(t, abstractenrichment.ValidateRecord(record))
	}
	return records
}

// TestHistoricalAbstractStoredMetadataConsistency 拒绝正文虽正确但存储身份或补录时间不一致的覆盖层。
func TestHistoricalAbstractStoredMetadataConsistency(t *testing.T) {
	r := verifiedAbstractFixtures(t)[0]
	at := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	metadata := historicalAbstractMetadata(r, 7, at)
	stored := ent.GleanObservation{ID: 12, RunID: 7, Source: r.Source, SourceID: r.SourceID, Version: r.SourceVersion, ObservedAt: at}
	require.NotNil(t, selectedStoredHistoricalAbstract(&stored, metadata))
	for name, mutate := range map[string]func(*ent.GleanObservation){
		"source":   func(o *ent.GleanObservation) { o.Source = "different" },
		"sourceId": func(o *ent.GleanObservation) { o.SourceID = "different" },
		"version":  func(o *ent.GleanObservation) { o.Version = "different" },
		"run":      func(o *ent.GleanObservation) { o.RunID++ },
		"time":     func(o *ent.GleanObservation) { o.ObservedAt = at.Add(time.Hour) },
	} {
		t.Run(name, func(t *testing.T) {
			copy := stored
			mutate(&copy)
			require.Nil(t, selectedStoredHistoricalAbstract(&copy, metadata))
		})
	}
}

// TestHistoricalAbstractProjection 只将精确核验的独立摘要投影到详情，不允许假身份或片段提升。
func TestHistoricalAbstractProjection(t *testing.T) {
	for _, record := range verifiedAbstractFixtures(t) {
		t.Run(record.OriginalID, func(t *testing.T) {
			at := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
			metadata := historicalAbstractMetadata(record, 7, at)
			observation := SourceObservationVO{ObservationInput: metadata, ID: 12}
			selected := selectedHistoricalAbstract(observation)
			require.NotNil(t, selected)
			require.Equal(t, record.Abstract, selected.Abstract)
			require.Equal(t, record.SourceURL, selected.SourceURL)
			require.Equal(t, at, selected.EnrichedAt)
			public, err := json.Marshal(observation)
			require.NoError(t, err)
			for _, forbidden := range []string{"/workspace/", "rawSourceCache", "source_cache", "Authorization", "legacy_abstract", "reviewed_abstract_repairs"} {
				require.NotContains(t, string(public), forbidden)
			}
			observation.Abstract += " fabricated"
			require.Nil(t, selectedHistoricalAbstract(observation))
			observation.Abstract = record.Abstract
			observation.AbstractEvidence.Completeness = "fragment"
			require.Nil(t, selectedHistoricalAbstract(observation))
		})
	}
}

// TestHistoricalAbstractRejectsInvalidInputBeforeDatabase 保证无效文本、隔离身份和匿名补录不触碰持久层。
func TestHistoricalAbstractRejectsInvalidInputBeforeDatabase(t *testing.T) {
	service, mock := catalogMock(t)
	r := verifiedAbstractFixtures(t)[0]
	_, err := service.EnrichHistoricalAbstract(context.Background(), r, 1, time.Now())
	require.ErrorContains(t, err, "explicit enrichment audit operator")
	r.Abstract = "method notes are not an abstract"
	_, err = service.EnrichHistoricalAbstract(context.Background(), r, 1, time.Now())
	kind, rejected := ObservationRejectionKind(err)
	require.True(t, rejected)
	require.Equal(t, "invalid_source_record", kind)
	r = verifiedAbstractFixtures(t)[0]
	r.OriginalID = "BAI2E6C229787"
	_, err = service.EnrichHistoricalAbstract(context.Background(), r, 1, time.Now())
	require.Error(t, err)
	input := observationFixture()
	input.Kind = historicalAbstractKind
	_, err = service.IngestObservation(context.Background(), input)
	require.ErrorContains(t, err, "restricted enrichment path")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestHistoricalAbstractFreshnessPredicate 保证缓存补录排除条件兼容旧记录且使用参数绑定。
func TestHistoricalAbstractFreshnessPredicate(t *testing.T) {
	table := sql.Table("glean_observation")
	selector := sql.Dialect(dialect.Postgres).Select(table.C("id")).From(table).Where(liveObservationSQL(table.C("metadata")))
	query, args := selector.Query()
	require.Contains(t, query, "COALESCE(")
	require.Contains(t, query, "->>'kind', '') <> $1")
	require.Equal(t, []any{historicalAbstractKind}, args)
	require.False(t, strings.Contains(query, historicalAbstractKind))
}
