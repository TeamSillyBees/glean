package httpclient

import (
	"context"
	"io"
	"net/http"

	"github.com/go-resty/resty/v2"
)

// Response 描述远程 HTTP 调用的统一响应。
type Response struct {
	StatusCode  int
	Header      http.Header
	Body        []byte
	Result      any
	ErrorResult any
}

// String 返回响应体字符串。
func (r *Response) String() string {
	if r == nil {
		return ""
	}
	return string(r.Body)
}

// IsSuccess 判断响应状态是否为 2xx。
func (r *Response) IsSuccess() bool {
	return r != nil && r.StatusCode >= http.StatusOK && r.StatusCode < http.StatusMultipleChoices
}

// StreamResponse 描述需要调用方关闭的流式响应。
type StreamResponse struct {
	StatusCode int
	Header     http.Header
	Body       io.ReadCloser
	cancel     context.CancelFunc
}

// Close 关闭流式响应体。
func (r *StreamResponse) Close() error {
	if r == nil {
		return nil
	}
	var err error
	if r.Body != nil {
		err = r.Body.Close()
	}
	if r.cancel != nil {
		r.cancel()
	}
	return err
}

func newResponse(resp *resty.Response, result any, errorResult any) *Response {
	if resp == nil {
		return nil
	}
	body := resp.Body()
	copiedBody := make([]byte, len(body))
	copy(copiedBody, body)
	return &Response{
		StatusCode:  resp.StatusCode(),
		Header:      resp.Header().Clone(),
		Body:        copiedBody,
		Result:      result,
		ErrorResult: errorResult,
	}
}
