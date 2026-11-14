// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// forkedGroups returns a service's groups nested depth levels, each level two
// groups sharing the slice of the level below, so the tree the encoding writes
// has 2^(depth+1)-2 groups.
func forkedGroups(depth int) *ir.Document {
	var g []ir.OperationGroup
	for i := range depth {
		g = []ir.OperationGroup{
			{ID: ir.GroupID(fmt.Sprintf("g/x/a%d", i)), Groups: g},
			{ID: ir.GroupID(fmt.Sprintf("g/x/b%d", i)), Groups: g},
		}
	}
	return &ir.Document{Services: []ir.Service{{ID: "s/x", Groups: g}}}
}

// groupsVisited returns how many groups ForEachGroupWithin calls fn for within
// limit, and its result.
func groupsVisited(limit int, doc *ir.Document) (int, bool) {
	var calls int
	cut := ir.ForEachGroupWithin(limit, doc, func(ir.OperationGroup) { calls++ })
	return calls, cut
}

// TestForEachGroup_ReadsToItsLimit holds a walk of groups to one step each: a
// document whose nested groups share their slices is read from each holder, to
// the limit and no further, and the result says whether any went unread.
func TestForEachGroup_ReadsToItsLimit(t *testing.T) {
	t.Parallel()
	doc := forkedGroups(10)

	var all int
	assert.False(t, ir.ForEachGroup(doc, func(ir.OperationGroup) { all++ }), "the real limit holds 2,046 groups")
	assert.Equal(t, 2046, all)

	calls, cut := groupsVisited(1000, doc)
	assert.Equal(t, 1000, calls)
	assert.True(t, cut)

	calls, cut = groupsVisited(2046, doc)
	assert.Equal(t, 2046, calls, "a limit of the groups there are reads them all")
	assert.False(t, cut)

	calls, cut = groupsVisited(2045, doc)
	assert.Equal(t, 2045, calls)
	assert.True(t, cut, "one fewer leaves a group unread")
}

// TestForEachOperation_ChargesOperationsAndGroups holds ForEachOperation to a
// step for each group and one for each operation: ten operations in one group
// cost eleven, and a limit of six reads the group and five of them.
func TestForEachOperation_ChargesOperationsAndGroups(t *testing.T) {
	t.Parallel()
	ops := make([]ir.Operation, 10)
	for i := range ops {
		ops[i] = ir.Operation{ID: ir.OpID(fmt.Sprintf("op/x/o%d", i))}
	}
	doc := &ir.Document{Services: []ir.Service{{ID: "s/x", Groups: []ir.OperationGroup{{ID: "g/x/a", Operations: ops}}}}}

	for _, tc := range []struct {
		limit   int
		want    int
		wantCut bool
	}{
		{6, 5, true},
		{11, 10, false},
		{10, 9, true},
	} {
		var read []ir.OpID
		cut := ir.ForEachOperationWithin(tc.limit, doc, func(op ir.Operation) { read = append(read, op.ID) })

		assert.Len(t, read, tc.want, "limit %d: operations read", tc.limit)
		assert.Equal(t, tc.wantCut, cut, "limit %d", tc.limit)
	}
}

// ctorFork returns a literal whose constructors nest depth levels, each taking
// the level below twice as arguments, so it holds 2^(depth+1)-1 constructors
// and yields as many edges.
func ctorFork(depth int) *ir.Literal {
	c := &ir.CtorValue{Scalar: "t/s"}
	for range depth {
		cv := ir.Value{Kind: ir.ValueCtor, Ctor: c}
		c = &ir.CtorValue{Scalar: "t/s", Args: []ir.Value{cv, cv}}
	}
	lit := &ir.Literal{Value: ir.Value{Kind: ir.ValueCtor, Ctor: c}}
	lit.ID = "t/x/L"
	return lit
}

// TestTypeEdges_ReadsEveryHolderUpToItsLimit holds TypeEdges to the whole fork
// of shared constructors at the real limit: one edge per constructor.
func TestTypeEdges_ReadsEveryHolderUpToItsLimit(t *testing.T) {
	t.Parallel()
	assert.Len(t, slices.Collect(ir.TypeEdges(ctorFork(10))), 2047)
}

// TestTypeEdges_ACutIsTheWalksCut holds a typed walk to being cut only where
// the reflection walk is: wherever TypeEdges yields fewer edges than the
// document has, or ForEachGroup returns true, a walk showing a visitor that
// prunes nothing eight values a step stopped as well. The lowest limit is
// reached by both typed walks, so the implication is not held vacuously.
func TestTypeEdges_ACutIsTheWalksCut(t *testing.T) {
	t.Parallel()
	lit := ctorFork(10)
	groups := forkedGroups(10)
	doc := &ir.Document{
		Types:    ir.TypeRegistry{lit.ID: lit},
		Services: groups.Services,
	}
	require.Len(t, slices.Collect(ir.TypeEdges(lit)), 2047, "the whole of the fork")

	var reachedEdges, reachedGroups bool
	for _, limit := range []int{1 << 8, 1 << 12} {
		edgesCut := len(slices.Collect(ir.TypeEdgesWithin(limit, lit))) < 2047
		_, groupsCut := groupsVisited(limit, doc)

		reachedEdges = reachedEdges || edgesCut
		reachedGroups = reachedGroups || groupsCut
		if edgesCut || groupsCut {
			assert.True(t, ir.WalkValuesWithin(8*limit, doc, "doc", &shownCounter{}),
				"limit %d: a typed walk was cut, so the walk of eight values a step is", limit)
		}
	}
	assert.True(t, reachedEdges, "a limit must cut TypeEdges, or the implication is not tested for it")
	assert.True(t, reachedGroups, "a limit must cut ForEachGroup, or the implication is not tested for it")
}

// TestMaxWalkSteps_AStepIsEightValues holds the two limits to the exchange
// that lets a typed walk's cut imply the reflection walk's: a node a step
// counts holds at least seven fields, which the reflection walk shows a
// visitor as at least eight values, and the steps' limit is no more than the
// values' limit divided by eight.
func TestMaxWalkSteps_AStepIsEightValues(t *testing.T) {
	t.Parallel()
	for _, node := range []reflect.Type{reflect.TypeFor[ir.Value](), reflect.TypeFor[ir.OperationGroup](), reflect.TypeFor[ir.Operation]()} {
		assert.GreaterOrEqual(t, node.NumField(), 7, "%s", node)
	}
	assert.LessOrEqual(t, 8*ir.MaxWalkSteps, ir.MaxWalkValues)
	assert.Positive(t, ir.MaxWalkSteps)
}
