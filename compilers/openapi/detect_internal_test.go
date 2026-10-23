package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers"
)

// TestDetectionBudget_ReadsTheCallersOptions pins the budget detection takes
// from each kind of options, another compiler's included, which mean this
// one's default as compilers.Compiler.Detect requires rather than no budget.
// Detect's own tests exceed only a budget the caller sets: a source past the
// default costs more than the gate's race run should pay, so another
// compiler's options read as no budget would pass them all.
func TestDetectionBudget_ReadsTheCallersOptions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		opts compilers.Options
		want int
	}{
		{"no options", compilers.Options{}, DefaultMaxSourceBytes},
		{"another compiler's options", compilers.Options{FormatOptions: "another compiler's options"}, DefaultMaxSourceBytes},
		{"a budget of its own", compilers.Options{FormatOptions: Options{Limits: Limits{MaxSourceBytes: 7}}}, 7},
		{"the budget turned off", compilers.Options{FormatOptions: Options{Limits: Limits{MaxSourceBytes: -1}}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, detectionBudget(tc.opts))
		})
	}
}
