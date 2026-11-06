package monitor

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"time"

	"github.com/teamsillybees/glean/internal/platform/httpclient"
)

// checkConnection 使用采集的同一 Executor、官方域名限制和上游退避；不进入 Fetch 或采集审计流程。
func (s *remoteSource) checkConnection(ctx context.Context) (int, error) {
	path, query := sourceConnectionRequest(s.name)
	if path == "" {
		return 0, malformed("unsupported connection probe")
	}
	resp, err := s.executor.Do(ctx, http.MethodGet, path,
		httpclient.WithQueryParams(query),
		httpclient.WithHeader("Accept", "application/json, application/xml"),
		httpclient.WithTimeout(sourceConnectionTimeout))
	status := 0
	if resp != nil {
		status = resp.StatusCode
		if !resp.IsSuccess() {
			return status, &SourceError{Kind: "http", StatusCode: status, RetryAfter: ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
		}
	}
	if err != nil {
		return status, err
	}
	if resp == nil {
		return 0, &SourceError{Kind: "transport"}
	}
	if len(resp.Body) > 16<<20 || !validConnectionResponse(s.name, resp.Body) {
		return status, malformed("invalid connection probe response")
	}
	return status, nil
}

func sourceConnectionRequest(source string) (string, map[string]string) {
	switch source {
	case "crossref":
		return "/works", map[string]string{"rows": "0"}
	case "pubmed":
		return "/entrez/eutils/einfo.fcgi", map[string]string{"db": "pubmed", "retmode": "json"}
	case "arxiv":
		return "/oai", map[string]string{"verb": "Identify"}
	case "biorxiv", "medrxiv":
		// 官方数字区间表示最近 N 篇；只取一篇，不启动日期回溯或翻页。
		return "/details/" + source + "/1", nil
	case "europepmc":
		return "/europepmc/webservices/rest/search", map[string]string{"query": "EXT_ID:1 AND SRC:MED", "format": "json", "resultType": "idlist", "pageSize": "1"}
	}
	return "", nil
}

func validConnectionResponse(source string, body []byte) bool {
	switch source {
	case "crossref":
		var data struct {
			Status  string `json:"status"`
			Message struct {
				Items []json.RawMessage `json:"items"`
			} `json:"message"`
		}
		return json.Unmarshal(body, &data) == nil && data.Status == "ok" && data.Message.Items != nil
	case "pubmed":
		var data struct {
			Result struct {
				Info []struct {
					Name string `json:"dbname"`
				} `json:"dbinfo"`
			} `json:"einforesult"`
		}
		return json.Unmarshal(body, &data) == nil && len(data.Result.Info) == 1 && data.Result.Info[0].Name == "pubmed"
	case "arxiv":
		var data struct {
			XMLName  xml.Name `xml:"OAI-PMH"`
			Identify *struct {
				Repository string `xml:"repositoryName"`
				Protocol   string `xml:"protocolVersion"`
			} `xml:"Identify"`
		}
		return xml.Unmarshal(body, &data) == nil && data.Identify != nil && data.Identify.Repository != "" && data.Identify.Protocol == "2.0"
	case "biorxiv", "medrxiv":
		var data struct {
			Messages []struct {
				Status string `json:"status"`
			} `json:"messages"`
			Collection []json.RawMessage `json:"collection"`
		}
		return json.Unmarshal(body, &data) == nil && len(data.Messages) > 0 && data.Messages[0].Status == "ok" && data.Collection != nil
	case "europepmc":
		var data struct {
			Hits *int `json:"hitCount"`
			List struct {
				Results []json.RawMessage `json:"result"`
			} `json:"resultList"`
		}
		return json.Unmarshal(body, &data) == nil && data.Hits != nil && *data.Hits >= 0 && data.List.Results != nil
	}
	return false
}
