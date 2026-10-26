package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/teamsillybees/glean/internal/platform/logx"
)

const jsonContentType = "application/json"

// Executor 是业务模块通常需要依赖的最小 HTTP 调用能力。
type Executor interface {
	Do(ctx context.Context, method string, path string, opts ...RequestOption) (*Response, error)
}

// Streamer 表示不把响应体读入内存的 HTTP 流式调用能力。
type Streamer interface {
	Stream(ctx context.Context, method string, path string, opts ...RequestOption) (*StreamResponse, error)
}

// Client 封装单个远程服务的 HTTP 调用能力。Resty 仅作为内部执行引擎，不向业务暴露。
type Client struct {
	name   string
	config ServiceConfig
	resty  *resty.Client
	logger *logx.Logger
}

func newClient(name string, global Config, cfg ServiceConfig, logger *logx.Logger, hooks []RequestHook) (*Client, error) {
	if logger == nil {
		logger = logx.NewNop()
	}
	if err := validateServiceConfig(name, cfg); err != nil {
		return nil, err
	}
	return &Client{
		name:   name,
		config: cfg,
		resty:  newRestyClient(global, cfg, hooks),
		logger: logger,
	}, nil
}

// Name 返回远程服务名称。
func (c *Client) Name() string {
	if c == nil {
		return ""
	}
	return c.name
}

// Do 使用统一请求模型发送任意 HTTP Method。默认接受全部 2xx，额外状态可由 WithAcceptedStatus 声明。
// 非成功响应同时返回 Response 和 *Error，调用方可以安全读取 Header、Body 和 ErrorResult。
func (c *Client) Do(ctx context.Context, method string, path string, opts ...RequestOption) (*Response, error) {
	if c == nil {
		return nil, clientUnavailableError(method, path)
	}
	if ctx == nil {
		return nil, c.invalidRequestError(method, path, errors.New("context 不能为空"))
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		return nil, c.invalidRequestError(method, path, errors.New("HTTP method 不能为空"))
	}
	options, err := ApplyRequestOptions(opts...)
	if err != nil {
		httpErr := c.invalidRequestError(method, path, err)
		c.logRequest(ctx, method, path, 0, 0, httpErr)
		return nil, httpErr
	}
	if err := validateDecodeOptions(options); err != nil {
		httpErr := c.invalidRequestError(method, path, err)
		c.logRequest(ctx, method, path, 0, 0, httpErr)
		return nil, httpErr
	}

	requestCtx, cancel := requestContext(ctx, options)
	if cancel != nil {
		defer cancel()
	}
	req, err := c.prepareRequest(requestCtx, options, false)
	if err != nil {
		httpErr := c.enrichError(err, ErrorKindInternal, method, path, 0, nil)
		c.logRequest(requestCtx, method, path, 0, 0, httpErr)
		return nil, httpErr
	}

	start := time.Now()
	resp, executeErr := req.Execute(method, path)
	duration := time.Since(start)
	wrapped := responseFromResty(resp, options)
	statusCode := responseStatusCode(resp)
	if executeErr != nil {
		httpErr := c.errorFromExecution(executeErr, resp, wrapped, method, path)
		c.logRequest(requestCtx, method, path, statusCode, duration, httpErr)
		return wrapped, httpErr
	}
	if resp == nil {
		httpErr := &Error{
			Kind:    ErrorKindInternal,
			Service: c.name,
			Method:  method,
			URL:     safeErrorURL(path),
			Code:    "empty_response",
			Message: "HTTP Client 未返回响应",
		}
		c.logRequest(requestCtx, method, path, 0, duration, httpErr)
		return nil, httpErr
	}

	if !isAcceptedStatus(statusCode, options.AcceptedStatusCodes) {
		if err := decodeCustom(resp.Body(), options.ErrorResult, options.ErrorDecoder); err != nil {
			wrapped = responseFromResty(resp, options)
			httpErr := c.enrichError(err, ErrorKindInternal, method, responseURL(resp, path), statusCode, wrapped)
			httpErr.Code = "error_response_parse_error"
			c.logRequest(requestCtx, method, path, statusCode, duration, httpErr)
			return wrapped, httpErr
		}
		wrapped = responseFromResty(resp, options)
		httpErr := &Error{
			Kind:       ErrorKindResponse,
			Service:    c.name,
			Method:     method,
			URL:        safeErrorURL(responseURL(resp, path)),
			StatusCode: statusCode,
			Code:       fmt.Sprint(statusCode),
			Message:    resp.Status(),
			Response:   wrapped,
		}
		c.logRequest(requestCtx, method, path, statusCode, duration, httpErr)
		return wrapped, httpErr
	}

	if err := decodeCustom(resp.Body(), options.Result, options.Decoder); err != nil {
		wrapped = responseFromResty(resp, options)
		httpErr := c.enrichError(err, ErrorKindInternal, method, responseURL(resp, path), statusCode, wrapped)
		httpErr.Code = "response_parse_error"
		c.logRequest(requestCtx, method, path, statusCode, duration, httpErr)
		return wrapped, httpErr
	}
	wrapped = responseFromResty(resp, options)
	c.logRequest(requestCtx, method, path, statusCode, duration, nil)
	return wrapped, nil
}

