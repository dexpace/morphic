// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
// branch, in either order of them and under oneOf and anyOf.
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
							requireErrorsAndWarnings(t, diags, nil)
							var kept []string
							for _, b := range variantBounds {
								if !b.model {
									kept = append(kept, "openapi:"+b.keyword)
								}
							}
							for _, at := range conjunctionPositions {
								union := variantUnionAt(t, doc, string(at), len(branches))
								assert.Equal(t, entryKept(kept...), keptOn(union), "what the union keeps at %s", at)
								assert.Len(t, degradedUnder(diags, at), 1, "one info for what is kept at %s", at)
								for j, b := range branches {
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
