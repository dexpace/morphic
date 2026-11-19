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

// rootDraft4 and rootExclusive are the pieces of the schemas below: draft 4
// reads `exclusiveMinimum: true` as a flag, 2020-12 refuses it.
const (
	rootDraft4    = "http://json-schema.org/draft-04/schema#"
	rootExclusive = "minimum: 0, exclusiveMinimum: true"
)

// rootErrors returns the codes of the error diagnostics.
func rootErrors(diags []ir.Diagnostic) []string {
	var out []string
	for _, diagnostic := range diags {
		if diagnostic.Severity == ir.SeverityError {
			out = append(out, diagnostic.Code)
		}
	}
	return out
}

// TestCompile_ASchemaOnlyARefReachesReadsItsOwnDialect pins that a schema
// written under an extension, which no Schema Object holds, is a dialect root
// once a $ref lands on it, and is read in the draft its $schema names, as the
// same schema written as a component is.
func TestCompile_ASchemaOnlyARefReachesReadsItsOwnDialect(t *testing.T) {
	t.Parallel()
	doc, diags := compileText(t, "spec.yaml", "openapi: 3.1.0\ninfo: {title: D, version: '1'}\npaths: {}\n"+
		"x-store:\n  Bound:\n    $schema: '"+rootDraft4+"'\n    type: number\n    minimum: 0\n    exclusiveMinimum: true\n"+
		"  Pair:\n    $schema: '"+rootDraft4+"'\n    type: array\n    items: [{type: string}, {type: integer}]\n"+
		"components:\n  schemas:\n    B: {$ref: '#/x-store/Bound'}\n    P: {$ref: '#/x-store/Pair'}\n"+
		"    C:\n      $schema: '"+rootDraft4+"'\n      type: number\n      minimum: 0\n      exclusiveMinimum: true\n",
		openapi.Options{})
	require.NotNil(t, doc)
	assert.Empty(t, rootErrors(diags), "%+v", diags)

	bound, ok := doc.Types["type/anonymous/x-store/Bound"].(*ir.Scalar)
	require.True(t, ok)
	control := dynScalar(t, doc, "C")
	require.NotNil(t, control.Constraints, "the component is read in draft 4")
	require.NotNil(t, control.Constraints.ExclusiveMin)
	require.NotNil(t, bound.Constraints)
	require.NotNil(t, bound.Constraints.ExclusiveMin, "an exclusive bound of x > 0, as the component has")
	assert.Equal(t, *control.Constraints.ExclusiveMin, *bound.Constraints.ExclusiveMin)
	assert.Nil(t, bound.Constraints.Min, "no inclusive bound beside it")

	pair, ok := doc.Types["type/anonymous/x-store/Pair"].(*ir.Tuple)
	require.True(t, ok, "a list of items is a tuple in draft 4, not %T", doc.Types["type/anonymous/x-store/Pair"])
	require.Len(t, pair.ElementTypes, 2)
	assert.Equal(t, ir.TypeID("type/primitive/string"), pair.ElementTypes[0].Target)
	assert.Equal(t, ir.TypeID("type/primitive/integer"), pair.ElementTypes[1].Target)
}

// rootNested is an extension's schema in draft 4 holding a property that names
// 2020-12 itself, and the components reaching it.
const rootNested = "openapi: 3.1.0\ninfo: {title: D, version: '1'}\npaths: {}\n" +
	"x-store:\n  Outer:\n    $schema: '" + rootDraft4 + "'\n    properties:\n" +
	"      p: {$schema: 'https://json-schema.org/draft/2020-12/schema', type: number, " + rootExclusive + "}\n" +
	"components:\n  schemas:\n"

// TestCompile_ASchemaAnotherLandingHoldsIsNotADialectRoot pins that only a
// landing no other landing's schema holds is a root: the property is read in
// draft 4 whichever of its two references is written first, and in its own
// dialect only where nothing holds it.
func TestCompile_ASchemaAnotherLandingHoldsIsNotADialectRoot(t *testing.T) {
	t.Parallel()
	const (
		o = "    O: {$ref: '#/x-store/Outer'}\n"
		p = "    P: {$ref: '#/x-store/Outer/properties/p'}\n"
	)
	const prop = ir.TypeID("type/anonymous/x-store/Outer/properties/p")
	var first *ir.Document
	for name, schemas := range map[string]string{"O first": o + p, "P first": p + o} {
		doc, diags := compileText(t, "spec.yaml", rootNested+schemas, openapi.Options{})
		require.NotNil(t, doc, name)
		assert.Empty(t, rootErrors(diags), "%s: p is read in draft 4 from either reference: %+v", name, diags)
		sc, ok := doc.Types[prop].(*ir.Scalar)
		require.True(t, ok, name)
		require.NotNil(t, sc.Constraints, name)
		require.NotNil(t, sc.Constraints.ExclusiveMin, "%s: an exclusive bound", name)
		if first == nil {
			first = doc
			continue
		}
		assert.Equal(t, jsonOf(t, first), jsonOf(t, doc), "the declaration order decides nothing")
	}

	_, diags := compileText(t, "spec.yaml", rootNested+p, openapi.Options{})
	bounds := byCode(diags, "openapi/invalid-exclusive-bound")
	require.Len(t, bounds, 1, "p alone is its own root, read in 2020-12: %+v", diags)
	assert.EqualValues(t, "/x-store/Outer/properties/p/exclusiveMinimum", bounds[0].Provenance.Pointer)
}

// TestCompile_AnIdentifierInAnExtensionDeclaresNothingWhereADialectRootIsRead
// pins that making a landing a dialect root makes its `id` no declaration: the
// pointer reaches it, and the URI it writes is no name the source answers.
func TestCompile_AnIdentifierInAnExtensionDeclaresNothingWhereADialectRootIsRead(t *testing.T) {
	t.Parallel()
	doc, diags := compileText(t, "spec.yaml", "openapi: 3.1.0\ninfo: {title: D, version: '1'}\npaths: {}\n"+
		"x-store:\n  Id:\n    $schema: '"+rootDraft4+"'\n    id: 'https://example.com/x'\n    type: string\n"+
		"components:\n  schemas:\n    A: {$ref: '#/x-store/Id'}\n    B: {$ref: 'https://example.com/x'}\n",
		openapi.Options{})
	require.NotNil(t, doc)

	a := dynScalar(t, doc, "A")
	require.NotNil(t, a.Base)
	assert.Equal(t, ir.TypeID("type/anonymous/x-store/Id"), a.Base.Target, "the pointer resolves")
	unresolved := diagsAt(diags, "openapi/unresolved-ref", "/components/schemas/B")
	assert.Equal(t, []ir.Severity{ir.SeverityError}, unresolved,
		"the URI an extension's identifier writes is read as a location, which external references refuse: %+v", diags)
	assert.Empty(t, diagsAt(diags, "openapi/unresolved-ref", "/components/schemas/A"))
}
