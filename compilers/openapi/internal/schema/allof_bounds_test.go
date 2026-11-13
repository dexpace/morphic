// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
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

// TestAllOf_ANodeHoldingMoreThanBoundsIsAdoptedInEitherOrder pins nodes alike
// in their members whose declarations differ in bounds and in a fact a node
// holds beside them, a deprecation or XML hints (GitHub #1042). Neither node
// says all the other does, so the field takes a node built of both, holding
// every bound and the fact whichever declaration writes it, with nothing kept
// or reported. Two that give one fact differently say what no node holds at
// once, so the second is still reported and kept.
func TestAllOf_ANodeHoldingMoreThanBoundsIsAdoptedInEitherOrder(t *testing.T) {
	t.Parallel()
	list := func(extra string) string { return "{type: array, items: {type: string}" + extra + "}" }
	mapOf := func(extra string) string { return "{type: object, additionalProperties: {type: string}" + extra + "}" }
	tuple := func(extra string) string { return "{type: array, prefixItems: [{type: string}]" + extra + "}" }
	custom := func(extra string) string { return "{type: string, format: x-custom" + extra + "}" }
	for _, tc := range []struct{ name, a, b string }{
		{"two lists meeting, one deprecated", list(", minItems: 1, deprecated: true"), list(", maxItems: 5")},
		{"two lists meeting, one with XML hints", list(", minItems: 1, xml: {wrapped: true}"), list(", maxItems: 5")},
		{"a narrower list and a broader one deprecated", list(", minItems: 1, maxItems: 5"), list(", minItems: 1, deprecated: true")},
		{"two maps meeting, one deprecated", mapOf(", minProperties: 1, deprecated: true"), mapOf(", maxProperties: 5")},
		{"two tuples meeting, one deprecated", tuple(", minItems: 1, deprecated: true"), tuple(", maxItems: 5")},
		{"two scalars meeting, one deprecated", custom(", minLength: 1, deprecated: true"), custom(", maxLength: 5")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			built := make([]ir.TypeDef, 0, 2)
			for _, order := range [][2]string{{tc.a, tc.b}, {tc.b, tc.a}} {
				p, doc, diags := redeclared(t, order[0], order[1])

				assert.Equal(t, ir.TypeID("t/composed/components/schemas/T/allOf/1/properties/v"), p.Type.Target)
				assert.Empty(t, p.Unmodeled)
				assert.Empty(t, diags)
				node := doc.Types[p.Type.Target]
				assert.True(t, node.Common().Deprecation != nil || node.Common().XML != nil, "the fact is held")
				built = append(built, node)
			}
			assert.Empty(t, cmp.Diff(built[0], built[1], cmpopts.IgnoreFields(ir.TypeCommon{}, "Name")),
				"the node does not depend on branch order")
		})
	}

	p, _, diags := redeclared(t, list(", minItems: 1, description: one"), list(", maxItems: 5, description: other"))
	assert.Equal(t, ir.TypeID("t/anon/components/schemas/T/allOf/0/properties/v"), p.Type.Target)
	assert.Contains(t, p.Unmodeled, "openapi:conflicting-redeclaration/components/schemas/T/allOf/1/properties/v")
	require.Len(t, diags, 1)
	assert.Equal(t, diag.DegradedConstruct, diags[0].Code)
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

// TestAllOf_ACountNoFieldStatesIsKeptInEitherOrder pins GitHub #1050 under
// allOf, in both orders: a count past int64 on a redeclared field, or on an
// inline branch, states no bound, so the field and the schema take the counts
// the other conjunct states, and it is kept verbatim with one info where it is
// written.
func TestAllOf_ACountNoFieldStatesIsKeptInEitherOrder(t *testing.T) {
	t.Parallel()
	const past, five = "{type: string, maxLength: 99999999999999999999}", "{type: string, maxLength: 5}"
	for i, order := range [][2]string{{past, five}, {five, past}} {
		p, _, diags := redeclared(t, order[0], order[1])
		assert.Equal(t, &ir.Constraints{MaxLength: new(int64(5))}, p.Constraints, "%v", order)
		assert.Equal(t, []string{"openapi:maxLength"}, unmodeledKeys(p.Unmodeled), "%v", order)
		require.Len(t, diags, 1, "%v", order)
		assert.Equal(t, jsontext.Pointer(fmt.Sprintf("/components/schemas/T/allOf/%d/properties/v", i)),
			diags[0].Provenance.Pointer, "%v", order)
	}

	const branch, other = "{type: string, maxLength: 99999999999999999999}", "{type: string, minLength: 2}"
	for i, order := range [][2]string{{branch, other}, {other, branch}} {
		doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec("    T: {allOf: ["+order[0]+", "+order[1]+"]}\n"))
		openapitest.RequireNoErrorDiags(t, diags)
		s, ok := doc.Types[componentID("T")].(*ir.Scalar)
		require.True(t, ok, "%v", order)
		assert.Equal(t, &ir.Constraints{MinLength: new(int64(2))}, s.Constraints, "%v", order)
		assert.Equal(t, []string{"openapi:maxLength"}, unmodeledKeys(s.Unmodeled), "%v", order)
		require.Len(t, diags, 1, "%v", order)
		assert.Equal(t, jsontext.Pointer(fmt.Sprintf("/components/schemas/T/allOf/%d", i)), diags[0].Provenance.Pointer, "%v", order)
	}
}

