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

// voicesDoc is a document of Base, an object, and a component S written as written.
func voicesDoc(written string) string {
	return openapitest.ComponentSpecVer("3.1.0", "    Base: {type: object, properties: {id: {type: string}}}\n"+
		"    S: "+written+"\n")
}

// TestVoices_AKeywordFoldsWhereTheAllOfWouldKeepIt holds a keyword on an inline
// branch to the same keyword beside the allOf: for each one the two lower to the
// same node, the branch's kept nowhere else and reported nowhere.
func TestVoices_AKeywordFoldsWhereTheAllOfWouldKeepIt(t *testing.T) {
	t.Parallel()
	for name, keyword := range map[string]string{
		"title":              "title: mine",
		"description":        "description: mine",
		"externalDocs":       "externalDocs: {url: 'https://example.com'}",
		"deprecated":         "deprecated: true",
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
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ref := "{$ref: '#/components/schemas/Base'}"
			beside, besideDiags := parseFull(t, voicesDoc("{allOf: ["+ref+"], "+keyword+"}"))
			branch, branchDiags := parseFull(t, voicesDoc("{allOf: ["+ref+", {"+keyword+"}]}"))
			assert.Empty(t, withCode(branchDiags, diag.DegradedConstruct), "nothing is left to keep or report")
			assert.Len(t, branchDiags, len(besideDiags))

			got, ok := typeByName(branch, "S").(*ir.Model)
			require.True(t, ok)
			want, ok := typeByName(beside, "S").(*ir.Model)
			require.True(t, ok)
			assert.Equal(t, want.Docs, got.Docs)
			assert.Equal(t, want.Deprecation, got.Deprecation)
			assert.Equal(t, want.Sensitive, got.Sensitive)
			assert.Equal(t, want.XML, got.XML)
			assert.Equal(t, want.Constraints, got.Constraints)
			require.Len(t, got.Examples, len(want.Examples))
			assert.Len(t, got.Unmodeled, len(want.Unmodeled))
			for key, entry := range want.Unmodeled {
				if assert.Contains(t, got.Unmodeled, key) {
					assert.JSONEq(t, string(entry.Value), string(got.Unmodeled[key].Value), key)
				}
			}
		})
	}
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
		"two bounds": {"{allOf: [" + ref + ", {maxProperties: 2}, {maxProperties: 3}]}",
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

// TestVoices_ADisagreementIsReportedAsOne pins the words a kept branch draws: its
// keywords have homes, so the report names the disagreement that kept them from
// folding, in a composition and in a refinement, and says neither that the node
// has no home nor that only a merge reads a branch. A branch with nothing to fold
// keeps the wording of what it does keep.
func TestVoices_ADisagreementIsReportedAsOne(t *testing.T) {
	t.Parallel()
	const disagreed = "the conjuncts of this allOf disagree on a keyword this inline branch writes"
	for name, tc := range map[string]struct{ schema, unsaid string }{
		"a composition": {
			"{allOf: [{$ref: '#/components/schemas/Obj'}, {description: one}, {description: two}]}", "only its properties",
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

	_, diags = parseFull(t, openapitest.ComponentSpec("    Obj: {type: object}\n    Other: {type: object}\n"+
		"    T:\n      oneOf: [{$ref: '#/components/schemas/Other'}, {type: object}]\n"+
		"      allOf: [{$ref: '#/components/schemas/Obj'}, {title: t}]\n"))
	for _, d := range withCode(diags, diag.DegradedConstruct) {
		assert.NotContains(t, d.Message, disagreed, "a distributed branch folds nowhere, so it disagrees with nothing")
	}

	deep := "{description: deep, allOf: [{type: object}]}"
	for range schema.MaxAllOfFlattenDepth {
		deep = "{allOf: [" + deep + "]}"
	}
	_, diags = parseFull(t, openapitest.ComponentSpec("    T: {description: top, allOf: ["+deep+"]}\n"))
	kept := withCode(diags, diag.DegradedConstruct)
	require.NotEmpty(t, kept)
	assert.Contains(t, kept[0].Message, "nested past the flattening depth", "a capped branch says so first")
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

// TestVoices_ADistributedCompositionKeepsEveryBranch pins that a branch distributed
// across a union folds nothing: each variant keeps it whole, and the Union the
// position lowers to takes nothing of it.
func TestVoices_ADistributedCompositionKeepsEveryBranch(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, voicesDoc(`{allOf: [{$ref: '#/components/schemas/Base'}, {description: mine, properties: {c: {type: string}}}],
      oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]}
    A: {type: object, properties: {a: {type: string}}}
    B: {type: object, properties: {b: {type: string}}}`))
	openapitest.RequireNoErrorDiags(t, diags)
	u, ok := typeByName(doc, "S").(*ir.Union)
	require.True(t, ok)
	assert.Empty(t, u.Docs.Description)
	for _, v := range u.Variants {
		assert.Contains(t, doc.Types[v.Type.Target].Common().Unmodeled, "openapi:allOf/1")
	}
}

// TestVoices_AUseBindingKeywordReachesItsCarrierOrItsNode pins readOnly and default
// on a branch: a carrier holds them as beside the allOf, and with no carrier a node
// the position owns keeps them verbatim, reported at the keyword.
func TestVoices_AUseBindingKeywordReachesItsCarrierOrItsNode(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    Base: {type: object, properties: {id: {type: string}}}
    Holder:
      type: object
      properties:
        f: {allOf: [{$ref: '#/components/schemas/Base'}, {readOnly: true, default: {id: a}}]}
    Bare: {allOf: [{$ref: '#/components/schemas/Base'}, {readOnly: true}], writeOnly: false}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	f := propertyOf(t, doc, "Holder", "f")
	assert.NotEmpty(t, f.Visibility.Only)
	assert.NotNil(t, f.Default)
	bare, ok := typeByName(doc, "Bare").(*ir.Model)
	require.True(t, ok)
	entry, ok := bare.Unmodeled["openapi:readOnly"]
	require.True(t, ok, "got %v", bare.Unmodeled)
	assert.Equal(t, "/components/schemas/Bare/allOf/1/readOnly", string(entry.Provenance.Pointer))
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

// TestVoices_AUnionBesideKeepsEveryBranch pins an allOf beside a union kept verbatim:
// its Model is no use a branch can speak for, so each branch stays whole.
func TestVoices_AUnionBesideKeepsEveryBranch(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, voicesDoc(`{allOf: [{$ref: '#/components/schemas/Base'}, {description: mine}],
      oneOf: [{required: [a]}, {required: [b]}]}`))
	openapitest.RequireNoErrorDiags(t, diags)
	m, ok := typeByName(doc, "S").(*ir.Model)
	require.True(t, ok)
	assert.Empty(t, m.Docs.Description)
	assert.Contains(t, m.Unmodeled, "openapi:allOf/1")
}
