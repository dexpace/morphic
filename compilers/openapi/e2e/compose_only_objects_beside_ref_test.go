// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
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
