// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// foldedEntriesDoc is a document of Base, A and B, objects, Meta, an object
// anchored dynamically, Holder, an object using S through a $ref, and a
// component S written as written.
func foldedEntriesDoc(written string) string {
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

// TestAllOfEntryFolding_AKeywordFoldsWhereTheAllOfWouldKeepIt holds a keyword
// on an inline entry to the same keyword beside the allOf, at each outcome
// besideTheAllOf names: for each one the two lower to the same nodes, a $ref
// use of S among them, the entry's kept nowhere else and reported nowhere but
// where it is written.
func TestAllOfEntryFolding_AKeywordFoldsWhereTheAllOfWouldKeepIt(t *testing.T) {
	t.Parallel()
	for name, keyword := range map[string]string{
		"title":             "title: mine",
		"description":       "description: mine",
		"externalDocs":      "externalDocs: {url: 'https://example.com'}",
		"deprecated":        "deprecated: true",
		"readOnly":          "readOnly: true",
		"default":           "default: {id: a}",
		"example":           "example: {id: a}",
		"examples":          "examples: [{id: a}]",
		"xml":               "xml: {name: mine}",
		"an extension":      "x-kept: 1",
		"maxProperties":     "maxProperties: 2",
		"not":               "not: {required: [a]}",
		"if and then":       "if: {required: [a]}, then: {required: [b]}",
		"else alone":        "else: {required: [b]}",
		"contains":          "contains: {type: string}, minContains: 1",
		"maxContains alone": "maxContains: 1",
		"dependentSchemas":  "dependentSchemas: {a: {required: [b]}}",
		"dependentRequired": "dependentRequired: {a: [b]}",
		"propertyNames":     "propertyNames: {pattern: '^a'}",
		"a redaction":       "format: password",
	} {
		for shape, tc := range besideTheAllOf {
			t.Run(shape+"/"+name, func(t *testing.T) {
				t.Parallel()
				ref := "{$ref: '#/components/schemas/Base'}"
				want, wantDiags := parseFull(t, foldedEntriesDoc("{allOf: ["+ref+"], "+keyword+tc.beside+"}"))
				got, gotDiags := parseFull(t, foldedEntriesDoc("{allOf: ["+ref+", {"+keyword+"}]"+tc.beside+"}"))
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
				for i, variant := range wantUnion.Variants {
					target := gotUnion.Variants[i].Type.Target
					require.Equal(t, variant.Type.Target, target)
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
		t.Errorf("node mismatch (-beside the allOf +on an entry):\n%s", diff)
	}
}

// severityCodes returns the severity and code of each of diags, sorted, so two
// runs reporting alike at other pointers compare equal.
func severityCodes(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, diagnostic := range diags {
		out = append(out, string(diagnostic.Severity)+" "+diagnostic.Code)
	}
	slices.Sort(out)
	return out
}

// TestVoices_KeptAndMetEntriesHoldAtEveryOutcome pins that what keeps an entry
// whole, or meets two, holds at each outcome besideTheAllOf names as it does
// alone (docs/compilers/openapi.md#allof-inline-residue): an entry disagreeing
// with a conjunct, or reading a $ref against another base, stays whole on each
// Model the composition lowers to, with one report saying why and nothing it
// writes folded, and two bounds meet there in either order.
func TestVoices_KeptAndMetEntriesHoldAtEveryOutcome(t *testing.T) {
	t.Parallel()
	const ref = "{$ref: '#/components/schemas/Base'}"
	const n = "\n    N: {$id: 'https://ex.test/a/n.json', type: string}" // what `n.json` names under https://ex.test/a/
	met := &ir.Constraints{MaxProperties: new(int64(3))}
	for name, tc := range map[string]struct {
		entries string
		kept    []string
		why     string
		cons    *ir.Constraints
	}{
		"a disagreement": {ref + ", {description: a}, {description: b}", []string{"allOf/1", "allOf/2"},
			"the conjuncts of this allOf disagree", nil},
		"a disagreement swapped": {"{description: b}, {description: a}, " + ref, []string{"allOf/0", "allOf/1"},
			"the conjuncts of this allOf disagree", nil},
		"another base": {ref + ", {$id: 'https://ex.test/a/x.json', not: {$ref: 'n.json'}, description: d}",
			[]string{"allOf/1"}, "reads its $refs against another base", nil},
		"bounds meeting":         {ref + ", {maxProperties: 3}, {maxProperties: 5}", nil, "", met},
		"bounds meeting swapped": {ref + ", {maxProperties: 5}, {maxProperties: 3}", nil, "", met},
	} {
		for shape, outcome := range besideTheAllOf {
			t.Run(shape+"/"+name, func(t *testing.T) {
				t.Parallel()
				doc, diags := parseFull(t, foldedEntriesDoc("{allOf: ["+tc.entries+"]"+outcome.beside+"}"+n))
				openapitest.RequireNoErrorDiags(t, diags)
				if outcome.code != "" {
					require.True(t, openapitest.HasDiagCodeAt(diags, outcome.code, "/components/schemas/S"+outcome.at),
						"the outcome this row names; got %v", diags)
				}
				for _, path := range tc.kept {
					got := openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/S/"+path)
					assert.Contains(t, got, tc.why, path)
				}
				assert.Empty(t, typeByName(doc, "S").Common().Docs.Description, "nothing a kept entry writes folds")
				for _, model := range composedModels(t, doc) {
					assert.ElementsMatch(t, tc.kept, keptEntries(model), model.ID)
					assert.Equal(t, tc.cons, model.Constraints, model.ID)
				}
			})
		}
	}
}

// composedModels returns the Models S's composition lowers to: S itself, or each
// variant of the Union a distributed union lowers it to.
func composedModels(t *testing.T, doc *ir.Document) []*ir.Model {
	t.Helper()
	typeDef := typeByName(doc, "S")
	union, distributed := typeDef.(*ir.Union)
	if !distributed {
		model, ok := typeDef.(*ir.Model)
		require.True(t, ok, "S lowered to a %T, want a Model or a Union", typeDef)
		return []*ir.Model{model}
	}
	out := make([]*ir.Model, 0, len(union.Variants))
	for _, variant := range union.Variants {
		model, ok := doc.Types[variant.Type.Target].(*ir.Model)
		require.True(t, ok, "variant %s lowered to a %T, want a Model", variant.Type.Target, doc.Types[variant.Type.Target])
		out = append(out, model)
	}
	return out
}

// keptEntries returns the path of each allOf entry m keeps whole.
func keptEntries(model *ir.Model) []string {
	var out []string
	for key := range model.Unmodeled {
		if path, ok := strings.CutPrefix(key, "openapi:"); ok && strings.HasPrefix(path, "allOf/") {
			out = append(out, path)
		}
	}
	return out
}

// TestAllOfEntryFolding_AFalseFlagAgreesWithATrue pins the boolean annotations
// JSON Schema reads in force where any occurrence is true: an entry writing one
// false says nothing another's true disagrees with, so both fold, in either
// order, and the use reads the flag in force.
func TestAllOfEntryFolding_AFalseFlagAgreesWithATrue(t *testing.T) {
	t.Parallel()
	const ref = "{$ref: '#/components/schemas/Base'}"
	for _, flags := range []string{"{readOnly: true}, {readOnly: false}", "{readOnly: false}, {readOnly: true}",
		"{deprecated: false}, {deprecated: true}", "{writeOnly: false}, {writeOnly: true}"} {
		t.Run(flags, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, foldedEntriesDoc("{allOf: ["+ref+", "+flags+"]}"))
			openapitest.RequireNoErrorDiags(t, diags)
			model, ok := typeByName(doc, "S").(*ir.Model)
			require.True(t, ok)
			assert.Empty(t, keptEntries(model), "no entry disagrees, so each folds")
			use := propertyOf(t, doc, "Holder", "s")
			assert.True(t, use.Deprecation != nil || len(use.Visibility.Only) > 0, "the true is in force at the use")
		})
	}
}

// TestVoices_AnEntryDisagreeingFoldsNothingInEveryOrder pins that a keyword two
// conjuncts write differently has no winner: the keywords beside the allOf stand,
// and each entry writing another value stays whole, whichever comes first.
func TestVoices_AnEntryDisagreeingFoldsNothingInEveryOrder(t *testing.T) {
	t.Parallel()
	const ref = "{$ref: '#/components/schemas/Base'}"
	for name, tc := range map[string]struct {
		schema string
		kept   []string
	}{
		"two entries": {"{allOf: [" + ref + ", {description: a}, {description: b}]}",
			[]string{"openapi:allOf/1", "openapi:allOf/2"}},
		"swapped": {"{allOf: [{description: b}, {description: a}, " + ref + "]}",
			[]string{"openapi:allOf/0", "openapi:allOf/1"}},
		"beside the allOf": {"{description: a, allOf: [" + ref + ", {description: b}]}", []string{"openapi:allOf/1"}},
		"a third agreeing": {"{allOf: [" + ref + ", {title: t}, {title: t}, {title: u}]}",
			[]string{"openapi:allOf/1", "openapi:allOf/2", "openapi:allOf/3"}},
		"two patterns": {"{allOf: [" + ref + ", {pattern: a}, {pattern: b}]}",
			[]string{"openapi:allOf/1", "openapi:allOf/2"}},
		"example against examples": {"{allOf: [" + ref + ", {example: {id: a}}, {examples: [{id: a}]}]}",
			[]string{"openapi:allOf/1", "openapi:allOf/2"}},
		"the second of two examples": {"{allOf: [" + ref + ", {example: {id: a}, examples: [{id: b}]}, " +
			"{example: {id: a}, examples: [{id: c}]}]}", []string{"openapi:allOf/1", "openapi:allOf/2"}},
		"the second keyword of a contains": {"{allOf: [" + ref + ", {contains: {type: string}, minContains: 1}, " +
			"{contains: {type: string}, minContains: 2}]}", []string{"openapi:allOf/1", "openapi:allOf/2"}},
		"the second keyword of an if": {"{allOf: [" + ref + ", {if: {required: [a]}, then: {required: [b]}}, " +
			"{if: {required: [a]}, then: {required: [c]}}]}", []string{"openapi:allOf/1", "openapi:allOf/2"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, foldedEntriesDoc(tc.schema))
			openapitest.RequireNoErrorDiags(t, diags)
			model, ok := typeByName(doc, "S").(*ir.Model)
			require.True(t, ok)
			var kept []string
			for key := range model.Unmodeled {
				kept = append(kept, key)
			}
			assert.ElementsMatch(t, tc.kept, kept)
			// An entry holding validation logic alone is reported as that.
			reports := append(withCode(diags, diag.DegradedConstruct), withCode(diags, diag.ValidationOnlyKeyword)...)
			assert.Len(t, reports, len(tc.kept))
		})
	}
	// A disagreeing entry folds nothing, not only the keyword it disagrees on.
	doc, diags := parseFull(t, foldedEntriesDoc("{allOf: ["+ref+", {description: a, title: only}, {description: b}]}"))
	openapitest.RequireNoErrorDiags(t, diags)
	irModel, ok := typeByName(doc, "S").(*ir.Model)
	require.True(t, ok)
	assert.Empty(t, irModel.Docs.Summary)
	assert.Empty(t, irModel.Docs.Description, "no precedence chooses between two descriptions")
}

// TestAllOfEntryFolding_BoundsMeetInEveryOrder pins value bounds two conjuncts
// write apart: they meet as a redeclared field's do, each taking what they say
// together, in either order. A writer stating no value, as a 3.0 exclusive
// modifier with no bound beside it or one of false, parts none (GitHub #1494):
// both fold, and the modifier that bounds nothing is kept and warned of at its
// own entry. A count folds into the model a Base declaring no type composes,
// and a numeric bound into the scalar a refinement of a number lowers to, the
// node of a kind that takes it.
func TestAllOfEntryFolding_BoundsMeetInEveryOrder(t *testing.T) {
	t.Parallel()
	const ref = "{$ref: '#/components/schemas/Base'}"
	const (
		untyped = "{description: declares no type}"
		number  = "{type: number}"
	)
	for name, tc := range map[string]struct {
		version, base, a, b string
		want                *ir.Constraints
	}{
		"a count": {"3.1.0", untyped, "{maxProperties: 9}", "{maxProperties: 4}", &ir.Constraints{MaxProperties: new(int64(4))}},
		"a step":  {"3.1.0", number, "{multipleOf: 4}", "{multipleOf: 6}", &ir.Constraints{MultipleOf: new(ir.BigVal("12"))}},
		"a bound and its exclusive form": {"3.1.0", number, "{minimum: 1}", "{exclusiveMinimum: 2}",
			&ir.Constraints{Min: new(ir.BigVal("1")), ExclusiveMin: new(ir.BigVal("2"))}},
		"a 3.0 modifier": {"3.0.3", number, "{minimum: 5, exclusiveMinimum: true}", "{minimum: 3}",
			&ir.Constraints{Min: new(ir.BigVal("3")), ExclusiveMin: new(ir.BigVal("5"))}},
		"a 3.0 modifier beside one of false": {"3.0.3", number, "{minimum: 3, exclusiveMinimum: true}", "{exclusiveMinimum: false}",
			&ir.Constraints{ExclusiveMin: new(ir.BigVal("3"))}},
	} {
		for _, order := range [][2]string{{tc.a, tc.b}, {tc.b, tc.a}} {
			doc, diags := parseFull(t, openapitest.ComponentSpecVer(tc.version,
				"    Base: "+tc.base+"\n    S: {allOf: ["+ref+", "+order[0]+", "+order[1]+"]}\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			node := typeByName(doc, "S")
			require.NotNil(t, node, name)
			assert.Equal(t, tc.want, heldBounds(node), "%s: %v", name, order)
			assert.Empty(t, node.Common().Unmodeled, "%s: %v", name, order)
		}
	}

	doc, _ := parseFull(t, foldedEntriesDoc("{maxProperties: 3, allOf: ["+ref+", {maxProperties: 5}]}"))
	model, ok := typeByName(doc, "S").(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, &ir.Constraints{MaxProperties: new(int64(3))}, model.Constraints, "a bound beside the allOf meets one on an entry")
	assert.Empty(t, model.Unmodeled)

	for i, order := range []string{"{exclusiveMinimum: true}, {exclusiveMinimum: false}", "{exclusiveMinimum: false}, {exclusiveMinimum: true}"} {
		doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.0.3",
			"    Base: "+number+"\n    S: {allOf: ["+ref+", "+order+"]}\n"))
		sc, ok := typeByName(doc, "S").(*ir.Scalar)
		require.True(t, ok)
		at := fmt.Sprintf("/components/schemas/S/allOf/%d", i+1)
		assert.Equal(t, []string{"openapi:exclusiveMinimum"}, slices.Collect(maps.Keys(sc.Unmodeled)), order)
		assert.Equal(t, jsontext.Pointer(at+"/exclusiveMinimum"), sc.Unmodeled["openapi:exclusiveMinimum"].Provenance.Pointer, order)
		require.Len(t, diags, 1, order)
		assert.Equal(t, ir.SeverityWarning, diags[0].Severity, order)
		assert.Equal(t, jsontext.Pointer(at), diags[0].Provenance.Pointer, order)
		assert.Nil(t, sc.Constraints, order)
	}
}

// TestVoices_ABoundTheComposedModelCannotTakeKeepsItsEntry pins a value bound
// written beside a Base declaring an object: the allOf lowers to a model, which
// takes property counts alone, so it holds the count written beside the allOf
// and keeps the string bound there under its keyword, and the numeric bound each
// inline entry writes under its own, each with one info at its entry, in
// either order of the entries (docs/compilers/openapi.md#constraints).
func TestVoices_ABoundTheComposedModelCannotTakeKeepsItsEntry(t *testing.T) {
	t.Parallel()
	const ref = "{$ref: '#/components/schemas/Base'}"
	for _, order := range [][2]string{{"{multipleOf: 4}", "{maximum: 9}"}, {"{maximum: 9}", "{multipleOf: 4}"}} {
		doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    Base: {type: object, description: an object}\n"+
			"    S: {allOf: ["+ref+", "+order[0]+", "+order[1]+"], minLength: 2, maxProperties: 3}\n"))
		openapitest.RequireNoErrorDiags(t, diags)
		model, ok := typeByName(doc, "S").(*ir.Model)
		require.True(t, ok, "%v", order)
		assert.Equal(t, &ir.Constraints{MaxProperties: new(int64(3))}, model.Constraints, "%v", order)
		assert.ElementsMatch(t, []string{"openapi:multipleOf", "openapi:maximum", "openapi:minLength"},
			slices.Collect(maps.Keys(model.Unmodeled)), "%v", order)
		assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/S"),
			"no home for minLength", "%v", order)
		for i, entry := range order {
			at := fmt.Sprintf("/components/schemas/S/allOf/%d", i+1)
			keyword := strings.Trim(strings.Split(entry, ":")[0], "{")
			assert.Equal(t, ir.ReasonDegradedLowering, model.Unmodeled["openapi:"+keyword].Reason, "%v", order)
			assert.Equal(t, jsontext.Pointer(at+"/"+keyword), model.Unmodeled["openapi:"+keyword].Provenance.Pointer, "%v", order)
			assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, at),
				"no home for "+keyword+" written on this inline entry", "%v", order)
		}
	}
}

