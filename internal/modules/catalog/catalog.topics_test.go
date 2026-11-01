package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

// TestTopicsGroupedQueries 固定五次查询和成员 COUNT(*)，不按主题逐个计数或去重论文。
func TestTopicsGroupedQueries(t *testing.T) {
	service, mock := catalogMock(t)
	mock.ExpectQuery(`SELECT .* FROM "glean_topic" .*ORDER BY`).WillReturnRows(sqlmock.NewRows([]string{"slug", "name"}).AddRow("custom", "Custom"))
	patterns := []string{
		`SELECT "glean_membership"\."topic", COUNT\(\*\) FROM "glean_membership" GROUP BY "glean_membership"\."topic"`,
		`SELECT "glean_membership"\."topic", COUNT\(\*\) FROM "glean_membership" WHERE .*"inclusion_status" = \$1 GROUP BY`,
		`SELECT "glean_membership"\."topic", COUNT\(\*\) FROM "glean_membership" WHERE .*"manual_review".* GROUP BY`,
		`SELECT "glean_membership"\."topic", COUNT\(\*\) FROM "glean_membership" WHERE .*"manual_review".*"dimensions".* GROUP BY`,
	}
	for i, pattern := range patterns {
		mock.ExpectQuery(pattern).WillReturnRows(sqlmock.NewRows([]string{"topic", "count"}).AddRow("biomed-ai", i+1).AddRow("custom", i+5))
	}
	got, err := service.Topics(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 4)
	require.Equal(t, [4]int{1, 2, 3, 4}, [4]int{got[0].MembershipCount, got[0].HistoricalCount, got[0].CandidateCount, got[0].CuratedCount})
	require.Zero(t, got[1].MembershipCount)
	require.Equal(t, [4]int{5, 6, 7, 8}, [4]int{got[3].MembershipCount, got[3].HistoricalCount, got[3].CandidateCount, got[3].CuratedCount})
}

// TestTopicsQueryErrors 不把配置或任一聚合失败误报为零计数成功。
func TestTopicsQueryErrors(t *testing.T) {
	for failAt := 0; failAt < 5; failAt++ {
		t.Run(string(rune('0'+failAt)), func(t *testing.T) {
			service, mock := catalogMock(t)
			for i := 0; i <= failAt; i++ {
				pattern := `SELECT .* FROM "glean_membership" .*GROUP BY`
				columns := []string{"topic", "count"}
				if i == 0 {
					pattern = `SELECT .* FROM "glean_topic"`
					columns = []string{"slug"}
				}
				expectation := mock.ExpectQuery(pattern)
				if i == failAt {
					expectation.WillReturnError(errors.New("database unavailable"))
				} else {
					expectation.WillReturnRows(sqlmock.NewRows(columns))
				}
			}
			got, err := service.Topics(context.Background())
			require.Error(t, err)
			require.Nil(t, got)
		})
	}
}
