package monitor

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/httpclient"
)

// TestArxivMixedPageQuarantinesInvalidItem 验证坏条目不丢失相邻论文或分页令牌；合成数据不是实际来源证据。
func TestArxivMixedPageQuarantinesInvalidItem(t *testing.T) {
	fixture := string(adapterFixture(t, "arxiv-oai-synthetic.xml").Body)
	start, end := strings.Index(fixture, "<record>"), strings.Index(fixture, "</record>")+len("</record>")
	good := fixture[start:end]
	badBase := strings.ReplaceAll(good, "2608.00001", "2608.00002")
	cases := map[string]string{
		"datestamp":            strings.ReplaceAll(badBase, "2026-10-23</datestamp>", "not-a-date</datestamp>"),
		"missing_identity":     strings.ReplaceAll(badBase, "<id>2608.00002</id>", "<id></id>"),
		"conflicting_identity": strings.ReplaceAll(badBase, "<id>2608.00002</id>", "<id>2608.99999</id>"),
		"missing_title":        strings.ReplaceAll(badBase, "<title>Clinical language agents</title>", "<title> </title>"),
		"missing_history":      strings.ReplaceAll(strings.ReplaceAll(badBase, `<version version="v1"><date>Mon, 03 Aug 2026 10:00:00 GMT</date></version>`, ""), `<version version="v2"><date>Fri, 23 Oct 2026 10:00:00 GMT</date></version>`, ""),
		"version_number":       strings.ReplaceAll(badBase, `version="v2"`, `version="invalid"`),
		"version_date":         strings.ReplaceAll(badBase, "Fri, 23 Oct 2026 10:00:00 GMT", "invalid"),
	}
	envelope := func(items, token string) string {
		return `<OAI-PMH xmlns="http://www.openarchives.org/OAI/2.0/"><ListRecords>` + items + `<resumptionToken>` + token + `</resumptionToken></ListRecords></OAI-PMH>`
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			f := &adapterExecutor{responses: []*httpclient.Response{
				{StatusCode: 200, Body: []byte(envelope(good+bad+strings.ReplaceAll(good, "2608.00001", "2608.00003"), "next-token"))},
				{StatusCode: 200, Body: []byte(envelope(strings.ReplaceAll(good, "2608.00001", "2608.00004"), ""))},
			}}
			source := NewArXivSource(f)
			req := adapterWindow()
			req.Window.Start = time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
			req.Window.End = req.Window.Start.Add(24 * time.Hour)
			page, err := source.Fetch(context.Background(), req)
			require.NoError(t, err)
			require.False(t, page.Complete)
			require.Len(t, page.Records, 3)
			require.Equal(t, "2608.00001", page.Records[0].SourceID)
			require.Equal(t, "2608.00003", page.Records[2].SourceID)
			require.Empty(t, page.Records[0].ValidationError)
			require.Empty(t, page.Records[2].ValidationError)
			rejected := page.Records[1]
			require.Equal(t, "invalid_source_record", rejected.ValidationError)
			var raw map[string]string
			require.NoError(t, json.Unmarshal(rejected.Raw, &raw))
			require.Equal(t, "xml", raw["format"])
			require.Equal(t, strings.TrimSuffix(strings.TrimPrefix(bad, "<record>"), "</record>"), raw["value"])
			var item arxivOAIRecord
			require.NoError(t, xml.Unmarshal([]byte(bad), &item))
			require.Equal(t, rejected, rejectedArxivRecord(item), "replay must produce stable quarantine evidence")
			if name == "missing_identity" || name == "conflicting_identity" {
				require.True(t, strings.HasPrefix(rejected.SourceID, "unparsed-sha256:"))
				require.Empty(t, rejected.SourceURL)
			} else {
				require.Equal(t, "2608.00002", rejected.SourceID)
				require.Equal(t, sourceRecordURL("arxiv", "2608.00002"), rejected.SourceURL)
			}
			cursor, err := decodeArxivCursor(page.NextCursor)
			require.NoError(t, err)
			require.Equal(t, "next-token", cursor.Token)
			req.Cursor = page.NextCursor
			next, err := source.Fetch(context.Background(), req)
			require.NoError(t, err)
			require.True(t, next.Complete)
			require.Empty(t, next.NextCursor)
			require.Len(t, next.Records, 1)
			require.Equal(t, "2608.00004", next.Records[0].SourceID)
			require.Equal(t, "next-token", f.requests[1].options.QueryParams.Get("resumptionToken"))
		})
	}
}

// TestArxivBrokenEnvelopeNeverAdvances 保持页级损坏失败关闭，不把截断 XML 当作逐条隔离成功。
func TestArxivBrokenEnvelopeNeverAdvances(t *testing.T) {
	for _, body := range []string{
		`<OAI-PMH><ListRecords><record><header></record></ListRecords></OAI-PMH>`,
		`<OAI-PMH><ListRecords><record><header><datestamp>2026-10-23</datestamp></header>`,
		`<OAI-PMH><ListRecords><resumptionToken>next</resumptionToken></ListRecords></OAI-PMH>`,
	} {
		page, err := NewArXivSource(&adapterExecutor{responses: []*httpclient.Response{{StatusCode: 200, Body: []byte(body)}}}).Fetch(context.Background(), adapterWindow())
		require.Error(t, err)
		require.Empty(t, page.Records)
		require.Empty(t, page.NextCursor)
		require.False(t, page.Complete)
	}
}

// TestArxivAllRejectedPageStillAdvances 验证整页均坏时仍保留隔离项并进入下一页，而非静默空页成功。
func TestArxivAllRejectedPageStillAdvances(t *testing.T) {
	body := `<OAI-PMH><ListRecords><record><header><datestamp>invalid</datestamp></header></record><resumptionToken>next</resumptionToken></ListRecords></OAI-PMH>`
	source := NewArXivSource(&adapterExecutor{responses: []*httpclient.Response{{StatusCode: 200, Body: []byte(body)}}})
	page, err := source.Fetch(context.Background(), adapterWindow())
	require.NoError(t, err)
	require.False(t, page.Complete)
	require.NotEmpty(t, page.NextCursor)
	require.Len(t, page.Records, 1)
	require.Equal(t, "invalid_source_record", page.Records[0].ValidationError)
}
