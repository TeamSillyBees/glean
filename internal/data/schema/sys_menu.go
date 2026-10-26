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

// SysMenu 系统菜单与按钮权限表，统一承载菜单、目录、按钮三类资源。
type SysMenu struct {
	ent.Schema
}

// Fields 返回系统菜单表字段定义。
func (SysMenu) Fields() []ent.Field {
	fields := []ent.Field{
		fieldx.ID(),
		field.Int64("parent_id").GoType(idgen.ID(0)).Optional().Nillable().Positive().
			Comment("父级菜单 ID，NULL 表示顶级目录。"),
		field.String("app_id").MaxLen(64).Optional().Nillable().
			Comment("所属应用编码，用于多应用场景区分菜单树。"),
		field.String("title").MaxLen(128).NotEmpty().
			Comment("菜单或按钮展示标题。"),
		field.Int16("menu_type").Min(0).Max(2).
			Comment("资源类型：0-菜单，1-按钮，2-目录。"),
		field.Text("route_path").Optional().Nillable().
			Comment("前端路由路径。"),
		field.Text("component_path").Optional().Nillable().
			Comment("前端组件路径。"),
		field.String("permission_code").MaxLen(128).Optional().Nillable().Unique().
			Comment("权限资源编码，例如 system:user:read。"),
		field.String("icon").MaxLen(128).Optional().Nillable().
			Comment("菜单图标标识。"),
		field.Bool("is_visible").Default(true).
			Comment("是否在前端菜单树中可见。"),
		field.Bool("is_cached").Default(true).
			Comment("前端页面是否缓存。"),
		field.Int32("sort_id").Default(0).
			Comment("排序值，值越小越靠前。"),
	}

	fields = append(fields, fieldx.SoftDelete()...)
	fields = append(fields, fieldx.Audit()...)

	return fields
}

// Edges 返回系统菜单表关系定义。
func (SysMenu) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("children", SysMenu.Type).
			From("parent").
			Unique().
			Field("parent_id"),
		edge.From("role_menus", SysRoleMenu.Type).
			Ref("menu"),
	}
}

// Indexes 返回系统菜单表索引定义。
func (SysMenu) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("parent_id"),
		indexx.SoftDelete(),
	}
}

// Annotations 返回系统菜单表数据库元信息。
func (SysMenu) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "sys_menu",
			Checks: map[string]string{
				"ck_sys_menu_parent_not_self": "parent_id IS NULL OR parent_id <> id",
				"ck_sys_menu_type":            "menu_type IN (0, 1, 2)",
			},
		},
		entsql.WithComments(true),
		schema.Comment("系统菜单与按钮权限表，统一承载菜单、目录、按钮三类资源。"),
	}
}
