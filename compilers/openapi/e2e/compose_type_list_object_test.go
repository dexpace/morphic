// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// petVariant is the object variant of Pet's type list, which the compiler
// mints at the list's position.
const petVariant ir.TypeID = "type/anonymous/components/schemas/Pet/type/0"

// flattenedNames is the wire names of the members model exposes across its
// composition.
func flattenedNames(doc *ir.Document, model ir.TypeID) []string {
	flat := ir.FlattenedProperties(doc.Types, model).Properties
	names := make([]string, 0, len(flat))
	for _, flatProperty := range flat {
		names = append(names, flatProperty.Property.WireName)
	}
	slices.Sort(names)
	return names
}

// TestAllOfCompose_ASubtypeOfATypeListComposesItsObjectVariant holds that a
// subtype composing a schema whose type list holds an object composes the
// object variant the list's union holds: its Base is that variant, a model, so
// it exposes the members the schema declares beside its own and keeps the tag
// the discriminator routes by, whichever component is lowered first, the
// subtype named before the schema or after it.
func TestAllOfCompose_ASubtypeOfATypeListComposesItsObjectVariant(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"First: {type: object, properties: {cat: {$ref: '#/components/schemas/Cat'}, pet: {$ref: '#/components/schemas/Pet'}}}",
		"Pet: {type: [object, string], properties: {kind: {type: string}}, " +
			"discriminator: {propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}}",
		"Cat: {allOf: [{$ref: '#/components/schemas/Pet'}], type: object, properties: {meow: {type: boolean}}}",
		"Dog: {allOf: [{$ref: '#/components/schemas/Pet'}, {type: object, properties: {bark: {type: boolean}}}]}",
		"Last: {type: object, properties: {dog: {$ref: '#/components/schemas/Dog'}}}",
	}
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		doc, diags := compileAnnotationSpec(t, "variant", admissionSpec("3.1.0", "", schemas...))
		requireErrorsAndWarnings(t, diags, nil)
		requireModelsNameModels(t, doc)

		_, ok := kindNode(t, doc, petVariant).(*ir.Model)
		require.True(t, ok, "the object variant is a model")
		pet, ok := kindNode(t, doc, componentID("Pet")).(*ir.Union)
		require.True(t, ok, "a type list is a union")
		assert.Contains(t, string(pet.Unmodeled["openapi:discriminator"].Value), `"cat":"#/components/schemas/Cat"`,
			"the union keeps the discriminator routing to the subtype")
		for name, tag := range map[string]string{"Cat": "cat", "Dog": "Dog"} {
			sub := requireModelOf(t, doc, name)
			require.NotNil(t, sub.Base, name)
			assert.Equal(t, petVariant, sub.Base.Target, "%s composes the object variant", name)
			assert.Empty(t, keptOn(sub), "%s keeps nothing", name)
			assert.Equal(t, tag, sub.DiscriminatorValue, name)
		}
		assert.Equal(t, []string{"kind", "meow"}, flattenedNames(doc, componentID("Cat")))
		assert.Equal(t, []string{"bark", "kind"}, flattenedNames(doc, componentID("Dog")))
	})
}

// TestAllOfCompose_AMemberTheVariantAndTheSubtypeDeclareFolds holds that a
// member the object variant and the subtype both declare folds as it does
// over a schema declaring the object alone: the subtype's members are the
// same in both, nothing is reported, and the composition names the variant.
func TestAllOfCompose_AMemberTheVariantAndTheSubtypeDeclareFolds(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"Pet: {type: [object, string], properties: {kind: {type: string}}}",
		"Obj: {type: object, properties: {kind: {type: string}}}",
		"OverPet: {allOf: [{$ref: '#/components/schemas/Pet'}], type: object, properties: {kind: {type: string, enum: [cat]}}}",
		"OverObj: {allOf: [{$ref: '#/components/schemas/Obj'}], type: object, properties: {kind: {type: string, enum: [cat]}}}",
	}
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		doc, diags := compileAnnotationSpec(t, "fold", admissionSpec("3.1.0", "", schemas...))
		requireErrorsAndWarnings(t, diags, nil)
		requireModelsNameModels(t, doc)
		overPet, overObj := requireModelOf(t, doc, "OverPet"), requireModelOf(t, doc, "OverObj")
		require.NotNil(t, overPet.Base)
		assert.Equal(t, petVariant, overPet.Base.Target)
		assert.Equal(t, flattenedNames(doc, componentID("OverObj")), flattenedNames(doc, componentID("OverPet")))
		require.Len(t, overPet.Properties, 1)
		require.Len(t, overObj.Properties, 1)
		assert.Equal(t, kindNode(t, doc, overObj.Properties[0].Type.Target).Kind(),
			kindNode(t, doc, overPet.Properties[0].Type.Target).Kind(), "the member folds alike")
	})
}

