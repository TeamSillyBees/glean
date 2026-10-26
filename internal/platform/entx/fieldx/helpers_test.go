package fieldx

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIDDescriptorUsesSnowflakeDefaults(t *testing.T) {
	descriptor := ID().Descriptor()

	require.NoError(t, descriptor.Err)
	require.Equal(t, "id", descriptor.Name)
	require.True(t, descriptor.Immutable)
	require.NotEmpty(t, descriptor.Validators)
	require.NotNil(t, descriptor.Default)
}

func TestAuditDescriptorsKeepExpectedLifecycle(t *testing.T) {
	fields := Audit()
	require.Len(t, fields, 4)

	createdAt := fields[0].Descriptor()
	updatedAt := fields[1].Descriptor()
	createdBy := fields[2].Descriptor()
	updatedBy := fields[3].Descriptor()
	require.Equal(t, "created_at", createdAt.Name)
	require.True(t, createdAt.Immutable)
	require.NotNil(t, createdAt.Default)
	require.Equal(t, "updated_at", updatedAt.Name)
	require.NotNil(t, updatedAt.UpdateDefault)
	require.True(t, createdBy.Optional)
	require.True(t, createdBy.Nillable)
	require.True(t, updatedBy.Optional)
	require.True(t, updatedBy.Nillable)
}

func TestSoftDeleteDescriptorIsOptionalAndNillable(t *testing.T) {
	fields := SoftDelete()
	require.Len(t, fields, 1)

	descriptor := fields[0].Descriptor()
	require.Equal(t, "deleted_at", descriptor.Name)
	require.True(t, descriptor.Optional)
	require.True(t, descriptor.Nillable)
}
