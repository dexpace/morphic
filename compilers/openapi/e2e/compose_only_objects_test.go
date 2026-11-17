// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestAllOfCompose_ObjectsAloneStayAModel holds that an allOf whose conjuncts
// can only be objects, null aside, is a model that names models: a $ref to an
// object is its base or a mixin, whether the target admits null as well or
// not, in both orders of the components and of the entries.
func TestAllOfCompose_ObjectsAloneStayAModel(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, swapped := range []bool{false, true} {
			list, _, _ := composeEntries(swapped, "{$ref: '#/components/schemas/Obj'}", "{$ref: '#/components/schemas/Obj2'}")
			schemas := []string{
				"Obj: {type: object, properties: {a: {type: string}}}",
				"Obj2: {type: object, properties: {c: {type: string}}}",
				"WithProps: {allOf: [{$ref: '#/components/schemas/Obj'}], properties: {b: {type: string}}}",
				"TwoObjs: {allOf: " + list + ", properties: {d: {type: string}}}",
				"ObjN: {type: object, nullable: true, properties: {a: {type: string}}}",
				"OverNullable: {allOf: [{$ref: '#/components/schemas/ObjN'}], properties: {b: {type: string}}}",
			}
			if version == "3.1.0" {
				schemas = append(schemas,
					"ObjOrNull: {type: [object, 'null'], properties: {a: {type: string}}}",
					"OverObjOrNull: {allOf: [{$ref: '#/components/schemas/ObjOrNull'}], properties: {b: {type: string}}}")
			}
			t.Run(version, func(t *testing.T) {
				t.Parallel()
				inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
					doc, diags := compileAnnotationSpec(t, "objects", admissionSpec(version, "", schemas...))
					requireErrorsAndWarnings(t, diags, nil)
					requireModelsNameModels(t, doc)

					with := requireModelOf(t, doc, "WithProps")
					require.NotNil(t, with.Base)
					assert.Equal(t, componentID("Obj"), with.Base.Target)

					two := requireModelOf(t, doc, "TwoObjs")
					parents := append([]ir.TypeRef{}, two.Mixins...)
					if two.Base != nil {
						parents = append(parents, *two.Base)
					}
					var targets []ir.TypeID
					for _, p := range parents {
						targets = append(targets, p.Target)
					}
					assert.ElementsMatch(t, []ir.TypeID{componentID("Obj"), componentID("Obj2")}, targets)

					over := requireModelOf(t, doc, "OverNullable")
					require.NotNil(t, over.Base)
					assert.Equal(t, componentID("ObjN"), over.Base.Target)
					assert.Empty(t, keptOn(over), "a nullable object is an object, null aside")
					if version == "3.1.0" {
						over = requireModelOf(t, doc, "OverObjOrNull")
						require.NotNil(t, over.Base)
						assert.Equal(t, componentID("ObjOrNull"), over.Base.Target)
						assert.Empty(t, keptOn(over))
					}
				})
			})
		}
	}
}

// TestAllOfCompose_ASelfReferenceStaysACompositionWithItsCycleFinding holds
// that an allOf whose only conjunct is the schema itself, which no read of its
// kinds ends, is the composition and is reported as one by the pass that
// finds composition cycles.
func TestAllOfCompose_ASelfReferenceStaysACompositionWithItsCycleFinding(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			schemas := []string{"C1: {allOf: [{$ref: '#/components/schemas/C1'}]}"}
			doc, diags := parseVerified(t, admissionSpec(version, "", schemas...))
			requireErrorsAndWarnings(t, diags, []wantFinding{{
				code: "pass/composition-cycle", pointer: "/components/schemas/C1",
			}})
			model := requireModelOf(t, doc, "C1")
			require.NotNil(t, model.Base)
			assert.Equal(t, componentID("C1"), model.Base.Target)
		})
	}
}

// TestAllOfRefine_AKeywordOfAnotherKindIsKeptOnWhatTheTargetDeclares holds
// three refinements that already say what the step asks of the others: a
// model's keyword beside a $ref to a string is kept under its own key, one in
// an entry beside a string $ref under the entry's index, and an inferred
// object claim never beats a declared string. Each is a use of the string with
// one info.
func TestAllOfRefine_AKeywordOfAnotherKindIsKeptOnWhatTheTargetDeclares(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, swapped := range []bool{false, true} {
			itemsList, _, itemsAt := composeEntries(swapped, "{$ref: '#/components/schemas/Str'}", "{items: {type: string}}")
			propsList, propsAt, _ := composeEntries(swapped, "{$ref: '#/components/schemas/Props'}", "{type: string}")
			schemas := []string{
				"Str: {type: string}",
				"Props: {properties: {a: {type: string}}}",
				"StrItems: {allOf: " + itemsList + "}",
				"DeclaredString: {allOf: " + propsList + "}",
			}
			if version == "3.1.0" {
				// A keyword beside a $ref applies from 3.1; before, it draws a warning.
				schemas = append(schemas, "StrPropsBeside: {$ref: '#/components/schemas/Str', properties: {a: {type: string}}}")
			}
			t.Run(fmt.Sprintf("%s-swapped-%t", version, swapped), func(t *testing.T) {
				t.Parallel()
				inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
					doc, diags := compileAnnotationSpec(t, "refine", admissionSpec(version, "", schemas...))
					requireErrorsAndWarnings(t, diags, nil)
					requireModelsNameModels(t, doc)

					if version == "3.1.0" {
						beside := requireScalarOver(t, doc, "StrPropsBeside", componentID("Str"))
						assert.Equal(t, degradedKept("openapi:properties"), keptOn(beside))
						assert.Equal(t, 1, degradedInfosAt(diags, "StrPropsBeside"))
					}

					items := requireScalarOver(t, doc, "StrItems", componentID("Str"))
					assert.Equal(t, degradedKept(entryKey(itemsAt)), keptOn(items))
					assert.Equal(t, 1, degradedInfosAt(diags, "StrItems"))

					declared := requireScalarOver(t, doc, "DeclaredString", prim(ir.PrimString))
					assert.Equal(t, degradedKept(entryKey(propsAt)), keptOn(declared))
					assert.Equal(t, 1, degradedInfosAt(diags, "DeclaredString"))
				})
			})
		}
	}
}