// TestAllOfCompose_OnlyATypeListsObjectVariantIsComposed holds what the rule
// leaves as it was: a type list of an object and null is an object, composed
// whole; one of no object is no model's to compose, and a oneOf or anyOf
// union holding an object branch is not unpacked, so its $ref is kept with
// one info.
func TestAllOfCompose_OnlyATypeListsObjectVariantIsComposed(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"PetN: {type: [object, 'null'], properties: {kind: {type: string}}}",
		"OverPetN: {allOf: [{$ref: '#/components/schemas/PetN'}], type: object, properties: {meow: {type: boolean}}}",
		"SI: {type: [string, integer]}",
		"OverSI: {allOf: [{$ref: '#/components/schemas/SI'}], type: object, properties: {a: {type: string}}}",
		"U: {oneOf: [{type: object, properties: {k: {type: string}}}, {type: string}]}",
		"OverU: {allOf: [{$ref: '#/components/schemas/U'}], type: object, properties: {a: {type: string}}}",
	}
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		doc, diags := compileAnnotationSpec(t, "limits", admissionSpec("3.1.0", "", schemas...))
		requireErrorsAndWarnings(t, diags, []wantFinding{disjointFinding("OverSI")})
		requireModelsNameModels(t, doc)

		overPetN := requireModelOf(t, doc, "OverPetN")
		require.NotNil(t, overPetN.Base)
		assert.Equal(t, componentID("PetN"), overPetN.Base.Target, "an object and null is an object")
		for _, name := range []string{"OverSI", "OverU"} {
			model := requireModelOf(t, doc, name)
			assert.Empty(t, parentsOf(model), name)
			assert.Equal(t, degradedKept("openapi:$ref"), keptOn(model), "%s: the $ref kept as a refinement keeps one", name)
			assert.Equal(t, 1, degradedInfosAt(diags, name), name)
		}
	})
}

// TestAllOfCompose_TheObjectVariantIsTheOneTheUnionHolds holds that a subtype
// composes the object variant the type list's union itself holds wherever the
// list writes its object, null among the others included, so a list whose
// null stands before the object names no node the document lacks.
func TestAllOfCompose_TheObjectVariantIsTheOneTheUnionHolds(t *testing.T) {
	t.Parallel()
	for _, types := range []string{
		"[string, 'null', object]", "['null', object, string]",
		"[integer, string, 'null', object]", "[string, object, integer]",
	} {
		schemas := []string{
			"Pet: {type: " + types + ", properties: {kind: {type: string}}}",
			"Cat: {allOf: [{$ref: '#/components/schemas/Pet'}, {type: object, properties: {meow: {type: boolean}}}]}",
		}
		t.Run(types, func(t *testing.T) {
			t.Parallel()
			inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
				doc, diags := compileAnnotationSpec(t, "variant", admissionSpec("3.1.0", "", schemas...))
				requireErrorsAndWarnings(t, diags, nil)
				requireModelsNameModels(t, doc)
				pet, ok := kindNode(t, doc, componentID("Pet")).(*ir.Union)
				require.True(t, ok, "a type list is a union")
				i := slices.IndexFunc(pet.Variants, func(variant ir.Variant) bool {
					_, model := doc.Types[variant.Type.Target].(*ir.Model)
					return model
				})
				require.GreaterOrEqual(t, i, 0, "the union holds an object variant")
				cat := requireModelOf(t, doc, "Cat")
				require.NotNil(t, cat.Base)
				assert.Equal(t, pet.Variants[i].Type.Target, cat.Base.Target)
				assert.Equal(t, []string{"kind", "meow"}, flattenedNames(doc, componentID("Cat")))
			})
		})
	}
}
