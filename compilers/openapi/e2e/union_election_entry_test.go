// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// bothKeywordsSite is a position a schema writing a oneOf and an anyOf stands
// at: the pointer it is written at, and the document holding it beside the
// component T, the components declared in the order given.
type bothKeywordsSite struct {
	name string
	at   jsontext.Pointer
	doc  func(version, schema string, reversed bool) string
}

// bothKeywordsSites are a component, a property, an array's items and a
// parameter's schema.
var bothKeywordsSites = []bothKeywordsSite{
	{"component", "/components/schemas/P", func(version, schema string, reversed bool) string {
		return bothKeywordsDoc(version, "", reversed, "P: "+schema)
	}},
	{"property", "/components/schemas/C/properties/p", func(version, schema string, reversed bool) string {
		return bothKeywordsDoc(version, "", reversed, "C: {type: object, properties: {p: "+schema+"}}")
	}},
	{"items", "/components/schemas/C/items", func(version, schema string, reversed bool) string {
		return bothKeywordsDoc(version, "", reversed, "C: {type: array, items: "+schema+"}")
	}},
	{"parameter", "/paths/~1x/get/parameters/0/schema", func(version, schema string, reversed bool) string {
		paths := "paths:\n  /x:\n    get:\n      parameters:\n        - {name: q, in: query, schema: " + schema +
			"}\n      responses: {'200': {description: ok}}\n"
		return bothKeywordsDoc(version, paths, reversed)
	}},
}

// bothKeywordsDoc is a document at version writing paths, the component T and
// the others given, in reverse where reversed.
func bothKeywordsDoc(version, paths string, reversed bool, others ...string) string {
	schemas := append([]string{"T: {type: object, properties: {a: {type: string}}}"}, others...)
	if reversed {
		slices.Reverse(schemas)
	}
	return admissionSpec(version, paths, schemas...)
}

// bothKeywordsOneOf is the oneOf every case writes, each branch an object
// declaring a member of its own.
const bothKeywordsOneOf = "[{type: object, properties: {b: {type: string}}}, {type: object, properties: {c: {type: string}}}]"

// bothKeywordsSpellings are the ways to write the oneOf and the anyOf any
// beside the body, an inline schema or a $ref: beside an allOf of the body, on
// two entries and on one, each in either order, and beside the body's own
// keywords where it is inline. The first is the one each other is compared to.
func bothKeywordsSpellings(body, anyOf string) map[string]string {
	return unionPairSpellings(body, bothKeywordsOneOf, anyOf)
}

// unionPairSpellings is bothKeywordsSpellings for the oneOf one.
func unionPairSpellings(body, one, anyOf string) map[string]string {
	spellings := map[string]string{
		"one entry":                    "{allOf: [" + body + ", {oneOf: " + one + ", anyOf: " + anyOf + "}]}",
		"one entry, the anyOf first":   "{allOf: [" + body + ", {anyOf: " + anyOf + ", oneOf: " + one + "}]}",
		"one entry, before the body":   "{allOf: [{oneOf: " + one + ", anyOf: " + anyOf + "}, " + body + "]}",
		"beside the allOf":             "{allOf: [" + body + "], oneOf: " + one + ", anyOf: " + anyOf + "}",
		"two entries":                  "{allOf: [" + body + ", {oneOf: " + one + "}, {anyOf: " + anyOf + "}]}",
		"two entries, the anyOf first": "{allOf: [" + body + ", {anyOf: " + anyOf + "}, {oneOf: " + one + "}]}",
		"two entries, before the body": "{allOf: [{anyOf: " + anyOf + "}, {oneOf: " + one + "}, " + body + "]}",
	}
	if inline, ok := strings.CutPrefix(body, "{type: object, "); ok {
		spellings["beside the body"] = "{type: object, oneOf: " + one + ", anyOf: " + anyOf + ", " + inline
	}
	return spellings
}