// TestAllOf_TheBindingCountIsKeptInEveryOrder pins counts no field states that
// several writers of one position give, on its branches or beside its allOf:
// the one binding the rest is kept, the smallest maximum and the largest
// minimum compared exactly, with one info where it was written, in every order.
// want is the kept entry's JSON.
func TestAllOf_TheBindingCountIsKeptInEveryOrder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, keyword string
		counts        []string
		want          string
	}{
		{"two maxima", "maxLength", []string{"99999999999999999999", "88888888888888888888"}, "88888888888888888888"},
		{"two minima", "minProperties", []string{"88888888888888888888", "99999999999999999999"}, "99999999999999999999"},
		{"three minima", "minLength", []string{"99999999999999999999", "88888888888888888888", "77777777777777777777"},
			"99999999999999999999"},
	} {
		for _, order := range permutations(len(tc.counts)) {
			branches := make([]string, len(order))
			for k, i := range order {
				branches[k] = "{" + tc.keyword + ": " + tc.counts[i] + "}"
			}
			for _, beside := range []bool{false, true} {
				spec := "    T: {type: string, allOf: [" + strings.Join(branches, ", ") + "]}\n"
				if beside {
					spec = "    T: {type: string, " + branches[0][1:len(branches[0])-1] + ", allOf: [" +
						strings.Join(branches[1:], ", ") + "]}\n"
				}
				doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(spec))
				openapitest.RequireNoErrorDiags(t, diags)
				um := typeByName(doc, "T").Common().Unmodeled
				require.Equal(t, []string{"openapi:" + tc.keyword}, unmodeledKeys(um), "%s %v %v", tc.name, order, beside)
				assert.Equal(t, tc.want, string(um["openapi:"+tc.keyword].Value), "%s %v %v", tc.name, order, beside)
				assert.Len(t, diags, 1, "%s %v %v: one report", tc.name, order, beside)
			}
		}
	}
}

// TestAllOf_TheBindingCountPastTheDecimalRangeIsRefusedInEveryOrder pins two
// maxima past what a BigVal holds, on branches or beside the allOf: the binding
// one is refused as any number past that range kept verbatim is
// (docs/compilers/openapi.md#verbatim-preservation), with one error where it is
// written and no entry, in every order.
func TestAllOf_TheBindingCountPastTheDecimalRangeIsRefusedInEveryOrder(t *testing.T) {
	t.Parallel()
	counts := []string{"2e1000000000000000000", "1e1000000000000000000"}
	for _, order := range permutations(len(counts)) {
		branches := make([]string, len(order))
		for k, i := range order {
			branches[k] = "{maxLength: " + counts[i] + "}"
		}
		binding := slices.Index(order, 1)
		for _, beside := range []bool{false, true} {
			spec := "    T: {type: string, allOf: [" + strings.Join(branches, ", ") + "]}\n"
			at := fmt.Sprintf("/components/schemas/T/allOf/%d", binding)
			if beside {
				spec = "    T: {type: string, " + branches[0][1:len(branches[0])-1] + ", allOf: [" +
					strings.Join(branches[1:], ", ") + "]}\n"
				at = "/components/schemas/T"
				if binding > 0 {
					at = fmt.Sprintf("/components/schemas/T/allOf/%d", binding-1)
				}
			}
			doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(spec))
			assert.Empty(t, typeByName(doc, "T").Common().Unmodeled, "%v %v", order, beside)
			require.Len(t, diags, 1, "%v %v: one report", order, beside)
			assert.Equal(t, ir.SeverityError, diags[0].Severity, "%v %v", order, beside)
			assert.Equal(t, diag.UnpreservableConstruct, diags[0].Code, "%v %v", order, beside)
			assert.Equal(t, jsontext.Pointer(at), diags[0].Provenance.Pointer, "%v %v", order, beside)
		}
	}
}

// permutations returns every order of 0..n-1.
func permutations(n int) [][]int {
	if n == 1 {
		return [][]int{{0}}
	}
	var out [][]int
	for _, rest := range permutations(n - 1) {
		for at := range n {
			order := slices.Insert(slices.Clone(rest), at, n-1)
			out = append(out, order)
		}
	}
	return out
}
