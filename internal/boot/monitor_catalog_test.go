package boot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/modules/catalog"
	"github.com/teamsillybees/glean/internal/modules/monitor"
	"github.com/teamsillybees/glean/internal/platform/entx"
	"github.com/teamsillybees/glean/internal/platform/httpclient"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

type observationWriterRecorder struct {
	inputs   []catalog.ObservationInput
	operator idgen.ID
	err      error
}

func (w *observationWriterRecorder) IngestObservation(ctx context.Context, input catalog.ObservationInput) (catalog.ObservationResult, error) {
	w.operator, _ = entx.OperatorIDFromContext(ctx)
	w.inputs = append(w.inputs, input)
	return catalog.ObservationResult{PaperID: 1, ObservationID: 2, Created: true}, w.err
}

// TestCatalogMonitorAdapterPreservesAuthenticity 验证真实论文和模拟审核分别传递，审计身份不是匿名默认值。
func TestCatalogMonitorAdapterPreservesAuthenticity(t *testing.T) {
	writer := &observationWriterRecorder{}
	publisher := &catalogMonitorPublisher{catalog: writer, operator: 7}
	now := time.Now().UTC()
	publication := monitor.Publication{Record: monitor.Record{Source: "crossref", TemplateVersion: "snapshot-frozen-version", SourceID: "10.1234/a", CoreDiscovery: true, PMID: "12345678", SourceURL: "https://doi.org/10.1234/a", Title: "Real paper", ArxivID: "2401.12345", Authors: []string{"A"}, PublicationType: "journal-article", Topics: []string{"biomed-ai"}, AcquiredAt: now, Raw: json.RawMessage(`{"title":"Real paper"}`)}, Review: monitor.Review{Provider: "mock", IsMock: true, ScientificReviewed: false, InclusionStatus: "pending", Pipeline: json.RawMessage(`{"status":"awaiting_configuration"}`)}}
	require.NoError(t, publisher.Publish(context.Background(), 100, []monitor.Publication{publication}))
	require.Len(t, writer.inputs, 1)
	require.Equal(t, "snapshot-frozen-version", writer.inputs[0].TemplateVersion)
	require.Equal(t, idgen.ID(7), writer.operator)
	require.Equal(t, "2401.12345", writer.inputs[0].ArxivID)
	require.True(t, writer.inputs[0].CoreDiscovery)
	require.Equal(t, "12345678", writer.inputs[0].PMID)
	require.Equal(t, "https://doi.org/10.1234/a", writer.inputs[0].SourceURL)
	require.JSONEq(t, string(publication.Review.Pipeline), string(writer.inputs[0].Review.Pipeline))
	require.JSONEq(t, string(publication.Review.Pipeline), string(writer.inputs[0].Pipeline))
	require.True(t, writer.inputs[0].Review.IsMock)
	require.False(t, writer.inputs[0].Review.ScientificReviewed)
	publication.Record.IsMock = true
	var rejected *monitor.RejectedRecordError
	require.ErrorAs(t, publisher.Publish(context.Background(), 101, []monitor.Publication{publication}), &rejected)
	require.Equal(t, "mock_source_record", rejected.Kind)
	require.Len(t, writer.inputs, 1)
	publisher.operator = 0
	require.ErrorContains(t, publisher.Publish(context.Background(), 102, nil), "explicit active operator")
}

// TestCatalogPublisherRejectsOnlyTypedPermanentErrors 验证单条隔离不能误吞数据库中断或无效操作员。
func TestCatalogPublisherRejectsOnlyTypedPermanentErrors(t *testing.T) {
	_, permanent := catalog.NewService(nil).IngestObservation(context.Background(), catalog.ObservationInput{})
	writer := &observationWriterRecorder{err: permanent}
	publisher := &catalogMonitorPublisher{catalog: writer, operator: 7}
	publications := []monitor.Publication{{Record: monitor.Record{Source: "crossref"}}}
	var rejected *monitor.RejectedRecordError
	require.ErrorAs(t, publisher.Publish(context.Background(), 1, publications), &rejected)
	require.Equal(t, "invalid_source_record", rejected.Kind)
	transient := errors.New("database unavailable")
	writer.err = transient
	require.ErrorIs(t, publisher.Publish(context.Background(), 1, publications), transient)
	require.ErrorContains(t, NewCatalogMonitorPublisher(nil, 7).Publish(context.Background(), 1, publications), "explicit active operator")
}

