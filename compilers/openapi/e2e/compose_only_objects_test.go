// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
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
					assert.Equal(t, degradedKept("openapi:$ref"), keptOn(declared))
					assert.Equal(t, entryPointer("DeclaredString", propsAt), declared.Unmodeled["openapi:$ref"].Provenance.Pointer)
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

// The $ref conjuncts the cases write between them.
const (
	anyRef   = "{$ref: '#/components/schemas/AnyT'}"
	fmtRef   = "{$ref: '#/components/schemas/Fmt'}"
	unionRef = "{$ref: '#/components/schemas/StrOrObj'}"
)

// composeVersions are the OpenAPI versions every case below is written at
// unless a keyword it needs says otherwise.
var composeVersions = []string{"3.0.3", "3.1.0"}

// keptPointersUnder is the sorted source pointers of every Unmodeled entry of
// the nodes lowered from the component named, its anonymous and composed nodes
// among them.
func keptPointersUnder(doc *ir.Document, name string) []string {
	prefixes := []string{
		string(componentID(name)),
		"t/anon/components/schemas/" + name,
		"t/composed/components/schemas/" + name,
	}
	var pointers []string
	for id, td := range doc.Types {
		for _, prefix := range prefixes {
			if string(id) != prefix && !strings.HasPrefix(string(id), prefix+"/") {
				continue
			}
			for _, entry := range td.Common().Unmodeled {
				pointers = append(pointers, string(entry.Provenance.Pointer))
			}
		}
	}
	slices.Sort(pointers)
	return pointers
}

// holderPropertyType is the type the named property of Holder names.
func holderPropertyType(t *testing.T, doc *ir.Document, property string) ir.TypeID {
	t.Helper()
	model := requireModelOf(t, doc, "Holder")
	for _, p := range model.Properties {
		if p.Name.Source == property {
			return p.Type.Target
		}
	}
	require.Failf(t, "no such property", "Holder holds no property %s", property)
	return ""
}

// requireNoModelOver holds the type a property names to be no Model: the
// property names a use of its target, directly or through the alias hoisted
// for it.
func requireNoModelOver(t *testing.T, doc *ir.Document, id ir.TypeID, what string) {
	t.Helper()
	td, ok := doc.Types[id]
	require.True(t, ok, "%s names %s, which the document holds", what, id)
	assert.NotEqual(t, ir.KindModel, td.Kind(), "%s is a use of a target that is no object alone", what)
}

// parentsOf is the targets a model names as its base or a mixin.
func parentsOf(model *ir.Model) []ir.TypeID {
	var targets []ir.TypeID
	if model.Base != nil {
		targets = append(targets, model.Base.Target)
	}
	for _, mixin := range model.Mixins {
		targets = append(targets, mixin.Target)
	}
	return targets
}

// TestAllOfCompose_ATargetAdmittingEveryKindIsUsedNotComposed holds that an
// allOf over a $ref to a schema admitting every kind, or a union of an object
// and a string, is a use of the target, not a model: whatever is written
// beside it, a description or a bound of another kind, and whether the bound
// is beside the allOf or in an entry. The bound lands on the use as it does
// beside a $ref, nothing is kept for it and no model names the target as its
// base.
func TestAllOfCompose_ATargetAdmittingEveryKindIsUsedNotComposed(t *testing.T) {
	t.Parallel()
	for _, version := range composeVersions {
		for _, swapped := range []bool{false, true} {
			entryList, _, boundAt := composeEntries(swapped, anyRef, "{minLength: 3}")
			schemas := []string{
				topSchema,
				"StrOrObj: {oneOf: [{type: string}, {type: object, properties: {a: {type: string}}}]}",
				"Described: {allOf: [" + anyRef + "], description: d}",
				"Bounded: {allOf: [" + anyRef + "], minLength: 3}",
				"Entry: {allOf: " + entryList + "}",
				"Union: {allOf: [" + unionRef + "], description: d}",
				"UnionBounded: {allOf: [" + unionRef + "], minLength: 3}",
				"Holder: {type: object, properties: {top: {allOf: [" + anyRef + "], description: d}, " +
					"union: {allOf: [" + unionRef + "], description: d}}}",
			}
			if version == "3.1.0" {
				schemas = append(schemas,
					"DescribedBeside: {$ref: '#/components/schemas/AnyT', description: d}",
					"BoundedBeside: {$ref: '#/components/schemas/AnyT', minLength: 3}")
			}
			t.Run(fmt.Sprintf("%s-swapped-%t", version, swapped), func(t *testing.T) {
				t.Parallel()
				inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
					doc, diags := compileAnnotationSpec(t, "top", admissionSpec(version, "", schemas...))
					requireErrorsAndWarnings(t, diags, nil)
					requireModelsNameModels(t, doc)

					described := requireScalarOver(t, doc, "Described", componentID("AnyT"))
					assert.Empty(t, keptOn(described))
					assert.Empty(t, described.Constraints)

					bounded := requireScalarOver(t, doc, "Bounded", componentID("AnyT"))
					require.NotNil(t, bounded.Constraints)
					require.NotNil(t, bounded.Constraints.MinLength)
					assert.EqualValues(t, 3, *bounded.Constraints.MinLength)
					assert.Empty(t, keptOn(bounded))
					assert.Zero(t, degradedInfosAt(diags, "Bounded"))

					entry := requireScalarOver(t, doc, "Entry", componentID("AnyT"))
					held := entry.Constraints != nil && entry.Constraints.MinLength != nil && *entry.Constraints.MinLength == 3
					_, kept := keptOn(entry)[entryKey(boundAt)]
					assert.True(t, held || kept, "the entry's bound is placed on the use or kept, not lost")

					requireScalarOver(t, doc, "Union", componentID("StrOrObj"))
					assert.Zero(t, degradedInfosAt(diags, "Union"))
					union := requireScalarOver(t, doc, "UnionBounded", componentID("StrOrObj"))
					require.NotNil(t, union.Constraints)
					require.NotNil(t, union.Constraints.MinLength)
					assert.EqualValues(t, 3, *union.Constraints.MinLength)
					assert.Empty(t, keptOn(union))

					requireNoModelOver(t, doc, holderPropertyType(t, doc, "top"), "Holder.top")
					requireNoModelOver(t, doc, holderPropertyType(t, doc, "union"), "Holder.union")
					if version == "3.1.0" {
						beside := requireScalarOver(t, doc, "DescribedBeside", componentID("AnyT"))
						assert.Equal(t, described.Constraints, beside.Constraints, "both spellings of a description")
						bounded := requireScalarOver(t, doc, "BoundedBeside", componentID("AnyT"))
						require.NotNil(t, bounded.Constraints)
						require.NotNil(t, bounded.Constraints.MinLength)
						assert.EqualValues(t, 3, *bounded.Constraints.MinLength)
					}
				})
			})
		}
	}
}

