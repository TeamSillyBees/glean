package monitor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/httpclient"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// TestPreprintDriftRetainsAcquisitionGap 验证真实适配路径在总数持续变化时保留原窗口、游标和采集缺口。
func TestPreprintDriftRetainsAcquisitionGap(t *testing.T) {
	for _, factory := range []func(httpclient.Executor) Source{NewBioRxivSource, NewMedRxivSource} {
		source := factory(&adapterExecutor{})
		t.Run(source.Name(), func(t *testing.T) {
			ctx := context.Background()
			responses := []*httpclient.Response{{StatusCode: 200, Body: []byte(`{"messages":[{"status":"no posts found"}],"collection":[]}`)}}
			// A,B are read at offset 0; removing A shifts C before offset 2, which now returns D.
			// Repeat this unstable traversal until the bounded replay budget is exhausted.
			for attempt := 0; attempt < 4; attempt++ {
				responses = append(responses,
					&httpclient.Response{StatusCode: 200, Body: []byte(`{"messages":[{"status":"ok","cursor":0,"total":4,"count":2}],"collection":[{"doi":"10.1234/a","title":"clinical A","version":"1"},{"doi":"10.1234/b","title":"clinical B","version":"1"}]}`)},
					&httpclient.Response{StatusCode: 200, Body: []byte(`{"messages":[{"status":"ok","cursor":2,"total":3,"count":1}],"collection":[{"doi":"10.1234/d","title":"clinical D","version":"1"}]}`)},
				)
			}
			executor := &adapterExecutor{responses: responses}
			source = factory(executor)
			ids, err := idgen.NewGenerator(8)
			require.NoError(t, err)
			cfg := DefaultConfig()
			cfg.RealFetchEnabled = true
			service, err := NewOfflineService(ids, cfg, []Source{source}, nil, &flakyPublisher{})
			require.NoError(t, err)
			service.templates = &mutableTemplates{items: []SearchTemplate{TopicSearchTemplate("drift", [][]string{{"clinical"}}, 1)}}
			now := scenarioTime
			service.clock = func() time.Time { return now }
			baseline, err := service.RunOnce(ctx, now)
			require.NoError(t, err)
			require.Equal(t, scenarioTime, baseline.Sources[source.Name()].AcquiredThrough)

			fixedEnd := scenarioTime.Add(72 * time.Hour)
			var final Snapshot
			var retainedCursor string
			var originalWindow Window
			for page := 0; page < 8; page++ {
				now = fixedEnd.Add(time.Duration(page) * time.Minute)
				final, err = service.RunOnce(ctx, now)
				require.NoError(t, err)
				require.Len(t, final.Runs, 2)
				progress := final.Runs[1].Sources[source.Name()]
				require.Equal(t, fixedEnd, final.Runs[1].FixedEnd)
				require.False(t, progress.Acquired)
				require.Equal(t, 0, progress.TemplateIndex)
				require.Equal(t, scenarioTime, final.Sources[source.Name()].AcquiredThrough)
				require.Equal(t, scenarioTime, final.Sources[source.Name()].TopicAcquiredThrough["drift"])
				if page == 0 {
					originalWindow = progress.Window
				}
				require.Equal(t, originalWindow, progress.Window)
				if page == 6 {
					retainedCursor = progress.Cursor
				}
			}
			progress := final.Runs[1].Sources[source.Name()]
			require.True(t, progress.Failed)
			require.Equal(t, "invalid_response", progress.LastError)
			require.Equal(t, 1, progress.Attempts)
			require.NotEmpty(t, retainedCursor)
			require.Equal(t, retainedCursor, progress.Cursor, "failed continuation must retain its retry context")
			require.True(t, strings.HasPrefix(retainedCursor, "rx1:"))
			raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(retainedCursor, "rx1:"))
			require.NoError(t, err)
			var cursor struct{ Offset, Total, Restarts int }
			require.NoError(t, json.Unmarshal(raw, &cursor))
			require.Equal(t, 2, cursor.Offset)
			require.Equal(t, 4, cursor.Total)
			require.Equal(t, 3, cursor.Restarts)
			require.Len(t, executor.requests, 9)
			for index, request := range executor.requests[1:] {
				require.True(t, strings.HasSuffix(request.path, fmt.Sprintf("/%d/json", 2*(index%2))), request.path)
			}
			// Saved state, not merely the returned snapshot, must retain the same incomplete window.
			state, err := service.load(ctx)
			require.NoError(t, err)
			require.Equal(t, retainedCursor, state.Runs[1].Sources[source.Name()].Cursor)
			require.Equal(t, scenarioTime, state.Sources[source.Name()].AcquiredThrough)
		})
	}
}
