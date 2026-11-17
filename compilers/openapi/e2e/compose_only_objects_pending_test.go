// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

//go:build pending

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
