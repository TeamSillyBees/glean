package httpdemo

import "github.com/teamsillybees/glean/internal/platform/response"

// HTTPBingoGetVO 是 HTTPBingo GET 示例对外暴露的 JSON DTO。
type HTTPBingoGetVO struct {
	Args    map[string][]string `json:"args"`
	Headers map[string][]string `json:"headers"`
	Method  string              `json:"method"`
	Origin  string              `json:"origin"`
	URL     string              `json:"url"`
}

// HTTPBingoFormPageVO 是 HTTPBingo 表单页示例对外暴露的 JSON DTO。
type HTTPBingoFormPageVO struct {
	ContentType string `json:"contentType"`
	Size        int32  `json:"size"`
	Body        string `json:"body"`
}

type getHTTPBingoRequest struct {
	Message string `query:"message" example:"hello from initra" doc:"传给 HTTPBingo /get 的 message 查询参数"`
}

type getHTTPBingoResponse struct {
	Body response.SuccessVO[HTTPBingoGetVO]
}

type getHTTPBingoFormPageRequest struct{}

type getHTTPBingoFormPageResponse struct {
	Body response.SuccessVO[HTTPBingoFormPageVO]
}

type httpBingoGetPayload struct {
	Args    map[string][]string `json:"args"`
	Headers map[string][]string `json:"headers"`
	Method  string              `json:"method"`
	Origin  string              `json:"origin"`
	URL     string              `json:"url"`
}
