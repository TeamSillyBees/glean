package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/teamsillybees/glean/internal/platform/entx/fieldx"
	"github.com/teamsillybees/glean/internal/platform/idgen"
	"github.com/teamsillybees/glean/internal/retrieval"
)

// GleanPaperReview 保存人工管理覆盖层，不修改原始来源证据。
type GleanPaperReview struct{ ent.Schema }

// Fields 返回字段定义。
func (GleanPaperReview) Fields() []ent.Field {
	return append([]ent.Field{fieldx.ID(), field.Int64("paper_id").GoType(idgen.ID(0)).Positive(), field.String("topic").NotEmpty(), field.Enum("status").Values("pending", "included", "excluded"), field.Text("note").Default(""), field.JSON("labels", []string{}).Default([]string{}), field.JSON("evidence", &retrieval.ScreeningEvidence{}).Optional(), field.Int("version").Default(1)}, fieldx.Audit()...)
}

// Annotations 返回表名。
func (GleanPaperReview) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "glean_paper_review"}}
}

// Indexes 返回逻辑关联索引。
func (GleanPaperReview) Indexes() []ent.Index {
	return []ent.Index{index.Fields("paper_id", "topic").Unique(), index.Fields("topic", "status")}
}
