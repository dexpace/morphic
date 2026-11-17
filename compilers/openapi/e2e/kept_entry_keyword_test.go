// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/v2"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
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
			// The principal declares a member inline: a $ref's target declares none
			// the closure reads (docs/compilers/openapi.md#additional-properties).
			name: "closure-where-two-conjuncts-declare-members", principal: "{type: object, properties: {a: {type: string}}}",
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
			// A keyword kept apart from the entry's $id would be read against the
			// position's base, so the reference it writes keeps the entry whole.
			name: "relative-ref-read-against-another-base", principal: keptObj, versions: keptOnly31, whole: true,
			keyword: "$id: 'https://example.com/e', $defs: {k: {type: string}}, items: {$ref: '#/$defs/k'}",
			want:    map[string]ir.UnmodeledReason{"openapi:allOf/{i}": ""},
			finds:   []keptFinding{keptDegraded("/allOf/{i}", "another base")},
		},
	})
}

func TestKeptEntryKeyword_ARelativeReferenceMergedInPlaceKeepsTheEntryApart(t *testing.T) {
	t.Parallel()
	runKeptCases(t, []keptKeywordCase{{
		name: "merged-member", principal: keptObj, versions: keptOnly31,
		keyword: "$id: 'https://example.com/e', $defs: {k: {type: string}}, properties: {x: {$ref: '#/$defs/k'}}",
		want:    map[string]ir.UnmodeledReason{"openapi:$id": ir.ReasonOutOfScope},
		finds:   []keptFinding{keptDegraded("/allOf/{i}/$id", "$id")},
		check: func(t *testing.T, doc *ir.Document, name string) {
			t.Helper()
			x, ok := propByWire(requireModelOf(t, doc, name), "x")
			require.True(t, ok, "%s: the member the entry writes is typed", name)
			_, isString := doc.Types[x.Type.Target].(*ir.Primitive)
			alias, isAlias := doc.Types[x.Type.Target].(*ir.Scalar)
			assert.True(t, isString || isAlias && alias.Base != nil, "%s: x is the string its entry's base names", name)
		},
	}})
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
		finds:   []keptFinding{keptDegraded("/allOf/{i}/readOnly", "readOnly")},
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

func TestKeptEntryKeyword_AKindKeywordIsKeptUnderItsOwnKey(t *testing.T) {
	t.Parallel()
	runKeptCases(t, []keptKeywordCase{{
		name: "type-narrowing-a-number", principal: keptNum, keyword: "type: integer", twin: true,
		want:  degradedKept("openapi:type"),
		finds: []keptFinding{keptDegraded("/allOf/{i}", "type")},
	}})
}

func TestKeptEntryKeyword_APerKindBoundNoHomeTakesIsKeptUnderItsOwnKey(t *testing.T) {
	t.Parallel()
	runKeptCases(t, []keptKeywordCase{
		{
			name: "number-bound-on-a-string", principal: keptStr, keyword: "minimum: 1", twin: true,
			want:  degradedKept("openapi:minimum"),
			finds: []keptFinding{keptDegraded("/allOf/{i}", "minimum")},
		},
		{
			name: "string-bound-on-an-object", principal: keptObj, keyword: "minLength: 1", twin: true,
			want:  degradedKept("openapi:minLength"),
			finds: []keptFinding{keptDegraded("/allOf/{i}", "minLength")},
		},
		{
			name: "string-pattern-on-an-object", principal: keptObj, keyword: "pattern: '^a'", twin: true,
			want:  degradedKept("openapi:pattern"),
			finds: []keptFinding{keptDegraded("/allOf/{i}", "pattern")},
		},
		{
			name: "property-count-on-a-string", principal: keptStr, keyword: "minProperties: 1", twin: true,
			want:  degradedKept("openapi:minProperties"),
			finds: []keptFinding{keptDegraded("/allOf/{i}", "minProperties")},
		},
		{
			name: "two-array-bounds-on-a-string", principal: keptStr, keyword: "minItems: 1, uniqueItems: true", twin: true,
			want:  degradedKept("openapi:minItems", "openapi:uniqueItems"),
			finds: []keptFinding{keptDegraded("/allOf/{i}", "minItems", "uniqueItems")},
		},
	})
}

func TestKeptEntryKeyword_AFormatNoTypePairsWithIsKeptUnderItsOwnKey(t *testing.T) {
	t.Parallel()
	runKeptCases(t, []keptKeywordCase{
		{
			name: "date-on-an-object", principal: keptObj, keyword: "format: date", twin: true,
			want:  degradedKept("openapi:format"),
			finds: []keptFinding{keptDegraded("/allOf/{i}", "format")},
		},
		{
			name: "unknown-format-on-a-string", principal: keptStr, keyword: "format: unknownfmt", twin: true,
			want:  degradedKept("openapi:format"),
			finds: []keptFinding{keptDegraded("/allOf/{i}", "format")},
		},
	})
}

func TestKeptEntryKeyword_ContentVocabularyIsKeptUnderItsOwnKeyAndReportedAtTheKey(t *testing.T) {
	t.Parallel()
	runKeptCases(t, []keptKeywordCase{{
		name: "media-type", principal: keptStr, keyword: "contentMediaType: text/html", versions: keptOnly31, twin: true,
		want:  map[string]ir.UnmodeledReason{"openapi:contentMediaType": ir.ReasonNoIRHome},
		finds: []keptFinding{keptDegraded("/allOf/{i}/contentMediaType", "contentMediaType")},
	}})
}

func TestKeptEntryKeyword_AShapeKeywordOnAUseOfAStringIsKeptUnderItsOwnKey(t *testing.T) {
	t.Parallel()
	runKeptCases(t, []keptKeywordCase{
		{
			name: "properties", principal: keptStr, keyword: "properties: {a: {type: string}}", twin: true,
			want:  degradedKept("openapi:properties"),
			finds: []keptFinding{keptDegraded("/allOf/{i}", "properties")},
		},
		{
			name: "items", principal: keptStr, keyword: "items: {type: string}", twin: true,
			want:  degradedKept("openapi:items"),
			finds: []keptFinding{keptDegraded("/allOf/{i}", "items")},
		},
	})
}

func TestKeptEntryKeyword_AChoiceIsKeptUnderItsKeywordWithItsReason(t *testing.T) {
	t.Parallel()
	runKeptCases(t, []keptKeywordCase{
		{
			name: "any-of-declaring-no-shape", principal: keptObj, keyword: "anyOf: [{required: [a]}, {required: [b]}]", twin: true,
			want:  map[string]ir.UnmodeledReason{"openapi:anyOf": ir.ReasonValidationOnly},
			finds: []keptFinding{keptValidationOnly("anyOf")},
		},
		{
			name: "one-of-saying-more-than-a-string", principal: keptStr, twin: true,
			keyword: "oneOf: [{type: string, maxLength: 5}, {type: boolean}]",
			want:    degradedKept("openapi:oneOf"),
			finds:   []keptFinding{keptDegraded("/allOf/{i}", "oneOf")},
		},
		{
			name: "one-of-sharing-no-kind-with-an-object", principal: keptObj, twin: true,
			keyword: "oneOf: [{type: string}, {type: boolean}]",
			want:    degradedKept("openapi:oneOf"),
			finds: []keptFinding{
				{severity: ir.SeverityWarning, code: diag.DegradedConstruct, at: "", words: []string{disjoint}},
				keptDegraded("/allOf/{i}", "oneOf"),
			},
		},
	})
}

func TestKeptEntryKeyword_ADynamicReferenceOnAnEntryOfAnItemIsKeptUnderItsOwnKey(t *testing.T) {
	t.Parallel()
	runKeptCases(t, []keptKeywordCase{{
		name: "dynamic-ref-at-items", principal: keptStr, keyword: "$dynamicRef: '#m'", versions: keptOnly31,
		wrap: "{type: array, items: %s}", pos: "/items", extra: []string{"Tree: {$dynamicAnchor: m, type: object}"},
		want:  degradedKept("openapi:$dynamicRef"),
		finds: []keptFinding{keptDegraded("/allOf/{i}/$dynamicRef", "$dynamicRef")},
	}})
}

func TestKeptEntryKeyword_AScopedKeywordIsKeptOutOfScopeAndAConsumedOneNowhere(t *testing.T) {
	t.Parallel()
	const commentText = "kept-comment-text"
	oos := func(key string) map[string]ir.UnmodeledReason {
		return map[string]ir.UnmodeledReason{key: ir.ReasonOutOfScope}
	}
	runKeptCases(t, []keptKeywordCase{
		{
			name: "schema", principal: keptStr, keyword: "$schema: 'https://json-schema.org/draft/2020-12/schema'",
			versions: keptOnly31, twin: true, want: oos("openapi:$schema"),
			finds: []keptFinding{keptDegraded("/allOf/{i}/$schema", "$schema")},
		},
		{
			name: "vocabulary", principal: keptStr, keyword: "$vocabulary: {'https://example.com/v': true}",
			versions: keptOnly31, twin: true, want: oos("openapi:$vocabulary"),
			finds: []keptFinding{keptDegraded("/allOf/{i}/$vocabulary", "$vocabulary")},
		},
		{
			name: "id-beside-members", principal: keptObj, keyword: "$id: 'https://example.com/e3', properties: {x: {type: string}}",
			versions: keptOnly31, want: oos("openapi:$id"),
			finds: []keptFinding{keptDegraded("/allOf/{i}/$id", "$id")},
			check: func(t *testing.T, doc *ir.Document, name string) {
				t.Helper()
				model := requireModelOf(t, doc, name)
				_, ok := propByWire(model, "x")
				assert.True(t, ok, "%s: the members the entry writes beside its $id are typed", name)
			},
		},
		{
			name: "comment-anchor-and-defs", principal: keptObj, versions: keptOnly31, twin: true,
			keyword: "$comment: " + commentText + ", $anchor: there, $defs: {I: {type: integer}}",
			check: func(t *testing.T, doc *ir.Document, name string) {
				t.Helper()
				raw, err := json.Marshal(doc)
				require.NoError(t, err)
				assert.NotContains(t, string(raw), commentText, "%s: a $comment no emitter may see reaches no node", name)
			},
		},
		{
			name: "description-beside-a-comment", principal: keptObj, keyword: "description: d, $comment: c",
			versions: keptOnly31, twin: true,
			check: func(t *testing.T, doc *ir.Document, name string) {
				t.Helper()
				assert.Equal(t, "d", kindNode(t, doc, componentID(name)).Common().Docs.Description, name)
			},
		},
	})
}

func TestKeptEntryKeyword_AnUnknownKeywordIsKeptOutOfScopeAndReportedAtTheKey(t *testing.T) {
	t.Parallel()
	runKeptCases(t, []keptKeywordCase{
		{
			name: "unknown", principal: keptStr, keyword: "unknownKeyword: 1", twin: true,
			want:  map[string]ir.UnmodeledReason{"openapi:unknownKeyword": ir.ReasonOutOfScope},
			finds: []keptFinding{keptInfo(diag.UnknownSchemaKeyword, "/allOf/{i}/unknownKeyword", "unknownKeyword")},
		},
	})
}

func TestKeptEntryKeyword_AnEntryWritingSeveralKeywordsKeepsEachUnderItsOwnReason(t *testing.T) {
	t.Parallel()
	runKeptCases(t, []keptKeywordCase{{
		name: "mixed", principal: keptStr, twin: true,
		keyword: "unknownKeyword: 1, minLength: 2, description: d, not: {enum: [a]}, x-a: 1, minimum: 3",
		want: map[string]ir.UnmodeledReason{
			"openapi:unknownKeyword": ir.ReasonOutOfScope,
			"openapi:not":            ir.ReasonValidationOnly,
			"openapi:x-a":            ir.ReasonVendorExtension,
			"openapi:minimum":        ir.ReasonDegradedLowering,
		},
		finds: []keptFinding{
			keptInfo(diag.UnknownSchemaKeyword, "/allOf/{i}/unknownKeyword", "unknownKeyword"),
			keptValidationOnly("not"),
			keptDegraded("/allOf/{i}", "minimum"),
		},
		check: func(t *testing.T, doc *ir.Document, name string) {
			t.Helper()
			assert.Equal(t, "d", kindNode(t, doc, componentID(name)).Common().Docs.Description, name)
		},
	}})
}

// keptEntryBuilder is a closed object whose members the two entries of its
// allOf each ask one of to be present, each entry a union declaring no shape:
// the two disagree on anyOf, so neither is folded.
const keptEntryBuilder = `Builder: {type: object, additionalProperties: false,
      properties: {to: {type: string}, cc: {type: string}, text: {type: string}, html: {type: string}},
      allOf: [%s, %s]}`

func TestKeptEntryKeyword_TwoUnionsDeclaringNoShapeAreKeptWholeAsValidationOnly(t *testing.T) {
	t.Parallel()
	first := "{anyOf: [{required: [to], type: object}, {required: [cc], type: object}]}"
	second := "{anyOf: [{required: [text], type: object}, {required: [html], type: object}]}"
	for _, version := range keptKeywordVersions {
		for _, swapped := range []bool{false, true} {
			schema := fmt.Sprintf(keptEntryBuilder, first, second)
			if swapped {
				schema = fmt.Sprintf(keptEntryBuilder, second, first)
			}
			t.Run(version, func(t *testing.T) {
				t.Parallel()
				doc, diags := compileAnnotationSpec(t, "builder", admissionSpec(version, "", schema))
				want := map[string]ir.UnmodeledReason{
					"openapi:allOf/0": ir.ReasonValidationOnly, "openapi:allOf/1": ir.ReasonValidationOnly,
				}
				got := keptKeywordsOf(doc, "Builder")
				require.Equal(t, want, keptKeywordReasons(got), "both entries are kept whole, as validation-only")
				for i := range 2 {
					entry := "/components/schemas/Builder/allOf/" + strconv.Itoa(i)
					assert.Equal(t, entry, got["openapi:allOf/"+strconv.Itoa(i)][0].pointer)
				}
				keptFindingsUnder(t, diags, "Builder", []keptFinding{
					keptInfo(diag.ValidationOnlyKeyword, "/allOf/0", "anyOf"), keptInfo(diag.ValidationOnlyKeyword, "/allOf/1", "anyOf"),
				})
			})
		}
	}
}

func TestKeptEntryKeyword_AnEntryDisagreeingWithAnotherKeepsItsKeywordsReasonWhenKeptWhole(t *testing.T) {
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
					want := map[string]ir.UnmodeledReason{
						"openapi:allOf/1": ir.ReasonVendorExtension, "openapi:allOf/2": ir.ReasonVendorExtension,
					}
					require.Equal(t, want, keptKeywordReasons(got), name)
					for _, i := range []string{"1", "2"} {
						assert.Equal(t, "/components/schemas/"+name+"/allOf/"+i, got["openapi:allOf/"+i][0].pointer, name)
					}
				}
			})
		})
	}
}