// TestAllOfEntryFolding_ARefusedBoundPartsNoWriter pins a bound beside one the
// rule table refuses, in either order (GitHub #1494): the refused value states
// nothing, so both entries fold, the stated bound reaches the node, and the
// refusal is reported once, at the keyword, as it is beside the allOf. Base is
// a number, so the node the entries fold into, a scalar over it, takes the
// bound.
func TestAllOfEntryFolding_ARefusedBoundPartsNoWriter(t *testing.T) {
	t.Parallel()
	const ref = "{$ref: '#/components/schemas/Base'}"
	for i, order := range []string{"{exclusiveMinimum: 5}, {exclusiveMinimum: true}", "{exclusiveMinimum: true}, {exclusiveMinimum: 5}"} {
		doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0",
			"    Base: {type: number}\n    S: {allOf: ["+ref+", "+order+"]}\n"))
		sc, ok := typeByName(doc, "S").(*ir.Scalar)
		require.True(t, ok)
		assert.Equal(t, &ir.Constraints{ExclusiveMin: new(ir.BigVal("5"))}, sc.Constraints, order)
		assert.Empty(t, sc.Unmodeled, order)
		require.Len(t, diags, 1, order)
		assert.Equal(t, diag.ExclusiveBoundForm, diags[0].Code, order)
		assert.Equal(t, jsontext.Pointer(fmt.Sprintf("/components/schemas/S/allOf/%d/exclusiveMinimum", 2-i)),
			diags[0].Provenance.Pointer, order)
	}
}

