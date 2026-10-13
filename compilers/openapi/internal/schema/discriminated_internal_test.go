package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// TestBesideUnionDiscriminator_ReportsWhatIsNoModel pins that a settlement
// whose model is missing, or is no model, is announced rather than skipped:
// the discriminator it was deferred for would otherwise vanish unremarked.
func TestBesideUnionDiscriminator_ReportsWhatIsNoModel(t *testing.T) {
	t.Parallel()
	for name, reg := range map[string]ir.TypeRegistry{
		"missing":  {},
		"no model": {"t/ghost": &ir.Scalar{ID: "t/ghost"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			d := besideUnionDiscriminator{c: lowering.Ctx{}, pointer: "/components/schemas/Ghost", model: "t/ghost"}

			diags := d.Settle(reg)

			require.Len(t, diags, 1)
			assert.Equal(t, diag.InternalInvariant, diags[0].Code)
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
		})
	}
}