// TestAllOfCompose_AModelKeywordOnAKindThatIsNoObjectIsKept holds that a
// keyword only a model holds, beside an allOf or in an entry, does not make a
// model of a position whose declared kind is a string or an array: the
// position is a use of the conjunct that declares the kind, the keyword is
// kept under its own key beside the allOf or under its entry's index, with one
// info and no warning.
func TestAllOfCompose_AModelKeywordOnAKindThatIsNoObjectIsKept(t *testing.T) {
	t.Parallel()
	for _, version := range composeVersions {
		for _, swapped := range []bool{false, true} {
			hList, _, hAt := composeEntries(swapped, "{$ref: '#/components/schemas/Str'}", "{properties: {a: {type: string}}}")
			iList, _, iAt := composeEntries(swapped, "{type: string}", "{properties: {a: {type: string}}}")
			jList, _, jAt := composeEntries(swapped, "{type: string}", "{required: [a]}")
			tList, tAt, _ := composeEntries(swapped, "{properties: {a: {type: string}}}", "{items: {type: string}}")
			schemas := []string{
				"Str: {type: string}",
				"Arr: {type: array, items: {type: string}}",
				"StrProps: {allOf: [{$ref: '#/components/schemas/Str'}], properties: {a: {type: string}}}",
				"ArrProps: {allOf: [{$ref: '#/components/schemas/Arr'}], properties: {a: {type: string}}}",
				"RefEntry: {allOf: " + hList + "}",
				"StrEntry: {allOf: " + iList + "}",
				"StrRequired: {allOf: " + jList + "}",
			}
			if version == "3.1.0" {
				// Before 3.1 an array declares its items beside its type.
				schemas = append(schemas, "ArrayWrap: {type: array, allOf: "+tList+"}")
			}
			t.Run(fmt.Sprintf("%s-swapped-%t", version, swapped), func(t *testing.T) {
				t.Parallel()
				inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
					doc, diags := compileAnnotationSpec(t, "kinds", admissionSpec(version, "", schemas...))
					requireErrorsAndWarnings(t, diags, nil)
					requireModelsNameModels(t, doc)

					for name, want := range map[string]struct {
						target ir.TypeID
						key    string
					}{
						"StrProps":    {componentID("Str"), "openapi:properties"},
						"ArrProps":    {componentID("Arr"), "openapi:properties"},
						"RefEntry":    {componentID("Str"), entryKey(hAt)},
						"StrEntry":    {prim(ir.PrimString), entryKey(iAt)},
						"StrRequired": {prim(ir.PrimString), entryKey(jAt)},
					} {
						node := requireScalarOver(t, doc, name, want.target)
						assert.Equal(t, degradedKept(want.key), keptOn(node), name)
						assert.Equal(t, 1, degradedInfosAt(diags, name), name)
					}

					if version != "3.1.0" {
						return
					}
					wrap, ok := kindNode(t, doc, componentID("ArrayWrap")).(*ir.List)
					require.True(t, ok, "a declared array is a list, not a model")
					assert.Contains(t, keptOn(wrap), entryKey(tAt), "the properties entry is kept")
					assert.NotContains(t, keptOn(wrap), "openapi:type", "the declared type is the list's own")
				})
			})
		}
	}
}

