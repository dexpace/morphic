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
