package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/teamsillybees/glean/internal/platform/entx/fieldx"
)

// GleanPaper 保存 Glean 的持久化业务记录，禁止隐式物理删除。
type GleanPaper struct{ ent.Schema }

// Fields 返回字段定义。
func (GleanPaper) Fields() []ent.Field {
	fields := []ent.Field{fieldx.ID(),
		field.String("identity_key").NotEmpty().Unique(),
		field.String("origin").Default("historical"),
		field.String("title").NotEmpty(), field.Text("title_zh").Default(""), field.Text("abstract").Default(""),
		field.JSON("authors", []string{}).Default([]string{}), field.Int("year").Default(0),
		field.String("venue").Default(""), field.String("publication_type").Default(""),
		field.String("doi").Optional().Nillable().Unique(), field.String("arxiv_id").Optional().Nillable().Unique(),
		field.Text("url").Default(""), field.Time("formal_online_date").Optional().Nillable(), field.Time("issue_date").Optional().Nillable(),
		field.Text("date_provenance").Default(""), field.JSON("evidence", []map[string]any{}).Default([]map[string]any{}),
		field.Bool("is_mock").Default(false),
	}
	return append(fields, fieldx.Audit()...)
}

// Indexes 返回逻辑关联和查询索引。
func (GleanPaper) Indexes() []ent.Index {
	return []ent.Index{index.Fields("year", "id"), index.Fields("venue"), index.Fields("formal_online_date", "id")}
}

// Annotations 返回无物理外键的数据表元信息。
func (GleanPaper) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "glean_paper"}}
}
