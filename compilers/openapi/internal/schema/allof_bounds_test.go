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

// redeclared lowers a model declaring field v as a and as b, and returns the
// field, the document and what it reported.
func redeclared(t *testing.T, a, b string) (ir.Property, *ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags := schema.LowerSpec(t, nonBareSpec("", a, b))
	openapitest.RequireNoErrorDiags(t, diags)
	m, ok := doc.Types[componentID("T")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, m.Properties, 1)
	return m.Properties[0], doc, diags
}

// TestAllOf_ABoundTakesItsTightestValueInEitherOrder pins a field typed by a
// shared primitive, where a bound stands on the property. Each pair is declared
// looser first as well as tighter first, since the first declaration's bound used
// to be kept: a fixture declaring only the tighter first passes on that code.
func TestAllOf_ABoundTakesItsTightestValueInEitherOrder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name            string
		looser, tighter string
		want            *ir.Constraints
	}{
		{
			"maxLength", "{type: string, maxLength: 20}", "{type: string, maxLength: 10}",
			&ir.Constraints{MaxLength: new(int64(10))},
		},
		{
			"minLength", "{type: string, minLength: 1}", "{type: string, minLength: 5}",
			&ir.Constraints{MinLength: new(int64(5))},
		},
		{
			"minimum", "{type: number, minimum: 1}", "{type: number, minimum: 5.5}",
			&ir.Constraints{Min: new(ir.BigVal("5.5"))},
		},
		{
			"maximum", "{type: number, maximum: 1e3}", "{type: number, maximum: 999}",
			&ir.Constraints{Max: new(ir.BigVal("999"))},
		},
		{
			"an exclusive bound, under its own keyword", "{type: number, exclusiveMinimum: 10}", "{type: number, exclusiveMinimum: 20}",
			&ir.Constraints{ExclusiveMin: new(ir.BigVal("20"))},
		},
		{
			// A magnitude no rational holds still has to be ordered: a bound
			// waved through at that scale would keep the first declaration's.
			"a minimum too large for a rational", "{type: number, minimum: 1e1000001}", "{type: number, minimum: 2e1000001}",
			&ir.Constraints{Min: new(ir.BigVal("2e1000001"))},
		},
		{
			"a step too large for a rational", "{type: number, multipleOf: 1e1000001}", "{type: number, multipleOf: 1e1000002}",
			&ir.Constraints{MultipleOf: new(ir.BigVal("1e1000002"))},
		},
		{
			"multipleOf, one dividing the other", "{type: number, multipleOf: 0.5}", "{type: number, multipleOf: 2}",
			&ir.Constraints{MultipleOf: new(ir.BigVal("2"))},
		},
		{
			"multipleOf, neither dividing", "{type: number, multipleOf: 4}", "{type: number, multipleOf: 6}",
			&ir.Constraints{MultipleOf: new(ir.BigVal("12"))},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, order := range [][2]string{{tc.looser, tc.tighter}, {tc.tighter, tc.looser}} {
				p, _, diags := redeclared(t, order[0], order[1])

				assert.Equal(t, tc.want, p.Constraints)
				assert.Empty(t, diags, "a bound the field implies is no disagreement")
				assert.Empty(t, p.Unmodeled, "and nothing of it is lost")
			}
		})
	}
}

