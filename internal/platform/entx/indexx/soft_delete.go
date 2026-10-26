package indexx

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/index"
)

// SoftDelete 返回逻辑删除常用索引。
func SoftDelete() ent.Index {
	return index.Fields("deleted_at")
}