// TestElection_BothKeywordsOnOneEntryDistributeTheOneOf holds that a oneOf
// and an anyOf written on one inline allOf entry lower as the same two do
// beside the allOf, on two entries and beside the body's own keywords: at every
// version, at a component, a property, items and a parameter, over an inline
// body and a $ref, in either order of the entries, of the keywords and of the
// components. The position is a Union of the oneOf keeping neither union,
// each variant composing the body and its branch and keeping the anyOf, as
// validation logic where its branches declare no shape against the position.
func TestElection_BothKeywordsOnOneEntryDistributeTheOneOf(t *testing.T) {
	t.Parallel()
	bodies := []struct{ name, schema string }{
		{"inline", "{type: object, properties: {a: {type: string}}}"},
		{"ref", "{$ref: '#/components/schemas/T'}"},
	}
	anyOfs := []struct {
		name, schema string
		reason       ir.UnmodeledReason
	}{
		{"declaring shape", "[{type: object, properties: {d: {type: string}}}, {type: object, properties: {e: {type: string}}}]",
			ir.ReasonDegradedLowering},
		{"declaring none", "[{required: [a]}, {maxProperties: 3}]", ir.ReasonValidationOnly},
		{"restating the body", "[{type: object}, {properties: {a: {type: string}}}]", ir.ReasonValidationOnly},
	}
	for _, version := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		for _, site := range bothKeywordsSites {
			for _, body := range bodies {
				for _, anyOf := range anyOfs {
					t.Run(fmt.Sprintf("%s/%s/%s/%s", version, site.name, body.name, anyOf.name), func(t *testing.T) {
						t.Parallel()
						doc, diags := compileSpellingsAlike(t, site, version, bothKeywordsSpellings(body.schema, anyOf.schema))
						requireErrorsAndWarnings(t, diags, nil)
						requireDistributedOneOf(t, doc, site.at, body.name == "ref", anyOf.reason)
					})
				}
			}
		}
	}
}

// compileSpellingsAlike compiles each spelling at site, its components in both
// orders, and holds every document equal to the one entry's with the components
// as declared, read with no allOf entry's index, and returns that document.
func compileSpellingsAlike(
	t *testing.T, site bothKeywordsSite, version string, spellings map[string]string,
) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	noEntry := func(text string) string { return entryNodes.ReplaceAllString(text, "") }
	doc, diags := compileAnnotationSpec(t, "both", site.doc(version, spellings["one entry"], false))
	want := electionView(t, doc, noEntry)
	for _, name := range slices.Sorted(maps.Keys(spellings)) {
		for _, reversed := range []bool{false, true} {
			got, _ := compileAnnotationSpec(t, "both", site.doc(version, spellings[name], reversed))
			if diff := cmp.Diff(want, electionView(t, got, noEntry)); diff != "" {
				t.Errorf("%s, reversed %t, differs from one entry (-one entry +%s):\n%s", name, reversed, name, diff)
			}
		}
	}
	return doc, diags
}

// requireDistributedOneOf holds the node at the position at to a Union of the
// oneOf written on its allOf's entry 1, keeping neither union, whose variants
// each compose the body, T where ref says so, and the branch, and keep the
// anyOf for reason.
func requireDistributedOneOf(t *testing.T, doc *ir.Document, at jsontext.Pointer, ref bool, reason ir.UnmodeledReason) {
	t.Helper()
	union, ok := kindNode(t, doc, entryPositionID(at)).(*ir.Union)
	require.True(t, ok, "the oneOf distributes")
	assert.True(t, union.Exclusive)
	assert.Empty(t, keptOn(union), "the Union keeps neither union")
	require.Len(t, union.Variants, 2)
	for i, v := range union.Variants {
		variant, isModel := kindNode(t, doc, v.Type.Target).(*ir.Model)
		require.True(t, isModel, "variant %d is a model", i)
		assert.Equal(t, map[string]ir.UnmodeledReason{"openapi:anyOf": reason}, keptOn(variant), "variant %d keeps the anyOf", i)
		composed := slices.Clone(variant.Mixins)
		if variant.Base != nil {
			composed = append(composed, *variant.Base)
		}
		targets := make([]ir.TypeID, 0, len(composed))
		for _, r := range composed {
			targets = append(targets, r.Target)
		}
		assert.Contains(t, targets, ir.TypeID(fmt.Sprintf("t/anon%s/allOf/1/oneOf/%d", at, i)), "variant %d composes its branch", i)
		if ref {
			assert.Contains(t, targets, componentID("T"), "variant %d composes the $ref", i)
			continue
		}
		assert.Equal(t, []string{"a"}, propertyNames(variant), "variant %d carries the body's members", i)
	}
}

