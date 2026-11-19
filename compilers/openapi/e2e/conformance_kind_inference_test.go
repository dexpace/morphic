// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// assertAllOfEntryInfersArray holds allof-entry-infers-array: an allOf entry
// claiming the array the policy reads the position as is its principal, so
// each allOf spelling is a use of the marked node its entry builds, as each
// flat spelling is that node, a bound beside it folding onto the use.
func assertAllOfEntryInfersArray(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	assert.Empty(t, degradedInfoPointers(diags), "nothing is kept")
	for name, kind := range map[string]ir.TypeKind{"Names": ir.KindList, "Pair": ir.KindTuple} {
		flat := kindNode(t, doc, componentID(name))
		assert.Equal(t, kind, flat.Kind(), name)
		assert.True(t, markedKindInference(flat), name)
		entry := kindNode(t, doc, componentID(name+"Entry"))
		assert.Equal(t, kind, entry.Kind(), "%sEntry is the node its entry builds", name)
		assert.True(t, markedKindInference(entry), "%sEntry's node is the policy's reading", name)
		assert.Equal(t, jsontext.Pointer("/components/schemas/"+name+"Entry"), entry.Common().Provenance.Pointer, name)
	}

	bounded, ok := doc.Types[componentID("NamesBounded")].(*ir.List)
	require.True(t, ok, "NamesBounded is the list its entry builds")
	require.NotNil(t, bounded.Constraints, "the bound folds onto the list")
	assert.Equal(t, new(int64(1)), bounded.Constraints.MinItems)

	tagged, ok := doc.Types[componentID("Tagged")].(*ir.Model)
	require.True(t, ok)
	props := openapitest.PropsByWire(tagged.Properties)
	assert.Equal(t, ir.KindList, kindNode(t, doc, props["flat"].Type.Target).Kind())
	assert.Equal(t, ir.KindList, kindNode(t, doc, props["entry"].Type.Target).Kind())
	require.NotNil(t, props["bounded"].Constraints)
	assert.Equal(t, new(int64(3)), props["bounded"].Constraints.MaxItems, "the carrier holds the use's bound")

	op := operationAt(t, doc, "GET", "/tags")
	require.Len(t, op.Params, 1)
	assert.Equal(t, ir.KindList, kindNode(t, doc, op.Params[0].Type.Target).Kind())
}

// assertRefUseBoundKeywords holds ref-use-bound-keywords: each carried use
// whose keywords beside its $ref to an object only bind the use names the
// object, holding the keyword, with no node at its position, and the component
// writing the same owns the Model over the object.
func assertRefUseBoundKeywords(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	filter := componentID("Filter")
	meeting, ok := doc.Types[componentID("Meeting")].(*ir.Model)
	require.True(t, ok)
	props := openapitest.PropsByWire(meeting.Properties)
	for _, name := range []string{"owner", "draft", "parent", "audit"} {
		assert.Equal(t, filter, props[name].Type.Target, "%s names the reference", name)
		assert.NotContains(t, doc.Types, ir.TypeID("t/anon/components/schemas/Meeting/properties/"+name), name)
	}
	assert.Equal(t, []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery}, props["owner"].Visibility.Only)
	assert.Equal(t, props["owner"].Visibility, props["audit"].Visibility, "both spellings hold it alike")
	require.NotNil(t, props["draft"].Default)
	assert.True(t, props["parent"].Type.Nullable)

	op := operationAt(t, doc, "GET", "/meetings")
	require.Len(t, op.Params, 1)
	assert.Equal(t, filter, op.Params[0].Type.Target)
	require.NotNil(t, op.Params[0].Default)

	component, ok := doc.Types[componentID("ReadOnlyFilter")].(*ir.Model)
	require.True(t, ok, "a component owns the Model over the object")
	require.NotNil(t, component.Base)
	assert.Equal(t, filter, component.Base.Target)
	assert.Contains(t, component.Unmodeled, "openapi:readOnly")
}