func TestKeptEntryKeyword_TwoWritersOfOneKeywordAndValueKeepOneEntryAtThePosition(t *testing.T) {
	t.Parallel()
	for _, version := range keptKeywordVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			schemas := []string{
				"Str: {type: string}",
				"Same: {allOf: [" + keptStr + ", {x-a: 1}, {x-a: 1}]}",
				"SameRev: {allOf: [{x-a: 1}, " + keptStr + ", {x-a: 1}]}",
				"Nested: {allOf: [" + keptStr + ", {x-a: 1}, {allOf: [{x-a: 1}]}]}",
				"NestedRev: {allOf: [" + keptStr + ", {allOf: [{x-a: 1}]}, {x-a: 1}]}",
			}
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, _ := compileAnnotationSpec(t, "same", admissionSpec(version, "", schemas...))
				for _, name := range []string{"Same", "SameRev", "Nested", "NestedRev"} {
					got := keptKeywordsOf(doc, name)
					require.Equal(t, map[string]ir.UnmodeledReason{"openapi:x-a": ir.ReasonVendorExtension},
						keptKeywordReasons(got), name)
					require.Len(t, got["openapi:x-a"], 1, name)
					assert.Equal(t, "/components/schemas/"+name, got["openapi:x-a"][0].pointer,
						"%s: two writers alike leave one entry at the position", name)
				}
			})
		})
	}
}