// TestElection_BothKeywordsOnOneEntryKeepWhatTheyKeepBesideTheAllOf holds that
// an entry writing a oneOf and an anyOf over a $ref lowers as the same keywords
// beside the allOf where the oneOf is not distributed or is routed: every
// branch admitting no object keeps both unions on a model of the $ref, with a
// warning; a $ref branch naming nothing keeps both, with its error; a
// discriminator written with them routes the distributed variants. The
// findings named are the one entry's.
func TestElection_BothKeywordsOnOneEntryKeepWhatTheyKeepBesideTheAllOf(t *testing.T) {
	t.Parallel()
	objects := "[{type: object, properties: {d: {type: string}}}, {type: object, properties: {e: {type: string}}}]"
	cases := []struct {
		name, extra, oneOf, anyOf string
		want                      []wantFinding
		union                     bool
	}{
		{"no branch admits an object", "", "[{type: string}, {type: array}]", objects,
			[]wantFinding{{code: diag.DegradedConstruct, pointer: "/components/schemas/P"}}, false},
		{"a branch names nothing", "", "[{$ref: '#/components/schemas/Missing'}, {type: object}]", objects,
			[]wantFinding{{code: diag.UnresolvedRef, pointer: "/components/schemas/P/allOf/1/oneOf/0"}}, false},
		{"a discriminator routes them", "discriminator: {propertyName: a}, ", bothKeywordsOneOf,
			"[{required: [a]}, {maxProperties: 3}]", nil, true},
	}
	site := bothKeywordsSites[0]
	for _, version := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		for _, tc := range cases {
			t.Run(version+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				oneFirst := tc.extra + "oneOf: " + tc.oneOf + ", anyOf: " + tc.anyOf
				anyFirst := tc.extra + "anyOf: " + tc.anyOf + ", oneOf: " + tc.oneOf
				doc, diags := compileSpellingsAlike(t, site, version, map[string]string{
					"one entry":                  "{allOf: [" + entryRef + ", {" + oneFirst + "}]}",
					"one entry, the anyOf first": "{allOf: [" + entryRef + ", {" + anyFirst + "}]}",
					"one entry, before the body": "{allOf: [{" + oneFirst + "}, " + entryRef + "]}",
					"beside the allOf":           "{allOf: [" + entryRef + "], " + oneFirst + "}",
				})
				requireErrorsAndWarnings(t, diags, slices.Clone(tc.want))
				node := kindNode(t, doc, componentID("P"))
				if tc.union {
					union, ok := node.(*ir.Union)
					require.True(t, ok, "the oneOf distributes")
					assert.Len(t, union.Variants, 2)
					return
				}
				model, ok := node.(*ir.Model)
				require.True(t, ok, "the position is a model of the $ref")
				require.NotNil(t, model.Base)
				assert.Equal(t, componentID("T"), model.Base.Target)
				assert.Equal(t, degradedKept("openapi:anyOf", "openapi:oneOf"), keptOn(model), "both unions are kept whole")
			})
		}
	}
}

// TestElection_AnEntryWritingBothBesideAnotherOneOfDistributesNeither holds
// that an entry writing a oneOf and an anyOf, beside another writing a oneOf
// and a body, distributes neither oneOf, in both orders of the entries and of
// the components: two unions of one keyword elect none, whatever else an entry
// writes. Both entries are kept whole, each reported.
func TestElection_AnEntryWritingBothBesideAnotherOneOfDistributesNeither(t *testing.T) {
	t.Parallel()
	both := "{" + oneObjBranches + ", " + anyObjBranches + "}"
	other := "{oneOf: [{type: object, properties: {f: {type: string}}}, {type: object, properties: {g: {type: string}}}]}"
	for _, version := range electionVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileInEveryOrder(t, version, func(swapped bool) []string {
				return pairSchemas(swapped, "BothOne", both, other, objBody)
			})
			requireErrorsAndWarnings(t, diags, nil)

			assert.Empty(t, unionsUnder(doc, "BothOne"), "neither oneOf stands for the position")
			requireKeptEntries(t, kindNode(t, doc, componentID("BothOne")), "both entries", 0, 1)
			requireOneInfoAtEach(t, diags, "BothOne", "both entries", 0, 1)
		})
	}
}

