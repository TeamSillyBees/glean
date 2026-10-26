package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/teamsillybees/glean/internal/platform/entx/fieldx"
	"github.com/teamsillybees/glean/internal/platform/entx/indexx"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// SysDictItem 系统字典项表，用于保存某个字典集下的具体值。
type SysDictItem struct {
	ent.Schema
}

// Fields 返回系统字典项表字段定义。
func (SysDictItem) Fields() []ent.Field {
	fields := []ent.Field{
		fieldx.ID(),
		field.Int64("collection_id").GoType(idgen.ID(0)).Positive().
			Comment("字典集 ID，关联 sys_dict_collection.id。"),
		field.String("collection_code").MaxLen(64).NotEmpty().
			Comment("字典集编码，便于按编码直接查询字典项。"),
		field.String("code").MaxLen(64).NotEmpty().
			Comment("字典项编码，程序实际使用的值。"),
		field.String("parent_code").MaxLen(64).Default("0").
			Comment("父级字典项编码，0 表示顶级节点。"),
		field.String("label").MaxLen(128).NotEmpty().
			Comment("字典项展示文本。"),
		field.Bool("is_default_value").Default(false).
			Comment("是否为默认值。"),
		field.Bool("is_enable").Default(true).
			Comment("字典项是否启用。"),
		field.Text("description").Optional().Nillable().
			Comment("字典项描述。"),
		field.Int32("sort_id").Default(0).
			Comment("排序值。"),
	}

	fields = append(fields, fieldx.SoftDelete()...)
	fields = append(fields, fieldx.Audit()...)

	return fields
}

// Edges 返回系统字典项表关系定义。
func (SysDictItem) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("collection", SysDictCollection.Type).
			Field("collection_id").
			Required().
			Unique(),
	}
}

// Indexes 返回系统字典项表索引定义。
func (SysDictItem) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("collection_id"),
		index.Fields("collection_code", "code").Unique(),
		indexx.SoftDelete(),
	}
}

// Annotations 返回系统字典项表数据库元信息。
func (SysDictItem) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "sys_dict_item"},
		entsql.WithComments(true),
		schema.Comment("系统字典项表，用于保存某个字典集下的具体值。"),
	}
}