// TestAllOfEntryFolding_AgreeingConjunctsSayItOnce pins that a keyword two
// conjuncts write alike is one statement: it folds, once.
func TestAllOfEntryFolding_AgreeingConjunctsSayItOnce(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, foldedEntriesDoc("{description: same, example: {id: a}, allOf: ["+
		"{$ref: '#/components/schemas/Base'}, {description: same, example: {id: a}}, {description: same}]}"))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, withCode(diags, diag.DegradedConstruct))
	model, ok := typeByName(doc, "S").(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, "same", model.Docs.Description)
	assert.Len(t, model.Examples, 1)
}

// TestAllOfEntryFolding_ANullKeywordWritesNothing pins an entry writing a
// keyword as null, which reads as the keyword left out: it disagrees with no
// conjunct, so what another entry writes folds, in either order, as it does
// with the key removed.
func TestAllOfEntryFolding_ANullKeywordWritesNothing(t *testing.T) {
	t.Parallel()
	for _, entries := range []string{"{description: null}, {description: d}", "{description: d}, {description: null}"} {
		doc, _ := parseFull(t, foldedEntriesDoc("{allOf: [{$ref: '#/components/schemas/Base'}, "+entries+"]}"))
		model, ok := typeByName(doc, "S").(*ir.Model)
		require.True(t, ok, entries)
		assert.Equal(t, "d", model.Docs.Description, entries)
		assert.Empty(t, model.Unmodeled, entries)
	}
}

