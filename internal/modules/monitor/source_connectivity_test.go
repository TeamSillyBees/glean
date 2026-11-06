package monitor

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/teamsillybees/glean/internal/platform/httpclient"
)

// TestConnectionProbesUseBoundedOfficialOperations 验证各来源只请求最小 API，不复用可能翻页或写状态的 Fetch。
func TestConnectionProbesUseBoundedOfficialOperations(t *testing.T) {
	for _, tt := range []struct {
		source, path, body string
		query              map[string]string
	}{
		{"crossref", "/works", `{"status":"ok","message":{"items":[]}}`, map[string]string{"rows": "0"}},
		{"pubmed", "/entrez/eutils/einfo.fcgi", `{"einforesult":{"dbinfo":[{"dbname":"pubmed"}]}}`, map[string]string{"db": "pubmed", "retmode": "json"}},
		{"arxiv", "/oai", `<OAI-PMH><Identify><repositoryName>arXiv</repositoryName><protocolVersion>2.0</protocolVersion></Identify></OAI-PMH>`, map[string]string{"verb": "Identify"}},
		{"biorxiv", "/details/biorxiv/1", `{"messages":[{"status":"ok"}],"collection":[{}]}`, nil},
		{"medrxiv", "/details/medrxiv/1", `{"messages":[{"status":"ok"}],"collection":[{}]}`, nil},
		{"europepmc", "/europepmc/webservices/rest/search", `{"hitCount":0,"resultList":{"result":[]}}`, map[string]string{"query": "EXT_ID:1 AND SRC:MED", "format": "json", "resultType": "idlist", "pageSize": "1"}},
	} {
		t.Run(tt.source, func(t *testing.T) {
			executor := &adapterExecutor{responses: []*httpclient.Response{{StatusCode: 200, Body: []byte(tt.body)}}}
			source := &remoteSource{name: tt.source, executor: executor}
			status, err := source.checkConnection(t.Context())
			require.NoError(t, err)
			require.Equal(t, 200, status)
			require.Len(t, executor.requests, 1)
			require.Equal(t, tt.path, executor.requests[0].path)
			options := executor.requests[0].options
			require.Equal(t, sourceConnectionTimeout, options.Timeout)
			require.Len(t, options.QueryParams, len(tt.query))
			for key, value := range tt.query {
				require.Equal(t, value, options.QueryParams.Get(key))
			}
			// HTTP 200 的网关网页或供应商错误 envelope 不能被标为连通成功。
			for _, body := range []string{`<html>Access denied</html>`, `{}`, `null`, `{"messages":[{"status":"error"}],"collection":[]}`} {
				executor.responses = []*httpclient.Response{{StatusCode: 200, Body: []byte(body)}}
				_, err = source.checkConnection(t.Context())
				var sourceErr *SourceError
				require.ErrorAs(t, err, &sourceErr)
				require.Equal(t, "invalid_response", sourceErr.Kind)
			}
		})
	}
}

// TestConnectionProbeRespectsSharedBackoff 验证检测不能绕过采集共用的 Retry-After 或禁止访问状态。
func TestConnectionProbeRespectsSharedBackoff(t *testing.T) {
	for _, status := range []int{403, 429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			header := http.Header{}
			if status != 403 {
				header.Set("Retry-After", "60")
			}
			base := &adapterExecutor{responses: []*httpclient.Response{{StatusCode: status, Header: header, Body: []byte("private-upstream-body")}}}
			source := &remoteSource{name: "crossref", executor: &pacedExecutor{base: base, gate: &requestGate{}}}
			code, err := source.checkConnection(t.Context())
			require.Equal(t, status, code)
			require.Error(t, err)
			result := sourceConnectionResult(source.name, code, err)
			require.NotEqual(t, "connected", result.Status)
			require.NotContains(t, result.Message, "private-upstream-body")
			code, err = source.checkConnection(t.Context())
			require.Zero(t, code, "被共享退避拦住的测试不得伪造本次 HTTP 响应")
			result = sourceConnectionResult(source.name, code, err)
			require.Contains(t, result.Message, "本次未发出请求")
			require.Len(t, base.requests, 1)
		})
	}
}

func TestConnectionProbePreservesTransportErrorsWithoutReturningPrivateDetails(t *testing.T) {
	for _, err := range []error{context.DeadlineExceeded, errors.New("https://user:secret@private.example/path?api_key=private")} {
		source := &remoteSource{name: "crossref", executor: &adapterExecutor{err: err}}
		code, got := source.checkConnection(t.Context())
		require.ErrorIs(t, got, err)
		result := sourceConnectionResult(source.name, code, got)
		require.NotEqual(t, "connected", result.Status)
		require.NotContains(t, result.Message, "secret")
		require.NotContains(t, result.Message, "private")
	}
}
