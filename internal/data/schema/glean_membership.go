package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/teamsillybees/glean/internal/platform/entx/fieldx"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// GleanMembership 保存 Glean 的持久化业务记录，禁止隐式物理删除。
type GleanMembership struct{ ent.Schema }

// Fields 返回字段定义。
func (GleanMembership) Fields() []ent.Field {
	fields := []ent.Field{fieldx.ID(),
		field.Int64("paper_id").GoType(idgen.ID(0)).Positive(),
		field.String("topic").NotEmpty(), field.String("original_id").NotEmpty(),
		field.String("inclusion_status").Default("historical"),
		field.String("tier").Default(""), field.String("primary_category").Default(""),
		field.JSON("categories", []string{}).Default([]string{}),
		field.JSON("dimensions", map[string][]string{}).Default(map[string][]string{}),
		field.String("read_status").Default("unread"), field.String("source").Default("historical-baseline"),
		field.Bool("is_mock").Default(false), field.JSON("raw", map[string]any{}).Default(map[string]any{}),
	}
	return append(fields, fieldx.Audit()...)
}

// Indexes 返回逻辑关联和查询索引。
func (GleanMembership) Indexes() []ent.Index {
	return []ent.Index{index.Fields("topic", "original_id").Unique(), index.Fields("paper_id"), index.Fields("tier")}
}

// Annotations 返回无物理外键的数据表元信息。
func (GleanMembership) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "glean_membership"}}
}