// TestAllOfCompose_EntriesClaimingTwoKindsAreNotAModel holds that entries
// whose keywords speak of an object and of an array do not compose a model:
// with no type declared, the position is the top type keeping both entries;
// beside a type list naming both, it is the union of an object and an array,
// the entries kept. Nothing is reported as unsatisfiable.
func TestAllOfCompose_EntriesClaimingTwoKindsAreNotAModel(t *testing.T) {
	t.Parallel()
	for _, version := range composeVersions {
		for _, swapped := range []bool{false, true} {
			list, propsAt, itemsAt := composeEntries(swapped, "{properties: {a: {type: string}}}", "{items: {type: string}}")
			schemas := []string{"TwoKinds: {allOf: " + list + "}"}
			if version == "3.1.0" {
				schemas = append(schemas, "Listed: {type: [object, array], allOf: "+list+"}")
			}
			t.Run(fmt.Sprintf("%s-swapped-%t", version, swapped), func(t *testing.T) {
				t.Parallel()
				inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
					doc, diags := compileAnnotationSpec(t, "claims", admissionSpec(version, "", schemas...))
					requireErrorsAndWarnings(t, diags, nil)
					requireModelsNameModels(t, doc)

					top := requireScalarOver(t, doc, "TwoKinds", prim(ir.PrimAny))
					assert.Equal(t, degradedKept(entryKey(propsAt), entryKey(itemsAt)), keptOn(top))
					assert.Equal(t, 2, degradedInfosAt(diags, "TwoKinds"), "one info for each entry kept")
					assert.False(t, markedKindInference(top), "no kind was read")

					if version != "3.1.0" {
						return
					}
					union, ok := kindNode(t, doc, componentID("Listed")).(*ir.Union)
					require.True(t, ok, "the type list's union")
					require.Len(t, union.Variants, 2)
					assert.Equal(t, ir.KindModel, kindNode(t, doc, union.Variants[0].Type.Target).Kind())
					assert.Equal(t, ir.KindList, kindNode(t, doc, union.Variants[1].Type.Target).Kind())
					assert.Equal(t, []string{"/components/schemas/Listed/allOf/0", "/components/schemas/Listed/allOf/1"},
						keptPointersUnder(doc, "Listed"), "each entry is kept")
				})
			})
		}
	}
}

// TestAllOfCompose_ABaseThatIsNoModelIsKeptNotComposed holds that a $ref
// conjunct is the base or a mixin of a composition only where its target is
// an object alone: where the target admits every kind, the conjunct is kept
// under its index with one info, and the composition declares what the other
// entries write, whichever order the entries are in.
func TestAllOfCompose_ABaseThatIsNoModelIsKeptNotComposed(t *testing.T) {
	t.Parallel()
	for _, version := range composeVersions {
		for _, swapped := range []bool{false, true} {
			declList, declTopAt, _ := composeEntries(swapped, anyRef, "{type: object, properties: {a: {type: string}}}")
			inferList, inferTopAt, _ := composeEntries(swapped, anyRef, "{properties: {a: {type: string}}}")
			reqList, reqAt, _ := composeEntries(swapped, "{$ref: '#/components/schemas/R'}", "{properties: {b: {type: string}}}")
			mixList, mixTopAt := "["+"{$ref: '#/components/schemas/Obj'}, "+anyRef+", {properties: {b: {type: string}}}]", 1
			if swapped {
				mixList, mixTopAt = "["+anyRef+", {$ref: '#/components/schemas/Obj'}, {properties: {b: {type: string}}}]", 0
			}
			schemas := []string{
				topSchema,
				"Obj: {type: object, properties: {a: {type: string}}}",
				"R: {required: [a]}",
				"Declared: {allOf: " + declList + "}",
				"Inferred: {allOf: " + inferList + "}",
				"OverRequired: {allOf: " + reqList + "}",
				"Mixed: {allOf: " + mixList + "}",
			}
			t.Run(fmt.Sprintf("%s-swapped-%t", version, swapped), func(t *testing.T) {
				t.Parallel()
				inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
					doc, diags := compileAnnotationSpec(t, "base", admissionSpec(version, "", schemas...))
					requireErrorsAndWarnings(t, diags, nil)
					requireModelsNameModels(t, doc)

					for name, at := range map[string]int{"Declared": declTopAt, "Inferred": inferTopAt, "OverRequired": reqAt} {
						model := requireModelOf(t, doc, name)
						assert.Empty(t, parentsOf(model), "%s names no base: its target is no model", name)
						require.Len(t, model.Properties, 1, name)
						assert.Equal(t, degradedKept(entryKey(at)), keptOn(model), name)
						assert.Equal(t, 1, degradedInfosAt(diags, name), name)
					}
					assert.False(t, markedKindInference(requireModelOf(t, doc, "Declared")), "its type is declared")
					assert.True(t, markedKindInference(requireModelOf(t, doc, "Inferred")), "its kind is read off a keyword")
					assert.True(t, markedKindInference(requireModelOf(t, doc, "OverRequired")), "its kind is read off a keyword")

					mixed := requireModelOf(t, doc, "Mixed")
					assert.Contains(t, parentsOf(mixed), componentID("Obj"), "the object is the base or a mixin")
					assert.NotContains(t, parentsOf(mixed), componentID("AnyT"))
					assert.Equal(t, degradedKept(entryKey(mixTopAt)), keptOn(mixed))
					assert.Equal(t, 1, degradedInfosAt(diags, "Mixed"))
				})
			})
		}
	}
}