// TestAllOfEntryFolding_ADisagreementIsReportedAsOne pins the words a kept
// entry draws: its keywords have homes, so the report names the disagreement
// that kept them from folding, in a composition and in a refinement, and says
// neither that the node has no home nor that nothing reads it. An entry with
// nothing to fold keeps the wording of what it does keep.
func TestAllOfEntryFolding_ADisagreementIsReportedAsOne(t *testing.T) {
	t.Parallel()
	const disagreed = "the conjuncts of this allOf disagree on a keyword this inline entry writes"
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
	assert.Contains(t, got, "has no home for format written on this inline entry", "nothing was disagreed on")

	_, diags = compileNullable(t, openapitest.ComponentSpec(
		"    Str: {type: string}\n    T: {allOf: [{$ref: '#/components/schemas/Str'}, {nullable: true}]}\n"), true)
	got = openapitest.DiagMessageAt(t, diags, diag.UnknownSchemaKeyword, ir.SeverityInfo, "/components/schemas/T/allOf/1/nullable")
	assert.NotContains(t, got, disagreed, "a keyword the policy keeps from folding was disagreed on by nothing")

	_, diags = parseFull(t, openapitest.ComponentSpec("    Obj: {type: object}\n"+
		"    T: {allOf: [{$ref: '#/components/schemas/Obj'}, {nullable: true, properties: {a: {type: string}}}]}\n"))
	got = openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/T/allOf/1")
	assert.NotContains(t, got, disagreed, "an entry that folds keeps what it declares of a shape, disagreed on by nothing")

	deep := "{description: deep, allOf: [{type: object}]}"
	for range statement.MaxFlattenDepth {
		deep = "{allOf: [" + deep + "]}"
	}
	_, diags = parseFull(t, openapitest.ComponentSpec("    T: {description: top, allOf: ["+deep+"]}\n"))
	kept := withCode(diags, diag.DegradedConstruct)
	require.NotEmpty(t, kept)
	assert.Contains(t, kept[0].Message, "nested past the flattening depth", "a capped entry says so first")
}

