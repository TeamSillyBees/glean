package entx

import (
	"context"

	"entgo.io/ent"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// OperatorFunc 从 context 中解析当前操作人 ID。
type OperatorFunc func(ctx context.Context) (idgen.ID, bool)

// AuditHookOptions 描述审计自动填充 hook 的运行时依赖。
type AuditHookOptions struct {
	Operator OperatorFunc
}

type createdBySetter interface {
	SetCreatedBy(idgen.ID)
}

type updatedBySetter interface {
	SetUpdatedBy(idgen.ID)
}

// AuditHook 为 Ent create/update mutation 自动填充操作人字段。
//
// 设计原则：
// 1. ID 不在 hook 中生成，应交给 schema DefaultFunc 或业务显式 SetID。
// 2. created_at 不在 hook 中写入，应交给 schema Default(time.Now)。
// 3. updated_at 不在 hook 中写入，应交给 schema Default(time.Now).UpdateDefault(time.Now)。
// 4. created_by 只在 Create 时写入。
// 5. updated_by 在 Create / Update / UpdateOne 时写入。
// 6. 如果 context 中没有操作人，则不写入 created_by / updated_by。
func AuditHook(options AuditHookOptions) ent.Hook {
	operator := options.Operator
	if operator == nil {
		operator = OperatorIDFromContext
	}

	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			operatorID, ok := operator(ctx)
			if !ok {
				return next.Mutate(ctx, mutation)
			}

			switch {
			case mutation.Op().Is(ent.OpCreate):
				if setter, ok := mutation.(createdBySetter); ok {
					setter.SetCreatedBy(operatorID)
				}
				if setter, ok := mutation.(updatedBySetter); ok {
					setter.SetUpdatedBy(operatorID)
				}

			case mutation.Op().Is(ent.OpUpdate | ent.OpUpdateOne):
				if setter, ok := mutation.(updatedBySetter); ok {
					setter.SetUpdatedBy(operatorID)
				}
			}

			return next.Mutate(ctx, mutation)
		})
	}
}
