package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/teamsillybees/glean/internal/platform/entx/fieldx"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// GleanPreference 保存 Glean 的持久化业务记录，禁止隐式物理删除。
type GleanPreference struct{ ent.Schema }

// Fields 返回字段定义。
func (GleanPreference) Fields() []ent.Field {
	fields := []ent.Field{fieldx.ID(),
		field.Int64("user_id").GoType(idgen.ID(0)).Positive().Unique(),
		field.JSON("filters", map[string]any{}).Default(map[string]any{}),
	}
	return append(fields, fieldx.Audit()...)
}

// Indexes 返回逻辑关联和查询索引。
func (GleanPreference) Indexes() []ent.Index { return []ent.Index{} }

// Annotations 返回无物理外键的数据表元信息。
func (GleanPreference) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "glean_preference"}}
}