// Stream 发送流式请求，成功时由调用方负责关闭返回的响应体。
// 非成功响应会在大小上限内读取并关闭响应体，再以普通 *Error 返回。
func (c *Client) Stream(ctx context.Context, method string, path string, opts ...RequestOption) (*StreamResponse, error) {
	if c == nil {
		return nil, clientUnavailableError(method, path)
	}
	if ctx == nil {
		return nil, c.invalidRequestError(method, path, errors.New("context 不能为空"))
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		return nil, c.invalidRequestError(method, path, errors.New("HTTP method 不能为空"))
	}
	options, err := ApplyRequestOptions(opts...)
	if err != nil {
		return nil, c.invalidRequestError(method, path, err)
	}
	if options.Result != nil || options.ErrorResult != nil || options.Decoder != nil || options.ErrorDecoder != nil {
		return nil, c.invalidRequestError(method, path, errors.New("流式请求不能配置响应反序列化目标"))
	}
	requestCtx, cancel := requestContext(ctx, options)
	req, err := c.prepareRequest(requestCtx, options, true)
	if err != nil {
		cancelRequest(cancel)
		return nil, c.enrichError(err, ErrorKindInternal, method, path, 0, nil)
	}

	start := time.Now()
	resp, executeErr := req.Execute(method, path)
	duration := time.Since(start)
	statusCode := responseStatusCode(resp)
	if executeErr != nil {
		cancelRequest(cancel)
		httpErr := c.errorFromExecution(executeErr, resp, nil, method, path)
		c.logRequest(requestCtx, method, path, statusCode, duration, httpErr)
		return nil, httpErr
	}
	if resp == nil || resp.RawResponse == nil || resp.RawBody() == nil {
		cancelRequest(cancel)
		httpErr := &Error{Kind: ErrorKindInternal, Service: c.name, Method: method, URL: safeErrorURL(path), Code: "empty_response", Message: "HTTP Client 未返回流式响应"}
		c.logRequest(requestCtx, method, path, statusCode, duration, httpErr)
		return nil, httpErr
	}
	if !isAcceptedStatus(statusCode, options.AcceptedStatusCodes) {
		body, readErr := readLimitedAndClose(resp.RawBody(), c.config.MaxResponseBodySize)
		cancelRequest(cancel)
		wrapped := &Response{StatusCode: statusCode, Header: resp.Header().Clone(), Body: body}
		httpErr := &Error{
			Kind:       ErrorKindResponse,
			Service:    c.name,
			Method:     method,
			URL:        safeErrorURL(responseURL(resp, path)),
			StatusCode: statusCode,
			Code:       fmt.Sprint(statusCode),
			Message:    resp.Status(),
			Cause:      readErr,
			Response:   wrapped,
		}
		c.logRequest(requestCtx, method, path, statusCode, duration, httpErr)
		return nil, httpErr
	}
	c.logRequest(requestCtx, method, path, statusCode, duration, nil)
	return &StreamResponse{StatusCode: statusCode, Header: resp.Header().Clone(), Body: resp.RawBody(), cancel: cancel}, nil
}

// Get 发送 HTTP GET 请求。
func (c *Client) Get(ctx context.Context, path string, opts ...RequestOption) (*Response, error) {
	return c.Do(ctx, http.MethodGet, path, opts...)
}

