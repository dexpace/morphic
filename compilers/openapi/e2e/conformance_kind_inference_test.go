// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
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
		alias, ok := doc.Types[componentID(name+"Entry")].(*ir.Scalar)
		require.True(t, ok, "%sEntry is a use of its entry", name)
		require.NotNil(t, alias.Base)
		entry := kindNode(t, doc, alias.Base.Target)
		assert.Equal(t, kind, entry.Kind(), name)
		assert.True(t, markedKindInference(entry), "%sEntry's entry is the policy's reading", name)
		assert.Equal(t, alias.Provenance.Pointer+"/allOf/0", entry.Common().Provenance.Pointer, name)
	}

	bounded, ok := doc.Types[componentID("NamesBounded")].(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, bounded.Base)
	assert.Equal(t, ir.KindList, kindNode(t, doc, bounded.Base.Target).Kind())
	require.NotNil(t, bounded.Constraints, "the bound folds onto the use")
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
