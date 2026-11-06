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

// GleanSearchRequest 每次调用追加独立请求，发送前持久化 pending，失败也保留。
type GleanSearchRequest struct{ ent.Schema }

// Fields 保存运行逻辑关联及脱敏请求结果。
func (GleanSearchRequest) Fields() []ent.Field {
	return append([]ent.Field{fieldx.ID(), field.Int64("run_id").GoType(idgen.ID(0)).Positive().Immutable(), field.String("query_id").NotEmpty().Immutable(), field.String("source").NotEmpty().Immutable(), field.String("status").NotEmpty(), field.JSON("payload", json.RawMessage{})}, fieldx.Audit()...)
}

// Indexes 覆盖运行关联和完整分页，避免被最左前缀覆盖的重复索引。
func (GleanSearchRequest) Indexes() []ent.Index { return []ent.Index{index.Fields("run_id", "id")} }

// Annotations 返回稳定表名。
func (GleanSearchRequest) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "glean_search_request"}}
}
