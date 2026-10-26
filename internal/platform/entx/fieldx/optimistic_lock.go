package fieldx

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// OptimisticLockVersion 返回乐观锁版本号字段助手。
// 它只定义字段，不提供 compare-and-swap 更新条件或版本递增逻辑。
func OptimisticLockVersion() ent.Field {
	return field.Int32("version").
		Default(1).
		Positive().
		Comment("乐观锁版本号。")
}
