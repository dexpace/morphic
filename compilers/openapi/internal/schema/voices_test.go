// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"slices"
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

// voicesDoc is a document of Base, A and B, objects, Meta, an object anchored
// dynamically, Holder, an object using S through a $ref, and a component S
// written as written.
func voicesDoc(written string) string {
	return openapitest.ComponentSpecVer("3.1.0", "    Base: {type: object, properties: {id: {type: string}}}\n"+
		"    A: {type: object, properties: {a: {type: string}}}\n"+
		"    B: {type: object, properties: {b: {type: string}}}\n"+
		"    Meta: {$dynamicAnchor: meta, type: object}\n"+
		"    Holder: {type: object, properties: {s: {$ref: '#/components/schemas/S'}}}\n"+
		"    S: "+written+"\n")
}

// besideTheAllOf is what S writes beside its allOf for each outcome the allOf
// composes at: nothing, a union it distributes across, a oneOf and an anyOf kept
// beside its Model, a union narrowing it alone
// (docs/compilers/openapi.md#union-beside-structure), and a $dynamicRef it keeps
// from expanding (docs/compilers/openapi.md#dynamic-ref). code is the report
// showing the outcome reached, drawn at S or at the keyword at names below it.
var besideTheAllOf = map[string]struct{ beside, code, at string }{
	"alone":       {},
	"distributed": {", oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]", diag.CompositionLowering, ""},
	"beside a degraded union": {", oneOf: [{$ref: '#/components/schemas/A'}], anyOf: [{$ref: '#/components/schemas/B'}]",
		diag.DegradedConstruct, ""},
	"beside a narrowing union": {", oneOf: [{required: [a]}, {required: [b]}]", diag.ValidationOnlyKeyword, ""},
	"beside a $dynamicRef":     {", $dynamicRef: '#meta'", diag.DegradedConstruct, "/$dynamicRef"},
}

// TestVoices_AKeywordFoldsWhereTheAllOfWouldKeepIt holds a keyword on an inline
// branch to the same keyword beside the allOf, beside each union outcome: for each
// one the two lower to the same nodes, a $ref use of S among them, the branch's
// kept nowhere else and reported nowhere but where it is written.
func TestVoices_AKeywordFoldsWhereTheAllOfWouldKeepIt(t *testing.T) {
	t.Parallel()
	for name, keyword := range map[string]string{
		"title":              "title: mine",
		"description":        "description: mine",
		"externalDocs":       "externalDocs: {url: 'https://example.com'}",
		"deprecated":         "deprecated: true",
		"readOnly":           "readOnly: true",
		"default":            "default: {id: a}",
		"example":            "example: {id: a}",
		"examples":           "examples: [{id: a}]",
		"xml":                "xml: {name: mine}",
		"an extension":       "x-kept: 1",
		"maxProperties":      "maxProperties: 2",
		"not":                "not: {required: [a]}",
		"if and then":        "if: {required: [a]}, then: {required: [b]}",
		"else alone":         "else: {required: [b]}",
		"contains":           "contains: {type: string}, minContains: 1",
		"maxContains alone":  "maxContains: 1",
		"unevaluatedItems":   "unevaluatedItems: false",
		"a kept unevaluated": "unevaluatedProperties: {type: string}",
		"dependentSchemas":   "dependentSchemas: {a: {required: [b]}}",
		"dependentRequired":  "dependentRequired: {a: [b]}",
		"propertyNames":      "propertyNames: {pattern: '^a'}",
		"a redaction":        "format: password",
	} {
		for shape, tc := range besideTheAllOf {
			t.Run(shape+"/"+name, func(t *testing.T) {
				t.Parallel()
				ref := "{$ref: '#/components/schemas/Base'}"
				want, wantDiags := parseFull(t, voicesDoc("{allOf: ["+ref+"], "+keyword+tc.beside+"}"))
				got, gotDiags := parseFull(t, voicesDoc("{allOf: ["+ref+", {"+keyword+"}]"+tc.beside+"}"))
				if tc.code != "" {
					require.True(t, openapitest.HasDiagCodeAt(gotDiags, tc.code, "/components/schemas/S"+tc.at),
						"the outcome this row names; got %v", gotDiags)
				}
				assert.False(t, openapitest.HasDiagCodeAt(gotDiags, diag.DegradedConstruct, "/components/schemas/S/allOf/1"),
					"nothing is left to keep or report; got %v", gotDiags)
				assert.Equal(t, severityCodes(wantDiags), severityCodes(gotDiags))

				holder := componentID("Holder")
				assertNodeAlike(t, want.Types[holder], got.Types[holder])
				id := componentID("S")
				assertNodeAlike(t, want.Types[id], got.Types[id])
				wantUnion, ok := want.Types[id].(*ir.Union)
				if !ok {
					return
				}
				gotUnion, ok := got.Types[id].(*ir.Union)
				require.True(t, ok)
				require.Len(t, gotUnion.Variants, len(wantUnion.Variants))
				for i, v := range wantUnion.Variants {
					target := gotUnion.Variants[i].Type.Target
					require.Equal(t, v.Type.Target, target)
					assertNodeAlike(t, want.Types[target], got.Types[target])
				}
			})
		}
	}
}

