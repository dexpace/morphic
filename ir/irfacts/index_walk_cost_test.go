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

// TestIndex_AWalkedQuestionOverSeveralParentsAllocatesNothing holds a walked
// question to its lists on the stack where they hold more than one type: a
// model with a Base, an Implements and a Mixins entry queues all three, and a
// question the walk cannot settle at the first reads each. Unrelated models
// fill the registry, so the walk is within the steps the index walks before it
// indexes. It is not run in parallel: AllocsPerRun measures the process.
func TestIndex_AWalkedQuestionOverSeveralParentsAllocatesNothing(t *testing.T) {
	model := func(id ir.TypeID, prop ir.PropID) *ir.Model {
		return &ir.Model{ID: id, Properties: []ir.Property{{ID: prop}}}
	}
	doc := &ir.Document{Types: ir.TypeRegistry{
		"t/x/A": model("t/x/A", "p/x/A/a"), "t/x/B": model("t/x/B", "p/x/B/b"), "t/x/C": model("t/x/C", "p/x/C/c"),
		"t/x/S": &ir.Model{ID: "t/x/S", Base: &ir.TypeRef{Target: "t/x/A"},
			Implements: []ir.TypeRef{{Target: "t/x/B"}}, Mixins: []ir.TypeRef{{Target: "t/x/C"}}},
	}}
	for i := range 16 {
		id := ir.TypeID(fmt.Sprintf("t/x/F%d", i))
		doc.Types[id] = &ir.Model{ID: id}
	}
	var kept *irfacts.Index // an Index left unused is inlined onto the stack and measures nothing
	construction := testing.AllocsPerRun(10, func() { kept = irfacts.New(doc) })
	require.NotNil(t, kept)

	subtype := testing.AllocsPerRun(10, func() { _ = irfacts.New(doc).IsSubtype("t/x/S", "t/x/C") })
	exposes := testing.AllocsPerRun(10, func() { _ = irfacts.New(doc).Exposes("t/x/S", "p/x/none") })

	assert.Equal(t, construction, subtype, "IsSubtype beyond New's allocations")
	assert.Equal(t, construction, exposes, "Exposes beyond New's allocations")
}
