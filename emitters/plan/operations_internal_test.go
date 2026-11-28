// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package plan

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// nestedOperation returns a chain of depth groups, none stating a tier, the
// innermost holding the operation id.
func nestedOperation(id ir.OperationID, depth int) ir.OperationGroup {
	chain := ir.OperationGroup{ID: ir.GroupID(fmt.Sprintf("group/%s/0", id)), Operations: []ir.Operation{{ID: id}}}
	for level := 1; level < depth; level++ {
		chain = ir.OperationGroup{ID: ir.GroupID(fmt.Sprintf("group/%s/%d", id, level)), Groups: []ir.OperationGroup{chain}}
	}
	return chain
}

// tierScans returns the groups the tier scans of one walk read over doc.
func tierScans(t *testing.T, doc *ir.Document) int {
	t.Helper()
	walk := operationWalk{}
	require.False(t, ir.ForEachOperationAt(doc, walk.plan), "the walk reads the whole document")
	return walk.steps
}

// TestOperationWalk_CostGrowsWithTheOperations counts the groups the tier
// scans read for n, 2n and 4n operations, each under a chain of 8 groups
// stating no tier, and holds their growth to the operations': each scan reads
// only the groups enclosing its operation, where scanning every group of the
// service would take the square of n. One operation at the depth cap reads
// each group enclosing it once.
func TestOperationWalk_CostGrowsWithTheOperations(t *testing.T) {
	t.Parallel()
	const n, depth = 200, 8
	scans := func(size int) int {
		groups := make([]ir.OperationGroup, 0, size)
		for i := range size {
			groups = append(groups, nestedOperation(ir.OperationID(fmt.Sprintf("operation/%d", i)), depth))
		}
		return tierScans(t, &ir.Document{Services: []ir.Service{{Groups: groups}}})
	}
	small, double, large := scans(n), scans(2*n), scans(4*n)

	require.Positive(t, double-small, "the scans read more groups over more operations")
	assert.LessOrEqual(t, float64(large-double), 2.5*float64(double-small),
		"%d, %d and %d groups read at %d, %d and %d operations", small, double, large, n, 2*n, 4*n)

	deepest := nestedOperation("operation/deepest", ir.MaxGroupDepth+1)
	read := tierScans(t, &ir.Document{Services: []ir.Service{{Groups: []ir.OperationGroup{deepest}}}})
	assert.Equal(t, ir.MaxGroupDepth+1, read, "levels 0 to the cap, each read once")
}
