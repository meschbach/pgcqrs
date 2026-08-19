package views

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestStatusStringFormatsValues verifies Status implements fmt.Stringer on value
// types (not just pointers), so fmt.Printf("%v", status) prints the human-readable
// form rather than a numeric value.
func TestStatusStringFormatsValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		status Status
		want   string
	}{
		{StatusOK, "ok"},
		{StatusNotFound, "not found"},
		{StatusStale, "stale"},
		{StatusTimeout, "timeout"},
	}

	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			// Test on value type (not pointer) — this is the regression case
			assert.Equal(t, tc.want, tc.status.String())
			// Test via fmt.Sprintf with %v on value type — verifies fmt.Stringer is satisfied
			//nolint:gocritic,staticcheck // Intentionally testing fmt.Stringer interface
			assert.Equal(t, tc.want, fmt.Sprintf("%v", tc.status))
			// Test via fmt.Sprintf with %s on value type
			//nolint:gocritic,staticcheck // Intentionally testing fmt.Stringer interface
			assert.Equal(t, tc.want, fmt.Sprintf("%s", tc.status))
		})
	}
}

// TestStatusUnknownValue verifies unknown Status values format as a numeric representation.
func TestStatusUnknownValue(t *testing.T) {
	t.Parallel()
	unknown := Status(99)
	// Should not panic, should return something
	result := unknown.String()
	assert.NotEmpty(t, result)
	assert.NotEqual(t, "ok", result)
}