// TestAllOfCompose_SeveralRefsThatAreNoObjectUseTheNarrower holds that an
// allOf of $refs to schemas that can be no object is a use of the narrower
// and keeps the other under its index with one info, in both orders of the
// entries, where it used to be a model whose mixins are not models.
func TestAllOfCompose_SeveralRefsThatAreNoObjectUseTheNarrower(t *testing.T) {
	t.Parallel()
	for _, version := range composeVersions {
		for _, swapped := range []bool{false, true} {
			fmtList, strAt, _ := composeEntries(swapped, "{$ref: '#/components/schemas/Str'}", fmtRef)
			listList, _, listEAt := composeEntries(swapped, "{$ref: '#/components/schemas/Str'}", "{$ref: '#/components/schemas/E'}")
			schemas := []string{
				"Str: {type: string}",
				"Fmt: {type: string, format: uuid}",
				"E: {items: {type: string}}",
				"StrFmt: {allOf: " + fmtList + "}",
				"StrList: {allOf: " + listList + "}",
			}
			t.Run(fmt.Sprintf("%s-swapped-%t", version, swapped), func(t *testing.T) {
				t.Parallel()
				inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
					doc, diags := compileAnnotationSpec(t, "refs", admissionSpec(version, "", schemas...))
					requireErrorsAndWarnings(t, diags, nil)
					requireModelsNameModels(t, doc)

					narrower := requireScalarOver(t, doc, "StrFmt", componentID("Fmt"))
					assert.Equal(t, degradedKept("openapi:$ref"), keptOn(narrower))
					assert.Equal(t, entryPointer("StrFmt", strAt), narrower.Unmodeled["openapi:$ref"].Provenance.Pointer)
					assert.Equal(t, 1, degradedInfosAt(diags, "StrFmt"))

					declared := requireScalarOver(t, doc, "StrList", componentID("Str"))
					assert.Equal(t, degradedKept("openapi:$ref"), keptOn(declared))
					assert.Equal(t, entryPointer("StrList", listEAt), declared.Unmodeled["openapi:$ref"].Provenance.Pointer)
					assert.Equal(t, 1, degradedInfosAt(diags, "StrList"))
				})
			})
		}
	}
}

// TestAllOfCompose_ConjunctsNoInstanceSatisfiesNameNoPlainBase holds that an
// allOf whose conjuncts share no value stays the composition with its one
// warning, and that its $ref conjuncts that are no model are not the base or a
// mixin of it.
func TestAllOfCompose_ConjunctsNoInstanceSatisfiesNameNoPlainBase(t *testing.T) {
	t.Parallel()
	for _, version := range composeVersions {
		schemas := []string{
			"Str: {type: string}",
			"Int: {type: integer}",
			"Obj: {type: object, properties: {a: {type: string}}}",
			"Arr: {type: array, items: {type: string}}",
			"StrInt: {allOf: [{$ref: '#/components/schemas/Str'}, {$ref: '#/components/schemas/Int'}]}",
			"ObjStr: {allOf: [{$ref: '#/components/schemas/Obj'}, {$ref: '#/components/schemas/Str'}], properties: {b: {type: string}}}",
			"ArrObj: {allOf: [{$ref: '#/components/schemas/Arr'}, {type: object, properties: {a: {type: string}}}]}",
		}
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "empty", admissionSpec(version, "", schemas...))
				requireErrorsAndWarnings(t, diags, []wantFinding{
					disjointFinding("StrInt"), disjointFinding("ObjStr"), disjointFinding("ArrObj"),
				})
				requireModelsNameModels(t, doc)
				for _, name := range []string{"StrInt", "ObjStr", "ArrObj"} {
					requireModelOf(t, doc, name)
				}
			})
		})
	}
}

// unionSummary is the bounds a union holds and each of its variants' kind,
// base and bounds, without the IDs the two spellings of one union differ by.
func unionSummary(t *testing.T, doc *ir.Document, name string) string {
	t.Helper()
	union, ok := kindNode(t, doc, componentID(name)).(*ir.Union)
	require.True(t, ok, "%s is a union holding the bound written beside it", name)
	raw, err := json.Marshal(union.Constraints, json.Deterministic(true))
	require.NoError(t, err)
	parts := make([]string, 0, 1+len(union.Variants))
	parts = append(parts, fmt.Sprintf("exclusive=%t constraints=%s", union.Exclusive, raw))
	for _, v := range union.Variants {
		variant, isScalar := kindNode(t, doc, v.Type.Target).(*ir.Scalar)
		require.True(t, isScalar, "%s variant", name)
		raw, err := json.Marshal(variant.Constraints, json.Deterministic(true))
		require.NoError(t, err)
		parts = append(parts, fmt.Sprintf("%s %s", variant.Base.Target, raw))
	}
	return strings.Join(parts, "\n")
}

