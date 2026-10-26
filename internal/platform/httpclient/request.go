package httpclient

import (
	"encoding"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	querystring "github.com/google/go-querystring/query"
)

// Decoder 将响应正文解析到 target。框架默认由 Resty 按 Content-Type 解析 JSON/XML，
// 只有上游使用自定义 envelope 或非标准响应协议时才需要设置 Decoder。
type Decoder func(body []byte, target any) error

// BodyKind 表示单次请求的正文编码方式。
type BodyKind string

const (
	// BodyKindNone 表示请求没有正文。
	BodyKindNone BodyKind = ""
	// BodyKindJSON 表示由客户端序列化 JSON 正文。
	BodyKindJSON BodyKind = "json"
	// BodyKindRaw 表示直接发送字符串、字节或 io.Reader。
	BodyKindRaw BodyKind = "raw"
	// BodyKindForm 表示 application/x-www-form-urlencoded 表单。
	BodyKindForm BodyKind = "form"
	// BodyKindMultipart 表示 multipart/form-data 表单。
	BodyKindMultipart BodyKind = "multipart"
)

// MultipartFile 描述 multipart 请求中的一个文件字段。
type MultipartFile struct {
	FieldName   string
	FileName    string
	ContentType string
	Reader      io.Reader
}

// RequestOption 用于按请求声明路径、查询、Header、正文、响应解析和超时等设置。
type RequestOption func(*RequestOptions)

// RequestOptions 是 RequestOption 归一化后的请求描述，主要供自定义 Executor 和测试使用。
type RequestOptions struct {
	Headers             http.Header
	QueryParams         url.Values
	PathParams          map[string]string
	Timeout             time.Duration
	Result              any
	ErrorResult         any
	Decoder             Decoder
	ErrorDecoder        Decoder
	ContentType         string
	Idempotent          bool
	BodyKind            BodyKind
	Body                any
	FormValues          url.Values
	MultipartValues     url.Values
	Files               []MultipartFile
	AcceptedStatusCodes map[int]struct{}
	err                 error
}

// WithHeader 设置单个请求 Header；相同名称会覆盖已有值。
func WithHeader(key, value string) RequestOption {
	return func(opts *RequestOptions) {
		opts.Headers.Set(key, value)
	}
}

// WithHeaders 批量设置单值请求 Header。
func WithHeaders(headers map[string]string) RequestOption {
	return func(opts *RequestOptions) {
		for key, value := range headers {
			opts.Headers.Set(key, value)
		}
	}
}

// WithHeaderValues 批量设置允许多值的请求 Header。
func WithHeaderValues(headers http.Header) RequestOption {
	return func(opts *RequestOptions) {
		for key, values := range headers {
			opts.Headers.Del(key)
			for _, value := range values {
				opts.Headers.Add(key, value)
			}
		}
	}
}

// WithQuery 添加一个查询参数。slice/array 会展开为同名多值参数。
func WithQuery(key string, value any) RequestOption {
	return func(opts *RequestOptions) {
		if opts.err != nil {
			return
		}
		if err := appendValue(opts.QueryParams, key, value); err != nil {
			opts.err = fmt.Errorf("encode query parameter %s: %w", key, err)
		}
	}
}

// WithQueryParams 批量设置单值查询参数。
func WithQueryParams(params map[string]string) RequestOption {
	return func(opts *RequestOptions) {
		for key, value := range params {
			opts.QueryParams.Set(key, value)
		}
	}
}

// WithQueryValues 批量添加允许重复键的查询参数。
func WithQueryValues(params url.Values) RequestOption {
	return func(opts *RequestOptions) {
		mergeValues(opts.QueryParams, params)
	}
}

// WithQueryStruct 根据 url tag 将结构体编码为查询参数，也接受 url.Values 和 map[string]string。
func WithQueryStruct(value any) RequestOption {
	return func(opts *RequestOptions) {
		if opts.err != nil || value == nil {
			return
		}
		switch typed := value.(type) {
		case url.Values:
			mergeValues(opts.QueryParams, typed)
			return
		case map[string]string:
			for key, item := range typed {
				opts.QueryParams.Set(key, item)
			}
			return
		}
		values, err := querystring.Values(value)
		if err != nil {
			opts.err = fmt.Errorf("encode query struct: %w", err)
			return
		}
		mergeValues(opts.QueryParams, values)
	}
}

// WithPath 设置单个路径模板参数，值会通过 fmt.Sprint 转换为字符串。
func WithPath(key string, value any) RequestOption {
	return func(opts *RequestOptions) {
		opts.PathParams[key] = fmt.Sprint(value)
	}
}

// WithPathParams 批量设置路径模板参数。
func WithPathParams(params map[string]string) RequestOption {
	return func(opts *RequestOptions) {
		for key, value := range params {
			opts.PathParams[key] = value
		}
	}
}

// WithTimeout 设置单次请求超时。
func WithTimeout(timeout time.Duration) RequestOption {
	return func(opts *RequestOptions) {
		opts.Timeout = timeout
	}
}

// WithResult 设置成功响应反序列化目标。普通业务优先使用 GetJSON、PostJSON 等泛型函数。
func WithResult(value any) RequestOption {
	return func(opts *RequestOptions) {
		opts.Result = value
	}
}

// WithErrorResult 设置非 2xx 响应的结构化反序列化目标。
func WithErrorResult(value any) RequestOption {
	return func(opts *RequestOptions) {
		opts.ErrorResult = value
	}
}

// WithDecoder 设置成功响应的自定义解析器。
func WithDecoder(decoder Decoder) RequestOption {
	return func(opts *RequestOptions) {
		opts.Decoder = decoder
	}
}

