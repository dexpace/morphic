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