// TestAllOfUnion_ABoundBesideAnAllOfNamingNoKindIsHeldByTheUnion holds that a
// union beside an allOf in which no conjunct names a kind is a union holding
// the allOf's bounds, as the union written without the allOf is, where it was
// a model keeping the union: nothing is reported, in both orders of the
// components and of the entries.
func TestAllOfUnion_ABoundBesideAnAllOfNamingNoKindIsHeldByTheUnion(t *testing.T) {
	t.Parallel()
	for _, version := range composeVersions {
		for _, swapped := range []bool{false, true} {
			twoList, _, _ := composeEntries(swapped, "{multipleOf: 2}", "{maximum: 9}")
			schemas := []string{
				"Alone: {allOf: [{multipleOf: 2}]}",
				"Beside: {allOf: [{multipleOf: 2}], anyOf: [{multipleOf: 3}, {multipleOf: 5}]}",
				"NoAllOf: {multipleOf: 2, anyOf: [{multipleOf: 3}, {multipleOf: 5}]}",
				"Bounded: {allOf: [{minLength: 1}], oneOf: [{maxLength: 3}, {pattern: '^a'}]}",
				"Two: {allOf: " + twoList + ", anyOf: [{multipleOf: 3}, {multipleOf: 5}]}",
			}
			t.Run(fmt.Sprintf("%s-swapped-%t", version, swapped), func(t *testing.T) {
				t.Parallel()
				inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
					doc, diags := compileAnnotationSpec(t, "bound", admissionSpec(version, "", schemas...))
					assert.Empty(t, diags, "the union holds the bound, so nothing is kept or reported")
					requireModelsNameModels(t, doc)

					assert.Equal(t, unionSummary(t, doc, "NoAllOf"), unionSummary(t, doc, "Beside"))
					beside := kindNode(t, doc, componentID("Beside"))
					assert.Empty(t, keptOn(beside))
					assert.Equal(t, "2", string(*beside.(*ir.Union).Constraints.MultipleOf))

					bounded, ok := kindNode(t, doc, componentID("Bounded")).(*ir.Union)
					require.True(t, ok)
					assert.True(t, bounded.Exclusive)
					require.NotNil(t, bounded.Constraints)
					require.NotNil(t, bounded.Constraints.MinLength)
					assert.EqualValues(t, 1, *bounded.Constraints.MinLength)
					assert.Empty(t, keptOn(bounded))

					two, ok := kindNode(t, doc, componentID("Two")).(*ir.Union)
					require.True(t, ok)
					require.NotNil(t, two.Constraints)
					require.NotNil(t, two.Constraints.MultipleOf)
					require.NotNil(t, two.Constraints.Max)
					assert.Equal(t, "2", string(*two.Constraints.MultipleOf))
					assert.Equal(t, "9", string(*two.Constraints.Max))

					requireScalarOver(t, doc, "Alone", prim(ir.PrimAny))
				})
			})
		}
	}
}

// TestAllOfUnion_AUnionSharingNoKindWithTheRestIsKeptNotDistributed holds that
// a union none of whose branches can be a value of what the allOf beside it
// admits is not distributed into models no instance satisfies: the position is
// what the allOf lowers to without it, the union is kept under its keyword
// with its reason and one info, and the schema draws the warning of conjuncts
// sharing no value. No union and no composed variant is in the document.
func TestAllOfUnion_AUnionSharingNoKindWithTheRestIsKeptNotDistributed(t *testing.T) {
	t.Parallel()
	for _, version := range composeVersions {
		schemas := []string{
			"Str: {type: string}",
			"Int: {type: integer}",
			"Obj: {type: object, properties: {a: {type: string}}}",
			"WrapDisj: {allOf: [{$ref: '#/components/schemas/Obj'}], oneOf: [{type: string}, {type: boolean}]}",
			"StrExcl: {allOf: [{$ref: '#/components/schemas/Str'}], oneOf: [{type: integer}, {type: boolean}]}",
			"IntExcl: {allOf: [{$ref: '#/components/schemas/Int'}], anyOf: [{type: string}, {type: boolean}]}",
		}
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "disjoint", admissionSpec(version, "", schemas...))
				requireErrorsAndWarnings(t, diags, []wantFinding{
					disjointFinding("WrapDisj"), disjointFinding("StrExcl"), disjointFinding("IntExcl"),
				})
				requireModelsNameModels(t, doc)
				for id, td := range doc.Types {
					assert.NotEqual(t, ir.KindUnion, td.Kind(), "%s: no union is distributed over the rest", id)
				}

				wrap := requireModelOf(t, doc, "WrapDisj")
				require.NotNil(t, wrap.Base)
				assert.Equal(t, componentID("Obj"), wrap.Base.Target)
				assert.Equal(t, degradedKept("openapi:oneOf"), keptOn(wrap))
				str := requireScalarOver(t, doc, "StrExcl", componentID("Str"))
				assert.Equal(t, degradedKept("openapi:oneOf"), keptOn(str))
				integer := requireScalarOver(t, doc, "IntExcl", componentID("Int"))
				assert.Equal(t, degradedKept("openapi:anyOf"), keptOn(integer))
				for _, name := range []string{"WrapDisj", "StrExcl", "IntExcl"} {
					assert.Equal(t, 1, degradedInfosAt(diags, name), name)
				}
			})
		})
	}
}

// TestAllOfCompose_AFormBodyOfAStringBesideMembersIsAString holds that a
// multipart body written as an allOf of a string and a conjunct declaring a
// part is the string, not a model with a part: the body names a string, the
// media type holds no encoding for a part no body has, and the body is
// reported as naming no part to send, as a string body is.
func TestAllOfCompose_AFormBodyOfAStringBesideMembersIsAString(t *testing.T) {
	t.Parallel()
	paths := `paths:
  /b:
    post:
      operationId: b
      requestBody:
        content:
          multipart/form-data:
            schema: {allOf: [{type: string}, {properties: {f: {type: string, format: binary}}}]}
      responses: {'204': {description: d}}
`
	for _, version := range composeVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileAnnotationSpec(t, "form", admissionSpec(version, paths, "Unused: {type: string}"))
			requireErrorsAndWarnings(t, diags, []wantFinding{{
				code: diag.PartlessFormBody, pointer: "/paths/~1b/post/requestBody/content/multipart~1form-data/schema",
			}})
			requireModelsNameModels(t, doc)

			op := openapitest.FirstOp(t, doc.Services[0])
			require.NotNil(t, op.Request)
			require.Len(t, op.Request.Contents, 1)
			content := op.Request.Contents[0]
			assert.Equal(t, "multipart/form-data", content.MediaType)
			assert.Empty(t, content.Encoding, "a string has no part to encode")

			body, ok := kindNode(t, doc, content.Type.Target).(*ir.Scalar)
			require.True(t, ok, "the body is a string, not a model")
			require.NotNil(t, body.Base)
			assert.Equal(t, prim(ir.PrimString), body.Base.Target)
		})
	}
}

