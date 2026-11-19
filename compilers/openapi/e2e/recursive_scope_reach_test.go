// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// recDialect is the draft that has a $recursiveRef.
const recDialect = "      $schema: 'https://json-schema.org/draft/2019-09/schema'\n"

// recTree and recChain are the two anchored resources of a document no
// reference joins: each $recursiveRef can only come back to its own.
const (
	recTree = "    Tree:\n" + recDialect +
		"      $id: 'https://example.com/tree'\n      $recursiveAnchor: true\n      type: object\n" +
		"      properties:\n        children: {type: array, items: {$recursiveRef: '#'}}\n"
	recChain = "    Chain:\n" + recDialect +
		"      $id: 'https://example.com/chain'\n      $recursiveAnchor: true\n      type: array\n" +
		"      items: {$recursiveRef: '#'}\n"

	recTreePointer  = "/components/schemas/Tree/properties/children/items/$recursiveRef"
	recChainPointer = "/components/schemas/Chain/items/$recursiveRef"

	// recReaches is the verdict for a reference another anchored resource
	// reaches the target of.
	recReaches = "is also declared in a schema resource that reaches this one, so the target depends on the evaluation path"
)

// recKept returns the infos reporting a $recursiveRef at pointer left
// unexpanded.
func recKept(diags []ir.Diagnostic, pointer string) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, diagnostic := range diags {
		if diagnostic.Code == "openapi/degraded-construct" && string(diagnostic.Provenance.Pointer) == pointer {
			out = append(out, diagnostic)
		}
	}
	return out
}

// recElemBase returns the type the scalar at id, a list's element, is based on.
func recElemBase(t *testing.T, doc *ir.Document, list ir.TypeID) ir.TypeID {
	t.Helper()
	irList, ok := doc.Types[list].(*ir.List)
	require.True(t, ok, "%s is a list, not %T", list, doc.Types[list])
	elem, ok := doc.Types[irList.ElementType.Target].(*ir.Scalar)
	require.True(t, ok, "its element is a scalar, not %T", doc.Types[irList.ElementType.Target])
	require.NotNil(t, elem.Base)
	return elem.Base.Target
}

// TestCompile_ARecursiveRefNoOtherAnchoredResourceReachesExpands pins that two
// anchored resources neither of which reaches the other each expand their own
// reference, as either does alone: no path can make a target of one of them
// the other.
func TestCompile_ARecursiveRefNoOtherAnchoredResourceReachesExpands(t *testing.T) {
	t.Parallel()
	for name, schemas := range map[string]string{"Tree first": recTree + recChain, "Chain first": recChain + recTree} {
		doc, diags := compileText(t, "spec.yaml", dynHead+schemas, openapi.Options{})
		require.NotNil(t, doc, name)

		assert.Equal(t, []ir.Severity{ir.SeverityInfo}, diagsAt(diags, "openapi/dynamic-ref-expanded", recTreePointer), name)
		assert.Equal(t, []ir.Severity{ir.SeverityInfo}, diagsAt(diags, "openapi/dynamic-ref-expanded", recChainPointer), name)
		assert.Empty(t, recKept(diags, recTreePointer), name)
		assert.Empty(t, recKept(diags, recChainPointer), name)
		assert.Equal(t, componentID("Tree"),
			recElemBase(t, doc, "type/anonymous/components/schemas/Tree/properties/children"), name)

		chain, ok := doc.Types[componentID("Chain")].(*ir.List)
		require.True(t, ok, name)
		elem, ok := doc.Types[chain.ElementType.Target].(*ir.Scalar)
		require.True(t, ok, name)
		require.NotNil(t, elem.Base, name)
		assert.Equal(t, componentID("Chain"), elem.Base.Target, name)
	}
}

