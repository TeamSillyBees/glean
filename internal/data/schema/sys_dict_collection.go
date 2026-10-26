package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"

	"github.com/teamsillybees/glean/internal/platform/entx/fieldx"
)

// SysDictCollection 系统字典集表，用于定义一类字典的元信息。
type SysDictCollection struct {
	ent.Schema
}

// Fields 返回系统字典集表字段定义。
func (SysDictCollection) Fields() []ent.Field {
	fields := []ent.Field{
		fieldx.ID(),
		field.String("code").MaxLen(64).NotEmpty().Unique().
			Comment("字典集唯一编码，程序通过该编码读取字典项。"),
		field.String("name").MaxLen(128).NotEmpty().
			Comment("字典集名称。"),
		field.Bool("is_enable").Default(true).
			Comment("字典集是否启用。"),
		field.Text("description").Optional().Nillable().
			Comment("字典集说明。"),
		field.Int32("item_length").Optional().Nillable().Positive().
			Comment("字典值推荐长度上限。"),
		field.Bool("is_builtin").Default(false).
			Comment("是否为系统内置字典集。"),
		field.Int32("sort_id").Default(0).
			Comment("排序值。"),
	}

	fields = append(fields, fieldx.SoftDelete()...)
	fields = append(fields, fieldx.Audit()...)

	return fields
}

// Edges 返回系统字典集表关系定义。
func (SysDictCollection) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("items", SysDictItem.Type).
			Ref("collection"),
	}
}

// Annotations 返回系统字典集表数据库元信息。
func (SysDictCollection) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "sys_dict_collection",
			Checks: map[string]string{
				"ck_sys_dict_collection_item_length": "item_length IS NULL OR item_length > 0",
			},
		},
		entsql.WithComments(true),
		schema.Comment("系统字典集表，用于定义一类字典的元信息。"),
	}
}
