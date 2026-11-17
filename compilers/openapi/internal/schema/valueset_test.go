// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// TestValueSet_AConstNullOnAnEntryStandsAtThePosition pins a const of null an
// entry writes beside a type admitting null: the value set of null alone
// stands at the position, typed by the conjunct declaring a string.
func TestValueSet_AConstNullOnAnEntryStandsAtThePosition(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0",
		"    S: {allOf: [{type: [string, 'null']}, {const: null}]}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	en, ok := typeByName(doc, "S").(*ir.Enum)
	require.True(t, ok, "got %T", typeByName(doc, "S"))
	assert.Empty(t, en.Members)
	assert.Equal(t, ir.PrimString, en.ValueType)
	assert.Empty(t, en.Unmodeled)
}

// TestValueSet_AnEnumPastTheBudgetOnAnEntryIsRefusedWhereWritten pins an entry's
// enum past the member budget: the position's node is the top type, refused at
// the enum the entry writes, keeping the $ref it would narrow and the format no
// type of it holds, where the entry writes it, and a format another conjunct
// writes, which no ValueType holds.
func TestValueSet_AnEnumPastTheBudgetOnAnEntryIsRefusedWhereWritten(t *testing.T) {
	t.Parallel()
	const uuidA, uuidB = "0b5c6f4e-6b9e-4a77-9f0f-5b0b8d5c1a11", "1b5c6f4e-6b9e-4a77-9f0f-5b0b8d5c1a11"
	l, _ := schema.LoweredFor(t, openapitest.ComponentSpecVer("3.1.0", `    Str: {type: string}
    S: {allOf: [{$ref: '#/components/schemas/Str'}, {enum: [a, b], format: email}]}
    F: {allOf: [{type: string, format: uuid}, {enum: [`+uuidA+`, `+uuidB+`]}]}
`))
	l.Ctx.Limits = lowering.Limits{MaxEnumMembers: budget.AtMost(1)}
	diags := schema.LowerComponentSchemas(t.Context(), l.Ctx, l.Types, l.Schemas())

	node, ok := typeByName(l.Document(t), "S").(*ir.Any)
	require.True(t, ok, "got %T", typeByName(l.Document(t), "S"))
	assert.Equal(t, []string{"openapi:$ref", "openapi:format"}, unmodeledKeys(node.Unmodeled))
	assert.Equal(t, jsontext.Pointer("/components/schemas/S/allOf/1/format"), node.Unmodeled["openapi:format"].Provenance.Pointer)
	formatted, ok := typeByName(l.Document(t), "F").(*ir.Any)
	require.True(t, ok, "got %T", typeByName(l.Document(t), "F"))
	assert.Equal(t, []string{"openapi:allOf/0"}, unmodeledKeys(formatted.Unmodeled))
	refused := withCode(diags, diag.BudgetExceeded)
	require.Len(t, refused, 2)
	assert.Equal(t, jsontext.Pointer("/components/schemas/S/allOf/1"), refused[0].Provenance.Pointer)
}

// TestValueSet_TwoRefsWrittenAlikeAreKeptOnce pins two $ref entries a value set
// narrows that write the same reference: one entry keeps them, at the position
// writing both, and each is reported where it is written.
func TestValueSet_TwoRefsWrittenAlikeAreKeptOnce(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    Str: {type: string}
    S: {allOf: [{$ref: '#/components/schemas/Str'}, {enum: [a]}, {$ref: '#/components/schemas/Str'}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	en, ok := typeByName(doc, "S").(*ir.Enum)
	require.True(t, ok, "got %T", typeByName(doc, "S"))
	assert.Equal(t, []string{"openapi:$ref"}, unmodeledKeys(en.Unmodeled))
	assert.Equal(t, jsontext.Pointer("/components/schemas/S"), en.Unmodeled["openapi:$ref"].Provenance.Pointer)
	for _, at := range []string{"/components/schemas/S/allOf/0", "/components/schemas/S/allOf/2"} {
		assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, at), "narrows")
	}
}

// TestValueSet_AnEmptyEnumBesideACompositionIsNotElected pins the value set
// whose own keywords leave it no value: it is no set to lower, so the allOf
// beside it composes and the election of competing keywords keeps it whole,
// and a use of the position reads through neither $ref.
func TestValueSet_AnEmptyEnumBesideACompositionIsNotElected(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    A: {type: object, properties: {a: {type: string}}, description: a}
    B: {type: object, properties: {b: {type: string}}, description: b}
    S: {enum: [], allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]}
    Holder:
      type: object
      properties:
        f: {$ref: '#/components/schemas/S'}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	en, ok := typeByName(doc, "S").(*ir.Enum)
	require.True(t, ok, "got %T", typeByName(doc, "S"))
	assert.Equal(t, []string{"openapi:allOf"}, unmodeledKeys(en.Unmodeled))
	assert.Empty(t, propertyOf(t, doc, "Holder", "f").Docs.Description)
}

// TestValueSet_ABoundBesideAnEntrysValueSetIsKeptByItsKeyword pins what the
// entry writing a value set writes beside it that the node has no field for: a
// bound no value set holds is kept under its keyword, at the entry, once, and
// the entry is not kept whole for it.
func TestValueSet_ABoundBesideAnEntrysValueSetIsKeptByItsKeyword(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    Str: {type: string}
    S: {allOf: [{$ref: '#/components/schemas/Str'}, {enum: [a], minLength: 3}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	en, ok := typeByName(doc, "S").(*ir.Enum)
	require.True(t, ok, "got %T", typeByName(doc, "S"))
	assert.Equal(t, []string{"openapi:$ref", "openapi:minLength"}, unmodeledKeys(en.Unmodeled))
	assert.Equal(t, jsontext.Pointer("/components/schemas/S/allOf/1/minLength"), en.Unmodeled["openapi:minLength"].Provenance.Pointer)
	assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo,
		"/components/schemas/S/allOf/1"), "no home for minLength")
}
