package entx

import (
	"context"

	"github.com/teamsillybees/glean/internal/platform/idgen"
)

type operatorIDKey struct{}

// WithOperatorID 将当前操作人 ID 写入 context，供 Ent audit hook 自动填充审计字段。
func WithOperatorID(ctx context.Context, operatorID idgen.ID) context.Context {
	return context.WithValue(ctx, operatorIDKey{}, operatorID)
}

// OperatorIDFromContext 从 context 中读取当前操作人 ID。
func OperatorIDFromContext(ctx context.Context) (idgen.ID, bool) {
	v, ok := ctx.Value(operatorIDKey{}).(idgen.ID)
	return v, ok
}
