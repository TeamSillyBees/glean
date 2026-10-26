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

// GleanObservation 保存只追加的真实来源证据，来源版本与科研判断分离。
type GleanObservation struct{ ent.Schema }

// Fields 返回证据字段及审计字段。
func (GleanObservation) Fields() []ent.Field {
	fields := []ent.Field{fieldx.ID(), field.Int64("paper_id").GoType(idgen.ID(0)).Positive(), field.Int64("run_id").GoType(idgen.ID(0)).Positive(),
		field.String("source").NotEmpty(), field.String("source_id").NotEmpty(), field.String("version").Default(""), field.String("content_hash").NotEmpty(),
		field.Time("observed_at"), field.Time("source_updated_at").Optional().Nillable(), field.JSON("metadata", json.RawMessage{}), field.JSON("publication_events", []map[string]any{}).Default([]map[string]any{}), field.JSON("review", map[string]any{})}
	return append(fields, fieldx.Audit()...)
}

// Indexes 返回幂等键和逻辑关联索引。
func (GleanObservation) Indexes() []ent.Index {
	return []ent.Index{index.Fields("source", "source_id", "content_hash").Unique(), index.Fields("paper_id", "observed_at", "id"), index.Fields("run_id")}
}

// Annotations 固定无物理外键的数据表名称。
func (GleanObservation) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "glean_observation"}}
}