// TestAllOfEntryFolding_AnExampleRenderingNothingStillAgrees pins that a
// keyword renders to nothing without failing: two conjuncts writing
// `example: null` agree, so both fold, where one that cannot be rendered agrees
// with nothing.
func TestAllOfEntryFolding_AnExampleRenderingNothingStillAgrees(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, foldedEntriesDoc("{allOf: [{$ref: '#/components/schemas/Base'}, "+
		"{example: null, description: d}, {example: null}]}"))
	for _, at := range []string{"/components/schemas/S/allOf/1", "/components/schemas/S/allOf/2"} {
		assert.False(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, at), at)
	}
	model, ok := doc.Types["type/openapi/components/schemas/S"].(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, "d", model.Docs.Description)
	assert.Empty(t, model.Unmodeled)
}

// TestAllOfEntryFolding_AValueThatWillNotRenderAgreesWithNothing pins two
// entries writing one value no JSON holds: neither can be shown to say what the
// other does, so neither folds, and each is reported as the whole entry it
// could not keep.
func TestAllOfEntryFolding_AValueThatWillNotRenderAgreesWithNothing(t *testing.T) {
	t.Parallel()
	const bad = "{x-t: " + unpreservableValue + "}"
	_, diags := parseFull(t, foldedEntriesDoc("{allOf: [{$ref: '#/components/schemas/Base'}, "+bad+", "+bad+"]}"))
	for _, at := range []string{"/components/schemas/S/allOf/1", "/components/schemas/S/allOf/2"} {
		assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnpreservableConstruct, at), at)
	}
}

