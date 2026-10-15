package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// TestBesideUnionDiscriminator_ReportsWhatItCannotSettle pins that a settlement
// whose model is missing or is no model, or whose union's node is missing, is
// announced rather than skipped: the discriminator it was deferred for would
// otherwise be settled nowhere, unremarked.
func TestBesideUnionDiscriminator_ReportsWhatItCannotSettle(t *testing.T) {
	t.Parallel()
	for name, reg := range map[string]ir.TypeRegistry{
		"model missing":          {"t/owner": &ir.Union{ID: "t/owner"}},
		"no model":               {"t/owner": &ir.Union{ID: "t/owner"}, "t/ghost": &ir.Scalar{ID: "t/ghost"}},
		"owner missing":          {"t/ghost": &ir.Model{ID: "t/ghost", Discriminator: &ir.Discriminator{Mapping: map[string]ir.TypeID{"cat": "t/cat"}}}},
		"owner of a map missing": {"t/ghost": &ir.MapT{ID: "t/ghost"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			d := besideUnionDiscriminator{c: lowering.Ctx{}, pointer: "/components/schemas/Ghost", owner: "t/owner", model: "t/ghost"}

			diags := d.Settle(reg)

			require.Len(t, diags, 1)
			assert.Equal(t, diag.InternalInvariant, diags[0].Code)
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
		})
	}
}

// TestBesideUnionDiscriminator_LeavesASettledMapAlone pins the second
// settlement for one model: the first moved the discriminator and left a MapT,
// so the second has nothing to settle and reports nothing.
func TestBesideUnionDiscriminator_LeavesASettledMapAlone(t *testing.T) {
	t.Parallel()
	mt := &ir.MapT{ID: "t/pet", Value: ir.TypeRef{Target: "t/prim/string"}}
	reg := ir.TypeRegistry{"t/owner": &ir.Union{ID: "t/owner"}, "t/pet": mt}
	d := besideUnionDiscriminator{c: lowering.Ctx{}, pointer: "/components/schemas/Pet", owner: "t/owner", model: "t/pet"}

	assert.Empty(t, d.Settle(reg))
	assert.Same(t, mt, reg["t/pet"], "the map is left in place")
}
