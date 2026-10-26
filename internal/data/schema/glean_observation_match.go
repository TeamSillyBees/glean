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

// GleanObservationMatch 保存只追加的真实来源证据，来源版本与科研判断分离。
type GleanObservationMatch struct{ ent.Schema }

// Fields 返回证据字段及审计字段。
func (GleanObservationMatch) Fields() []ent.Field {
	fields := []ent.Field{fieldx.ID(), field.Int64("observation_id").GoType(idgen.ID(0)).Positive(), field.String("topic").NotEmpty(), field.String("template_version").Default("")}
	return append(fields, fieldx.Audit()...)
}

// Indexes 返回幂等键和逻辑关联索引。
func (GleanObservationMatch) Indexes() []ent.Index {
	return []ent.Index{index.Fields("observation_id", "topic").Unique(), index.Fields("topic")}
}

// Annotations 固定无物理外键的数据表名称。
func (GleanObservationMatch) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "glean_observation_match"}}
}