// assertNodeAlike holds got to want whole but for where provenance places each
// part, which differs between the two spellings: what it marks inferred and
// which document wrote it do not.
func assertNodeAlike(t *testing.T, want, got ir.TypeDef) {
	t.Helper()
	require.NotNil(t, want)
	require.NotNil(t, got)
	at := cmpopts.IgnoreFields(ir.Provenance{}, "Pointer", "Position", "Node")
	if diff := cmp.Diff(want, got, at); diff != "" {
		t.Errorf("node mismatch (-beside the allOf +on a branch):\n%s", diff)
	}
}

// severityCodes returns the severity and code of each of diags, sorted, so two
// runs reporting alike at other pointers compare equal.
func severityCodes(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, string(d.Severity)+" "+d.Code)
	}
	slices.Sort(out)
	return out
}

// TestVoices_ABranchDisagreeingFoldsNothingInEveryOrder pins that a keyword two
// conjuncts write differently has no winner: the keywords beside the allOf stand,
// and each branch writing another value stays whole, whichever comes first.
func TestVoices_ABranchDisagreeingFoldsNothingInEveryOrder(t *testing.T) {
	t.Parallel()
	const ref = "{$ref: '#/components/schemas/Base'}"
	for name, tc := range map[string]struct {
		schema string
		kept   []string
	}{
		"two branches": {"{allOf: [" + ref + ", {description: a}, {description: b}]}",
			[]string{"openapi:allOf/1", "openapi:allOf/2"}},
		"swapped": {"{allOf: [{description: b}, {description: a}, " + ref + "]}",
			[]string{"openapi:allOf/0", "openapi:allOf/1"}},
		"beside the allOf": {"{description: a, allOf: [" + ref + ", {description: b}]}", []string{"openapi:allOf/1"}},
		"a third agreeing": {"{allOf: [" + ref + ", {title: t}, {title: t}, {title: u}]}",
			[]string{"openapi:allOf/1", "openapi:allOf/2", "openapi:allOf/3"}},
		"two patterns": {"{allOf: [" + ref + ", {pattern: a}, {pattern: b}]}",
			[]string{"openapi:allOf/1", "openapi:allOf/2"}},
		"a flag": {"{allOf: [" + ref + ", {readOnly: true}, {readOnly: false}]}",
			[]string{"openapi:allOf/1", "openapi:allOf/2"}},
		"example against examples": {"{allOf: [" + ref + ", {example: {id: a}}, {examples: [{id: a}]}]}",
			[]string{"openapi:allOf/1", "openapi:allOf/2"}},
		"the second of two examples": {"{allOf: [" + ref + ", {example: {id: a}, examples: [{id: b}]}, " +
			"{example: {id: a}, examples: [{id: c}]}]}", []string{"openapi:allOf/1", "openapi:allOf/2"}},
		"the second keyword of a contains": {"{allOf: [" + ref + ", {contains: {type: string}, minContains: 1}, " +
			"{contains: {type: string}, minContains: 2}]}", []string{"openapi:allOf/1", "openapi:allOf/2"}},
		"the second keyword of an if": {"{allOf: [" + ref + ", {if: {required: [a]}, then: {required: [b]}}, " +
			"{if: {required: [a]}, then: {required: [c]}}]}", []string{"openapi:allOf/1", "openapi:allOf/2"}},
		"the second keyword of unevaluated": {"{allOf: [" + ref + ", {unevaluatedItems: true, unevaluatedProperties: {type: string}}, " +
			"{unevaluatedItems: true, unevaluatedProperties: {type: integer}}]}", []string{"openapi:allOf/1", "openapi:allOf/2"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, voicesDoc(tc.schema))
			openapitest.RequireNoErrorDiags(t, diags)
			m, ok := typeByName(doc, "S").(*ir.Model)
			require.True(t, ok)
			var kept []string
			for key := range m.Unmodeled {
				kept = append(kept, key)
			}
			assert.ElementsMatch(t, tc.kept, kept)
			assert.Len(t, withCode(diags, diag.DegradedConstruct), len(tc.kept))
		})
	}
	// A disagreeing branch folds nothing, not only the keyword it disagrees on.
	doc, diags := parseFull(t, voicesDoc("{allOf: ["+ref+", {description: a, title: only}, {description: b}]}"))
	openapitest.RequireNoErrorDiags(t, diags)
	m, ok := typeByName(doc, "S").(*ir.Model)
	require.True(t, ok)
	assert.Empty(t, m.Docs.Summary)
	assert.Empty(t, m.Docs.Description, "no precedence chooses between two descriptions")
}