// TestAllOfRefine_ABoundOverATopTargetLandsOnTheUse holds the control the
// top-target cases are read against: the bound a use of a schema admitting
// every kind holds is a Scalar over it carrying the bound beside a $ref, and a
// use of a string takes the same bound from an allOf, so a bound is placed by
// the kind its home takes and not by the spelling.
func TestAllOfRefine_ABoundOverATopTargetLandsOnTheUse(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			schemas := []string{
				topSchema,
				"Str: {type: string}",
				"StrWrap: {allOf: [{$ref: '#/components/schemas/Str'}], maxLength: 3}",
				"StrEntry: {allOf: [{$ref: '#/components/schemas/Str'}, {maxLength: 3}]}",
			}
			if version == "3.1.0" {
				schemas = append(schemas, "BoundedBeside: {$ref: '#/components/schemas/AnyT', minLength: 3}")
			}
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "bound", admissionSpec(version, "", schemas...))
				requireErrorsAndWarnings(t, diags, nil)
				requireModelsNameModels(t, doc)

				if version == "3.1.0" {
					top := requireScalarOver(t, doc, "BoundedBeside", componentID("AnyT"))
					require.NotNil(t, top.Constraints)
					require.NotNil(t, top.Constraints.MinLength)
					assert.EqualValues(t, 3, *top.Constraints.MinLength)
					assert.Empty(t, keptOn(top))
				}
				for _, name := range []string{"StrWrap", "StrEntry"} {
					str := requireScalarOver(t, doc, name, componentID("Str"))
					require.NotNil(t, str.Constraints, name)
					require.NotNil(t, str.Constraints.MaxLength, name)
					assert.EqualValues(t, 3, *str.Constraints.MaxLength, name)
					assert.Empty(t, keptOn(str), name)
				}
			})
		})
	}
}

// TestAllOfUnion_AUnionSharingAKindWithTheRestStaysDistributed holds that a
// union beside an allOf is distributed across the models it conjoins with
// only where its branches can be objects, written beside the allOf or in an
// entry, and that an entry holding a union sharing no kind with a model is the
// composition keeping the branch whole with the disjoint warning, and that a
// union naming a kind keeps the bound written beside it.
func TestAllOfUnion_AUnionSharingAKindWithTheRestStaysDistributed(t *testing.T) {
	t.Parallel()
	branches := "[{type: object, properties: {b: {type: string}}}, {type: object, properties: {c: {type: string}}}]"
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, swapped := range []bool{false, true} {
			objList, _, _ := composeEntries(swapped, "{$ref: '#/components/schemas/Obj'}", "{oneOf: "+branches+"}")
			disjList, _, disjAt := composeEntries(swapped, "{$ref: '#/components/schemas/Obj'}", "{oneOf: [{type: string}, {type: boolean}]}")
			schemas := []string{
				"Obj: {type: object, properties: {a: {type: string}}}",
				"BWrapObj: {allOf: [{$ref: '#/components/schemas/Obj'}], oneOf: " + branches + "}",
				"BEntryObj: {allOf: " + objList + "}",
				"BEntryDisj: {allOf: " + disjList + "}",
				"BesideTyped: {allOf: [{multipleOf: 2}], anyOf: [{type: integer}, {type: string}]}",
			}
			t.Run(fmt.Sprintf("%s-swapped-%t", version, swapped), func(t *testing.T) {
				t.Parallel()
				inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
					doc, diags := compileAnnotationSpec(t, "unions", admissionSpec(version, "", schemas...))
					requireErrorsAndWarnings(t, diags, []wantFinding{disjointFinding("BEntryDisj")})
					requireModelsNameModels(t, doc)

					for _, name := range []string{"BWrapObj", "BEntryObj"} {
						union, ok := kindNode(t, doc, componentID(name)).(*ir.Union)
						require.True(t, ok, "%s is distributed", name)
						require.Len(t, union.Variants, 2, name)
						for _, v := range union.Variants {
							assert.Equal(t, ir.KindModel, kindNode(t, doc, v.Type.Target).Kind(), name)
						}
					}

					disj := requireModelOf(t, doc, "BEntryDisj")
					require.NotNil(t, disj.Base)
					assert.Equal(t, componentID("Obj"), disj.Base.Target)
					assert.Equal(t, degradedKept(entryKey(disjAt)), keptOn(disj))

					typed, ok := kindNode(t, doc, componentID("BesideTyped")).(*ir.Union)
					require.True(t, ok, "a union naming its kinds holds the bound")
					require.NotNil(t, typed.Constraints)
					require.NotNil(t, typed.Constraints.MultipleOf)
					assert.Equal(t, "2", string(*typed.Constraints.MultipleOf))
					assert.Empty(t, keptOn(typed))
				})
			})
		}
	}
}