// TestKeptEntryKeyword_AKeywordWrittenAlikeBesideTheAllOfIsKeptAsThere holds a
// keyword an entry writes as the keywords beside its allOf write it to what
// those keep: one entry, at the keyword beside the allOf, with that keyword's
// report alone, in both orders of the components.
func TestKeptEntryKeyword_AKeywordWrittenAlikeBesideTheAllOfIsKeptAsThere(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, keyword, key string
		reason             ir.UnmodeledReason
		find               keptFinding
	}{
		{"Format", "format: email", "format", ir.ReasonDegradedLowering, keptDegraded("", "format")},
		{"Unknown", "foo: 1", "foo", ir.ReasonOutOfScope, keptInfo(diag.UnknownSchemaKeyword, "/foo", "foo")},
		{
			"Content", "contentMediaType: text/html", "contentMediaType", ir.ReasonNoIRHome,
			keptDegraded("/contentMediaType", "contentMediaType"),
		},
	}
	schemas := make([]string, 0, 1+len(cases))
	schemas = append(schemas, "Str: {type: string}")
	for _, c := range cases {
		schemas = append(schemas, c.name+": {allOf: ["+keptStr+", {"+c.keyword+"}], "+c.keyword+"}")
	}
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		doc, diags := compileAnnotationSpec(t, "alike", admissionSpec("3.1.0", "", schemas...))
		for _, c := range cases {
			got := keptKeywordsOf(doc, c.name)
			key := "openapi:" + c.key
			require.Equal(t, map[string]ir.UnmodeledReason{key: c.reason}, keptKeywordReasons(got), c.name)
			require.Len(t, got[key], 1, c.name)
			assert.Equal(t, "/components/schemas/"+c.name+"/"+c.key, got[key][0].pointer,
				"%s: the keyword beside the allOf holds the entry", c.name)
			keptFindingsUnder(t, diags, c.name, []keptFinding{c.find})
		}
	})
}