// TestVoices_BoundsMeetInEveryOrder pins value bounds two conjuncts write apart:
// they meet as a redeclared field's do, each taking what they say together, in
// either order, unless a writer states no value, as a lone 3.0 exclusive modifier
// does, where two spellings disagree as any other keyword's.
func TestVoices_BoundsMeetInEveryOrder(t *testing.T) {
	t.Parallel()
	const ref = "{$ref: '#/components/schemas/Base'}"
	for name, tc := range map[string]struct {
		version, a, b string
		want          *ir.Constraints
	}{
		"a count": {"3.1.0", "{maxProperties: 9}", "{maxProperties: 4}", &ir.Constraints{MaxProps: new(int64(4))}},
		"a step":  {"3.1.0", "{multipleOf: 4}", "{multipleOf: 6}", &ir.Constraints{MultipleOf: new(ir.BigVal("12"))}},
		"a bound and its exclusive form": {"3.1.0", "{minimum: 1}", "{exclusiveMinimum: 2}",
			&ir.Constraints{Min: new(ir.BigVal("1")), ExclusiveMin: new(ir.BigVal("2"))}},
		"a 3.0 modifier": {"3.0.3", "{minimum: 5, exclusiveMinimum: true}", "{minimum: 3}",
			&ir.Constraints{Min: new(ir.BigVal("3")), ExclusiveMin: new(ir.BigVal("5"))}},
	} {
		for _, order := range [][2]string{{tc.a, tc.b}, {tc.b, tc.a}} {
			doc, diags := parseFull(t, openapitest.ComponentSpecVer(tc.version,
				"    Base: {type: object}\n    S: {allOf: ["+ref+", "+order[0]+", "+order[1]+"]}\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			m, ok := typeByName(doc, "S").(*ir.Model)
			require.True(t, ok, name)
			assert.Equal(t, tc.want, m.Constraints, "%s: %v", name, order)
			assert.Empty(t, m.Unmodeled, "%s: %v", name, order)
		}
	}

	doc, _ := parseFull(t, voicesDoc("{maxProperties: 3, allOf: ["+ref+", {maxProperties: 5}]}"))
	m, ok := typeByName(doc, "S").(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, &ir.Constraints{MaxProps: new(int64(3))}, m.Constraints, "a bound beside the allOf meets one on a branch")
	assert.Empty(t, m.Unmodeled)

	for _, order := range []string{"{exclusiveMinimum: true}, {exclusiveMinimum: false}", "{exclusiveMinimum: false}, {exclusiveMinimum: true}"} {
		doc, _ := parseFull(t, openapitest.ComponentSpecVer("3.0.3",
			"    Base: {type: object}\n    S: {allOf: ["+ref+", "+order+"]}\n"))
		m, ok := typeByName(doc, "S").(*ir.Model)
		require.True(t, ok)
		assert.Contains(t, m.Unmodeled, "openapi:allOf/1", order)
		assert.Contains(t, m.Unmodeled, "openapi:allOf/2", order)
	}
}

// TestVoices_AgreeingConjunctsSayItOnce pins that a keyword two conjuncts write
// alike is one statement: it folds, once.
func TestVoices_AgreeingConjunctsSayItOnce(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, voicesDoc("{description: same, example: {id: a}, allOf: ["+
		"{$ref: '#/components/schemas/Base'}, {description: same, example: {id: a}}, {description: same}]}"))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, withCode(diags, diag.DegradedConstruct))
	m, ok := typeByName(doc, "S").(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, "same", m.Docs.Description)
	assert.Len(t, m.Examples, 1)
}