// WithErrorDecoder 设置非 2xx 响应的自定义解析器。
func WithErrorDecoder(decoder Decoder) RequestOption {
	return func(opts *RequestOptions) {
		opts.ErrorDecoder = decoder
	}
}

// WithJSONBody 设置 JSON 请求体。
func WithJSONBody(body any) RequestOption {
	return func(opts *RequestOptions) {
		setBody(opts, BodyKindJSON, body)
	}
}

// WithRawBody 设置不经结构化编码的请求体及 Content-Type。
func WithRawBody(body any, contentType string) RequestOption {
	return func(opts *RequestOptions) {
		setBody(opts, BodyKindRaw, body)
		if opts.err != nil {
			return
		}
		opts.ContentType = strings.TrimSpace(contentType)
	}
}

// WithForm 设置 application/x-www-form-urlencoded 表单，支持同名多值字段。
func WithForm(values url.Values) RequestOption {
	return func(opts *RequestOptions) {
		setBody(opts, BodyKindForm, nil)
		if opts.err != nil {
			return
		}
		mergeValues(opts.FormValues, values)
	}
}

// WithMultipartValues 添加 multipart 文本字段，支持同名多值字段。
func WithMultipartValues(values url.Values) RequestOption {
	return func(opts *RequestOptions) {
		setBody(opts, BodyKindMultipart, nil)
		if opts.err != nil {
			return
		}
		mergeValues(opts.MultipartValues, values)
	}
}

// WithMultipartField 添加一个 multipart 文本字段。
func WithMultipartField(key string, value any) RequestOption {
	return func(opts *RequestOptions) {
		setBody(opts, BodyKindMultipart, nil)
		if opts.err != nil {
			return
		}
		if err := appendValue(opts.MultipartValues, key, value); err != nil {
			opts.err = fmt.Errorf("encode multipart field %s: %w", key, err)
		}
	}
}

// WithFile 添加一个 multipart 文件字段。
func WithFile(fieldName string, fileName string, reader io.Reader) RequestOption {
	return WithFileContentType(fieldName, fileName, "", reader)
}

// WithFileContentType 添加带显式 Content-Type 的 multipart 文件字段。
func WithFileContentType(fieldName string, fileName string, contentType string, reader io.Reader) RequestOption {
	return func(opts *RequestOptions) {
		setBody(opts, BodyKindMultipart, nil)
		if opts.err != nil {
			return
		}
		if strings.TrimSpace(fieldName) == "" || strings.TrimSpace(fileName) == "" || reader == nil {
			opts.err = fmt.Errorf("multipart file requires field name, file name and reader")
			return
		}
		opts.Files = append(opts.Files, MultipartFile{
			FieldName:   fieldName,
			FileName:    fileName,
			ContentType: strings.TrimSpace(contentType),
			Reader:      reader,
		})
	}
}

// WithContentType 覆盖单次请求 Content-Type。JSON、Form、Multipart 通常无需手工设置。
func WithContentType(contentType string) RequestOption {
	return func(opts *RequestOptions) {
		opts.ContentType = strings.TrimSpace(contentType)
	}
}

// WithIdempotent 标记当前 POST/PATCH 请求可安全重试。
func WithIdempotent(idempotent bool) RequestOption {
	return func(opts *RequestOptions) {
		opts.Idempotent = idempotent
	}
}

// WithAcceptedStatus 将额外状态码视为成功；默认始终接受全部 2xx。
func WithAcceptedStatus(statusCodes ...int) RequestOption {
	return func(opts *RequestOptions) {
		if opts.err != nil {
			return
		}
		for _, statusCode := range statusCodes {
			if statusCode < http.StatusContinue || statusCode > 999 {
				opts.err = fmt.Errorf("invalid accepted HTTP status code %d", statusCode)
				return
			}
			opts.AcceptedStatusCodes[statusCode] = struct{}{}
		}
	}
}

// ApplyRequestOptions 归一化 RequestOption，供自定义 Executor 和业务测试复用。
func ApplyRequestOptions(options ...RequestOption) (RequestOptions, error) {
	opts := RequestOptions{
		Headers:             make(http.Header),
		QueryParams:         make(url.Values),
		PathParams:          make(map[string]string),
		FormValues:          make(url.Values),
		MultipartValues:     make(url.Values),
		AcceptedStatusCodes: make(map[int]struct{}),
	}
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}
	return opts, opts.err
}

func setBody(opts *RequestOptions, kind BodyKind, body any) {
	if opts.err != nil {
		return
	}
	if opts.BodyKind != BodyKindNone && opts.BodyKind != kind {
		opts.err = fmt.Errorf("request body modes %q and %q cannot be combined", opts.BodyKind, kind)
		return
	}
	opts.BodyKind = kind
	if body != nil {
		opts.Body = body
	}
}

func mergeValues(target url.Values, source url.Values) {
	for key, values := range source {
		for _, value := range values {
			target.Add(key, value)
		}
	}
}

func appendValue(target url.Values, key string, value any) error {
	if value == nil {
		return nil
	}
	if marshaler, ok := value.(encoding.TextMarshaler); ok {
		encoded, err := marshaler.MarshalText()
		if err != nil {
			return err
		}
		target.Add(key, string(encoded))
		return nil
	}
	reflected := reflect.ValueOf(value)
	for reflected.Kind() == reflect.Pointer || reflected.Kind() == reflect.Interface {
		if reflected.IsNil() {
			return nil
		}
		reflected = reflected.Elem()
	}
	if reflected.Kind() == reflect.Slice || reflected.Kind() == reflect.Array {
		for index := 0; index < reflected.Len(); index++ {
			if err := appendValue(target, key, reflected.Index(index).Interface()); err != nil {
				return err
			}
		}
		return nil
	}
	target.Add(key, fmt.Sprint(value))
	return nil
}
