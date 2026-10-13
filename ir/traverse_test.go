package ir_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// groupChain returns a service whose single operation sits level groups down,
// the top-level group counting as level 0.
func groupChain(level int) *ir.Document {
	groups := []ir.OperationGroup{{Operations: []ir.Operation{{ID: ir.OpID(fmt.Sprintf("op%d", level))}}}}
	for range level {
		groups = []ir.OperationGroup{{Groups: groups}}
	}
	return &ir.Document{Services: []ir.Service{{Groups: groups}}}
}

func TestForEachOperation_NilDocument(t *testing.T) {
	t.Parallel()
	called := false
	truncated := ir.ForEachOperation(nil, func(ir.Operation) { called = true })
	assert.False(t, truncated)
	assert.False(t, called)
}

func TestForEachOperation_VisitsDeclarationOrder(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Services: []ir.Service{
		{Groups: []ir.OperationGroup{
			{Operations: []ir.Operation{{ID: "a"}}, Groups: []ir.OperationGroup{{Operations: []ir.Operation{{ID: "b"}}}}},
			{Operations: []ir.Operation{{ID: "c"}}},
		}},
		{Groups: []ir.OperationGroup{{Operations: []ir.Operation{{ID: "d"}}}}},
	}}
	var got []ir.OpID
	truncated := ir.ForEachOperation(doc, func(op ir.Operation) { got = append(got, op.ID) })
	assert.False(t, truncated)
	assert.Equal(t, []ir.OpID{"a", "b", "c", "d"}, got)
}

func TestForEachOperation_AtTheCap(t *testing.T) {
	t.Parallel()
	var got []ir.OpID
	truncated := ir.ForEachOperation(groupChain(ir.MaxGroupDepth), func(op ir.Operation) { got = append(got, op.ID) })
	assert.False(t, truncated, "an operation at the cap is still reached")
	assert.Equal(t, []ir.OpID{ir.OpID(fmt.Sprintf("op%d", ir.MaxGroupDepth))}, got)
}

func TestForEachOperation_OneBeyondTheCap(t *testing.T) {
	t.Parallel()
	var got []ir.OpID
	truncated := ir.ForEachOperation(groupChain(ir.MaxGroupDepth+1), func(op ir.Operation) { got = append(got, op.ID) })
	require.True(t, truncated, "an operation past the cap goes unvisited, and the walk says so")
	assert.Empty(t, got)
}

// TestForEachOperation_TruncationSkipsOnlyTheDeepGroups declares the truncated
// group first, among its siblings and among services, so a clean one after it
// would lose the report if its result overwrote it, or go unvisited if the cut
// ended the walk.
func TestForEachOperation_TruncationSkipsOnlyTheDeepGroups(t *testing.T) {
	t.Parallel()
	deep := groupChain(ir.MaxGroupDepth + 1).Services[0].Groups
	doc := &ir.Document{Services: []ir.Service{
		{Groups: append(deep, ir.OperationGroup{Operations: []ir.Operation{{ID: "sibling"}}})},
		{Groups: []ir.OperationGroup{{Operations: []ir.Operation{{ID: "later"}}}}},
	}}
	var got []ir.OpID
	truncated := ir.ForEachOperation(doc, func(op ir.Operation) { got = append(got, op.ID) })
	assert.True(t, truncated, "a clean group or service after the cut keeps the report")
	assert.Equal(t, []ir.OpID{"sibling", "later"}, got, "the cut skips only the groups past the cap")
}

// opIDs collects the operations ForEachOperation visits, in order, and whether
// it reported a cut.
func opIDs(doc *ir.Document) ([]ir.OpID, bool) {
	var got []ir.OpID
	truncated := ir.ForEachOperation(doc, func(op ir.Operation) { got = append(got, op.ID) })
	return got, truncated
}

// TestForEachOperation_ASelfContainingGroupSliceIsWalkedOnce holds the walk to
// ending on groups that contain themselves. An unguarded walk follows them to
// the cap, which takes for ever in practice once there are two. Each operation
// is visited once and nothing is reported cut, since nothing was skipped that
// had not been seen.
func TestForEachOperation_ASelfContainingGroupSliceIsWalkedOnce(t *testing.T) {
	t.Parallel()
	one := []ir.OperationGroup{{Operations: []ir.Operation{{ID: "a"}}}}
	one[0].Groups = one

	outer := []ir.OperationGroup{{Operations: []ir.Operation{{ID: "x"}}}}
	inner := []ir.OperationGroup{{Operations: []ir.Operation{{ID: "y"}}, Groups: outer}}
	outer[0].Groups = inner

	for _, tt := range []struct {
		name   string
		groups []ir.OperationGroup
		want   []ir.OpID
	}{
		{"a group that is its own subgroup", one, []ir.OpID{"a"}},
		{"two slices that hold each other", outer, []ir.OpID{"x", "y"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, truncated := opIDs(&ir.Document{Services: []ir.Service{{Groups: tt.groups}}})
			assert.False(t, truncated)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestForEachOperation_SharingWithoutACycleStillCostsTheUnfoldedSize pins what
// stays as it was. A group slice two places hold is walked from each, and so is
// a prefix of a slice from inside it, which is another reference: only the
// way back into a slice already being walked is cut.
func TestForEachOperation_SharingWithoutACycleStillCostsTheUnfoldedSize(t *testing.T) {
	t.Parallel()
	shared := []ir.OperationGroup{{Operations: []ir.Operation{{ID: "s"}}}}
	prefix := []ir.OperationGroup{
		{Operations: []ir.Operation{{ID: "p"}}},
		{Operations: []ir.Operation{{ID: "q"}}},
	}
	prefix[1].Groups = prefix[:1]

	got, truncated := opIDs(&ir.Document{Services: []ir.Service{
		{Groups: shared}, {Groups: shared}, {Groups: prefix},
	}})
	assert.False(t, truncated)
	assert.Equal(t, []ir.OpID{"s", "s", "p", "q", "p"}, got)
}

// TestForEachOperation_AWalkCutAtTheCapLeavesWhatItEntered holds the walk to
// leaving every group slice it entered, on the way back from a cut as from
// anywhere else. Two services share one chain that runs past the cap, so the
// second walks the slices the first did: one the first left marked as entered
// would read to the second as a cycle, and the operation at its top would go
// unvisited.
func TestForEachOperation_AWalkCutAtTheCapLeavesWhatItEntered(t *testing.T) {
	t.Parallel()
	chain := groupChain(ir.MaxGroupDepth + 1).Services[0].Groups
	chain[0].Operations = []ir.Operation{{ID: "top"}}

	got, truncated := opIDs(&ir.Document{Services: []ir.Service{{Groups: chain}, {Groups: chain}}})
	assert.True(t, truncated)
	assert.Equal(t, []ir.OpID{"top", "top"}, got, "the second service walks the chain as the first did")
}
