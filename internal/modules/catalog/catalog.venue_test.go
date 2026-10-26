package catalog

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"testing"
)

// TestCoreDiscoveryDoesNotInventQualification 验证未知年份也可凭历史收录发现，但不借用旧刊名指标或把会议当期刊。
func TestCoreDiscoveryDoesNotInventQualification(t *testing.T) {
	service, mock := catalogMock(t)
	rows := sqlmock.NewRows([]string{"id", "name", "kind", "jif", "metric_year", "source", "evidence", "issns", "qualification_status", "registry_version"}).
		AddRow(1, "Medical Image Analysis", "journal", 14.0, 0, "historical source", `{"discoveryEnabled":true,"discoveryTopics":["biomed-ai"],"metricVerified":false}`, `["1361-8415","1361-8423"]`, "historical-curated-discovery", "v1").
		AddRow(2, "Old verified title", "journal", 8.0, 2025, "historical metric", `{}`, `["0028-0836"]`, "historical-verified", "v2").
		AddRow(3, "Renamed title", "journal", nil, 0, "historical curated source", `{"discoveryEnabled":true,"discoveryTopics":["llm-agent"]}`, `["0028-0836"]`, "historical-curated-discovery", "v3").
		AddRow(4, "Conference proceedings fixture", "conference", 99.0, 2025, "original conference evidence", `{"discoveryEnabled":true,"discoveryTopics":["biomed-ai"]}`, `["0018-9219"]`, "historical-verified", "v4")
	mock.ExpectQuery(`SELECT .* FROM "glean_venue" .*`).WithArgs(0, sqlmock.AnyArg()).WillReturnRows(rows)
	actual, err := service.CoreJournals(context.Background())
	require.NoError(t, err)
	require.Len(t, actual, 3)
	require.Equal(t, []string{"biomed-ai"}, actual[0].DiscoveryTopics)
	require.Zero(t, actual[0].MetricYear)
	require.False(t, actual[0].QualificationVerified)
	require.Empty(t, actual[0].EligibleTopics)
	require.True(t, actual[1].QualificationVerified)
	require.NotContains(t, actual[1].DiscoveryTopics, "biomed-ai")
	require.False(t, actual[2].QualificationVerified)
	require.Nil(t, actual[2].JIF, "same ISSN does not transfer an old title's JIF")
}
