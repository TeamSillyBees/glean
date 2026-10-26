package entx

import (
	"context"
	"errors"
	"fmt"

	"entgo.io/ent"
)

// ErrPhysicalDeleteRejected 表示 Ent 物理删除被统一保护拦截。
var ErrPhysicalDeleteRejected = errors.New("physical delete rejected")

// RejectDeleteHook 拦截 Ent 物理删除，要求业务仓储显式实现软删除。
func RejectDeleteHook() ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			if mutation.Op().Is(ent.OpDelete | ent.OpDeleteOne) {
				return nil, fmt.Errorf("%w: 请使用 repository 的 soft delete 方法，不允许直接物理删除", ErrPhysicalDeleteRejected)
			}
			return next.Mutate(ctx, mutation)
		})
	}
}
