package indexx

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSoftDeleteIndexesDeletedAt(t *testing.T) {
	descriptor := SoftDelete().Descriptor()

	require.Equal(t, []string{"deleted_at"}, descriptor.Fields)
	require.False(t, descriptor.Unique)
}