// TestCompile_ARecursiveRefAnotherAnchoredResourceReachesIsKept pins that a
// resource reaching the target by a $ref makes the dynamic scope's outermost
// anchor ambiguous: its reference is kept, while the reaching resource's own,
// which nothing reaches, expands.
func TestCompile_ARecursiveRefAnotherAnchoredResourceReachesIsKept(t *testing.T) {
	t.Parallel()
	chain := "    Chain:\n" + recDialect +
		"      $id: 'https://example.com/chain'\n      $recursiveAnchor: true\n      type: object\n" +
		"      items: {$ref: 'https://example.com/tree'}\n" +
		"      properties: {next: {$recursiveRef: '#'}}\n"
	const nextPointer = "/components/schemas/Chain/properties/next/$recursiveRef"
	for name, schemas := range map[string]string{"Tree first": recTree + chain, "Chain first": chain + recTree} {
		doc, diags := compileText(t, "spec.yaml", dynHead+schemas, openapi.Options{})
		require.NotNil(t, doc, name)

		kept := recKept(diags, recTreePointer)
		require.Len(t, kept, 1, "%s: %+v", name, diags)
		assert.Equal(t, ir.SeverityInfo, kept[0].Severity, name)
		assert.Contains(t, kept[0].Message, recReaches, name)
		assert.Empty(t, diagsAt(diags, "openapi/dynamic-ref-expanded", recTreePointer), name)

		assert.Equal(t, []ir.Severity{ir.SeverityInfo}, diagsAt(diags, "openapi/dynamic-ref-expanded", nextPointer),
			"%s: nothing reaches Chain, so its own reference expands", name)
		assert.Empty(t, recKept(diags, nextPointer), name)
	}
}

// TestCompile_ARecursiveRefAnotherDocumentsAnchoredResourceReachesIsKept pins
// the same verdict where the reaching resource is in another document, which
// is read only because the source names it: with that reference removed the
// document is not read and the reference expands.
func TestCompile_ARecursiveRefAnotherDocumentsAnchoredResourceReachesIsKept(t *testing.T) {
	t.Parallel()
	const other = "Outer:\n  $schema: 'https://json-schema.org/draft/2019-09/schema'\n" +
		"  $id: 'https://example.com/outer'\n  $recursiveAnchor: true\n  type: object\n" +
		"  properties:\n    tree: {$ref: 'https://example.com/tree'}\n"
	for name, use := range map[string]string{"read": "    Use: {$ref: 'other.yaml#/Outer'}\n", "unread": ""} {
		dir := t.TempDir()
		layout(t, dir, map[string]string{"main.yaml": dynHead + recTree + use, "other.yaml": other})
		doc, diags := compileDir(t, dir, openapi.Options{})
		require.NotNil(t, doc, name)
		require.Empty(t, byCode(diags, "openapi/unresolved-ref"), "%s: %+v", name, diags)

		if name == "read" {
			kept := recKept(diags, recTreePointer)
			require.Len(t, kept, 1, "%+v", diags)
			assert.Contains(t, kept[0].Message, recReaches)
			assert.Empty(t, diagsAt(diags, "openapi/dynamic-ref-expanded", recTreePointer))
			continue
		}
		assert.Equal(t, []ir.Severity{ir.SeverityInfo}, diagsAt(diags, "openapi/dynamic-ref-expanded", recTreePointer))
		assert.Empty(t, recKept(diags, recTreePointer))
	}
}

// TestCompile_ARecursiveRefInAResourceNestedInAnAnchoredOneIsKept pins that a
// resource nested in an anchored one is reached by it: the outer resource
// holds the inner one, so the reference in the inner one is not its own.
func TestCompile_ARecursiveRefInAResourceNestedInAnAnchoredOneIsKept(t *testing.T) {
	t.Parallel()
	const pointer = "/components/schemas/Outer/properties/in/properties/r/$recursiveRef"
	doc, diags := compileText(t, "spec.yaml", dynHead+
		"    Outer:\n"+recDialect+"      $id: 'https://example.com/o'\n      $recursiveAnchor: true\n"+
		"      properties:\n        in:\n          $id: 'https://example.com/i'\n          $recursiveAnchor: true\n"+
		"          properties:\n            r: {$recursiveRef: '#'}\n", openapi.Options{})
	require.NotNil(t, doc)
	kept := recKept(diags, pointer)
	require.Len(t, kept, 1, "%+v", diags)
	assert.Empty(t, diagsAt(diags, "openapi/dynamic-ref-expanded", pointer))
	assert.Contains(t, kept[0].Message, "$recursiveAnchor: true is")
}