// TestAllOfEntryFolding_KeywordsOneEntryHoldsDisagreeAsOne pins the keywords a
// position keeps in one entry: written on two entries they would overwrite each
// other, so they are compared as the entry. The same entry written twice folds.
func TestAllOfEntryFolding_KeywordsOneEntryHoldsDisagreeAsOne(t *testing.T) {
	t.Parallel()
	const ref = "{$ref: '#/components/schemas/Base'}"
	for name, entries := range map[string]string{
		"if and then":            "{if: {required: [a]}}, {then: {required: [b]}}",
		"contains and its bound": "{contains: {type: string}}, {minContains: 1}",
		"unrenderable if":        "{if: {required: [a]}}, {if: {x-t: " + unpreservableValue + "}}",
		"unrenderable if first":  "{if: {x-t: " + unpreservableValue + "}}, {if: {required: [a]}}",
		"both unrenderable":      "{if: {x-t: " + unpreservableValue + "}}, {if: {x-t: " + unpreservableValue + "}}",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, _ := parseFull(t, foldedEntriesDoc("{allOf: ["+ref+", "+entries+"]}"))
			model, ok := typeByName(doc, "S").(*ir.Model)
			require.True(t, ok)
			assert.NotContains(t, model.Unmodeled, "openapi:if-then-else")
			assert.NotContains(t, model.Unmodeled, "openapi:contains")
		})
	}
	doc, diags := parseFull(t, foldedEntriesDoc("{allOf: ["+ref+", {if: {required: [a]}}, {if: {required: [a]}}]}"))
	openapitest.RequireNoErrorDiags(t, diags)
	irModel, ok := typeByName(doc, "S").(*ir.Model)
	require.True(t, ok)
	assert.Contains(t, irModel.Unmodeled, "openapi:if-then-else")
}

