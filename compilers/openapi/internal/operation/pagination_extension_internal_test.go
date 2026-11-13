// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// TestOperationsOf_ReadsInOrderToTheGroupDepthBound holds the walk to
// declaration order, a group's own operations before those it nests, and to
// ir.MaxGroupDepth, as indexOperations is held: a group a level past the bound,
// deeper than any document the compiler builds, is not read.
func TestOperationsOf_ReadsInOrderToTheGroupDepthBound(t *testing.T) {
	t.Parallel()
	nested := func(id ir.OpID, level int) ir.OperationGroup {
		g := ir.OperationGroup{Operations: []ir.Operation{{ID: id}}}
		for range level {
			g = ir.OperationGroup{Groups: []ir.OperationGroup{g}}
		}
		return g
	}
	outer := nested("op/x/outer", 0)
	outer.Groups = []ir.OperationGroup{nested("op/x/inner", 0)}
	svc := &ir.Service{Groups: []ir.OperationGroup{
		outer, nested("op/x/cap", ir.MaxGroupDepth), nested("op/x/past", ir.MaxGroupDepth+1), nested("op/x/last", 0),
	}}
	ops := operationsOf(svc)
	got := make([]ir.OpID, 0, len(ops))
	for _, op := range ops {
		got = append(got, op.ID)
	}
	assert.Equal(t, []ir.OpID{"op/x/outer", "op/x/inner", "op/x/cap", "op/x/last"}, got)
}

// TestMarker_AUseOfATypeTheRegistryLacksMarksNothing drives the guard no
// document reaches: a use naming a type the registry does not hold, as a
// dangling reference would, is marked by its own keys alone.
func TestMarker_AUseOfATypeTheRegistryLacksMarksNothing(t *testing.T) {
	t.Parallel()
	c := newRawLowerer(&soa.OpenAPI{}).ctx
	pg := &pager{c: c, p: c.Promoter(), lk: newLookups(ir.TypeRegistry{}), flat: map[ir.TypeID][]*ir.Property{}}
	m := &marker{pg: pg, target: lowering.TargetPaginationItems,
		src: lowering.ExtensionSource{Key: "x-k", Values: []string{"items"}}}
	keeper, marked := m.marksUse(nil, &ir.Provenance{}, "t/gone")
	assert.Nil(t, keeper)
	assert.False(t, marked)
}

// TestPager_AParameterOnlyABindingNamesResolvesNothing drives the guard no
// document reaches: a binding naming a parameter its operation does not hold
// gives a member of it no type to walk, so the value resolves to nothing.
func TestPager_AParameterOnlyABindingNamesResolvesNothing(t *testing.T) {
	t.Parallel()
	c := newRawLowerer(&soa.OpenAPI{}).ctx
	pg := &pager{c: c, p: c.Promoter(), lk: newLookups(ir.TypeRegistry{}), flat: map[ir.TypeID][]*ir.Property{}}
	in, failure := pg.paramInput(&ir.Operation{ID: "op/x"}, "param/x/gone", []string{"a"})
	assert.Nil(t, in)
	assert.Contains(t, failure, "which a binding names and the operation does not hold")
}