// keptEntryIssueSchemas is the schemas with Text, a string, and Obj, an
// object, written before them.
func keptEntryIssueSchemas(schemas ...string) []string {
	return slices.Concat([]string{
		"Text: {type: string}",
		"Obj: {type: object, properties: {a: {type: string}}}",
	}, schemas)
}

func TestKeptEntryKeyword_EverySpellingOfAContentOrUnknownKeywordKeepsOneKeyAndReason(t *testing.T) {
	t.Parallel()
	const text = "{$ref: '#/components/schemas/Text'}"
	schemas := keptEntryIssueSchemas(
		"Beside: {$ref: '#/components/schemas/Text', contentMediaType: text/html}",
		"Wrapped: {allOf: ["+text+"], contentMediaType: text/html}",
		"Entry: {allOf: ["+text+", {contentMediaType: text/html}]}",
		"BesideUnknown: {$ref: '#/components/schemas/Text', unknownKeyword: 1}",
		"EntryUnknown: {allOf: ["+text+", {unknownKeyword: 1}]}",
		"EntryMixed: {allOf: ["+text+", {unknownKeyword: 1, minLength: 2, description: d}]}",
	)
	media := map[string]ir.UnmodeledReason{"openapi:contentMediaType": ir.ReasonNoIRHome}
	unknown := map[string]ir.UnmodeledReason{"openapi:unknownKeyword": ir.ReasonOutOfScope}
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		doc, diags := compileAnnotationSpec(t, "spellings", admissionSpec("3.1.0", "", schemas...))
		for _, name := range []string{"Beside", "Wrapped", "Entry", "BesideUnknown", "EntryUnknown", "EntryMixed"} {
			scalar, ok := kindNode(t, doc, componentID(name)).(*ir.Scalar)
			require.True(t, ok, "%s is a use of Text", name)
			require.NotNil(t, scalar.Base, name)
			assert.Equal(t, componentID("Text"), scalar.Base.Target, name)
		}
		entryAt := func(name string) string { return "/components/schemas/" + name + "/allOf/1" }
		requireKeptKeywords(t, "Beside", keptKeywordsOf(doc, "Beside"), media, "/components/schemas/Beside", false)
		requireKeptKeywords(t, "Wrapped", keptKeywordsOf(doc, "Wrapped"), media, "/components/schemas/Wrapped", false)
		requireKeptKeywords(t, "Entry", keptKeywordsOf(doc, "Entry"), media, entryAt("Entry"), false)
		requireKeptKeywords(t, "BesideUnknown", keptKeywordsOf(doc, "BesideUnknown"), unknown, "/components/schemas/BesideUnknown", false)
		requireKeptKeywords(t, "EntryUnknown", keptKeywordsOf(doc, "EntryUnknown"), unknown, entryAt("EntryUnknown"), false)
		requireKeptKeywords(t, "EntryMixed", keptKeywordsOf(doc, "EntryMixed"), unknown, entryAt("EntryMixed"), false)

		keptFindingsUnder(t, diags, "Entry", []keptFinding{keptDegraded("/allOf/1/contentMediaType", "contentMediaType")})
		keptFindingsUnder(t, diags, "EntryUnknown", []keptFinding{
			keptInfo(diag.UnknownSchemaKeyword, "/allOf/1/unknownKeyword", "unknownKeyword"),
		})
		keptFindingsUnder(t, diags, "EntryMixed", []keptFinding{
			keptInfo(diag.UnknownSchemaKeyword, "/allOf/1/unknownKeyword", "unknownKeyword"),
		})
		mixed, ok := kindNode(t, doc, componentID("EntryMixed")).(*ir.Scalar)
		require.True(t, ok)
		require.NotNil(t, mixed.Constraints)
		require.NotNil(t, mixed.Constraints.MinLength)
		assert.Equal(t, int64(2), *mixed.Constraints.MinLength)
		assert.Equal(t, "d", mixed.Docs.Description)
	})
}

