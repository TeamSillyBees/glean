package fieldx

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// SoftDelete 返回逻辑删除字段。
func SoftDelete() []ent.Field {
	return []ent.Field{
		field.Time("deleted_at").
			Optional().
			Nillable().
			Comment("逻辑删除时间，NULL 表示未删除。"),
	}
}
