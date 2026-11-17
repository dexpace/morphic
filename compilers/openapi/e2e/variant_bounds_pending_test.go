// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

//go:build pending

package e2e_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestVariantBounds_AVariantHoldsOnlyTheBoundsItsBranchTakes holds that each
// variant of a union distributed over an object body holds the bounds of the
// kinds its own branch takes beside that body: a variant over an object, or
// over a branch that is an object among others, the bounds of an object, and
// a variant over a string, an array or an integer, which the body leaves no
// instance of, none. The variant is still made for the branch, in either
// order of the branches and under oneOf and anyOf.
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
							requireErrorsAndWarnings(t, diags, nil)
							for _, at := range conjunctionPositions {
								union := variantUnionAt(t, doc, string(at), len(branches))
								for j, b := range branches {
									want := map[string]any{}
									if b.object {
										want = map[string]any{"minProps": 1.0, "maxProps": 5.0}
									}
									assert.Equal(t, want, variantConstraints(t, doc, union.Variants[j].Type.Target),
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
// nor copied onto the variant that cannot take it.
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
				requireErrorsAndWarnings(t, diags, nil)
				for _, at := range conjunctionPositions {
					union := variantUnionAt(t, doc, string(at), 2)
					assert.Equal(t, entryKept("openapi:minLength"), keptOn(union), "kept at %s", at)
					for j, b := range branches {
						want := map[string]any{}
						if b.object {
							want = map[string]any{"minProps": 1.0}
						}
						assert.Equal(t, want, variantConstraints(t, doc, union.Variants[j].Type.Target), "variant over %s at %s", b.name, at)
					}
					assert.Equal(t, ir.KindUnion, kindNode(t, doc, usedAt(t, doc, at)).Kind(), at)
				}
			})
		}
	}
}

// TestVariantBounds_AStringBoundBesideAnArrayAndAStringBranchGoesToTheString
// holds the shape the bound is reported for: a union of an array and a string
// beside object keywords and a string bound is a union of the two, the
// string's variant holding the bound, the array's none, and nothing kept for
// it, in either order of the branches.
func TestVariantBounds_AStringBoundBesideAnArrayAndAStringBranchGoesToTheString(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, order := range [][]string{{"Arr", "Str"}, {"Str", "Arr"}} {
			t.Run(fmt.Sprintf("%s/%v", version, order), func(t *testing.T) {
				t.Parallel()
				schema := "{properties: {a: {type: string}}, minLength: 1, oneOf: [" +
					"{$ref: '#/components/schemas/" + order[0] + "'}, {$ref: '#/components/schemas/" + order[1] + "'}]}"
				doc, _ := compileAnnotationSpec(t, "issue", conjunctionSpec(version, schema,
					variantComponents["Str"], variantComponents["Arr"]))
				for _, at := range conjunctionPositions {
					union := variantUnionAt(t, doc, string(at), 2)
					assert.NotContains(t, keptOn(union), "openapi:minLength", "nothing is kept for the bound at %s", at)
					for j, name := range order {
						want := map[string]any{}
						if name == "Str" {
							want = map[string]any{"minLength": 1.0}
						}
						assert.Equal(t, want, variantConstraints(t, doc, union.Variants[j].Type.Target), "variant over %s at %s", name, at)
					}
				}
			})
		}
	}
}