func TestKeptEntryKeyword_ACommentAnchorOrDefsOnAnEntryReachesNoNode(t *testing.T) {
	t.Parallel()
	const obj = "{$ref: '#/components/schemas/Obj'}"
	schemas := keptEntryIssueSchemas(
		"Plain: {allOf: ["+obj+"]}",
		"Beside: {allOf: ["+obj+"], $comment: kept-comment-text}",
		"Entry: {allOf: ["+obj+", {$comment: kept-comment-text}]}",
		"EntryAnchor: {allOf: ["+obj+", {$anchor: here}]}",
		"EntryDefs: {allOf: ["+obj+", {$defs: {I: {type: integer}}}]}",
		"EntryDesc: {allOf: ["+obj+", {description: d, $comment: kept-comment-text}]}",
		"ScopedEntry: {allOf: ["+obj+", {$comment: kept-comment-text, $anchor: there, $defs: {I: {type: integer}}}]}",
	)
	doc, diags := compileAnnotationSpec(t, "scoped", admissionSpec("3.1.0", "", schemas...))
	for _, name := range []string{"Plain", "Beside", "Entry", "EntryAnchor", "EntryDefs", "EntryDesc", "ScopedEntry"} {
		assert.Empty(t, keptKeywordsOf(doc, name), "%s keeps nothing under Unmodeled", name)
		keptFindingsUnder(t, diags, name, nil)
	}
	assert.Equal(t, "d", kindNode(t, doc, componentID("EntryDesc")).Common().Docs.Description)
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "kept-comment-text", "a $comment no emitter may see reaches no node")
}

