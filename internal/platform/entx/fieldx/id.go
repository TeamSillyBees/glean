package fieldx

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"

	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// ID 返回统一的雪花 ID 主键字段。
func ID() ent.Field {
	return field.Int64("id").
		GoType(idgen.ID(0)).
		DefaultFunc(idgen.NextID).
		Immutable().
		Positive().
		Comment("雪花算法生成的主键 ID。")
}
