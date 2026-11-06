package schema

import (
	"encoding/json"
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/teamsillybees/glean/internal/platform/entx/fieldx"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// GleanSearchOccurrence 保存来源原始出现，跨查询和重试均不覆盖旧记录。
type GleanSearchOccurrence struct{ ent.Schema }

// Fields 将出现、实际请求和规范化记录分别关联。
func (GleanSearchOccurrence) Fields() []ent.Field {
	return append([]ent.Field{fieldx.ID(), field.Int64("run_id").GoType(idgen.ID(0)).Positive().Immutable(), field.Int64("request_id").GoType(idgen.ID(0)).Positive().Immutable(), field.Int("ordinal").NonNegative().Immutable(), field.String("source").NotEmpty().Immutable(), field.String("source_id").Immutable(), field.String("record_key").Immutable(), field.JSON("payload", json.RawMessage{}).Immutable()}, fieldx.Audit()...)
}

// Indexes 保护同次响应写入幂等，覆盖两个逻辑父关联。
func (GleanSearchOccurrence) Indexes() []ent.Index {
	return []ent.Index{index.Fields("request_id", "ordinal").Unique(), index.Fields("run_id", "id")}
}

// Annotations 返回稳定表名。
func (GleanSearchOccurrence) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "glean_search_occurrence"}}
}
