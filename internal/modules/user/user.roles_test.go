package user

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNormalizeRoleCodes 验证角色编码去空白、去重、保序和默认角色规则。
func TestNormalizeRoleCodes(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{name: "nil uses viewer", want: []string{"viewer"}},
		{name: "blank values use viewer", input: []string{"", "  "}, want: []string{"viewer"}},
		{name: "trim deduplicate and preserve order", input: []string{" admin ", "viewer", "admin", ""}, want: []string{"admin", "viewer"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, normalizeRoleCodes(tt.input))
		})
	}
}
