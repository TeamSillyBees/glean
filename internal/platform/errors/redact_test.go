package apperrors

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

type sanitizeCredentialFixture struct {
	Token string `mapstructure:"token"`
	Name  string `mapstructure:"name"`
}

type sanitizeNodeFixture struct {
	Next *sanitizeNodeFixture `json:"next"`
}

type sanitizeConfigFixture struct {
	DSN      string                               `mapstructure:"dsn"`
	Proxy    string                               `mapstructure:"proxy"`
	Raw      []byte                               `mapstructure:"raw"`
	Nested   *sanitizeCredentialFixture           `json:"nested"`
	Services map[string]sanitizeCredentialFixture `json:"services"`
	Cycle    *sanitizeNodeFixture                 `json:"cycle"`
	Deep     *sanitizeNodeFixture                 `json:"deep"`
}

func TestSanitizeValueHandlesStructPointersTypedMapsAndCycles(t *testing.T) {
	cycle := &sanitizeNodeFixture{}
	cycle.Next = cycle
	deep := &sanitizeNodeFixture{}
	cursor := deep
	for index := 0; index < maxSanitizeDepth+2; index++ {
		cursor.Next = &sanitizeNodeFixture{}
		cursor = cursor.Next
	}

	got := SanitizeValue("config", sanitizeConfigFixture{
		DSN:      "postgres://user:password@localhost/database",
		Proxy:    "https://proxy-user:proxy-password@proxy.example.test",
		Raw:      []byte("password=byte-secret"),
		Nested:   &sanitizeCredentialFixture{Token: "nested-secret", Name: "safe"},
		Services: map[string]sanitizeCredentialFixture{"primary": {Token: "map-secret", Name: "visible"}},
		Cycle:    cycle,
		Deep:     deep,
	}).(map[string]any)

	require.Equal(t, redactedValue, got["dsn"])
	require.NotContains(t, got["proxy"], "proxy-password")
	require.Equal(t, redactedValue, got["raw"])
	require.Equal(t, redactedValue, got["nested"].(map[string]any)["token"])
	require.Equal(t, "safe", got["nested"].(map[string]any)["name"])
	primary := got["services"].(map[string]any)["primary"].(map[string]any)
	require.Equal(t, redactedValue, primary["token"])
	require.Equal(t, "visible", primary["name"])
	encoded, err := json.Marshal(got)
	require.NoError(t, err)
	require.Contains(t, string(encoded), redactedValue)
}
