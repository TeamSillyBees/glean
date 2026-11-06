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

// GleanSearchHit 追加保存来源版本的检索路径，不参与原始证据身份。
type GleanSearchHit struct{ ent.Schema }

// Fields 定义检索命中及审计字段。
func (GleanSearchHit) Fields() []ent.Field {
	return append([]ent.Field{fieldx.ID(), field.Int64("observation_id").GoType(idgen.ID(0)).Positive().Immutable(), field.Int64("run_id").GoType(idgen.ID(0)).Positive().Immutable(), field.String("query_id").NotEmpty().Immutable(), field.String("query_version").NotEmpty().Immutable(), field.String("plan_version").NotEmpty().Immutable(), field.String("source").NotEmpty().Immutable(), field.Text("query_text").Immutable(), field.String("mode").NotEmpty().Immutable(), field.Time("discovered_at").Immutable()}, fieldx.Audit()...)
}

// Indexes 保护幂等键并覆盖逻辑关联，不创建物理外键。
func (GleanSearchHit) Indexes() []ent.Index {
	return []ent.Index{index.Fields("observation_id", "run_id", "query_id", "query_version").Unique(), index.Fields("run_id")}
}

// Annotations 固定数据表名称。
func (GleanSearchHit) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "glean_search_hit"}}
}
