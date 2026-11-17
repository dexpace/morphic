// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

//go:build pending

package e2e_test

import (
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// The $ref conjuncts the pending cases write between them.
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
					assert.Equal(t, degradedKept(entryKey(strAt)), keptOn(narrower))
					assert.Equal(t, 1, degradedInfosAt(diags, "StrFmt"))

					declared := requireScalarOver(t, doc, "StrList", componentID("Str"))
					assert.Equal(t, degradedKept(entryKey(listEAt)), keptOn(declared))
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

// TestAllOfUnion_AUnionBesideARefSharingNoKindIsKeptLikeTheAllOfSpelling holds
// that the $ref spelling of a union sharing no kind with its target is the
// model over the target keeping the union, as the allOf spelling is. The $ref
// spelling is read by the rule that reads a $ref and its siblings as one
// conjunction, so this case may stay red until that rule's commit lands.
func TestAllOfUnion_AUnionBesideARefSharingNoKindIsKeptLikeTheAllOfSpelling(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"Obj: {type: object, properties: {a: {type: string}}}",
		"WrapDisj: {allOf: [{$ref: '#/components/schemas/Obj'}], oneOf: [{type: string}, {type: boolean}]}",
		"BesideDisj: {$ref: '#/components/schemas/Obj', oneOf: [{type: string}, {type: boolean}]}",
	}
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		doc, diags := compileAnnotationSpec(t, "beside", admissionSpec("3.1.0", "", schemas...))
		requireErrorsAndWarnings(t, diags, []wantFinding{disjointFinding("WrapDisj"), disjointFinding("BesideDisj")})
		for _, name := range []string{"WrapDisj", "BesideDisj"} {
			model := requireModelOf(t, doc, name)
			require.NotNil(t, model.Base, name)
			assert.Equal(t, componentID("Obj"), model.Base.Target, name)
			assert.Equal(t, degradedKept("openapi:oneOf"), keptOn(model), name)
		}
	})
}

// TestAllOfCompose_AFormBodyOfAStringBesideMembersIsAString holds that a
// multipart body written as an allOf of a string and a conjunct declaring a
// part is the string, not a model with a part: the body names a string and
// the media type holds no encoding for a part no body has.
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
			requireErrorsAndWarnings(t, diags, nil)
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
