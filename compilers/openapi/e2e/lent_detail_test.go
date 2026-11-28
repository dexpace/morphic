// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// The components of the lent-description document: Money describes itself,
// Warning's OriginalPrice is a bare $ref to it, and Response's items restate
// that member, in allOf entries lentDescriptionSpec writes, to describe it.
const (
	lentMoney = `    Money:
      type: object
      description: Container for price information.
      properties:
        Value: {type: string}
`
	lentWarning = `    Warning:
      type: object
      properties:
        OriginalPrice: {$ref: '#/components/schemas/Money'}
`
	lentResponse = `    Response:
      type: object
      properties:
        Warnings:
          type: array
          items:
            allOf: %s
`
	warningEntry  = "{$ref: '#/components/schemas/Warning'}"
	restatedEntry = "{properties: {OriginalPrice: {description: The price before the change.}}}"
)

// lentDescriptionSpec is the document of GitHub #2152, the allOf of Response's
// items written as entries, its components declared Money first or, where
// responseFirst says, Response first.
func lentDescriptionSpec(entries string, responseFirst bool) string {
	response := fmt.Sprintf(lentResponse, entries)
	if responseFirst {
		return openapitest.ComponentSpecVer("3.0.3", response+lentWarning+lentMoney)
	}
	return openapitest.ComponentSpecVer("3.0.3", lentMoney+lentWarning+response)
}

// TestCompile_AWrittenDescriptionIsKeptOverALentOne pins GitHub #2152 end to
// end: an allOf entry describing a member whose other declaration is a bare
// $ref to a described target keeps its description in either order of the
// entries and of the components, and draws no report, as that declaration
// wrote none. The target keeps its own. The documents are one but for the
// member's identity, the entry restating it, which moves with that entry.
func TestCompile_AWrittenDescriptionIsKeptOverALentOne(t *testing.T) {
	t.Parallel()
	const itemsID = "type/anonymous/components/schemas/Response/properties/Warnings/items"
	inOrder, reversed := "["+warningEntry+", "+restatedEntry+"]", "["+restatedEntry+", "+warningEntry+"]"
	want, diags := parseVerified(t, lentDescriptionSpec(inOrder, false))
	assert.Empty(t, diags)
	wantItems, ok := want.Types[itemsID].(*ir.Model)
	require.True(t, ok)
	require.Len(t, wantItems.Properties, 1)
	assert.Equal(t, "The price before the change.", wantItems.Properties[0].Docs.Description)
	assert.Empty(t, wantItems.Properties[0].Unmodeled)
	wantMoney, ok := want.Types["type/openapi/components/schemas/Money"].(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, "Container for price information.", wantMoney.Docs.Description)

	for _, tc := range []struct {
		entries       string
		responseFirst bool
	}{{inOrder, true}, {reversed, false}, {reversed, true}} {
		got, gotDiags := parseVerified(t, lentDescriptionSpec(tc.entries, tc.responseFirst))
		assert.Empty(t, gotDiags, "%s, Response first: %t", tc.entries, tc.responseFirst)
		items, ok := got.Types[itemsID].(*ir.Model)
		require.True(t, ok)
		require.Len(t, items.Properties, 1)
		if tc.entries == reversed {
			items.Properties[0].ID, items.Properties[0].Provenance = wantItems.Properties[0].ID, wantItems.Properties[0].Provenance
		}
		assert.Empty(t, cmp.Diff(want.Types, got.Types), "%s, Response first: %t", tc.entries, tc.responseFirst)
	}
}

// TestCompile_ALentDetailSurvivesPruningItsReferent pins that a detail losing
// to a written one is still in the IR where the $defs node lending it is one
// the member's type leaves out: the declaration borrowing it is reported and
// kept verbatim, so its reference holds the node past pruning, in either order
// of the entries. A component the same declaration names is never pruned, so
// nothing is reported or kept for it.
func TestCompile_ALentDetailSurvivesPruningItsReferent(t *testing.T) {
	t.Parallel()
	const (
		lenderID = "type/anonymous/components/schemas/Both/$defs/Lender"
		written  = "{properties: {p: {$ref: '#/components/schemas/Narrow', description: Written.}}}"
		spec     = `    Narrow: {type: string, enum: [a]}
    Named: {type: [string, integer], description: Lent.}
    Both: {$defs: {Lender: {%s, description: Lent.}}, allOf: [%s, %s]}
`
	)
	for _, lender := range []string{"type: [string, integer]", "$ref: '#/components/schemas/Named'"} {
		for _, use := range []string{"$ref: '#/components/schemas/Both/$defs/Lender'", "allOf: [{$ref: '#/components/schemas/Both/$defs/Lender'}]"} {
			lent := "{properties: {p: {" + use + "}}}"
			for lentAt, entries := range [][2]string{{lent, written}, {written, lent}} {
				doc, diags := parseVerified(t, openapitest.ComponentSpec(fmt.Sprintf(spec, lender, entries[0], entries[1])))

				at := fmt.Sprintf("%s by %s, entry %d", lender, use, lentAt)
				field := lentField(t, doc)
				assert.Equal(t, "Written.", field.Docs.Description, at)
				require.Contains(t, doc.Types, ir.TypeID(lenderID), at)
				assert.Equal(t, "Lent.", doc.Types[lenderID].Common().Docs.Description, at)
				pointer := fmt.Sprintf("/components/schemas/Both/allOf/%d/properties/p", lentAt)
				entry, kept := field.Unmodeled["openapi:conflicting-redeclaration"+pointer]
				require.True(t, kept, at)
				assert.Contains(t, slices.Collect(maps.Values(entry.Targets)), ir.TypeID(lenderID), at)
				require.Len(t, diags, 1, at)
				assert.Equal(t, pointer, string(diags[0].Provenance.Pointer), at)
				assert.Contains(t, diags[0].Message, pointer+" borrows from a schema the field does not take as its type", at)
			}
		}
	}

	named := "{properties: {p: {$ref: '#/components/schemas/Named'}}}"
	for _, entries := range [][2]string{{named, written}, {written, named}} {
		doc, diags := parseVerified(t, openapitest.ComponentSpec(fmt.Sprintf(spec, "type: string", entries[0], entries[1])))
		assert.Empty(t, diags)
		field := lentField(t, doc)
		assert.Equal(t, "Written.", field.Docs.Description)
		assert.Empty(t, field.Unmodeled)
		assert.Equal(t, "Lent.", doc.Types["type/openapi/components/schemas/Named"].Common().Docs.Description)
	}
}

// lentField returns the one property of component Both.
func lentField(t *testing.T, doc *ir.Document) ir.Property {
	t.Helper()
	both, ok := doc.Types["type/openapi/components/schemas/Both"].(*ir.Model)
	require.True(t, ok)
	require.Len(t, both.Properties, 1)
	return both.Properties[0]
}
