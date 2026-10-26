package fieldx

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"

	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// Audit 返回创建与更新审计字段。
func Audit() []ent.Field {
	return []ent.Field{
		field.Time("created_at").
			Default(time.Now).
			Immutable().
			Comment("创建时间。"),

		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			Comment("最后更新时间。"),

		field.Int64("created_by").
			GoType(idgen.ID(0)).
			Optional().
			Nillable().
			Comment("创建人用户 ID。"),

		field.Int64("updated_by").
			GoType(idgen.ID(0)).
			Optional().
			Nillable().
			Comment("最后更新人用户 ID。"),
	}
}