// Post 发送 JSON POST 请求。
func (c *Client) Post(ctx context.Context, path string, body any, opts ...RequestOption) (*Response, error) {
	return c.Do(ctx, http.MethodPost, path, prependOption(WithJSONBody(body), opts)...)
}

// Put 发送 JSON PUT 请求。
func (c *Client) Put(ctx context.Context, path string, body any, opts ...RequestOption) (*Response, error) {
	return c.Do(ctx, http.MethodPut, path, prependOption(WithJSONBody(body), opts)...)
}

// Patch 发送 JSON PATCH 请求。
func (c *Client) Patch(ctx context.Context, path string, body any, opts ...RequestOption) (*Response, error) {
	return c.Do(ctx, http.MethodPatch, path, prependOption(WithJSONBody(body), opts)...)
}

// Delete 发送 HTTP DELETE 请求；需要正文时可传入 WithJSONBody、WithForm 或 WithRawBody。
func (c *Client) Delete(ctx context.Context, path string, opts ...RequestOption) (*Response, error) {
	return c.Do(ctx, http.MethodDelete, path, opts...)
}

// Head 发送 HTTP HEAD 请求。
func (c *Client) Head(ctx context.Context, path string, opts ...RequestOption) (*Response, error) {
	return c.Do(ctx, http.MethodHead, path, opts...)
}

// DoJSON 发送任意 Method 请求并把响应解析为 T。
func DoJSON[T any](ctx context.Context, client Executor, method string, path string, opts ...RequestOption) (T, error) {
	var result T
	if client == nil {
		return result, clientUnavailableError(method, path)
	}
	_, err := client.Do(ctx, method, path, appendOption(opts, WithResult(&result))...)
	return result, err
}

// GetJSON 发送 GET 请求并把响应解析为 T。
func GetJSON[T any](ctx context.Context, client Executor, path string, opts ...RequestOption) (T, error) {
	return DoJSON[T](ctx, client, http.MethodGet, path, opts...)
}

// PostJSON 发送 JSON POST 请求并把响应解析为 T。
func PostJSON[T any](ctx context.Context, client Executor, path string, body any, opts ...RequestOption) (T, error) {
	return DoJSON[T](ctx, client, http.MethodPost, path, prependOption(WithJSONBody(body), opts)...)
}

// PutJSON 发送 JSON PUT 请求并把响应解析为 T。
func PutJSON[T any](ctx context.Context, client Executor, path string, body any, opts ...RequestOption) (T, error) {
	return DoJSON[T](ctx, client, http.MethodPut, path, prependOption(WithJSONBody(body), opts)...)
}

// PatchJSON 发送 JSON PATCH 请求并把响应解析为 T。
func PatchJSON[T any](ctx context.Context, client Executor, path string, body any, opts ...RequestOption) (T, error) {
	return DoJSON[T](ctx, client, http.MethodPatch, path, prependOption(WithJSONBody(body), opts)...)
}

// DeleteJSON 发送 DELETE 请求并把响应解析为 T；请求体可通过 RequestOption 声明。
func DeleteJSON[T any](ctx context.Context, client Executor, path string, opts ...RequestOption) (T, error) {
	return DoJSON[T](ctx, client, http.MethodDelete, path, opts...)
}

// PostForm 发送 application/x-www-form-urlencoded POST 请求并把响应解析为 T。
func PostForm[T any](ctx context.Context, client Executor, path string, form url.Values, opts ...RequestOption) (T, error) {
	return DoJSON[T](ctx, client, http.MethodPost, path, prependOption(WithForm(form), opts)...)
}

// PostMultipart 发送 multipart/form-data POST 请求并把响应解析为 T。
// 文本字段和文件通过 WithMultipartValues、WithMultipartField、WithFile 声明。
func PostMultipart[T any](ctx context.Context, client Executor, path string, opts ...RequestOption) (T, error) {
	return DoJSON[T](ctx, client, http.MethodPost, path, prependOption(WithMultipartValues(nil), opts)...)
}

// DoBytes 发送任意 Method 请求并返回原始响应体副本。
func DoBytes(ctx context.Context, client Executor, method string, path string, opts ...RequestOption) ([]byte, *Response, error) {
	if client == nil {
		return nil, nil, clientUnavailableError(method, path)
	}
	resp, err := client.Do(ctx, method, path, opts...)
	if resp == nil {
		return nil, nil, err
	}
	body := append([]byte(nil), resp.Body...)
	return body, resp, err
}