// TestVoices_ANullKeywordWritesNothing pins a branch writing a keyword as null,
// which reads as the keyword left out: it disagrees with no conjunct, so what
// another branch writes folds, in either order, as it does with the key removed.
func TestVoices_ANullKeywordWritesNothing(t *testing.T) {
	t.Parallel()
	for _, branches := range []string{"{description: null}, {description: d}", "{description: d}, {description: null}"} {
		doc, _ := parseFull(t, voicesDoc("{allOf: [{$ref: '#/components/schemas/Base'}, "+branches+"]}"))
		m, ok := typeByName(doc, "S").(*ir.Model)
		require.True(t, ok, branches)
		assert.Equal(t, "d", m.Docs.Description, branches)
		assert.Empty(t, m.Unmodeled, branches)
	}
}

// TestVoices_ADisagreementIsReportedAsOne pins the words a kept branch draws: its
// keywords have homes, so the report names the disagreement that kept them from
// folding, in a composition and in a refinement, and says neither that the node
// has no home nor that nothing reads it. A branch with nothing to fold keeps the
// wording of what it does keep.
func TestVoices_ADisagreementIsReportedAsOne(t *testing.T) {
	t.Parallel()
	const disagreed = "the conjuncts of this allOf disagree on a keyword this inline branch writes"
	for name, tc := range map[string]struct{ schema, unsaid string }{
		"a composition": {
			"{allOf: [{$ref: '#/components/schemas/Obj'}, {description: one}, {description: two}]}", "nothing reads",
		},
		"a refinement": {
			"{allOf: [{$ref: '#/components/schemas/Str'}, {description: one}, {description: two}]}", "has no home",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, diags := parseFull(t, openapitest.ComponentSpec(
				"    Obj: {type: object}\n    Str: {type: string}\n    T: "+tc.schema+"\n"))
			for _, at := range []string{"/components/schemas/T/allOf/1", "/components/schemas/T/allOf/2"} {
				got := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, at)
				assert.Contains(t, got, disagreed)
				assert.NotContains(t, got, tc.unsaid)
			}
		})
	}

	_, diags := parseFull(t, openapitest.ComponentSpec(
		"    Str: {type: string}\n    T: {allOf: [{$ref: '#/components/schemas/Str'}, {format: uuid}, {description: d}]}\n"))
	got := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/T/allOf/1")
	assert.Contains(t, got, "has no home for what this inline branch adds", "nothing was disagreed on")

	_, diags = compileNullable(t, openapitest.ComponentSpec(
		"    Str: {type: string}\n    T: {allOf: [{$ref: '#/components/schemas/Str'}, {nullable: true}]}\n"), true)
	got = openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/T/allOf/1")
	assert.NotContains(t, got, disagreed, "a keyword the policy keeps from folding was disagreed on by nothing")

	_, diags = parseFull(t, openapitest.ComponentSpec("    Obj: {type: object}\n"+
		"    T: {allOf: [{$ref: '#/components/schemas/Obj'}, {nullable: true, properties: {a: {type: string}}}]}\n"))
	got = openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/T/allOf/1")
	assert.NotContains(t, got, disagreed, "a branch that folds keeps what it declares of a shape, disagreed on by nothing")

	deep := "{description: deep, allOf: [{type: object}]}"
	for range schema.MaxAllOfFlattenDepth {
		deep = "{allOf: [" + deep + "]}"
	}
	_, diags = parseFull(t, openapitest.ComponentSpec("    T: {description: top, allOf: ["+deep+"]}\n"))
	kept := withCode(diags, diag.DegradedConstruct)
	require.NotEmpty(t, kept)
	assert.Contains(t, kept[0].Message, "nested past the flattening depth", "a capped branch says so first")
}

