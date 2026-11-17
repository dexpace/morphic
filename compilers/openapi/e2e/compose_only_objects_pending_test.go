// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

//go:build pending

package e2e_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestAllOfUnion_AUnionBesideARefSharingNoKindIsKeptLikeTheAllOfSpelling holds
// that the $ref spelling of a union sharing no kind with its target is the
// model over the target keeping the union, as the allOf spelling is. It stays
// behind the pending tag until a $ref and the keywords beside it are read as
// one conjunction: today a union beside a $ref keeps its own rule.
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

// TestAllOfRefine_ATargetInferredNarrowerThanTheDeclaredKindsIsNoPrincipal
// holds that a $ref whose target the kind-inference policy reads as an object
// alone is no principal beside a type list or a union declaring a string too:
// the position is the union its own keywords declare, as the inline spelling
// is, and the $ref is kept with one info. It stays behind the pending tag until
// voteOf weighs an inferred target against every kind the position declares.
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
			assert.Equal(t, degradedKept("openapi:allOf/0"), keptOn(union), name)
			assert.Equal(t, 1, degradedInfosUnder(diags, "/components/schemas/"+name+"/allOf/0"), name)
		}
	})
}

// TestAllOfRefine_ADynamicRefBesideTheAllOfMasksNoKind holds that a $dynamicRef
// the position does not expand, written beside an allOf, admits every kind as
// it does on an entry: the position is a use of what its conjuncts declare, not
// a Model, and keeps the $dynamicRef. It stays behind the pending tag until
// #2009 is fixed.
func TestAllOfRefine_ADynamicRefBesideTheAllOfMasksNoKind(t *testing.T) {
	t.Parallel()
	schemas := []string{
		"T: {type: array, items: {type: string}}",
		"AnchorM: {$dynamicAnchor: m, type: string}",
		"Wrap: {allOf: [{$ref: '#/components/schemas/T'}], $dynamicRef: '#m'}",
		"WrapStr: {allOf: [{type: string}], $dynamicRef: '#m'}",
	}
	inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
		doc, diags := compileAnnotationSpec(t, "dynamic", admissionSpec("3.1.0", "", schemas...))
		requireErrorsAndWarnings(t, diags, nil)
		requireModelsNameModels(t, doc)
		wrap := requireScalarOver(t, doc, "Wrap", componentID("T"))
		assert.Equal(t, degradedKept("openapi:$dynamicRef"), keptOn(wrap))
		str := requireScalarOver(t, doc, "WrapStr", ir.TypeID("t/prim/string"))
		assert.Equal(t, degradedKept("openapi:$dynamicRef"), keptOn(str))
	})
}

// TestAllOfUnion_ARefAdmittingEveryKindBesideAUnionIsKeptAsTheInlineSpellingIs
// holds that a union beside an allOf whose one conjunct is a $ref to a schema
// saying nothing of its kind is the Union holding the bounds, as the same
// schema written inline or with no allOf is, the $ref kept with one info. It
// stays behind the pending tag until voteOf outvotes such a $ref wherever
// another conjunct names a kind.
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
		assert.Equal(t, degradedKept("openapi:allOf/0"), keptOn(u))
		assert.Equal(t, 1, degradedInfosUnder(diags, "/components/schemas/ERef/allOf/0"))
	})
}

// TestAllOfRefine_ARefLosingATieIsReportedAsATie holds that the info a $ref
// kept beside an equal one draws says the tie went by reference text, not that
// the other lies within more conjuncts, which it does not. It stays behind the
// pending tag until keptWhy words the narrowed $ref so.
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
