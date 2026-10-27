package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/teamsillybees/glean/internal/platform/entx/fieldx"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// GleanPaperCorrection 保存人工管理覆盖层，不修改原始来源证据。
type GleanPaperCorrection struct{ ent.Schema }

// Fields 返回字段定义。
func (GleanPaperCorrection) Fields() []ent.Field {
	return append([]ent.Field{fieldx.ID(), field.Int64("paper_id").GoType(idgen.ID(0)).Positive().Unique(), field.JSON("fields", map[string]any{}).Default(map[string]any{}), field.Text("reason").Default(""), field.Int("version").Default(1)}, fieldx.Audit()...)
}

// Annotations 返回表名。
func (GleanPaperCorrection) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "glean_paper_correction"}}
}
