// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// TestDialect_AnIdentifierBesideAReferenceIsReadForNothing pins that a schema
// of a draft-07 dialect writing an $id and a description beside its $ref, one
// conjunction of the two, still reads the $id as its dialect does beside a
// $ref: for nothing.
func TestDialect_AnIdentifierBesideAReferenceIsReadForNothing(t *testing.T) {
	t.Parallel()
	_, diags := schema.LowerSpec(t, `openapi: 3.1.0
jsonSchemaDialect: 'http://json-schema.org/draft-07/schema#'
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
    T: {type: string}
    U: {$ref: '#/components/schemas/T', $id: 'https://example.com/u', description: d}
`)
	assert.Contains(t,
		openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/U/$id"),
		"is read for nothing, as this schema's dialect ignores the keywords beside a $ref")
}

// TestRefinement_AReferenceAValueSetNarrowsIsKeptAsWritten pins that a const
// object beside a $ref to an object, a value set narrowing it, keeps the
// reference under openapi:$ref, as written, and says so of this schema.
func TestRefinement_AReferenceAValueSetNarrowsIsKeptAsWritten(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpecVer("3.1.0", `    Obj: {type: object, properties: {a: {type: string}}}
    A: {$ref: '#/components/schemas/Obj', const: {a: x}}
`))
	lit, ok := typeByName(doc, "A").(*ir.Literal)
	require.True(t, ok, "the const is the value")
	require.Len(t, lit.Unmodeled, 1)
	entry := lit.Unmodeled["openapi:$ref"]
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.JSONEq(t, `{"$ref":"#/components/schemas/Obj"}`, string(entry.Value))
	assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/A"),
		"the value set of this schema narrows this $ref")
}

// TestVariantHint_AUseDeclaringAMemberIsNamedByItsPosition pins that a union
// variant writing a member beside its $ref, or on an entry beside it, a model
// of its own, is named by its position, and one writing only a note beside it
// by its target.
func TestVariantHint_AUseDeclaringAMemberIsNamedByItsPosition(t *testing.T) {
	t.Parallel()
	doc, _ := schema.LowerSpec(t, openapitest.ComponentSpecVer("3.1.0", `    Obj: {type: object, properties: {a: {type: string}}}
    U:
      oneOf:
        - {$ref: '#/components/schemas/Obj', properties: {b: {type: string}}}
        - {$ref: '#/components/schemas/Obj', description: d}
        - {allOf: [{$ref: '#/components/schemas/Obj'}, {required: [a]}]}
`))
	u, ok := typeByName(doc, "U").(*ir.Union)
	require.True(t, ok)
	require.Len(t, u.Variants, 3)
	assert.Equal(t, "variant_0", u.Variants[0].Name.Hint, "the member makes it no use of Obj alone")
	assert.Equal(t, "obj", u.Variants[1].Name.Hint)
	assert.Equal(t, "variant_2", u.Variants[2].Name.Hint, "nor does a member an entry declares")
}

// TestValueSet_AnEmptyEnumBesideAReferenceKeepsTheReference pins the value set
// that leaves no value beside a $ref and an allOf, as beside an allOf alone:
// no set to lower, so the election keeps the conjuncts it passed over, the
// $ref under openapi:$ref as written and the allOf whole, and reports both.
func TestValueSet_AnEmptyEnumBesideAReferenceKeepsTheReference(t *testing.T) {
	t.Parallel()
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpecVer("3.1.0", `    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    S: {enum: [], $ref: '#/components/schemas/A', allOf: [{$ref: '#/components/schemas/B'}]}
`))
	en, ok := typeByName(doc, "S").(*ir.Enum)
	require.True(t, ok, "got %T", typeByName(doc, "S"))
	assert.ElementsMatch(t, []string{"openapi:$ref", "openapi:allOf"}, unmodeledKeys(en.Unmodeled))
	assert.JSONEq(t, `{"$ref":"#/components/schemas/A"}`, string(en.Unmodeled["openapi:$ref"].Value))
	assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/S"),
		"declares $ref and allOf beside its enum")
}
