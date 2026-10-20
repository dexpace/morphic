package pass_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// chainedDiscriminators is the document of GitHub #850: n+1 models chained by
// Base, and n bases, each routing a discriminator mapping to the bottom of the
// chain. Validate asks, for every base, whether that model is its subtype, and
// the answer is no.
func chainedDiscriminators(n int) *ir.Document {
	chain := func(i int) ir.TypeID { return ir.TypeID(fmt.Sprintf("t/c%05d", i)) }
	types := ir.TypeRegistry{chain(0): &ir.Model{ID: chain(0)}}
	for i := 1; i <= n; i++ {
		types[chain(i)] = &ir.Model{ID: chain(i), Base: &ir.TypeRef{Target: chain(i - 1)}}
	}
	for k := range n {
		id := ir.TypeID(fmt.Sprintf("t/m%05d", k))
		types[id] = &ir.Model{ID: id, Discriminator: &ir.Discriminator{
			Mapping: map[string]ir.TypeID{"x": chain(n)},
		}}
	}
	return &ir.Document{IRVersion: ir.IRVersion, Name: "t", Version: "1", Types: types}
}

// validateAllocations returns how many allocations one Validate of the chained
// document of size n makes, and how many routes it reported as not subtypes.
func validateAllocations(t *testing.T, n int) (allocs float64, rejected int) {
	t.Helper()
	doc := chainedDiscriminators(n)
	allocs = testing.AllocsPerRun(2, func() { _ = pass.Validate(doc) })
	for _, d := range pass.Validate(doc) {
		if d.Code == "pass/discriminator-missing-variant" {
			rejected++
		}
	}
	return allocs, rejected
}

// TestValidate_DiscriminatorRoutesCostLinearAllocations holds the work of
// validating #850's document to linear growth, counted in allocations rather
// than timed. A walk of the chain per route allocates about four times as much
// at double the size; one walk per target, however many bases route to it,
// allocates about twice as much. It does not run in parallel, since the count
// is process-wide.
func TestValidate_DiscriminatorRoutesCostLinearAllocations(t *testing.T) {
	small, rejectedSmall := validateAllocations(t, 1000)
	large, rejectedLarge := validateAllocations(t, 2000)

	require.Equal(t, 1000, rejectedSmall, "every base's route must reach the subtype question, or the count compares nothing")
	require.Equal(t, 2000, rejectedLarge)
	assert.LessOrEqual(t, large, 2.5*small,
		"doubling the document must roughly double the allocations: %.0f at 1000, %.0f at 2000", small, large)
}
