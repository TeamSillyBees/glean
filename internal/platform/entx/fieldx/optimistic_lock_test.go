package fieldx

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOptimisticLockVersionOnlyDefinesVersionField(t *testing.T) {
	descriptor := OptimisticLockVersion().Descriptor()

	require.NoError(t, descriptor.Err)
	require.Equal(t, "version", descriptor.Name)
	require.EqualValues(t, 1, descriptor.Default)
	require.Nil(t, descriptor.UpdateDefault)
}
