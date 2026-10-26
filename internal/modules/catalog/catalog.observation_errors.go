package catalog

import "errors"

type observationRejection struct {
	kind string
	err  error
}

// Error 返回不包含原始来源负载的明确拒绝原因。
func (e *observationRejection) Error() string { return e.err.Error() }

// Unwrap 保留可审计的底层业务错误类型。
func (e *observationRejection) Unwrap() error { return e.err }

// ObservationRejectionKind 只分类明确的单条来源坏数据；配置、身份失效及数据库故障仍须重试或修复。
func ObservationRejectionKind(err error) (string, bool) {
	var rejected *observationRejection
	if errors.As(err, &rejected) {
		return rejected.kind, true
	}
	return "", false
}
