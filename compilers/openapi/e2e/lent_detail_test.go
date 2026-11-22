// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
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

// TestCompile_ALentDescriptionIsNoPartyToAConflict pins GitHub #2152 end to
// end: an allOf entry describing a member whose base declaration is a bare
// $ref to a described target draws no report, as that declaration wrote no
// description. The member keeps the entry's description and the target its
// own, whichever order the components are declared in. Written first, the
// entry's description is the one set aside, which is reported.
func TestCompile_ALentDescriptionIsNoPartyToAConflict(t *testing.T) {
	t.Parallel()
	inOrder := "[" + warningEntry + ", " + restatedEntry + "]"
	doc, diags := parseVerified(t, lentDescriptionSpec(inOrder, false))
	assert.Empty(t, diags)
	items, ok := doc.Types["type/anonymous/components/schemas/Response/properties/Warnings/items"].(*ir.Model)
	require.True(t, ok)
	require.Len(t, items.Properties, 1)
	assert.Equal(t, "The price before the change.", items.Properties[0].Docs.Description)
	assert.Empty(t, items.Properties[0].Unmodeled)
	assert.Equal(t, "Container for price information.", doc.Types["type/openapi/components/schemas/Money"].Common().Docs.Description)

	swapped, swappedDiags := parseVerified(t, lentDescriptionSpec(inOrder, true))
	assert.Empty(t, cmp.Diff(doc.Types, swapped.Types), "the components' order decides nothing")
	assert.Empty(t, swappedDiags)

	reversed, reversedDiags := parseVerified(t, lentDescriptionSpec("["+restatedEntry+", "+warningEntry+"]", false))
	require.Len(t, reversedDiags, 1)
	assert.Equal(t, diag.DegradedConstruct, reversedDiags[0].Code)
	assert.Equal(t, "/components/schemas/Response/properties/Warnings/items/allOf/0/properties/OriginalPrice",
		string(reversedDiags[0].Provenance.Pointer))
	reversedItems, ok := reversed.Types["type/anonymous/components/schemas/Response/properties/Warnings/items"].(*ir.Model)
	require.True(t, ok)
	require.Len(t, reversedItems.Properties, 1)
	assert.Equal(t, "Container for price information.", reversedItems.Properties[0].Docs.Description, "the lent description, written last")
}
