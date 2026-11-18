// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// TestUnion_AnAnyOfAloneDeclaringShapeIsElected pins a oneOf narrowing only
// what the body declares beside an anyOf of objects of their own, beside an
// allOf and on one entry of it: the anyOf is distributed, and each variant
// keeps the oneOf as validation logic, the union passed over being a conjunct
// of the rest.
func TestUnion_AnAnyOfAloneDeclaringShapeIsElected(t *testing.T) {
	t.Parallel()
	const keywords = "oneOf: [{required: [a]}, {maxProperties: 3}], " +
		"anyOf: [{type: object, properties: {d: {type: string}}}, {type: object, properties: {e: {type: string}}}]"
	doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(`    T: {type: object, properties: {a: {type: string}}}
    Beside: {allOf: [{$ref: '#/components/schemas/T'}], `+keywords+`}
    Entry: {allOf: [{$ref: '#/components/schemas/T'}, {`+keywords+`}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	for _, name := range []string{"Beside", "Entry"} {
		union, ok := typeByName(doc, name).(*ir.Union)
		require.True(t, ok, "%s distributes", name)
		assert.False(t, union.Exclusive, "%s elects its anyOf", name)
		require.Len(t, union.Variants, 2)
		for _, v := range union.Variants {
			kept := doc.Types[v.Type.Target].Common().Unmodeled
			require.Contains(t, kept, "openapi:oneOf", name)
			assert.Equal(t, ir.ReasonValidationOnly, kept["openapi:oneOf"].Reason, name)
		}
	}
}

// TestUnion_AnEntryHoldingBothIsReadWithoutThemWhereverListed pins an entry
// writing a oneOf of strings and integers beside an anyOf of objects, on the
// allOf, nested one or two allOfs deeper, under a wrapper writing a type of
// its own, and beside a $ref to the body: each keeps both unions, as the
// oneOf declares shape against the body read without them, where read with
// them the body admits nothing and the anyOf was elected.
func TestUnion_AnEntryHoldingBothIsReadWithoutThemWhereverListed(t *testing.T) {
	t.Parallel()
	const both = "{oneOf: [{type: string}, {type: integer}], " +
		"anyOf: [{type: object, properties: {d: {type: string}}}, {type: object, properties: {e: {type: string}}}]}"
	const ref = "{$ref: '#/components/schemas/T'}"
	doc, _ := schema.LowerSpec(t, openapitest.ComponentSpec(`    T: {type: object, properties: {a: {type: string}}}
    Entry: {allOf: [`+ref+`, `+both+`]}
    Nested: {allOf: [`+ref+`, {allOf: [`+both+`]}]}
    Deeper: {allOf: [`+ref+`, {allOf: [{allOf: [`+both+`]}]}]}
    Typed: {allOf: [`+ref+`, {type: object, allOf: [`+both+`]}]}
    BesideRef: {allOf: [{$ref: '#/components/schemas/T', `+both[1:]+`]}
`))
	for _, name := range []string{"Entry", "Nested", "Deeper", "Typed", "BesideRef"} {
		model, ok := typeByName(doc, name).(*ir.Model)
		require.True(t, ok, "%s keeps both unions on a Model", name)
		assert.Contains(t, model.Unmodeled, "openapi:oneOf", name)
		assert.Contains(t, model.Unmodeled, "openapi:anyOf", name)
	}
}

// TestUnion_EntriesOneAllOfDeeperJudgeEachOtherByNeither pins two unions on
// two entries nested one allOf deeper beside an object body, a oneOf of
// strings and an anyOf of booleans: each declares shape against the body read
// without the other, so both are kept as degraded_lowering, as on the allOf's
// own entries.
func TestUnion_EntriesOneAllOfDeeperJudgeEachOtherByNeither(t *testing.T) {
	t.Parallel()
	const unions = "{oneOf: [{type: string}]}, {anyOf: [{type: boolean}]}"
	doc, _ := schema.LowerSpec(t, openapitest.ComponentSpec(`    T: {type: object, properties: {a: {type: string}}}
    Entries: {allOf: [{$ref: '#/components/schemas/T'}, `+unions+`]}
    Deeper: {allOf: [{$ref: '#/components/schemas/T'}, {allOf: [`+unions+`]}]}
`))
	for _, name := range []string{"Entries", "Deeper"} {
		model, ok := typeByName(doc, name).(*ir.Model)
		require.True(t, ok, "%s keeps both unions on a Model", name)
		for _, key := range []string{"openapi:oneOf", "openapi:anyOf"} {
			require.Contains(t, model.Unmodeled, key, name)
			assert.Equal(t, ir.ReasonDegradedLowering, model.Unmodeled[key].Reason, "%s %s", name, key)
		}
	}
}

// TestUnion_TwoUnionsAloneLowerAsOneSchemaWritingBoth pins a oneOf of objects
// and an anyOf every one of them satisfies, with nothing beside them, on one
// schema, on one allOf entry and on two: each is the Union of the oneOf
// keeping the anyOf as degraded_lowering, the union passed over kept though
// the elected one says it, and an anyOf alone declaring shape is elected over
// a oneOf narrowing nothing it declares. Two oneOfs elect neither, and a keyword beside
// the two that no branch says is a body they distribute over.
func TestUnion_TwoUnionsAloneLowerAsOneSchemaWritingBoth(t *testing.T) {
	t.Parallel()
	const oneOf = "oneOf: [{type: object, properties: {p: {type: string}}}, {type: object, properties: {q: {type: string}}}]"
	const anyOf = "anyOf: [{type: object}]"
	doc, _ := schema.LowerSpec(t, openapitest.ComponentSpec(`    Flat: {`+oneOf+`, `+anyOf+`}
    OneEntry: {allOf: [{`+oneOf+`, `+anyOf+`}]}
    TwoEntries: {allOf: [{`+anyOf+`}, {`+oneOf+`}]}
    TwoOneOfs: {allOf: [{`+oneOf+`}, {oneOf: [{type: object, properties: {r: {type: string}}}, {required: [r]}]}]}
    Required: {allOf: [{`+oneOf+`}, {`+anyOf+`}, {required: [p]}]}
    Shapeless: {allOf: [{oneOf: [{required: [p]}, {}]}, {anyOf: [{type: object, properties: {x: {type: string}}}]}]}
`))
	for _, name := range []string{"Flat", "OneEntry", "TwoEntries"} {
		union, ok := typeByName(doc, name).(*ir.Union)
		require.True(t, ok, "%s is the Union of its oneOf", name)
		assert.True(t, union.Exclusive, name)
		require.Contains(t, union.Unmodeled, "openapi:anyOf", name)
		assert.Equal(t, ir.ReasonDegradedLowering, union.Unmodeled["openapi:anyOf"].Reason, name)
		for _, v := range union.Variants {
			assert.Contains(t, string(v.Type.Target), "t/anon/", "%s's variant is its branch", name)
		}
	}
	shapeless, ok := typeByName(doc, "Shapeless").(*ir.Union)
	require.True(t, ok, "the anyOf declaring shape is the Union")
	assert.False(t, shapeless.Exclusive)
	require.Contains(t, shapeless.Unmodeled, "openapi:oneOf")
	assert.Equal(t, ir.ReasonValidationOnly, shapeless.Unmodeled["openapi:oneOf"].Reason)
	_, isModel := typeByName(doc, "TwoOneOfs").(*ir.Model)
	assert.True(t, isModel, "two oneOfs elect neither")
	required, ok := typeByName(doc, "Required").(*ir.Union)
	require.True(t, ok, "the oneOf distributes over the body")
	for _, v := range required.Variants {
		assert.Contains(t, string(v.Type.Target), "t/composed/", "the variant composes the body")
	}
}
