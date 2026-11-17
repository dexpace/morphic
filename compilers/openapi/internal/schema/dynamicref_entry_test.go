// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// dynamicSpelling is what a position writing an unexpanded $dynamicRef beside
// a conjunct lowered to: the kind of its node, what that node is a use of or
// composes, and the reason and value it keeps the $dynamicRef under.
type dynamicSpelling struct {
	Kind   ir.TypeKind
	Base   ir.TypeID
	Reason ir.UnmodeledReason
	Value  string
}

// dynamicSpellingOf reads td as dynamicSpelling does.
func dynamicSpellingOf(t *testing.T, td ir.TypeDef) dynamicSpelling {
	t.Helper()
	require.NotNil(t, td)
	kept, ok := td.Common().Unmodeled["openapi:$dynamicRef"]
	require.True(t, ok, "no $dynamicRef kept on %s", td.Common().ID)
	out := dynamicSpelling{Kind: td.Kind(), Reason: kept.Reason, Value: string(kept.Value)}
	switch n := td.(type) {
	case *ir.Model:
		if n.Base != nil {
			out.Base = n.Base.Target
		}
	case *ir.Scalar:
		out.Base = n.Base.Target
	default:
	}
	return out
}

// TestDynamicRefEntry_OnAnEntryLowersAsBesideTheAllOf pins a $dynamicRef no
// position expands, written on an allOf entry and beside the allOf: it masks
// no kind in either spelling (docs/compilers/openapi.md#allof-refinement), so
// over an object the two are one Model over it, over a string or the top type
// one use of it, each keeping the $dynamicRef, whatever its anchor names, at a
// component and at an array's items.
func TestDynamicRefEntry_OnAnEntryLowersAsBesideTheAllOf(t *testing.T) {
	t.Parallel()
	targets := map[string]ir.TypeKind{"Obj": ir.KindModel, "Str": ir.KindScalar, "Any": ir.KindScalar}
	for _, anchor := range []string{"str", "obj"} {
		var schemas strings.Builder
		schemas.WriteString("    Obj: {type: object, properties: {a: {type: string}}}\n    Str: {type: string}\n    Any: {}\n" +
			"    AnchorStr: {$dynamicAnchor: str, type: string}\n    AnchorObj: {$dynamicAnchor: obj, type: object}\n")
		for target := range targets {
			ref := "{$ref: '#/components/schemas/" + target + "'}"
			schemas.WriteString("    Entry" + target + ": {allOf: [" + ref + ", {$dynamicRef: '#" + anchor + "'}]}\n" +
				"    Beside" + target + ": {allOf: [" + ref + "], $dynamicRef: '#" + anchor + "'}\n" +
				"    EntryItems" + target + ": {type: array, items: {allOf: [" + ref + ", {$dynamicRef: '#" + anchor + "'}]}}\n" +
				"    BesideItems" + target + ": {type: array, items: {allOf: [" + ref + "], $dynamicRef: '#" + anchor + "'}}\n")
		}
		doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", schemas.String()))
		openapitest.RequireNoErrorDiags(t, diags)
		for target, kind := range targets {
			entry := dynamicSpellingOf(t, typeByName(doc, "Entry"+target))
			assert.Equal(t, dynamicSpellingOf(t, typeByName(doc, "Beside"+target)), entry, "%s over %s", anchor, target)
			assert.Equal(t, kind, entry.Kind, "%s over %s", anchor, target)
			assert.Equal(t, componentID(target), entry.Base, "%s over %s", anchor, target)
			assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason, "%s over %s", anchor, target)
			items := dynamicSpellingOf(t, doc.Types[ir.TypeID("t/anon/components/schemas/EntryItems"+target+"/items")])
			assert.Equal(t, dynamicSpellingOf(t, doc.Types[ir.TypeID("t/anon/components/schemas/BesideItems"+target+"/items")]), items,
				"%s over %s at items", anchor, target)
		}
	}
}
