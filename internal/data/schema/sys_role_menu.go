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

// SysRoleMenu 系统角色与菜单/按钮资源关系表，用于角色授权。
type SysRoleMenu struct {
	ent.Schema
}

// Fields 返回角色菜单关系表字段定义。
func (SysRoleMenu) Fields() []ent.Field {
	fields := []ent.Field{
		fieldx.ID(),
		field.Int64("role_id").GoType(idgen.ID(0)).Positive().
			Comment("系统角色 ID，关联 sys_role.id。"),
		field.Int64("menu_id").GoType(idgen.ID(0)).Positive().
			Comment("系统菜单资源 ID，关联 sys_menu.id。"),
	}
	fields = append(fields, fieldx.SoftDelete()...)
	fields = append(fields, fieldx.Audit()...)

	return fields
}

// Edges 返回角色菜单关系表关系定义。
func (SysRoleMenu) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("role", SysRole.Type).
			Field("role_id").
			Required().
			Unique(),
		edge.To("menu", SysMenu.Type).
			Field("menu_id").
			Required().
			Unique(),
	}
}

// Indexes 返回角色菜单关系表索引定义。
func (SysRoleMenu) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("menu_id"),
		index.Fields("role_id", "menu_id").Unique(),
		indexx.SoftDelete(),
	}
}

// Annotations 返回角色菜单关系表数据库元信息。
func (SysRoleMenu) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "sys_role_menu"},
		entsql.WithComments(true),
		schema.Comment("系统角色与菜单/按钮资源关系表，用于角色授权。"),
	}
}