// TestAllOfCompose_ATargetNamingAKindWithAnUnexpandedDynamicRefIsUsed holds
// that a $dynamicRef a schema does not expand masks no kind: an allOf over a
// target declaring an array or a string beside it is a use of the target
// whether the target writes the keyword or not, the keyword kept on the target
// with its one info, and the default a property holds is no finding against a
// model it is not.
func TestAllOfCompose_ATargetNamingAKindWithAnUnexpandedDynamicRefIsUsed(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"Target: {type: array, prefixItems: [{type: string}], $dynamicRef: '#a', $defs: {d: {$dynamicAnchor: a}}}",
		"Plain: {type: array, prefixItems: [{type: string}]}",
		"StrDyn: {type: string, $dynamicRef: '#a', $defs: {d: {$dynamicAnchor: a}}}",
		"StrPlain: {type: string}",
		"Described: {allOf: [{$ref: '#/components/schemas/StrDyn'}], description: d}",
		"DescribedPlain: {allOf: [{$ref: '#/components/schemas/StrPlain'}], description: d}",
		"Holder: {type: object, properties: {" +
			"refined: {allOf: [{$ref: '#/components/schemas/Target'}], default: [foo]}, " +
			"refinedPlain: {allOf: [{$ref: '#/components/schemas/Plain'}], default: [foo]}, " +
			"beside: {$ref: '#/components/schemas/Target', default: [foo]}}}",
	}
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		doc, diags := parseVerified(t, admissionSpec("3.1.0", "", schemas...))
		requireErrorsAndWarnings(t, diags, nil)
		requireModelsNameModels(t, doc)

		assert.Equal(t, componentID("Target"), holderPropertyType(t, doc, "refined"))
		assert.Equal(t, componentID("Plain"), holderPropertyType(t, doc, "refinedPlain"))
		assert.Equal(t, componentID("Target"), holderPropertyType(t, doc, "beside"))

		requireScalarOver(t, doc, "Described", componentID("StrDyn"))
		requireScalarOver(t, doc, "DescribedPlain", componentID("StrPlain"))
		for _, name := range []string{"Target", "StrDyn"} {
			assert.Equal(t, degradedKept("openapi:$dynamicRef"),
				keptOn(kindNode(t, doc, componentID(name))), name)
			assert.Equal(t, 1, degradedInfosUnder(diags, "/components/schemas/"+name+"/$dynamicRef"), name)
		}
	})
}

// pickSummary is what each component named lowers to, read past the order its
// entries and union branches are written in: its kind, what it is a use of or
// composes, the bounds it holds and what it keeps, each kept entry by its value
// rather than its index, with the findings as a sorted multiset whose pointers
// name no index.
func pickSummary(t *testing.T, doc *ir.Document, diags []ir.Diagnostic, names []string) []string {
	t.Helper()
	index := regexp.MustCompile(`/(allOf|oneOf|anyOf)/[0-9]+`)
	out := make([]string, 0, len(names)+len(diags))
	for _, name := range names {
		td := kindNode(t, doc, componentID(name))
		var parents []string
		switch n := td.(type) {
		case *ir.Scalar:
			parents = append(parents, string(n.Base.Target))
		case *ir.Model:
			for _, p := range parentsOf(n) {
				parents = append(parents, string(p))
			}
		}
		var kept []string
		for _, entry := range td.Common().Unmodeled {
			kept = append(kept, branchSet(t, entry.Value))
		}
		slices.Sort(parents)
		slices.Sort(kept)
		raw, err := json.Marshal(heldConstraints(td), json.Deterministic(true))
		require.NoError(t, err)
		out = append(out, fmt.Sprintf("%s %s %v %s %v", name, td.Kind(), parents, raw, kept))
	}
	for _, d := range diags {
		out = append(out, fmt.Sprintf("%s %s %s", d.Severity, d.Code, index.ReplaceAllString(string(d.Provenance.Pointer), "/$1/*")))
	}
	slices.Sort(out)
	return out
}

// branchSet is a kept value read past the order a union lists its branches in:
// a list's items sorted, anything else as written.
func branchSet(t *testing.T, raw ir.RawValue) string {
	t.Helper()
	var items []jsontext.Value
	if json.Unmarshal(raw, &items) != nil {
		return string(raw)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, string(item))
	}
	slices.Sort(out)
	return strings.Join(out, ",")
}

// heldConstraints is the Constraints a Scalar, a Model or a Union holds.
func heldConstraints(td ir.TypeDef) *ir.Constraints {
	switch n := td.(type) {
	case *ir.Scalar:
		return n.Constraints
	case *ir.Model:
		return n.Constraints
	case *ir.Union:
		return n.Constraints
	default:
		return nil
	}
}

