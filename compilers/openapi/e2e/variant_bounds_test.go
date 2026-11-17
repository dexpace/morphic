// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// variantBound is a bound a body writes beside a union: the keyword it is
// written as, the key the IR's constraints hold it under, and whether a model
// takes it.
type variantBound struct {
	keyword, field string
	model          bool
}

// variantBounds are the bounds the body of every case below writes, one or
// two of each kind a schema bounds: a string's, a number's, an array's and an
// object's.
var variantBounds = []variantBound{
	{keyword: "minLength", field: "minLength"},
	{keyword: "pattern", field: "pattern"},
	{keyword: "minimum", field: "min"},
	{keyword: "multipleOf", field: "multipleOf"},
	{keyword: "minItems", field: "minItems"},
	{keyword: "uniqueItems", field: "uniqueItems"},
	{keyword: "minProperties", field: "minProps", model: true},
	{keyword: "maxProperties", field: "maxProps", model: true},
}

// variantBranch is a branch of the union the cases distribute: the component
// it names, and whether it admits an object beside the body, whose type is
// object.
type variantBranch struct {
	name   string
	object bool
	only31 bool
}

// variantBranches are the branches the cases write, a string, an array and an
// integer among them, which no object satisfies, and objects, one of them
// also a string or an integer where the version lists types.
var variantBranches = []variantBranch{
	{name: "A", object: true},
	{name: "Str"},
	{name: "Arr"},
	{name: "Int"},
	{name: "AS", object: true, only31: true},
	{name: "AI", object: true, only31: true},
}

// variantComponents are the components the branches name, by the name each
// takes in variantBranches.
var variantComponents = map[string]string{
	"A":   "A: {type: object, properties: {x: {type: string}}}",
	"Str": "Str: {type: string}",
	"Arr": "Arr: {type: array, items: {type: string}}",
	"Int": "Int: {type: integer}",
	"AS":  "AS: {type: [object, string]}",
	"AI":  "AI: {type: [object, integer]}",
}

// variantComponentsOf returns the components the branches name, one line of
// YAML each.
func variantComponentsOf(branches []variantBranch) []string {
	out := make([]string, 0, len(branches))
	for _, b := range branches {
		out = append(out, variantComponents[b.name])
	}
	return out
}

// variantBranchesAt returns the branches a version can write, in the order
// written, reversed where reverse is set.
func variantBranchesAt(version string, reverse bool) []variantBranch {
	var out []variantBranch
	for _, b := range variantBranches {
		if !b.only31 || version == "3.1.0" {
			out = append(out, b)
		}
	}
	if reverse {
		slices.Reverse(out)
	}
	return out
}

// variantSchema writes an object body bounded by every variantBounds entry,
// beside the union keyword listing the branches.
func variantSchema(keyword string, branches []variantBranch) string {
	refs := make([]string, 0, len(branches))
	for _, b := range branches {
		refs = append(refs, "{$ref: '#/components/schemas/"+b.name+"'}")
	}
	return "{type: object, properties: {a: {type: string}}, minLength: 1, pattern: '^a', minimum: 2, " +
		"multipleOf: 2, minItems: 3, uniqueItems: true, minProperties: 1, maxProperties: 5, " +
		keyword + ": [" + strings.Join(refs, ", ") + "]}"
}

// variantUnionAt returns the union the schema written at at lowers to, which
// distributes its body over one variant per branch.
func variantUnionAt(t *testing.T, doc *ir.Document, at string, branches int) *ir.Union {
	t.Helper()
	for _, p := range conjunctionPositions {
		if string(p) != at {
			continue
		}
		union, ok := kindNode(t, doc, usedAt(t, doc, p)).(*ir.Union)
		require.True(t, ok, "a union at %s", at)
		require.Len(t, union.Variants, branches, "a variant for each branch at %s", at)
		return union
	}
	require.Failf(t, "no such position", "%s is no position of the cases", at)
	return nil
}