// TestVoices_AnUnevaluatedKeywordStaysWithItsEntry pins that no unevaluated
// keyword folds: on an entry it applies to what that entry alone evaluates,
// which beside the allOf it would not, so it stays with the entry kept whole,
// whatever its form, whether or not the entry speaks for the position, and
// the position keeps none of it.
func TestVoices_AnUnevaluatedKeywordStaysWithItsEntry(t *testing.T) {
	t.Parallel()
	const ref = "{$ref: '#/components/schemas/Base'}"
	for name, entry := range map[string]string{
		"a closure":                   "{unevaluatedProperties: false}",
		"a closure beside what folds": "{description: d, unevaluatedProperties: false}",
		"a schema":                    "{unevaluatedProperties: {type: string}}",
		"a schema beside what folds":  "{description: d, unevaluatedProperties: {type: string}}",
		"items":                       "{unevaluatedItems: false}",
		"items beside what folds":     "{description: d, unevaluatedItems: false}",
		"inert beside its catch-all":  "{description: d, additionalProperties: true, unevaluatedProperties: false}",
		"inert beside its items":      "{description: d, items: true, unevaluatedItems: false}",
		"both":                        "{description: d, unevaluatedProperties: false, unevaluatedItems: true}",
	} {
		for shape, tc := range map[string]struct{ schema, at string }{
			"a model":         {"{allOf: [" + ref + ", " + entry + "]}", "allOf/1"},
			"a model, turned": {"{allOf: [" + entry + ", " + ref + "]}", "allOf/0"},
			"a list":          {"{type: array, allOf: [" + entry + "]}", "allOf/0"},
			"a tuple":         {"{type: array, prefixItems: [{type: string}], allOf: [" + entry + "]}", "allOf/0"},
		} {
			t.Run(shape+"/"+name, func(t *testing.T) {
				t.Parallel()
				doc, diags := parseFull(t, foldedEntriesDoc(tc.schema))
				openapitest.RequireNoErrorDiags(t, diags)
				common := typeByName(doc, "S").Common()
				require.Contains(t, common.Unmodeled, "openapi:"+tc.at, "the entry is kept whole")
				assert.NotContains(t, common.Unmodeled, "openapi:unevaluated", "the position keeps none of it")
				at := make([]ir.Diagnostic, 0, 1)
				for _, code := range []string{diag.ValidationOnlyKeyword, diag.DegradedConstruct} {
					at = append(at, diagsAtPointer(diags, code, "/components/schemas/S/"+tc.at)...)
				}
				require.Len(t, at, 1, "the entry's one report: nothing folds to be reported; got %v", diags)
				assert.Contains(t, at[0].Message, "the entry (", "the report is the entry's")
				if model, ok := typeByName(doc, "S").(*ir.Model); ok {
					assert.Empty(t, model.Additional, "an entry's closure is not the model's")
				}
			})
		}
	}
}

