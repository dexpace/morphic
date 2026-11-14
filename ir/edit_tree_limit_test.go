// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestEditor_RefusesAGroupItsReadDidNotReach holds the edit's read of the
// service tree to the steps a walk of it may take: in groups whose slices two
// groups share at each level, the second top-level group follows the 1,023
// groups below the first, so a read held to 1,000 steps does not reach it, and
// an update of it is refused, naming both bounds, where the full read updates
// it.
func TestEditor_RefusesAGroupItsReadDidNotReach(t *testing.T) {
	t.Parallel()
	const past = ir.GroupID("g/x/b9")
	doc := forkedGroups(10)
	calls, cut := groupsVisited(1000, doc)
	require.True(t, cut)
	require.Equal(t, 1000, calls)

	whole := ir.NewEditor(doc)
	whole.UpdateGroup(past, func(g *ir.OperationGroup) { g.Name.Source = "changed" })
	_, diags := whole.Finish()
	require.Empty(t, diags, "the control: a read to the real limit reaches the group")

	held := ir.NewEditorWithin(1000, doc)
	_, found := held.Group(past)
	held.UpdateGroup(past, func(g *ir.OperationGroup) { g.Name.Source = "changed" })
	_, diags = held.Finish()

	assert.False(t, found, "a group the read did not reach is not held")
	assert.Equal(t, []string{fmt.Sprintf(`internal: edit: update rejected: no group %q to update; an edit adds `+
		`no group, and reaches none in groups nested deeper than %d levels or past the first %d groups and operations`,
		past, ir.MaxGroupDepth, ir.MaxWalkSteps)}, refusals(t, diags))
}

// TestEditor_HoldsTheOperationsForEachOperationReachesWithinItsSteps holds the
// read's step for each operation to ForEachOperation's: within a limit that
// ends among one group's operations, the edit holds exactly the operations the
// walk reaches within the same limit.
func TestEditor_HoldsTheOperationsForEachOperationReachesWithinItsSteps(t *testing.T) {
	t.Parallel()
	ops := make([]ir.Operation, 10)
	for i := range ops {
		ops[i] = ir.Operation{ID: ir.OpID(fmt.Sprintf("op/x/o%d", i))}
	}
	doc := &ir.Document{Services: []ir.Service{{ID: "s/x", Groups: []ir.OperationGroup{{ID: "g/x/a", Operations: ops}}}}}
	const limit = 6
	var walked []ir.OpID
	require.True(t, ir.ForEachOperationWithin(limit, doc, func(op ir.Operation) { walked = append(walked, op.ID) }))

	e := ir.NewEditorWithin(limit, doc)
	var held []ir.OpID
	for _, op := range ops {
		if _, ok := e.Operation(op.ID); ok {
			held = append(held, op.ID)
		}
	}

	assert.Equal(t, walked, held)
	assert.Len(t, held, limit-1, "the group takes a step, and each operation one")
}
