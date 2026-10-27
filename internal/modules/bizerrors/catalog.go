package bizerrors

import (
	"net/http"

	apperrors "github.com/teamsillybees/glean/internal/platform/errors"
)

// CatalogForbidden 表示当前身份不能修改共享目录。
func CatalogForbidden() error {
	return newError(apperrors.CodeForbidden, "shared catalog management requires a super administrator")
}

// CatalogConflict 表示管理记录已被其他请求修改。
func CatalogConflict() error {
	return newError(apperrors.CodeBadRequest, "record changed; refresh before saving", apperrors.WithStatus(http.StatusConflict))
}

// StatusOf 返回业务错误的 HTTP 状态，供边界适配和契约验证使用。
func StatusOf(err error) int { return apperrors.StatusOf(err) }