// TestAllOfEntryFolding_AUseBindingKeywordReachesItsCarrierOrItsNode pins
// readOnly and default on an entry: a carrier holds them as beside the allOf, a
// union distributed across it or not, and with no carrier a node the position
// owns keeps them verbatim, reported at the keyword.
func TestAllOfEntryFolding_AUseBindingKeywordReachesItsCarrierOrItsNode(t *testing.T) {
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
		property := propertyOf(t, doc, "Holder", name)
		assert.NotEmpty(t, property.Visibility.Only, name)
		assert.NotNil(t, property.Default, name)
		assert.Empty(t, doc.Types[property.Type.Target].Common().Unmodeled, name)
	}
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.CompositionLowering, "/components/schemas/Holder/properties/g"))
	bare, ok := typeByName(doc, "Bare").(*ir.Model)
	require.True(t, ok)
	entry, ok := bare.Unmodeled["openapi:readOnly"]
	require.True(t, ok, "got %v", bare.Unmodeled)
	assert.Equal(t, "/components/schemas/Bare/allOf/1/readOnly", string(entry.Provenance.Pointer), "the nearest of the two")
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/components/schemas/Bare/allOf/1/readOnly"))
}

// TestVoices_APromotedExtensionReadsFromAnEntry pins a key the promotion policy
// maps: on an entry it reads as it does beside the allOf.
func TestVoices_APromotedExtensionReadsFromAnEntry(t *testing.T) {
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
	property := propertyOf(t, doc, "Holder", "f")
	require.NotNil(t, property.Deprecation)
	assert.Equal(t, "gone", property.Deprecation.Message)
}

// TestAllOfEntryFolding_AnEntrysKeywordsBesideItsRefAreAFoldedEntry pins that
// what an allOf entry writes beside its $ref is a conjunct of the position, as
// an inline entry is: it folds where it agrees with every other conjunct, and
// where it disagrees neither folds and each entry is kept whole. The $ref is
// the Base itself in both, with no node hoisted at the entry.
func TestAllOfEntryFolding_AnEntrysKeywordsBesideItsRefAreAFoldedEntry(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, beside string
		kept         []string
		description  string
	}{
		{"agreeing", "same", nil, "same"},
		{"disagreeing", "beside", []string{"openapi:allOf/0", "openapi:allOf/1"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			other := "same"
			if tc.kept != nil {
				other = "other"
			}
			doc, diags := parseFull(t, foldedEntriesDoc("{allOf: [{$ref: '#/components/schemas/Base', description: "+tc.beside+
				"}, {title: mine, description: "+other+"}]}"))
			openapitest.RequireNoErrorDiags(t, diags)
			model, ok := typeByName(doc, "S").(*ir.Model)
			require.True(t, ok)
			require.NotNil(t, model.Base)
			assert.Equal(t, componentID("Base"), model.Base.Target, "the $ref is the Base itself")
			assert.Equal(t, tc.description, model.Docs.Description)
			assert.Equal(t, tc.kept, slices.Sorted(maps.Keys(model.Unmodeled)))
			assert.NotContains(t, doc.Types, ir.TypeID("type/anonymous/components/schemas/S/allOf/0"), "no node at the entry")
		})
	}
}
