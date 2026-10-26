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

// SysUserRole 系统用户与角色关系表，用于描述一个用户拥有多个角色。
type SysUserRole struct {
	ent.Schema
}

// Fields 返回用户角色关系表字段定义。
func (SysUserRole) Fields() []ent.Field {
	fields := []ent.Field{
		fieldx.ID(),
		field.Int64("user_id").GoType(idgen.ID(0)).Positive().
			Comment("系统用户 ID，关联 sys_user.id。"),
		field.Int64("role_id").GoType(idgen.ID(0)).Positive().
			Comment("系统角色 ID，关联 sys_role.id。"),
	}
	fields = append(fields, fieldx.SoftDelete()...)
	fields = append(fields, fieldx.Audit()...)

	return fields
}

// Edges 返回用户角色关系表关系定义。
func (SysUserRole) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("user", SysUser.Type).
			Field("user_id").
			Required().
			Unique(),
		edge.To("role", SysRole.Type).
			Field("role_id").
			Required().
			Unique(),
	}
}

// Indexes 返回用户角色关系表索引定义。
func (SysUserRole) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("role_id"),
		index.Fields("user_id", "role_id").Unique(),
		indexx.SoftDelete(),
	}
}

// Annotations 返回用户角色关系表数据库元信息。
func (SysUserRole) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "sys_user_role"},
		entsql.WithComments(true),
		schema.Comment("系统用户与角色关系表，用于描述一个用户拥有多个角色。"),
	}
}