// TestElection_BothKeywordsOnOneEntryOfARoutedSubtypeKeepTheAnyOfAsBeside holds
// that a subtype a discriminator routes to, writing a oneOf and an anyOf on one
// entry of its allOf, composes the oneOf as a mixin keeping the anyOf as the
// same keywords beside the allOf do: as validation logic where its branches
// restate what the position declares, though they say more than the entry.
func TestElection_BothKeywordsOnOneEntryOfARoutedSubtypeKeepTheAnyOfAsBeside(t *testing.T) {
	t.Parallel()
	base := "Base: {type: object, properties: {kind: {type: string}}, required: [kind], " +
		"discriminator: {propertyName: kind, mapping: {p: '#/components/schemas/P'}}}"
	site := bothKeywordsSite{"component", "/components/schemas/P", func(version, schema string, reversed bool) string {
		return bothKeywordsDoc(version, "", reversed, base, "P: "+schema)
	}}
	ref := "{$ref: '#/components/schemas/Base'}"
	keywords := "oneOf: " + bothKeywordsOneOf + ", anyOf: [{type: object}, {properties: {kind: {type: string}}}]"
	for _, version := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileSpellingsAlike(t, site, version, map[string]string{
				"one entry":        "{allOf: [" + ref + ", {" + keywords + "}]}",
				"beside the allOf": "{allOf: [" + ref + "], " + keywords + "}",
			})
			requireErrorsAndWarnings(t, diags, nil)
			model, ok := kindNode(t, doc, componentID("P")).(*ir.Model)
			require.True(t, ok, "the subtype stays a model")
			require.Len(t, model.Mixins, 1)
			union, isUnion := kindNode(t, doc, model.Mixins[0].Target).(*ir.Union)
			require.True(t, isUnion, "the model composes the oneOf")
			assert.Equal(t, map[string]ir.UnmodeledReason{"openapi:anyOf": ir.ReasonValidationOnly}, keptOn(union))
		})
	}
}

// TestElection_TwoUnionsOverABodyLowerAlikeWhereverWritten holds that a oneOf
// and an anyOf over a body lower alike beside the allOf, on one entry or on two,
// at every version and position, where the anyOf alone declares shape, where
// it admits objects alone beside a oneOf branch of another kind, and where it
// shares no kind with the body: the union declaring shape is elected, and the
// other is a conjunct of the rest wherever it is written.
func TestElection_TwoUnionsOverABodyLowerAlikeWhereverWritten(t *testing.T) {
	t.Parallel()
	objects := "[{type: object, properties: {d: {type: string}}}, {type: object, properties: {e: {type: string}}}]"
	shapes := []struct {
		name, oneOf, anyOf string
		bodies             []string
		check              func(t *testing.T, doc *ir.Document, diags []ir.Diagnostic, at jsontext.Pointer)
	}{
		{"the anyOf alone declares shape", "[{required: [a]}, {maxProperties: 3}]", objects,
			[]string{"{type: object, properties: {a: {type: string}}}", entryRef},
			func(t *testing.T, doc *ir.Document, diags []ir.Diagnostic, at jsontext.Pointer) {
				t.Helper()
				requireErrorsAndWarnings(t, diags, nil)
				union := requireUnionOf(t, doc, at, 2)
				assert.False(t, union.Exclusive, "the anyOf is elected")
				for _, v := range union.Variants {
					assert.Equal(t, map[string]ir.UnmodeledReason{"openapi:oneOf": ir.ReasonValidationOnly},
						keptOn(kindNode(t, doc, v.Type.Target)), "each variant keeps the oneOf")
				}
			}},
		{"the anyOf refuses a oneOf branch", "[{type: object, properties: {b: {type: string}}}, {type: array}]",
			"[{type: object}]", []string{"{properties: {a: {type: string}}}"},
			func(t *testing.T, doc *ir.Document, diags []ir.Diagnostic, at jsontext.Pointer) {
				t.Helper()
				requireErrorsAndWarnings(t, diags, []wantFinding{{code: diag.DegradedConstruct, pointer: string(at)}})
				union := requireUnionOf(t, doc, at, 1)
				assert.Equal(t, degradedKept("openapi:oneOf/1"), keptOn(union), "the dead branch is kept on the Union")
			}},
		{"the anyOf shares no kind with the body", bothKeywordsOneOf, "[{type: string}, {type: integer}]",
			[]string{"{type: object, properties: {a: {type: string}}}", entryRef},
			func(t *testing.T, doc *ir.Document, diags []ir.Diagnostic, at jsontext.Pointer) {
				t.Helper()
				requireErrorsAndWarnings(t, diags, []wantFinding{{code: diag.DegradedConstruct, pointer: string(at),
					contains: []string{"share no value", "string or integer"}}})
				union := requireUnionOf(t, doc, at, 2)
				for _, v := range union.Variants {
					assert.Equal(t, degradedKept("openapi:anyOf"), keptOn(kindNode(t, doc, v.Type.Target)))
				}
			}},
	}
	for _, version := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		for _, site := range bothKeywordsSites {
			for _, shape := range shapes {
				for i, body := range shape.bodies {
					t.Run(fmt.Sprintf("%s/%s/%s/%d", version, site.name, shape.name, i), func(t *testing.T) {
						t.Parallel()
						doc, diags := compileSpellingsAlike(t, site, version, unionPairSpellings(body, shape.oneOf, shape.anyOf))
						shape.check(t, doc, diags, site.at)
					})
				}
			}
		}
	}
}

