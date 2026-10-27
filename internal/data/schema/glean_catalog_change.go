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

// GleanCatalogChange 保存人工管理覆盖层，不修改原始来源证据。
type GleanCatalogChange struct{ ent.Schema }

// Fields 返回字段定义。
func (GleanCatalogChange) Fields() []ent.Field {
	return append([]ent.Field{fieldx.ID(), field.Int64("paper_id").GoType(idgen.ID(0)).Default(0).Immutable(), field.String("topic").Default("").Immutable(), field.String("kind").NotEmpty().Immutable(), field.JSON("before", map[string]any{}).Default(map[string]any{}).Immutable(), field.JSON("after", map[string]any{}).Default(map[string]any{}).Immutable()}, fieldx.Audit()...)
}

// Annotations 返回表名。
func (GleanCatalogChange) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "glean_catalog_change"}}
}

// Indexes 返回逻辑关联索引。
func (GleanCatalogChange) Indexes() []ent.Index {
	return []ent.Index{index.Fields("paper_id", "id"), index.Fields("topic", "id")}
}
