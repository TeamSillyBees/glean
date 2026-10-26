package httpclient

import (
	"errors"
	"fmt"
)

var (
	// ErrDisabled 表示 HTTP Client 能力未启用。
	ErrDisabled = errors.New("httpclient disabled")
	// ErrInvalidConfig 表示 HTTP Client 配置不完整或非法。
	ErrInvalidConfig = errors.New("httpclient invalid config")
	// ErrServiceNotFound 表示请求的远程服务未配置。
	ErrServiceNotFound = errors.New("httpclient service not found")
)

// ErrorKind 表示 HTTP Client 错误分类。
type ErrorKind string

const (
	// ErrorKindRequest 表示网络错误、连接失败或超时。
	ErrorKindRequest ErrorKind = "request"
	// ErrorKindResponse 表示 HTTP 状态不在默认或调用方声明的成功范围内。
	ErrorKindResponse ErrorKind = "response"
	// ErrorKindInternal 表示配置、解析或本地处理错误。
	ErrorKindInternal ErrorKind = "internal"
)

// Error 描述远程 HTTP 调用的统一错误模型。
type Error struct {
	Kind       ErrorKind
	Service    string
	Method     string
	URL        string
	StatusCode int
	Code       string
	Message    string
	Cause      error
	Response   *Response
}

// Error 返回适合日志和调试的错误描述。
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	message := e.Message
	if message == "" && e.Cause != nil {
		message = e.Cause.Error()
	}
	if message == "" {
		message = "http client error"
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("httpclient %s error: service=%s method=%s url=%s status=%d code=%s message=%s", e.Kind, e.Service, e.Method, e.URL, e.StatusCode, e.Code, message)
	}
	return fmt.Sprintf("httpclient %s error: service=%s method=%s url=%s code=%s message=%s", e.Kind, e.Service, e.Method, e.URL, e.Code, message)
}

// Unwrap 返回底层错误。
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// IsResponseError 判断错误是否为远程响应错误。
func IsResponseError(err error) bool {
	var target *Error
	return errors.As(err, &target) && target.Kind == ErrorKindResponse
}

// IsRequestError 判断错误是否为请求发送错误。
func IsRequestError(err error) bool {
	var target *Error
	return errors.As(err, &target) && target.Kind == ErrorKindRequest
}

// IsStatus 判断错误是否来自指定 HTTP 状态码。
func IsStatus(err error, statusCode int) bool {
	var target *Error
	return errors.As(err, &target) && target.StatusCode == statusCode
}