// variantConstraints returns the constraints the node with the ID given holds,
// by the key the IR's JSON holds each under, and without the one flag a node
// holds whether it bounds anything or not.
func variantConstraints(t *testing.T, doc *ir.Document, id ir.TypeID) map[string]any {
	t.Helper()
	tree, ok := documentTree(t, doc).(map[string]any)
	require.True(t, ok)
	types, ok := tree["types"].(map[string]any)
	require.True(t, ok)
	node, ok := types[string(id)].(map[string]any)
	require.True(t, ok, "the document holds %s", id)
	held, _ := node["constraints"].(map[string]any)
	out := map[string]any{}
	for key, value := range held {
		if key == "uniqueItems" && value == false {
			continue
		}
		out[key] = value
	}
	return out
}

// TestVariantBounds_ABoundNoVariantTakesIsKeptOnceOnTheUnion holds that of the
// bounds written beside a union distributed over an object body, those that
// bound a string, a number or an array are kept on the union under their own
// keys for want of a home, once and reported once, and held by no variant;
// and that the bounds of an object are held, by the variant over the object
// branch, and kept nowhere: nothing is lost. A variant is made for every
// branch an object satisfies, the others kept on the union under one warning,
// in either order of them and under oneOf and anyOf.
func TestVariantBounds_ABoundNoVariantTakesIsKeptOnceOnTheUnion(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, keyword := range []string{"oneOf", "anyOf"} {
			for _, reverse := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/reversed-%t", version, keyword, reverse), func(t *testing.T) {
					t.Parallel()
					branches := variantBranchesAt(version, reverse)
					inBothMappingOrders(t, conjunctionSpec(version, variantSchema(keyword, branches), variantComponentsOf(branches)...),
						func(t *testing.T, spec string) {
							doc, diags := compileAnnotationSpec(t, "kept", spec)
							requireErrorsAndWarnings(t, diags, deadWarnings(keyword, branches))
							kept := deadKeys(keyword, branches)
							for _, b := range variantBounds {
								if !b.model {
									kept = append(kept, "openapi:"+b.keyword)
								}
							}
							live := liveBranches(branches)
							for _, at := range conjunctionPositions {
								union := variantUnionAt(t, doc, string(at), len(live))
								assert.Equal(t, entryKept(kept...), keptOn(union), "what the union keeps at %s", at)
								assert.Equal(t, 1, countFindings(diags, diag.DegradedConstruct, ir.SeverityInfo, at),
									"one info for the bounds kept at %s", at)
								assert.Len(t, degradedUnder(diags, at), 2+keptBranches(live),
									"that, the warning, and one at each branch a variant keeps at %s", at)
								for j, b := range live {
									held := variantConstraints(t, doc, union.Variants[j].Type.Target)
									for _, bound := range variantBounds {
										if !bound.model {
											assert.NotContains(t, held, bound.field, "%s holds %s at %s", b.name, bound.keyword, at)
										}
									}
									if b.name == "A" {
										assert.EqualValues(t, 1, held["minProps"], "A holds minProperties at %s", at)
										assert.EqualValues(t, 5, held["maxProps"], "A holds maxProperties at %s", at)
									}
								}
							}
						})
				})
			}
		}
	}
}

// TestVariantBounds_AnObjectBoundBesideObjectBranchesLandsOnEveryVariant
// holds the control the cases that move a bound are read against: where every
// branch is an object, each variant holds the bounds of an object and nothing
// else, and the rest are kept on the union.
func TestVariantBounds_AnObjectBoundBesideObjectBranchesLandsOnEveryVariant(t *testing.T) {
	t.Parallel()
	objects := []variantBranch{{name: "A", object: true}, {name: "AO", object: true}}
	components := []string{variantComponents["A"], "AO: {type: object, properties: {y: {type: string}}}"}
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileAnnotationSpec(t, "objects", conjunctionSpec(version, variantSchema("oneOf", objects), components...))
			requireErrorsAndWarnings(t, diags, nil)
			for _, at := range conjunctionPositions {
				union := variantUnionAt(t, doc, string(at), len(objects))
				for j := range objects {
					assert.Equal(t, map[string]any{"minProps": 1.0, "maxProps": 5.0},
						variantConstraints(t, doc, union.Variants[j].Type.Target), "variant %d at %s", j, at)
				}
			}
		})
	}
}

