package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/teamsillybees/glean/internal/platform/entx/fieldx"
	"github.com/teamsillybees/glean/internal/retrieval"
)

// GleanTopic 保存人工管理覆盖层，不修改原始来源证据。
type GleanTopic struct{ ent.Schema }

// Fields 返回字段定义。
func (GleanTopic) Fields() []ent.Field {
	return append([]ent.Field{fieldx.ID(), field.String("slug").NotEmpty().Unique(), field.String("name").NotEmpty(), field.Text("description").Default(""), field.Text("rule").Default(""), field.Bool("search_enabled").Default(true), field.JSON("term_groups", [][]string{}).Default([][]string{}), field.JSON("search_routes", []retrieval.Route{}).Default([]retrieval.Route{}), field.Int("version").Default(1)}, fieldx.Audit()...)
}

// Annotations 返回表名。
func (GleanTopic) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "glean_topic"}}
}