// TestAllOf_ANodesBoundsReachTheFieldInEitherOrder pins a field whose type's own
// node holds its bounds: a list, a map, a tuple, and a string of an unknown
// format. A bound one declaration adds reaches the field's type, on the declared
// node where one says it all and on a node of its own where each adds a bound.
func TestAllOf_ANodesBoundsReachTheFieldInEitherOrder(t *testing.T) {
	t.Parallel()
	list := func(extra string) string { return "{type: array, items: {type: string}" + extra + "}" }
	mapOf := func(extra string) string { return "{type: object, additionalProperties: {type: string}" + extra + "}" }
	tuple := func(extra string) string { return "{type: array, prefixItems: [{type: string}]" + extra + "}" }
	custom := func(extra string) string { return "{type: string, format: x-custom" + extra + "}" }
	lo, hi := new(int64(1)), new(int64(5))
	for _, tc := range []struct {
		name   string
		a, b   string
		want   *ir.Constraints
		minted bool
	}{
		{name: "a list adding a bound", a: list(""), b: list(", maxItems: 5"), want: &ir.Constraints{MaxItems: hi}},
		{name: "a list tightening a bound", a: list(", maxItems: 9"), b: list(", maxItems: 5"), want: &ir.Constraints{MaxItems: hi}},
		{name: "two lists each adding a bound", a: list(", minItems: 1"), b: list(", maxItems: 5"), want: &ir.Constraints{MinItems: lo, MaxItems: hi}, minted: true},
		{name: "two maps each adding a bound", a: mapOf(", minProperties: 1"), b: mapOf(", maxProperties: 5"), want: &ir.Constraints{MinProps: lo, MaxProps: hi}, minted: true},
		{name: "two tuples each adding a bound", a: tuple(", minItems: 1"), b: tuple(", maxItems: 5"), want: &ir.Constraints{MinItems: lo, MaxItems: hi}, minted: true},
		{name: "a scalar adding a bound", a: custom(""), b: custom(", maxLength: 5"), want: &ir.Constraints{MaxLength: hi}},
		{name: "two scalars each adding a bound", a: custom(", minLength: 1"), b: custom(", maxLength: 5"), want: &ir.Constraints{MinLength: lo, MaxLength: hi}, minted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, order := range [][2]string{{tc.a, tc.b}, {tc.b, tc.a}} {
				p, doc, diags := redeclared(t, order[0], order[1])

				td, ok := doc.Types[p.Type.Target]
				require.True(t, ok, "the field's type is in the document")
				assert.Equal(t, tc.want, openapitest.NodeConstraints(t, td))
				assert.Contains(t, string(p.Type.Target), map[bool]string{true: "t/composed/", false: "t/anon/"}[tc.minted])
				assert.Empty(t, diags)
				assert.Empty(t, p.Unmodeled)
			}
		})
	}
}

// TestAllOf_ADefaultABoundOnThePropertyRefusesIsKeptInEitherOrder pins a default
// against a bound another declaration wrote on the property, which lowers as the
// same bound on a node does: not adopted, reported, and the declaration kept. A
// declaration whose own bound refuses its own default is left to it.
func TestAllOf_ADefaultABoundOnThePropertyRefusesIsKeptInEitherOrder(t *testing.T) {
	t.Parallel()
	valued, bound := "{type: string, default: hello}", "{type: string, maxLength: 3}"
	for _, order := range []struct{ a, b, kept string }{
		{valued, bound, "/components/schemas/T/allOf/0/properties/v"},
		{bound, valued, "/components/schemas/T/allOf/1/properties/v"},
	} {
		p, _, diags := redeclared(t, order.a, order.b)

		assert.Nil(t, p.Default)
		assert.Equal(t, &ir.Constraints{MaxLength: new(int64(3))}, p.Constraints)
		assert.Contains(t, p.Unmodeled, "openapi:conflicting-redeclaration"+order.kept)
		require.Len(t, diags, 1)
		assert.Equal(t, diag.DegradedConstruct, diags[0].Code)
		assert.Equal(t, order.kept, string(diags[0].Provenance.Pointer))
	}

	p, _, diags := redeclared(t, "{type: string, maxLength: 3, default: hello}", "{type: string}")
	require.NotNil(t, p.Default, "a default its own bound refuses stays as it was")
	assert.Equal(t, "hello", p.Default.Str)
	assert.Empty(t, diags)
}

// TestAllOf_AFoldedDeclarationLeavesNoRecordOfItself pins what a field keeps of a
// declaration folded into it (docs/compilers/openapi.md#allof-redeclared-property):
// its values and nothing naming where they came from. A keyword on the second
// branch compiles to the field it does on the first, and the field's provenance
// names the first either way.
func TestAllOf_AFoldedDeclarationLeavesNoRecordOfItself(t *testing.T) {
	t.Parallel()
	bare, bounded := "{type: string}", "{type: string, maxLength: 5, default: a}"

	onSecond, _, diags := redeclared(t, bare, bounded)
	onFirst, _, _ := redeclared(t, bounded, bare)

	assert.Empty(t, diags)
	assert.Equal(t, onFirst, onSecond)
	assert.Equal(t, "/components/schemas/T/allOf/0/properties/v", string(onSecond.Provenance.Pointer))
	assert.Empty(t, onSecond.Unmodeled, "nothing was lost, so nothing is kept")
}
