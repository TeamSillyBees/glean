package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/teamsillybees/glean/internal/platform/entx/fieldx"
)

// GleanVenue 保存 Glean 的持久化业务记录，禁止隐式物理删除。
type GleanVenue struct{ ent.Schema }

// Fields 返回字段定义。
func (GleanVenue) Fields() []ent.Field {
	fields := []ent.Field{fieldx.ID(),
		field.String("name").NotEmpty(),
		field.JSON("issns", []string{}).Default([]string{}), field.JSON("aliases", []string{}).Default([]string{}), field.String("kind").Default("journal"),
		field.Float("jif").Optional().Nillable(), field.String("ccf").Default(""), field.Int("metric_year").Default(0),
		field.String("registry_version").NotEmpty(), field.String("source").Default(""),
		field.JSON("evidence", map[string]any{}).Default(map[string]any{}),
		field.JSON("eligible_topics", []string{}).Default([]string{}), field.String("qualification_status").Default("pending"),
	}
	return append(fields, fieldx.Audit()...)
}

// Indexes 返回逻辑关联和查询索引。
func (GleanVenue) Indexes() []ent.Index {
	return []ent.Index{index.Fields("name", "registry_version").Unique()}
}

// Annotations 返回无物理外键的数据表元信息。
func (GleanVenue) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "glean_venue"}}
}