func (c *Client) prepareRequest(ctx context.Context, options RequestOptions, stream bool) (*resty.Request, error) {
	req := c.resty.R().
		SetContext(ctx).
		SetQueryParamsFromValues(options.QueryParams).
		SetPathParams(options.PathParams)
	if err := applyStaticAuth(req, c.config.Auth); err != nil {
		return nil, err
	}
	for key, values := range options.Headers {
		req.Header.Del(key)
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	if !stream {
		if options.Result != nil && options.Decoder == nil {
			req.SetResult(options.Result)
		}
		if options.ErrorResult != nil && options.ErrorDecoder == nil {
			req.SetError(options.ErrorResult)
		}
	} else {
		req.SetDoNotParseResponse(true)
	}
	if err := applyRequestBody(req, options); err != nil {
		return nil, err
	}
	if options.ContentType != "" {
		req.SetHeader("Content-Type", options.ContentType)
	}
	return req, nil
}

func applyRequestBody(req *resty.Request, options RequestOptions) error {
	switch options.BodyKind {
	case BodyKindNone:
		return nil
	case BodyKindJSON:
		if options.Body != nil {
			req.SetBody(options.Body)
			if options.ContentType == "" {
				req.SetHeader("Content-Type", jsonContentType)
			}
		}
	case BodyKindRaw:
		if options.Body == nil {
			return errors.New("raw request body 不能为空")
		}
		req.SetBody(options.Body)
	case BodyKindForm:
		req.SetFormDataFromValues(options.FormValues)
	case BodyKindMultipart:
		for key, values := range options.MultipartValues {
			for _, value := range values {
				req.SetMultipartField(key, "", "text/plain; charset=utf-8", strings.NewReader(value))
			}
		}
		for _, file := range options.Files {
			if file.ContentType == "" {
				req.SetFileReader(file.FieldName, file.FileName, file.Reader)
				continue
			}
			req.SetMultipartField(file.FieldName, file.FileName, file.ContentType, file.Reader)
		}
	default:
		return fmt.Errorf("unsupported request body kind %q", options.BodyKind)
	}
	return nil
}

func validateDecodeOptions(options RequestOptions) error {
	if options.Decoder != nil && options.Result == nil {
		return errors.New("WithDecoder 必须与响应目标一起使用")
	}
	if options.ErrorDecoder != nil && options.ErrorResult == nil {
		return errors.New("WithErrorDecoder 必须与错误响应目标一起使用")
	}
	return nil
}

func decodeCustom(body []byte, target any, decoder Decoder) error {
	if target == nil || decoder == nil {
		return nil
	}
	return decoder(body, target)
}

func responseFromResty(resp *resty.Response, options RequestOptions) *Response {
	if resp == nil {
		return nil
	}
	result := options.Result
	if result == nil {
		result = resp.Result()
	}
	errorResult := options.ErrorResult
	if errorResult == nil {
		errorResult = resp.Error()
	}
	return newResponse(resp, result, errorResult)
}

func responseStatusCode(resp *resty.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode()
}

func responseURL(resp *resty.Response, fallback string) string {
	if resp != nil && resp.Request != nil && resp.Request.URL != "" {
		return resp.Request.URL
	}
	return fallback
}

func isAcceptedStatus(statusCode int, additional map[int]struct{}) bool {
	if statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices {
		return true
	}
	_, ok := additional[statusCode]
	return ok
}

func requestContext(ctx context.Context, opts RequestOptions) (context.Context, context.CancelFunc) {
	if opts.Idempotent {
		ctx = context.WithValue(ctx, retryIdempotentContextKey{}, true)
	}
	if opts.Timeout <= 0 {
		return ctx, nil
	}
	return context.WithTimeout(ctx, opts.Timeout)
}

func cancelRequest(cancel context.CancelFunc) {
	if cancel != nil {
		cancel()
	}
}

func (c *Client) errorFromExecution(err error, resp *resty.Response, wrapped *Response, method string, path string) *Error {
	statusCode := responseStatusCode(resp)
	urlValue := responseURL(resp, path)
	kind := ErrorKindRequest
	code := executionErrorCode(kind, err)
	var hookErr *requestHookError
	if errors.As(err, &hookErr) {
		kind = ErrorKindInternal
		code = "request_hook_error"
	} else if resp != nil && resp.RawResponse != nil {
		kind = ErrorKindInternal
		code = "response_parse_error"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		kind = ErrorKindRequest
		code = executionErrorCode(kind, err)
	}
	return &Error{
		Kind:       kind,
		Service:    c.name,
		Method:     method,
		URL:        safeErrorURL(urlValue),
		StatusCode: statusCode,
		Code:       code,
		Message:    err.Error(),
		Cause:      err,
		Response:   wrapped,
	}
}

func (c *Client) enrichError(err error, kind ErrorKind, method string, path string, statusCode int, response *Response) *Error {
	if httpErr, ok := errors.AsType[*Error](err); ok {
		if httpErr.Service == "" {
			httpErr.Service = c.name
		}
		if httpErr.Method == "" {
			httpErr.Method = method
		}
		if httpErr.URL == "" {
			httpErr.URL = safeErrorURL(path)
		}
		if httpErr.StatusCode == 0 {
			httpErr.StatusCode = statusCode
		}
		if httpErr.Response == nil {
			httpErr.Response = response
		}
		return httpErr
	}
	return &Error{
		Kind:       kind,
		Service:    c.name,
		Method:     method,
		URL:        safeErrorURL(path),
		StatusCode: statusCode,
		Code:       "internal_error",
		Message:    err.Error(),
		Cause:      err,
		Response:   response,
	}
}

func (c *Client) invalidRequestError(method string, path string, cause error) *Error {
	return &Error{
		Kind:    ErrorKindInternal,
		Service: c.name,
		Method:  method,
		URL:     safeErrorURL(path),
		Code:    "invalid_request",
		Message: cause.Error(),
		Cause:   cause,
	}
}

func clientUnavailableError(method string, path string) *Error {
	return &Error{
		Kind:    ErrorKindInternal,
		Method:  method,
		URL:     safeErrorURL(path),
		Code:    "nil_client",
		Message: "HTTP Client 不能为空",
	}
}

func (c *Client) logRequest(ctx context.Context, method string, path string, statusCode int, duration time.Duration, err *Error) {
	if c.logger == nil {
		return
	}
	fields := []logx.Field{
		logx.String("service", c.name),
		logx.String("method", method),
		logx.String("url_path", safeLogPath(path)),
		logx.Int("status_code", statusCode),
		logx.Duration("duration", duration),
	}
	if err != nil {
		fields = append(fields,
			logx.String("error_kind", string(err.Kind)),
			logx.String("error_code", err.Code),
		)
		c.logger.Error(ctx, "http client request failed", err, fields...)
		return
	}
	fields = append(fields, logx.String("error_kind", ""))
	c.logger.Info(ctx, "http client request completed", fields...)
}

func (c *Client) closeIdleConnections() {
	if c == nil || c.resty == nil || c.resty.GetClient() == nil {
		return
	}
	c.resty.GetClient().CloseIdleConnections()
}

func executionErrorCode(kind ErrorKind, err error) string {
	if kind == ErrorKindRequest {
		if errors.Is(err, context.DeadlineExceeded) {
			return "timeout"
		}
		if errors.Is(err, context.Canceled) {
			return "canceled"
		}
		return "request_error"
	}
	return "response_parse_error"
}

func prependOption(first RequestOption, options []RequestOption) []RequestOption {
	result := make([]RequestOption, 0, len(options)+1)
	result = append(result, first)
	result = append(result, options...)
	return result
}

func appendOption(options []RequestOption, last RequestOption) []RequestOption {
	result := make([]RequestOption, 0, len(options)+1)
	result = append(result, options...)
	result = append(result, last)
	return result
}

func readLimitedAndClose(body io.ReadCloser, limit int64) ([]byte, error) {
	defer body.Close()
	reader := io.Reader(body)
	if limit > 0 {
		reader = io.LimitReader(body, limit+1)
	}
	content, err := io.ReadAll(reader)
	if err != nil {
		return content, err
	}
	if limit > 0 && int64(len(content)) > limit {
		return content[:limit], resty.ErrResponseBodyTooLarge
	}
	return content, nil
}
