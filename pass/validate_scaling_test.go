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
// than timed, at three sizes: the growth from 2n to 4n is at most 2.5 times the
// growth from n to 2n, so fixed costs cancel, linear work gives 2 and a walk of
// the chain per route 4. It does not run in parallel, since the count is
// process-wide.
func TestValidate_DiscriminatorRoutesCostLinearAllocations(t *testing.T) {
	var allocs [3]float64
	for k, n := range []int{500, 1000, 2000} {
		var rejected int
		allocs[k], rejected = validateAllocations(t, n)
		require.Equal(t, n, rejected, "every base's route must reach the subtype question, or the count compares nothing")
	}
	assert.LessOrEqual(t, allocs[2]-allocs[1], 2.5*(allocs[1]-allocs[0]),
		"allocations at 500, 1000, 2000: %v", allocs)
}
