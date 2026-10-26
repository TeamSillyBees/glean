package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/teamsillybees/glean/internal/platform/entx/fieldx"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// GleanPaperState 保存 Glean 的持久化业务记录，禁止隐式物理删除。
type GleanPaperState struct{ ent.Schema }

// Fields 返回字段定义。
func (GleanPaperState) Fields() []ent.Field {
	fields := []ent.Field{fieldx.ID(),
		field.Int64("paper_id").GoType(idgen.ID(0)).Positive(), field.Int64("user_id").GoType(idgen.ID(0)).Positive(),
		field.Bool("hidden").Default(false), field.String("read_status").Default("unread"),
	}
	return append(fields, fieldx.Audit()...)
}

// Indexes 返回逻辑关联和查询索引。
func (GleanPaperState) Indexes() []ent.Index {
	return []ent.Index{index.Fields("user_id", "paper_id").Unique(), index.Fields("paper_id")}
}

// Annotations 返回无物理外键的数据表元信息。
func (GleanPaperState) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "glean_paper_state"}}
}
