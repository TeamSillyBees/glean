package entx

// 审计与软删除字段名是 Ent hook 和业务 schema 之间的稳定契约。
const (
	// FieldID 是实体主键字段名。
	FieldID = "id"
	// FieldCreatedAt 是实体创建时间字段名。
	FieldCreatedAt = "created_at"
	// FieldUpdatedAt 是实体更新时间字段名。
	FieldUpdatedAt = "updated_at"
	// FieldCreatedBy 是实体创建人字段名。
	FieldCreatedBy = "created_by"
	// FieldUpdatedBy 是实体更新人字段名。
	FieldUpdatedBy = "updated_by"
	// FieldDeletedAt 是实体软删除时间字段名。
	FieldDeletedAt = "deleted_at"
)