// TestVariantBounds_ATypeListHoldsEachBoundOnTheVariantOfItsKind holds that
// a type list beside bounds gives each variant the bounds of its own type,
// the reading a distributed union's variants are held to.
func TestVariantBounds_ATypeListHoldsEachBoundOnTheVariantOfItsKind(t *testing.T) {
	t.Parallel()
	doc, diags := compileAnnotationSpec(t, "typelist", conjunctionSpec("3.1.0",
		"{type: [string, integer, object], properties: {a: {type: string}}, minLength: 1, minimum: 2, minProperties: 1}"))
	requireErrorsAndWarnings(t, diags, nil)
	for _, at := range conjunctionPositions {
		union := variantUnionAt(t, doc, string(at), 3)
		assert.Empty(t, keptOn(union), at)
		want := []map[string]any{{"minLength": 1.0}, {"min": "2"}, {"minProps": 1.0}}
		for j, w := range want {
			assert.Equal(t, w, variantConstraints(t, doc, union.Variants[j].Type.Target), "variant %d at %s", j, at)
		}
	}
}

// TestVariantBounds_AUseRepeatedLeavesTheVariantsAlone holds that the variants
// of a distributed union hold the same bounds whether one property uses the
// union or several positions do.
func TestVariantBounds_AUseRepeatedLeavesTheVariantsAlone(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			branches := variantBranchesAt(version, false)
			requireRepeatedUsesLeaveNodesAlone(t, version, variantSchema("oneOf", branches), variantComponentsOf(branches)...)
		})
	}
}

// TestVariantBounds_AVariantHoldsOnlyTheBoundsItsBranchTakes holds that each
// variant of a union distributed over an object body holds the bounds of the
// kinds its own branch takes beside that body: a variant over an object, or
// over a branch that is an object among others, the bounds of an object. A
// string, an array or an integer, which the body leaves no instance of, is no
// variant, in either order of the branches and under oneOf and anyOf.
func TestVariantBounds_AVariantHoldsOnlyTheBoundsItsBranchTakes(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, keyword := range []string{"oneOf", "anyOf"} {
			for _, reverse := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/reversed-%t", version, keyword, reverse), func(t *testing.T) {
					t.Parallel()
					branches := variantBranchesAt(version, reverse)
					inBothMappingOrders(t, conjunctionSpec(version, variantSchema(keyword, branches), variantComponentsOf(branches)...),
						func(t *testing.T, spec string) {
							doc, diags := compileAnnotationSpec(t, "variants", spec)
							requireErrorsAndWarnings(t, diags, deadWarnings(keyword, branches))
							live := liveBranches(branches)
							for _, at := range conjunctionPositions {
								union := variantUnionAt(t, doc, string(at), len(live))
								for j, b := range live {
									assert.Equal(t, map[string]any{"minProps": 1.0, "maxProps": 5.0},
										variantConstraints(t, doc, union.Variants[j].Type.Target),
										"the bounds of the variant over %s at %s", b.name, at)
								}
							}
						})
				})
			}
		}
	}
}