// TestVoices_AnExampleRenderingNothingStillAgrees pins that a keyword renders to
// nothing without failing: two conjuncts writing `example: null` agree, so both
// fold, where one that cannot be rendered agrees with nothing.
func TestVoices_AnExampleRenderingNothingStillAgrees(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, voicesDoc("{allOf: [{$ref: '#/components/schemas/Base'}, "+
		"{example: null, description: d}, {example: null}]}"))
	for _, at := range []string{"/components/schemas/S/allOf/1", "/components/schemas/S/allOf/2"} {
		assert.False(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, at), at)
	}
	m, ok := doc.Types["t/openapi/components/schemas/S"].(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, "d", m.Docs.Description)
	assert.Empty(t, m.Unmodeled)
}

// TestVoices_AValueThatWillNotRenderAgreesWithNothing pins two branches writing one
// value no JSON holds: neither can be shown to say what the other does, so neither
// folds, and each is reported as the whole branch it could not keep.
func TestVoices_AValueThatWillNotRenderAgreesWithNothing(t *testing.T) {
	t.Parallel()
	const bad = "{x-t: " + unpreservableValue + "}"
	_, diags := parseFull(t, voicesDoc("{allOf: [{$ref: '#/components/schemas/Base'}, "+bad+", "+bad+"]}"))
	for _, at := range []string{"/components/schemas/S/allOf/1", "/components/schemas/S/allOf/2"} {
		assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnpreservableConstruct, at), at)
	}
}

// TestVoices_KeywordsOneEntryHoldsDisagreeAsOne pins the keywords a position keeps
// in one entry: written on two branches they would overwrite each other, so they
// are compared as the entry. The same entry written twice folds.
func TestVoices_KeywordsOneEntryHoldsDisagreeAsOne(t *testing.T) {
	t.Parallel()
	const ref = "{$ref: '#/components/schemas/Base'}"
	for name, branches := range map[string]string{
		"if and then":            "{if: {required: [a]}}, {then: {required: [b]}}",
		"contains and its bound": "{contains: {type: string}}, {minContains: 1}",
		"unevaluated":            "{unevaluatedItems: true}, {unevaluatedProperties: {type: string}}",
		"unrenderable if":        "{if: {required: [a]}}, {if: {x-t: " + unpreservableValue + "}}",
		"unrenderable if first":  "{if: {x-t: " + unpreservableValue + "}}, {if: {required: [a]}}",
		"both unrenderable":      "{if: {x-t: " + unpreservableValue + "}}, {if: {x-t: " + unpreservableValue + "}}",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, _ := parseFull(t, voicesDoc("{allOf: ["+ref+", "+branches+"]}"))
			m, ok := typeByName(doc, "S").(*ir.Model)
			require.True(t, ok)
			assert.NotContains(t, m.Unmodeled, "openapi:if-then-else")
			assert.NotContains(t, m.Unmodeled, "openapi:contains")
			assert.NotContains(t, m.Unmodeled, "openapi:unevaluated")
		})
	}
	doc, diags := parseFull(t, voicesDoc("{allOf: ["+ref+", {if: {required: [a]}}, {if: {required: [a]}}]}"))
	openapitest.RequireNoErrorDiags(t, diags)
	m, ok := typeByName(doc, "S").(*ir.Model)
	require.True(t, ok)
	assert.Contains(t, m.Unmodeled, "openapi:if-then-else")
}

