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
)

// SysRole 系统角色表，用于承载后台角色定义。
type SysRole struct {
	ent.Schema
}

// Fields 返回系统角色表字段定义。
func (SysRole) Fields() []ent.Field {
	fields := []ent.Field{
		fieldx.ID(),
		field.String("code").MaxLen(64).NotEmpty().Unique().
			Comment("角色编码，程序内稳定引用，例如 admin、viewer。"),
		field.String("name").MaxLen(128).NotEmpty().
			Comment("角色名称，用于管理界面展示。"),
		field.Text("remark").Optional().Nillable().
			Comment("角色备注说明。"),
		field.Bool("is_builtin").Default(false).
			Comment("是否为系统内置角色，内置角色通常不允许删除。"),
		field.Bool("is_enable").Default(true).
			Comment("角色是否启用，禁用后不参与授权。"),
		field.Int32("sort_id").Default(0).
			Comment("角色排序值，值越小越靠前。"),
	}

	fields = append(fields, fieldx.SoftDelete()...)
	fields = append(fields, fieldx.Audit()...)

	return fields
}

// Edges 返回系统角色表关系定义。
func (SysRole) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user_roles", SysUserRole.Type).
			Ref("role"),
		edge.From("role_menus", SysRoleMenu.Type).
			Ref("role"),
	}
}

// Indexes 返回系统角色表索引定义。
func (SysRole) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("is_enable"),
		indexx.SoftDelete(),
	}
}

// Annotations 返回系统角色表数据库元信息。
func (SysRole) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "sys_role"},
		entsql.WithComments(true),
		schema.Comment("系统角色表，用于承载后台角色定义。"),
	}
}