// TestVariantBounds_ABoundOfAKindNoBranchTakesIsNotLostToTheVariants holds that
// a body bounded by an object's and a string's, beside two branches one of
// which is a string, gives the object's to the variant over the object and
// keeps the string's once on the union, whichever order the branches are
// written in: the bound each takes is held and the other is neither dropped
// nor copied onto a variant. The string branch, which no object satisfies, is
// no variant, and the union keeps it beside the bound.
func TestVariantBounds_ABoundOfAKindNoBranchTakesIsNotLostToTheVariants(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, order := range [][]string{{"Str", "A"}, {"A", "Str"}} {
			t.Run(fmt.Sprintf("%s/%v", version, order), func(t *testing.T) {
				t.Parallel()
				branches := make([]variantBranch, 0, len(order))
				for _, name := range order {
					branches = append(branches, variantBranch{name: name, object: name == "A"})
				}
				schema := "{type: object, properties: {a: {type: string}}, minLength: 1, minProperties: 1, oneOf: [" +
					"{$ref: '#/components/schemas/" + order[0] + "'}, {$ref: '#/components/schemas/" + order[1] + "'}]}"
				doc, diags := compileAnnotationSpec(t, "lost", conjunctionSpec(version, schema, variantComponentsOf(branches)...))
				requireErrorsAndWarnings(t, diags, deadWarnings("oneOf", branches))
				for _, at := range conjunctionPositions {
					union := variantUnionAt(t, doc, string(at), 1)
					assert.Equal(t, entryKept(append(deadKeys("oneOf", branches), "openapi:minLength")...), keptOn(union),
						"kept at %s", at)
					assert.Equal(t, map[string]any{"minProps": 1.0}, variantConstraints(t, doc, union.Variants[0].Type.Target),
						"the variant over A at %s", at)
					assert.Equal(t, ir.KindUnion, kindNode(t, doc, usedAt(t, doc, at)).Kind(), at)
				}
			})
		}
	}
}

// variantBases are the Bases of the variants over the branches that compose:
// an object, and the object variant of each type list holding one.
var variantBases = map[string]ir.TypeID{
	"A":  componentID("A"),
	"AS": "t/anon/components/schemas/AS/type/0",
	"AI": "t/anon/components/schemas/AI/type/0",
}

// keptBranches counts the branches no variant composes (variantBases).
func keptBranches(branches []variantBranch) int {
	n := 0
	for _, b := range branches {
		if _, composed := variantBases[b.name]; !composed {
			n++
		}
	}
	return n
}

// TestVariantBounds_AVariantNamesOnlyAnObjectBranchAsItsBase holds that the
// variant over a branch that is an object alone names it as its Base, and the
// one over a type list holding an object that list's object variant, and that
// a branch no object satisfies, a string, an array or an integer, is no
// variant: the union keeps it verbatim under its path from the position, with
// no finding at the branch, in either order of the branches and of every
// mapping, under oneOf and anyOf.
func TestVariantBounds_AVariantNamesOnlyAnObjectBranchAsItsBase(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, keyword := range []string{"oneOf", "anyOf"} {
			for _, reverse := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/reversed-%t", version, keyword, reverse), func(t *testing.T) {
					t.Parallel()
					branches := variantBranchesAt(version, reverse)
					inBothMappingOrders(t, conjunctionSpec(version, variantSchema(keyword, branches), variantComponentsOf(branches)...),
						func(t *testing.T, spec string) {
							doc, diags := compileAnnotationSpec(t, "bases", spec)
							requireErrorsAndWarnings(t, diags, deadWarnings(keyword, branches))
							live := liveBranches(branches)
							for _, at := range conjunctionPositions {
								union := variantUnionAt(t, doc, string(at), len(live))
								kept := keptOn(union)
								for j, b := range branches {
									branchAt := at + jsontext.Pointer(fmt.Sprintf("/%s/%d", keyword, j))
									assert.Zero(t, countOf(degradedUnder(diags, branchAt), branchAt), "%s at %s", b.name, at)
									assert.Equal(t, !b.object, kept[fmt.Sprintf("openapi:%s/%d", keyword, j)] != "",
										"the union keeps %s at %s where it is no variant", b.name, at)
								}
								for j, b := range live {
									v, ok := doc.Types[union.Variants[j].Type.Target].(*ir.Model)
									require.True(t, ok, "the variant over %s at %s is a model", b.name, at)
									assert.Empty(t, v.Mixins, "%s at %s", b.name, at)
									require.NotNil(t, v.Base, "the variant over %s at %s", b.name, at)
									assert.Equal(t, variantBases[b.name], v.Base.Target, "the variant over %s at %s", b.name, at)
									assert.Empty(t, v.Unmodeled, "%s keeps nothing at %s", b.name, at)
								}
							}
						})
				})
			}
		}
	}
}
