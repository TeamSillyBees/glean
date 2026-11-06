package schema

import (
	"encoding/json"
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/teamsillybees/glean/internal/platform/entx/fieldx"
)

// GleanSearchRun 长期保存冻结运行，不受监控热快照保留条数影响。
type GleanSearchRun struct{ ent.Schema }

// Fields 定义运行归属和版本化账本。
func (GleanSearchRun) Fields() []ent.Field {
	return append([]ent.Field{fieldx.ID(), field.String("state_key").NotEmpty().Immutable(), field.JSON("payload", json.RawMessage{})}, fieldx.Audit()...)
}

// Indexes 覆盖按实例逐页读取的稳定游标。
func (GleanSearchRun) Indexes() []ent.Index { return []ent.Index{index.Fields("state_key", "id")} }

// Annotations 返回稳定表名。
func (GleanSearchRun) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "glean_search_run"}}
}