// requireUnionOf holds the node at the position at to a Union of n variants.
func requireUnionOf(t *testing.T, doc *ir.Document, at jsontext.Pointer, n int) *ir.Union {
	t.Helper()
	union, ok := kindNode(t, doc, entryPositionID(at)).(*ir.Union)
	require.True(t, ok, "the position is a Union")
	require.Len(t, union.Variants, n)
	return union
}

// TestElection_AnEntryHoldingBothIsJudgedWithoutThemAtAnyDepth holds that an
// entry writing a oneOf and an anyOf lowers as one directly on the allOf where
// it is nested in an allOf entry, and from 3.1 where it writes them beside a
// $ref to the body: each union is judged against the rest of the position
// read without the entry's own two, wherever the entry is listed. Read with
// them, the rest admits nothing, so a oneOf of strings and integers restated it
// and the anyOf was elected, or an anyOf of them was kept as validation logic.
func TestElection_AnEntryHoldingBothIsJudgedWithoutThemAtAnyDepth(t *testing.T) {
	t.Parallel()
	objects := "[{type: object, properties: {d: {type: string}}}, {type: object, properties: {e: {type: string}}}]"
	apart := "[{type: string}, {type: integer}]"
	pairs := []struct{ name, oneOf, anyOf string }{
		{"a oneOf sharing no kind with the body", apart, objects},
		{"an anyOf sharing no kind with the body", bothKeywordsOneOf, apart},
	}
	for _, version := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		for _, site := range bothKeywordsSites {
			for _, pair := range pairs {
				t.Run(fmt.Sprintf("%s/%s/%s", version, site.name, pair.name), func(t *testing.T) {
					t.Parallel()
					both := "oneOf: " + pair.oneOf + ", anyOf: " + pair.anyOf
					spellings := map[string]string{
						"one entry":                "{allOf: [" + entryRef + ", {" + both + "}]}",
						"one entry, in an allOf":   "{allOf: [" + entryRef + ", {allOf: [{" + both + "}]}]}",
						"one entry, two allOfs in": "{allOf: [" + entryRef + ", {allOf: [{allOf: [{" + both + "}]}]}]}",
					}
					if version != "3.0.3" {
						spellings["one entry, beside the $ref"] = "{allOf: [{$ref: '#/components/schemas/T', " + both + "}]}"
					}
					compileSpellingsAlike(t, site, version, spellings)
				})
			}
		}
	}
}