// TestAllOfCompose_APickAmongSeveralReadsNoOrder holds each row that picks one
// conjunct of several, or sets a union aside, to one result whichever order the
// components, the allOf's entries and the union's branches are written in: the
// narrower of two $refs, the least reference text of two alike, a declared
// kind over a target admitting every kind, a union sharing no kind with the
// rest and one beside an allOf naming none.
func TestAllOfCompose_APickAmongSeveralReadsNoOrder(t *testing.T) {
	t.Parallel()
	rows := map[string][2]string{
		"StrFmt":  {"{allOf: [{$ref: '#/components/schemas/Str'}, " + fmtRef + "]}", ""},
		"RStr":    {"{allOf: [{$ref: '#/components/schemas/R'}, {type: string}]}", ""},
		"TopStr":  {"{allOf: [" + anyRef + ", {$ref: '#/components/schemas/Str'}]}", ""},
		"Tie":     {"{allOf: [{$ref: '#/components/schemas/Str'}, {$ref: '#/components/schemas/Str2'}]}", ""},
		"StrExcl": {"{allOf: [{$ref: '#/components/schemas/Str'}], oneOf: [{type: integer}, {type: boolean}]}", ""},
		"ObjExcl": {"{allOf: [{$ref: '#/components/schemas/Obj'}], anyOf: [{type: string}, {type: boolean}]}", ""},
		"Beside":  {"{allOf: [{multipleOf: 2}, {maximum: 9}], anyOf: [{multipleOf: 3}, {multipleOf: 5}]}", ""},
	}
	reversed := regexp.MustCompile(`(allOf|oneOf|anyOf): \[(\{[^\[\]]*\}), (\{[^\[\]]*\})\]`)
	names := slices.Sorted(maps.Keys(rows))
	targets := []string{topSchema, "Str: {type: string}", "Str2: {type: string}", "Fmt: {type: string, format: uuid}", "R: {required: [a]}",
		"Obj: {type: object, properties: {a: {type: string}}}"}
	for _, version := range composeVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			written, swapped := make([]string, 0, len(names)), make([]string, 0, len(names))
			for _, name := range names {
				written = append(written, name+": "+rows[name][0])
				swapped = append(swapped, name+": "+reversed.ReplaceAllString(rows[name][0], "$1: [$3, $2]"))
			}
			require.NotEqual(t, written, swapped, "every row writes a pair to reverse")
			var want []string
			for _, schemas := range [][]string{written, swapped} {
				inBothOrders(t, append(slices.Clone(targets), schemas...), func(t *testing.T, schemas []string) {
					doc, diags := compileAnnotationSpec(t, "order", admissionSpec(version, "", schemas...))
					got := pickSummary(t, doc, diags, names)
					if want == nil {
						want = got
						return
					}
					assert.Empty(t, cmp.Diff(want, got), "no order written moves a pick")
				})
			}
		})
	}
}

// nodesUnder is the nodes of doc whose ID names a position at or under one of
// the pointers given.
func nodesUnder(doc *ir.Document, pointers ...string) map[ir.TypeID]ir.TypeDef {
	out := map[ir.TypeID]ir.TypeDef{}
	for id, td := range doc.Types {
		for _, p := range pointers {
			if strings.HasSuffix(string(id), p) || strings.Contains(string(id), p+"/") {
				out[id] = td
			}
		}
	}
	return out
}

// TestAllOfCompose_APositionUsedTwiceLowersAsOnce holds that what a position
// lowers to does not turn on how many uses reach it: each row, as a component
// schema and as the schema of a component response, lowers to the same nodes
// whether one use names it or two properties, a parameter and two operations do.
func TestAllOfCompose_APositionUsedTwiceLowersAsOnce(t *testing.T) {
	t.Parallel()
	rows := []string{
		"{allOf: [" + anyRef + "], description: d}",
		"{allOf: [" + anyRef + "], minLength: 3}",
		"{allOf: [{$ref: '#/components/schemas/Str'}], properties: {a: {type: string}}}",
		"{allOf: [{$ref: '#/components/schemas/Str'}, " + fmtRef + "]}",
		"{allOf: [" + anyRef + ", {type: object, properties: {a: {type: string}}}]}",
		"{allOf: [{$ref: '#/components/schemas/Str'}], oneOf: [{type: integer}, {type: boolean}]}",
		"{allOf: [{multipleOf: 2}], anyOf: [{multipleOf: 3}, {multipleOf: 5}]}",
		"{allOf: [{properties: {a: {type: string}}}, {items: {type: string}}]}",
	}
	targets := []string{topSchema, "Str: {type: string}", "Fmt: {type: string, format: uuid}"}
	op := func(name, param string) string {
		return "  /" + name + ":\n    get:\n      operationId: " + name + "\n" + param +
			"      responses: {'200': {$ref: '#/components/responses/R'}}\n"
	}
	spec := func(version, row string, twice bool) string {
		paths, holder := "paths:\n"+op("a", ""), "Holder: {type: object, properties: {p: {$ref: '#/components/schemas/X'}}}"
		if twice {
			paths += op("b", "      parameters: [{name: q, in: query, schema: {$ref: '#/components/schemas/X'}}]\n")
			holder = "Holder: {type: object, properties: {p: {$ref: '#/components/schemas/X'}, q: {$ref: '#/components/schemas/X'}}}"
		}
		return admissionSpec(version, paths, append(slices.Clone(targets), "X: "+row, holder)...) +
			"  responses:\n    R:\n      description: d\n      content: {application/json: {schema: " + row + "}}\n"
	}
	for _, version := range composeVersions {
		for _, row := range rows {
			t.Run(version+"/"+row, func(t *testing.T) {
				t.Parallel()
				once, onceDiags := compileAnnotationSpec(t, "once", spec(version, row, false))
				twice, twiceDiags := compileAnnotationSpec(t, "twice", spec(version, row, true))
				at := []string{"components/schemas/X", "components/responses/R"}
				require.NotEmpty(t, nodesUnder(once, at...))
				assert.Empty(t, cmp.Diff(nodesUnder(once, at...), nodesUnder(twice, at...)), "the nodes")
				assert.ElementsMatch(t, findingsUnder(onceDiags, "/components/"), findingsUnder(twiceDiags, "/components/"))
			})
		}
	}
}

