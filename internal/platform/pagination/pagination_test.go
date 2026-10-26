package pagination

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewPageVOCalculatesMetadataAndNormalizesItems(t *testing.T) {
	result := NewPageVO[string](nil, 41, PageQuery{Page: 2, PageSize: 20})

	require.Empty(t, result.Items)
	require.NotNil(t, result.Items)
	require.Equal(t, int32(41), result.Total)
	require.Equal(t, int32(3), result.TotalPages)
	require.Equal(t, int32(20), PageQuery{Page: 2, PageSize: 20}.Offset())
}

func TestNewOffsetVOHandlesInvalidLimit(t *testing.T) {
	result := NewOffsetVO([]int{1}, -1, OffsetQuery{Offset: 10, Limit: 0})

	require.Equal(t, int32(0), result.Total)
	require.Equal(t, int32(0), result.Page)
	require.Equal(t, int32(0), result.TotalPages)
}

func TestNewCursorVONormalizesNilItems(t *testing.T) {
	result := NewCursorVO[int](nil, "next", true, CursorQuery{Limit: 10})

	require.NotNil(t, result.Items)
	require.Equal(t, "next", result.NextCursor)
	require.True(t, result.HasMore)
	require.Equal(t, int32(10), result.Limit)
}