// TestVoices_UnevaluatedFalseStaysTheModels pins the one validation keyword no
// annotation reader keeps: `unevaluatedProperties: false` is a model's openness,
// so on a branch it stays residue rather than folding to nowhere.
func TestVoices_UnevaluatedFalseStaysTheModels(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, voicesDoc("{allOf: [{$ref: '#/components/schemas/Base'}, {unevaluatedProperties: false}]}"))
	openapitest.RequireNoErrorDiags(t, diags)
	m, ok := typeByName(doc, "S").(*ir.Model)
	require.True(t, ok)
	assert.Contains(t, m.Unmodeled, "openapi:allOf/1")
}

// TestVoices_AUseBindingKeywordReachesItsCarrierOrItsNode pins readOnly and default
// on a branch: a carrier holds them as beside the allOf, a union distributed across
// it or not, and with no carrier a node the position owns keeps them verbatim,
// reported at the keyword.
func TestVoices_AUseBindingKeywordReachesItsCarrierOrItsNode(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    Base: {type: object, properties: {id: {type: string}}}
    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}
    Holder:
      type: object
      properties:
        f: {allOf: [{$ref: '#/components/schemas/Base'}, {readOnly: true, default: {id: a}}]}
        g:
          allOf: [{$ref: '#/components/schemas/Base'}, {readOnly: true, default: {id: a}}]
          oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]
    Bare: {allOf: [{$ref: '#/components/schemas/Base'}, {readOnly: true}, {readOnly: true}], writeOnly: false}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	for _, name := range []string{"f", "g"} {
		p := propertyOf(t, doc, "Holder", name)
		assert.NotEmpty(t, p.Visibility.Only, name)
		assert.NotNil(t, p.Default, name)
		assert.Empty(t, doc.Types[p.Type.Target].Common().Unmodeled, name)
	}
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.CompositionLowering, "/components/schemas/Holder/properties/g"))
	bare, ok := typeByName(doc, "Bare").(*ir.Model)
	require.True(t, ok)
	entry, ok := bare.Unmodeled["openapi:readOnly"]
	require.True(t, ok, "got %v", bare.Unmodeled)
	assert.Equal(t, "/components/schemas/Bare/allOf/1/readOnly", string(entry.Provenance.Pointer), "the nearest of the two")
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/components/schemas/Bare/allOf/1/readOnly"))
}

// TestVoices_APromotedExtensionReadsFromABranch pins a key the promotion policy
// maps: on a branch it reads as it does beside the allOf.
func TestVoices_APromotedExtensionReadsFromABranch(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    Base: {type: object, properties: {a: {type: string}}}
    Plain: {type: string}
    Composed:
      allOf:
        - {$ref: '#/components/schemas/Base'}
        - {x-deprecated-reason: gone, deprecated: true}
    Holder:
      type: object
      properties:
        f: {allOf: [{$ref: '#/components/schemas/Plain'}, {deprecated: true, x-deprecated-reason: gone}]}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	composed, ok := typeByName(doc, "Composed").(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, composed.Deprecation)
	assert.Equal(t, "gone", composed.Deprecation.Message)
	f := propertyOf(t, doc, "Holder", "f")
	require.NotNil(t, f.Deprecation)
	assert.Equal(t, "gone", f.Deprecation.Message)
}

// TestVoices_ARefBranchIsNoVoice pins that what a $ref branch writes beside its
// $ref stays on the alias it hoists: it neither folds to the position nor
// disagrees with a branch that does.
func TestVoices_ARefBranchIsNoVoice(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, voicesDoc("{allOf: [{$ref: '#/components/schemas/Base', description: beside}, {title: mine, description: other}]}"))
	openapitest.RequireNoErrorDiags(t, diags)
	m, ok := typeByName(doc, "S").(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, "other", m.Docs.Description)
	assert.Equal(t, "mine", m.Docs.Summary)
	assert.Empty(t, m.Unmodeled)
	require.NotNil(t, m.Base)
	assert.Equal(t, "beside", doc.Types[m.Base.Target].Common().Docs.Description)
}