// findingsUnder is each diagnostic at or under the pointer prefix given, by
// severity, code and pointer.
func findingsUnder(diags []ir.Diagnostic, prefix string) []string {
	var out []string
	for _, d := range diags {
		if at := string(d.Provenance.Pointer); strings.HasPrefix(at, prefix) {
			out = append(out, fmt.Sprintf("%s %s %s", d.Severity, d.Code, at))
		}
	}
	return out
}

// TestAllOfRefine_ATargetInferredNarrowerThanTheDeclaredKindsIsNoPrincipal
// holds that a $ref whose target the kind-inference policy reads as an object
// alone is no principal beside a type list or a union declaring a string too:
// the position is the union its own keywords declare, as the inline spelling
// is, and the $ref is kept with one info.
func TestAllOfRefine_ATargetInferredNarrowerThanTheDeclaredKindsIsNoPrincipal(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"Unt: {properties: {b: {type: integer}}}",
		"TypeListRef: {type: [object, string], allOf: [{$ref: '#/components/schemas/Unt'}]}",
		"UnionRef: {oneOf: [{type: object, properties: {u: {type: string}}}, {type: string}], " +
			"allOf: [{$ref: '#/components/schemas/Unt'}]}",
	}
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		doc, diags := compileAnnotationSpec(t, "narrower", admissionSpec("3.1.0", "", schemas...))
		requireErrorsAndWarnings(t, diags, nil)
		requireModelsNameModels(t, doc)
		for _, name := range []string{"TypeListRef", "UnionRef"} {
			union, ok := kindNode(t, doc, componentID(name)).(*ir.Union)
			require.True(t, ok, "%s is the union its own keywords declare", name)
			assert.True(t, slices.ContainsFunc(union.Variants, func(v ir.Variant) bool {
				return v.Type.Target == ir.TypeID("t/prim/string")
			}), "%s admits the string it declares", name)
			assert.Equal(t, degradedKept("openapi:$ref"), keptOn(union), name)
			assert.Equal(t, entryPointer(name, 0), union.Unmodeled["openapi:$ref"].Provenance.Pointer, name)
			assert.Equal(t, 1, degradedInfosUnder(diags, "/components/schemas/"+name+"/allOf/0"), name)
		}
	})
}

// TestAllOfUnion_ARefAdmittingEveryKindBesideAUnionIsKeptAsTheInlineSpellingIs
// holds that a union beside an allOf whose one conjunct is a $ref to a schema
// saying nothing of its kind is the Union holding the bounds, as the same
// schema written inline or with no allOf is, the $ref kept with one info: a
// conjunct naming a kind outvotes such a $ref.
func TestAllOfUnion_ARefAdmittingEveryKindBesideAUnionIsKeptAsTheInlineSpellingIs(t *testing.T) {
	t.Parallel()
	union := "oneOf: [{properties: {a: {type: string}}, required: [a]}, {type: string}], minLength: 1, minProperties: 2"
	schemas := []string{
		"Open: {description: declares no type}",
		"ERef: {allOf: [{$ref: '#/components/schemas/Open'}], " + union + "}",
		"EInline: {allOf: [{description: declares no type}], " + union + "}",
		"EFlat: {" + union + "}",
	}
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		doc, diags := compileAnnotationSpec(t, "every", admissionSpec("3.1.0", "", schemas...))
		requireErrorsAndWarnings(t, diags, nil)
		want := &ir.Constraints{MinLength: new(int64(1)), MinProps: new(int64(2))}
		for _, name := range []string{"ERef", "EInline", "EFlat"} {
			u, ok := kindNode(t, doc, componentID(name)).(*ir.Union)
			require.True(t, ok, "%s is the union its flat spelling is", name)
			assert.Equal(t, want, u.Constraints, name)
		}
		u, ok := kindNode(t, doc, componentID("ERef")).(*ir.Union)
		require.True(t, ok)
		assert.Equal(t, degradedKept("openapi:$ref"), keptOn(u))
		assert.Equal(t, entryPointer("ERef", 0), u.Unmodeled["openapi:$ref"].Provenance.Pointer)
		assert.Equal(t, 1, degradedInfosUnder(diags, "/components/schemas/ERef/allOf/0"))
	})
}

// TestAllOfRefine_ARefLosingATieIsReportedAsATie holds that the info a $ref
// kept beside an equal one draws says the tie went by reference text, not that
// the other lies within more conjuncts, which it does not.
func TestAllOfRefine_ARefLosingATieIsReportedAsATie(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"S1: {type: string}",
		"S2: {type: string}",
		"Tie: {allOf: [{$ref: '#/components/schemas/S2'}, {$ref: '#/components/schemas/S1'}]}",
	}
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		doc, diags := compileAnnotationSpec(t, "tie", admissionSpec("3.1.0", "", schemas...))
		requireErrorsAndWarnings(t, diags, nil)
		requireScalarOver(t, doc, "Tie", componentID("S1"))
		msg := diagMessageOf(t, diags, "/components/schemas/Tie/allOf/0")
		assert.NotContains(t, msg, "lies within more")
		assert.Contains(t, msg, "least reference text")
	})
}
