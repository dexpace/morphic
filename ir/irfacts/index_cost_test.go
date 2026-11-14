// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irfacts_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
)

// hierarchyOf returns a registry of n models, each the base of twelve subtypes,
// each base declaring one property.
func hierarchyOf(n int) *ir.Document {
	doc := &ir.Document{Types: ir.TypeRegistry{}}
	for i := range n {
		base := ir.TypeID(fmt.Sprintf("t/x/M%d", i))
		doc.Types[base] = &ir.Model{ID: base, Properties: []ir.Property{{ID: ir.PropID(fmt.Sprintf("p/x/M%d/a", i))}}}
		for j := range 12 {
			sub := ir.TypeID(fmt.Sprintf("t/x/M%d/S%d", i, j))
			doc.Types[sub] = &ir.Model{ID: sub, Base: &ir.TypeRef{Target: base}}
		}
	}
	return doc
}

// TestIndex_AWalkedQuestionAllocatesNothing holds a question the index answers
// by walking, before it has indexed the registry, to allocating nothing beyond
// the index's own construction. It is not run in parallel: AllocsPerRun
// measures the process.
func TestIndex_AWalkedQuestionAllocatesNothing(t *testing.T) {
	doc := hierarchyOf(50)
	var kept *irfacts.Index // an Index left unused is inlined onto the stack and measures nothing
	construction := testing.AllocsPerRun(10, func() { kept = irfacts.New(doc) })
	require.NotNil(t, kept)

	subtype := testing.AllocsPerRun(10, func() { _ = irfacts.New(doc).IsSubtype("t/x/M0/S0", "t/x/M0") })
	exposes := testing.AllocsPerRun(10, func() { _ = irfacts.New(doc).Exposes("t/x/M0/S0", "p/x/M0/a") })

	assert.Equal(t, construction, subtype, "IsSubtype beyond New's allocations")
	assert.Equal(t, construction, exposes, "Exposes beyond New's allocations")
}

// TestIndex_TheHierarchyGrowsByLessThanAnAllocationATypeAdded holds the cost of
// building the hierarchy to steps rather than per type: a registry twice the
// size costs fewer allocations more than the types it adds. It is not run in
// parallel: AllocsPerRun measures the process.
func TestIndex_TheHierarchyGrowsByLessThanAnAllocationATypeAdded(t *testing.T) {
	allocs := map[int]float64{}
	for _, n := range []int{50, 100, 200} {
		doc := hierarchyOf(n)
		allocs[n] = testing.AllocsPerRun(5, func() { _ = irfacts.New(doc).Subtypes("t/x/M0") })
	}

	for _, n := range []int{50, 100} {
		added := float64(13 * n) // each base and its twelve subtypes
		perType := (allocs[2*n] - allocs[n]) / added
		assert.Less(t, perType, 1.0, "from %d models to %d: %v allocations to %v, %.2f a type added",
			n, 2*n, allocs[n], allocs[2*n], perType)
	}
}