// TestOfficialSourceOriginGuard 验证真实来源只使用官方 HTTPS，重定向也无法绕过白名单。
func TestOfficialSourceOriginGuard(t *testing.T) {
	config, hooks := monitorHTTPConfig(&Config{})
	config.Enabled = true
	require.Len(t, hooks, 7)
	factory, err := httpclient.NewFactory(config, nil, hooks...)
	require.NoError(t, err)
	client, err := factory.Get("glean-crossref")
	require.NoError(t, err)
	// 测试实际运行时工厂使用的 hook；所有请求在网络传输前被拒绝。
	for _, target := range []string{"http://api.crossref.org/works", "https://127.0.0.1/", "http://169.254.169.254/latest/", "https://api.crossref.org.evil.example/", "https://user:secret@api.crossref.org/works", "https://www.ebi.ac.uk/"} {
		_, err := client.Do(context.Background(), http.MethodGet, target)
		require.Error(t, err)
	}
	_, err = client.Do(context.Background(), http.MethodPost, "/works")
	require.ErrorContains(t, err, "official HTTPS origin")
	for _, service := range config.Services {
		require.False(t, service.Retry.Enabled)
		require.NotContains(t, service.Headers["User-Agent"], "contact:")
	}
}

// archiveObservationRecorder 测试归档读写往返，不触发模型或外部网络。
type archiveObservationRecorder struct {
	observationWriterRecorder
	input                 catalog.ObservationInput
	loaded                idgen.ID
	source, sourceID, key string
}

func (w *archiveObservationRecorder) LoadPipelineObservation(_ context.Context, id idgen.ID) (catalog.ObservationInput, error) {
	w.loaded = id
	return w.input, nil
}
func (w *archiveObservationRecorder) FindPipelineObservation(_ context.Context, source, sourceID, key string) (catalog.ObservationInput, bool, error) {
	w.source, w.sourceID, w.key = source, sourceID, key
	return w.input, true, nil
}

// TestCatalogPipelineArchiveRoundTrip 验证归档还原完整Record及包含模板/历史的Pipeline，不读取列表瘦身结果。
func TestCatalogPipelineArchiveRoundTrip(t *testing.T) {
	writer := &archiveObservationRecorder{}
	publisher := &catalogMonitorPublisher{catalog: writer, operator: 7}
	now := time.Now().UTC()
	original := monitor.Publication{
		Record: monitor.Record{Source: "crossref", SourceID: "10.1234/archive", SourceURL: "https://doi.org/10.1234/archive", DOI: "10.1234/archive", Title: "Original", Abstract: "Complete abstract", Raw: json.RawMessage(`{"original":"source payload"}`), Authors: []string{"A"}, ISSNs: []string{"0028-0836"}, Topics: []string{"biomed-ai"}, Journal: "Source journal", URL: "https://example.org/paper", PMID: "123", ArxivID: "2401.12345", PublicationType: "journal-article", Version: "v2", TemplateVersion: "v3", CoreDiscovery: true, AcquiredAt: now, SourceUpdatedAt: now, PublicationEvents: []monitor.PublicationEvent{{Kind: "online", Date: now, Evidence: "source"}}},
		Review: monitor.Review{Pipeline: json.RawMessage(`{"candidateId":"321","key":"record-key","runId":"9","templates":[{"topic":"biomed-ai","version":"v3"}],"history":[{"status":"completed"}]}`), Summary: "model summary"},
	}
	require.NoError(t, publisher.Publish(context.Background(), 9, []monitor.Publication{original}))
	writer.input = writer.inputs[0]
	loaded, err := publisher.LoadPipeline(context.Background(), 321)
	require.NoError(t, err)
	require.Equal(t, idgen.ID(321), writer.loaded)
	require.Equal(t, original, loaded)
	found, ok, err := publisher.FindPipeline(context.Background(), original.Record, "record-key")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, original, found)
	require.Equal(t, original.Record.Source, writer.source)
	require.Equal(t, original.Record.SourceID, writer.sourceID)
	require.Equal(t, "record-key", writer.key)
}
