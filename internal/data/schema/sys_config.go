package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"

	"github.com/teamsillybees/glean/internal/platform/entx/fieldx"
)

// SysConfig 系统配置表，用于集中存放可在后台维护的运行时配置。
type SysConfig struct {
	ent.Schema
}

// Fields 返回系统配置表字段定义。
func (SysConfig) Fields() []ent.Field {
	fields := []ent.Field{
		fieldx.ID(),
		field.String("config_key").MaxLen(128).NotEmpty().Unique().
			Comment("配置键，程序通过该键读取配置。"),
		field.Text("config_value").Default("").
			Comment("配置值。"),
		field.Text("config_desc").Optional().Nillable().
			Comment("配置项描述。"),
		field.Bool("is_builtin").Default(false).
			Comment("是否为系统内置配置，内置配置通常不允许删除。"),
		field.Int32("sort_id").Default(0).
			Comment("排序值。"),
	}

	fields = append(fields, fieldx.SoftDelete()...)
	fields = append(fields, fieldx.Audit()...)

	return fields
}

// Annotations 返回系统配置表数据库元信息。
func (SysConfig) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "sys_config"},
		entsql.WithComments(true),
		schema.Comment("系统配置表，用于集中存放可在后台维护的运行时配置。"),
	}
}
