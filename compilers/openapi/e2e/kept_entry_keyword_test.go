// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func TestKeptEntryKeyword_AValueKeywordThatLosesTheElectionIsKeptUnderItsOwnKey(t *testing.T) {
	t.Parallel()
	runKeptCases(t, []keptKeywordCase{{
		name: "enum-beside-const", principal: "{type: string}", keyword: "enum: [a, b], const: a", versions: keptOnly31,
		twin: true, want: degradedKept("openapi:enum"),
		finds: []keptFinding{keptDegraded("/allOf/{i}", "enum")},
	}})
}

func TestKeptEntryKeyword_AClosureTheEntryCannotTypeKeepsTheBranchWhole(t *testing.T) {
	t.Parallel()
	runKeptCases(t, []keptKeywordCase{
		{
			name: "closure-on-a-use-of-a-string", principal: keptStr, keyword: "additionalProperties: false", whole: true,
			want:  degradedKept("openapi:allOf/{i}"),
			finds: []keptFinding{keptDegraded("/allOf/{i}", "additionalProperties")},
		},
		{
			name: "closure-where-two-conjuncts-declare-members", principal: keptObj,
			keyword: "properties: {b: {type: string}}, additionalProperties: false", whole: true,
			want:  degradedKept("openapi:allOf/{i}"),
			finds: []keptFinding{keptDegraded("/allOf/{i}", "additionalProperties")},
		},
		{
			name: "unevaluated-properties", principal: keptObj, keyword: "unevaluatedProperties: false", versions: keptOnly31,
			whole: true, want: map[string]ir.UnmodeledReason{"openapi:allOf/{i}": ""},
			finds: []keptFinding{keptDegraded("/allOf/{i}", "unevaluatedProperties")},
		},
		{
			name: "relative-ref-read-against-another-base", principal: keptObj, versions: keptOnly31, whole: true,
			keyword: "$id: 'https://example.com/e', $defs: {k: {type: string}}, properties: {x: {$ref: '#/$defs/k'}}",
			want:    map[string]ir.UnmodeledReason{"openapi:allOf/{i}": ""},
			finds:   []keptFinding{keptDegraded("/allOf/{i}")},
		},
	})
}

func TestKeptEntryKeyword_AValidationOnlyKeywordIsKeptUnderItsOwnKey(t *testing.T) {
	t.Parallel()
	vo := func(key string) map[string]ir.UnmodeledReason {
		return map[string]ir.UnmodeledReason{key: ir.ReasonValidationOnly}
	}
	runKeptCases(t, []keptKeywordCase{
		{
			name: "not", principal: keptStr, keyword: "not: {enum: [a]}", twin: true, want: vo("openapi:not"),
			finds: []keptFinding{keptValidationOnly("not")},
		},
		{
			name: "if-then", principal: keptStr, keyword: "if: {minLength: 1}, then: {maxLength: 3}", versions: keptOnly31,
			twin: true, want: vo("openapi:if-then-else"),
			finds: []keptFinding{keptValidationOnly("if")},
		},
		{
			name: "property-names", principal: keptObj, keyword: "propertyNames: {pattern: '^a'}", versions: keptOnly31,
			twin: true, want: vo("openapi:propertyNames"),
			finds: []keptFinding{keptValidationOnly("propertyNames")},
		},
		{
			name: "dependent-required", principal: keptObj, keyword: "dependentRequired: {a: [b]}", versions: keptOnly31,
			twin: true, want: vo("openapi:dependentRequired"),
			finds: []keptFinding{keptValidationOnly("dependentRequired")},
		},
		{
			name: "contains", principal: keptArr, keyword: "contains: {type: string}, minContains: 1", versions: keptOnly31,
			twin: true, want: vo("openapi:contains"),
			finds: []keptFinding{keptValidationOnly("contains")},
		},
	})
}

func TestKeptEntryKeyword_ANoteWithNoFieldIsKeptUnderItsOwnKeyAndTheRestAreTyped(t *testing.T) {
	t.Parallel()
	runKeptCases(t, []keptKeywordCase{{
		name: "docs-and-visibility", principal: keptStr, twin: true,
		keyword: "description: d, deprecated: true, readOnly: true, xml: {name: x}, example: q",
		want:    map[string]ir.UnmodeledReason{"openapi:readOnly": ir.ReasonNoIRHome},
		finds:   []keptFinding{keptDegraded("/allOf/{i}/readOnly|/allOf/{i}", "readOnly")},
		check: func(t *testing.T, doc *ir.Document, name string) {
			t.Helper()
			assert.Equal(t, "d", kindNode(t, doc, componentID(name)).Common().Docs.Description, name)
		},
	}})
}

func TestKeptEntryKeyword_AVendorExtensionIsKeptUnderItsOwnKey(t *testing.T) {
	t.Parallel()
	runKeptCases(t, []keptKeywordCase{{
		name: "vendor-extension", principal: keptStr, keyword: "x-a: 1", twin: true,
		want: map[string]ir.UnmodeledReason{"openapi:x-a": ir.ReasonVendorExtension},
	}})
}

func TestKeptEntryKeyword_ABoundTheTypeTakesIsTypedAndKeptNowhere(t *testing.T) {
	t.Parallel()
	runKeptCases(t, []keptKeywordCase{{
		name: "string-bound-on-a-string", principal: keptStr, keyword: "minLength: 2", twin: true,
		check: func(t *testing.T, doc *ir.Document, name string) {
			t.Helper()
			scalar, ok := kindNode(t, doc, componentID(name)).(*ir.Scalar)
			require.True(t, ok, name)
			require.NotNil(t, scalar.Constraints, name)
			require.NotNil(t, scalar.Constraints.MinLength, name)
			assert.Equal(t, int64(2), *scalar.Constraints.MinLength, name)
		},
	}})
}

func TestKeptEntryKeyword_AnEntryDisagreeingWithAnotherIsKeptWholeInEveryOrder(t *testing.T) {
	t.Parallel()
	for _, version := range keptKeywordVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			schemas := []string{
				"Str: {type: string}",
				"Two: {allOf: [" + keptStr + ", {x-a: 1}, {x-a: 2}]}",
				"TwoRev: {allOf: [" + keptStr + ", {x-a: 2}, {x-a: 1}]}",
			}
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, _ := compileAnnotationSpec(t, "disagree", admissionSpec(version, "", schemas...))
				for _, name := range []string{"Two", "TwoRev"} {
					got := keptKeywordsOf(doc, name)
					require.Equal(t, []string{"openapi:allOf/1", "openapi:allOf/2"}, slices.Sorted(maps.Keys(got)), name)
					for _, i := range []string{"1", "2"} {
						assert.Equal(t, "/components/schemas/"+name+"/allOf/"+i, got["openapi:allOf/"+i][0].pointer, name)
					}
				}
			})
		})
	}
}

func TestKeptEntryKeyword_ANullableNoPolicyAppliesIsKeptOutOfScopeUnderBothPolicies(t *testing.T) {
	t.Parallel()
	runKeptNullableCases(t, func(t *testing.T, doc *ir.Document) {
		t.Helper()
		requireKeptKeywords(t, "Own", keptKeywordsOf(doc, "Own"),
			map[string]ir.UnmodeledReason{"openapi:nullable": ir.ReasonOutOfScope}, "/components/schemas/Own", false)
	})
}
