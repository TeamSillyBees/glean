package storage

import "errors"

var (
	// ErrDisabled 表示存储能力未启用。
	ErrDisabled = errors.New("storage disabled")
	// ErrInvalidConfig 表示存储配置不完整或非法。
	ErrInvalidConfig = errors.New("storage invalid config")
	// ErrInvalidKey 表示对象 key 为空或包含非法路径。
	ErrInvalidKey = errors.New("storage invalid object key")
	// ErrNotFound 表示对象不存在。
	ErrNotFound = errors.New("storage object not found")
	// ErrObjectExists 表示对象已存在且不允许覆盖。
	ErrObjectExists = errors.New("storage object already exists")
	// ErrUnsupported 表示当前 provider 不支持该能力。
	ErrUnsupported = errors.New("storage operation unsupported")
)

// IsNotFound 判断错误是否表示对象不存在。
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}