func TestKeptEntryKeyword_ANullableOnAnEntryNoPolicyAppliesIsKeptOutOfScopeUnderBothPolicies(t *testing.T) {
	t.Parallel()
	runKeptNullableCases(t, func(t *testing.T, doc *ir.Document) {
		t.Helper()
		requireKeptKeywords(t, "Entry", keptKeywordsOf(doc, "Entry"),
			map[string]ir.UnmodeledReason{"openapi:nullable": ir.ReasonOutOfScope}, "/components/schemas/Entry/allOf/1", false)
	})
}

func TestKeptEntryKeyword_TwoEntriesKeepingOneKeywordAlikeKeepOneEntryAtThePosition(t *testing.T) {
	t.Parallel()
	for _, version := range keptKeywordVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			schemas := []string{
				"Str: {type: string}",
				"Same: {allOf: [" + keptStr + ", {minimum: 1}, {minimum: 1}]}",
				"SameRev: {allOf: [{minimum: 1}, " + keptStr + ", {minimum: 1}]}",
			}
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "same", admissionSpec(version, "", schemas...))
				for _, name := range []string{"Same", "SameRev"} {
					got := keptKeywordsOf(doc, name)
					require.Equal(t, degradedKept("openapi:minimum"), keptKeywordReasons(got), name)
					require.Len(t, got["openapi:minimum"], 1, name)
					assert.Equal(t, "/components/schemas/"+name, got["openapi:minimum"][0].pointer,
						"%s: two entries keeping it alike leave one entry at the position", name)
					assert.Equal(t, 2, degradedInfosAt(diags, name), "%s: each entry reports what it writes", name)
				}
			})
		})
	}
}

