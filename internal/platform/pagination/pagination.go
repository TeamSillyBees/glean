package pagination

// PageQuery 描述 page/pageSize HTTP 查询参数，可嵌入 Huma request 结构体。
type PageQuery struct {
	Page     int32 `query:"page" example:"1" minimum:"1" default:"1" doc:"当前页码，从 1 开始，不传默认为 1"`
	PageSize int32 `query:"pageSize" example:"20" minimum:"1" default:"20" doc:"每页记录数，不传默认为 20"`
}

// Offset 将页码和每页数量换算为数据库查询偏移量。
func (p PageQuery) Offset() int32 {
	return (p.Page - 1) * p.PageSize
}

// Limit 返回当前分页查询要求的每页记录数。
func (p PageQuery) Limit() int32 {
	return p.PageSize
}

// PageMetaVO 描述带总数分页响应中的通用 JSON 字段，可嵌入业务 VO。
type PageMetaVO struct {
	Total      int32 `json:"total" doc:"总记录数"`
	Page       int32 `json:"page" doc:"当前页码"`
	PageSize   int32 `json:"pageSize" doc:"每页记录数"`
	TotalPages int32 `json:"totalPages" doc:"总页数"`
}

// PageVO 描述带总数的通用分页 JSON DTO。
type PageVO[T any] struct {
	Items []T `json:"items" doc:"当前页数据列表"`
	PageMetaVO
}

// NewPageVO 创建 page/pageSize 分页输出。
func NewPageVO[T any](items []T, total int32, query PageQuery) PageVO[T] {
	return PageVO[T]{
		Items:      normalizeItems(items),
		PageMetaVO: newPageMetaVO(total, query.Page, query.PageSize),
	}
}

// OffsetQuery 描述 offset/limit HTTP 查询参数，可嵌入 Huma request 结构体。
type OffsetQuery struct {
	Offset int32 `query:"offset" example:"0" minimum:"0" default:"0" doc:"偏移量，从 0 开始，不传默认为 0"`
	Limit  int32 `query:"limit" example:"20" minimum:"1" default:"20" doc:"返回记录数，不传默认为 20"`
}

// Page 返回 offset/limit 对应的当前页码。
func (p OffsetQuery) page() int32 {
	if p.Limit <= 0 {
		return 0
	}
	return p.Offset/p.Limit + 1
}

// NewOffsetVO 创建 offset/limit 分页输出，并按 offset/limit 推导当前页。
func NewOffsetVO[T any](items []T, total int32, query OffsetQuery) PageVO[T] {
	return PageVO[T]{
		Items:      normalizeItems(items),
		PageMetaVO: newPageMetaVO(total, query.page(), query.Limit),
	}
}

// CursorQuery 描述 cursor pagination HTTP 查询参数，可嵌入 Huma request 结构体。
type CursorQuery struct {
	Cursor string `query:"cursor" example:"eyJpZCI6MTAwMX0" doc:"分页游标，首次查询不传或传空字符串"`
	Limit  int32  `query:"limit" example:"20" minimum:"1" default:"20" doc:"返回记录数，不传默认为 20"`
}

// CursorMetaVO 描述 cursor pagination 响应中的通用 JSON 字段。
type CursorMetaVO struct {
	NextCursor string `json:"nextCursor,omitempty" doc:"下一页游标，为空字符串表示已是最后一页"`
	HasMore    bool   `json:"hasMore" doc:"是否还有更多数据"`
	Limit      int32  `json:"limit" doc:"每页记录数"`
}

// CursorVO 描述 cursor pagination 通用 JSON DTO。
type CursorVO[T any] struct {
	Items []T `json:"items" doc:"当前页数据列表"`
	CursorMetaVO
}

// NewCursorVO 创建 cursor pagination 输出。
func NewCursorVO[T any](items []T, nextCursor string, hasMore bool, query CursorQuery) CursorVO[T] {
	return CursorVO[T]{
		Items: normalizeItems(items),
		CursorMetaVO: CursorMetaVO{
			NextCursor: nextCursor,
			HasMore:    hasMore,
			Limit:      query.Limit,
		},
	}
}

func newPageMetaVO(total int32, page int32, pageSize int32) PageMetaVO {
	if total < 0 {
		total = 0
	}
	return PageMetaVO{
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages(total, pageSize),
	}
}

func totalPages(total int32, pageSize int32) int32 {
	if total <= 0 || pageSize <= 0 {
		return 0
	}
	return int32((total + int32(pageSize) - 1) / int32(pageSize))
}

func normalizeItems[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}
