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
	model := func(id ir.TypeID, prop ir.PropertyID) *ir.Model {
		return &ir.Model{ID: id, Properties: []ir.Property{{ID: prop}}}
	}
	doc := &ir.Document{Types: ir.TypeRegistry{
		"type/x/A": model("type/x/A", "property/x/A/a"), "type/x/B": model("type/x/B", "property/x/B/b"), "type/x/C": model("type/x/C", "property/x/C/c"),
		"type/x/S": &ir.Model{ID: "type/x/S", Base: &ir.TypeRef{Target: "type/x/A"},
			Implements: []ir.TypeRef{{Target: "type/x/B"}}, Mixins: []ir.TypeRef{{Target: "type/x/C"}}},
	}}
	for i := range 16 {
		id := ir.TypeID(fmt.Sprintf("type/x/F%d", i))
		doc.Types[id] = &ir.Model{ID: id}
	}
	var kept *irfacts.DocumentFacts // an Index left unused is inlined onto the stack and measures nothing
	construction := testing.AllocsPerRun(10, func() { kept = irfacts.New(doc) })
	require.NotNil(t, kept)

	subtype := testing.AllocsPerRun(10, func() { _ = irfacts.New(doc).IsSubtype("type/x/S", "type/x/C") })
	exposes := testing.AllocsPerRun(10, func() { _ = irfacts.New(doc).Exposes("type/x/S", "property/x/none") })

	assert.Equal(t, construction, subtype, "IsSubtype beyond New's allocations")
	assert.Equal(t, construction, exposes, "Exposes beyond New's allocations")
}