func TestKeptEntryKeyword_AValueSetPrincipalsEntryKeepsWhatTheBesideSpellingKeeps(t *testing.T) {
	t.Parallel()
	for _, version := range keptKeywordVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			schemas := []string{
				"Str: {type: string}",
				"Entry: {allOf: [" + keptStr + ", {enum: [a, b], x-note: 1, fancy: 1}], pattern: '^a'}",
				"Beside: {allOf: [" + keptStr + "], enum: [a, b], x-note: 1, fancy: 1, pattern: '^a'}",
			}
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "valueset", admissionSpec(version, "", schemas...))
				want := map[string]ir.UnmodeledReason{
					"openapi:$ref": ir.ReasonDegradedLowering, "openapi:pattern": ir.ReasonDegradedLowering,
					"openapi:x-note": ir.ReasonVendorExtension, "openapi:fancy": ir.ReasonOutOfScope,
				}
				for _, name := range []string{"Entry", "Beside"} {
					_, ok := kindNode(t, doc, componentID(name)).(*ir.Enum)
					require.True(t, ok, "%s is the value set", name)
					got := keptKeywordsOf(doc, name)
					assert.Equal(t, want, keptKeywordReasons(got), name)
					for key, entries := range got {
						assert.Len(t, entries, 1, "%s: %s is kept once", name, key)
					}
				}
				keptFindingsUnder(t, diags, "Entry", []keptFinding{
					keptDegraded("", "pattern"), keptDegraded("/allOf/0", "$ref"),
					keptInfo(diag.UnknownSchemaKeyword, "/allOf/1/fancy", "fancy"),
				})
				keptFindingsUnder(t, diags, "Beside", []keptFinding{
					keptDegraded("", "pattern"), keptDegraded("/allOf/0", "$ref"),
					keptInfo(diag.UnknownSchemaKeyword, "/fancy", "fancy"),
				})
			})
		})
	}
}
