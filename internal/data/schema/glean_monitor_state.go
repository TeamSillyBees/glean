package schema

import (
	"encoding/json"
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"github.com/teamsillybees/glean/internal/platform/entx/fieldx"
)

// GleanMonitorState 保存 Glean 的持久化业务记录，禁止隐式物理删除。
type GleanMonitorState struct{ ent.Schema }

// Fields 返回字段定义。
func (GleanMonitorState) Fields() []ent.Field {
	fields := []ent.Field{fieldx.ID(),
		field.String("key").NotEmpty().Unique(), field.JSON("snapshot", json.RawMessage{}), field.Int("revision").Default(0),
	}
	return append(fields, fieldx.Audit()...)
}

// Indexes 返回逻辑关联和查询索引。
func (GleanMonitorState) Indexes() []ent.Index { return []ent.Index{} }

// Annotations 返回无物理外键的数据表元信息。
func (GleanMonitorState) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "glean_monitor_state"}}
}
