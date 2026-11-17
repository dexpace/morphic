// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/ir"
)

// TestCompile_AVariantKeepingItsBranchBesideAMapIsThatMap holds that a variant
// composing nothing over a body that is a map alone is the map's MapT, as the
// body alone is, keeping its branch and the property count beside it, and that
// the document passes every harness oracle at both versions, in both orders of
// the branches: a Model holding nothing but a catch-all breaks an invariant.
func TestCompile_AVariantKeepingItsBranchBesideAMapIsThatMap(t *testing.T) {
	t.Parallel()
	const kept = "{anyOf: [{type: object}, {type: string}]}"
	const named = "{$ref: '#/components/schemas/B'}"
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, keptAt := range []int{0, 1} {
			branches := named + ", " + kept
			if keptAt == 0 {
				branches = kept + ", " + named
			}
			spec := admissionSpec(version, "",
				"B: {type: object, properties: {b: {type: integer}}}",
				"M: {type: object, description: d, minProperties: 1, additionalProperties: {type: string}, oneOf: ["+
					branches+"]}")
			t.Run(version+"/"+branches, func(t *testing.T) {
				t.Parallel()
				res := harness.Check(t.Context(), "spec.yaml", []byte(spec))
				require.Equal(t, harness.OutcomeOK, res.Outcome, res.Detail)

				doc, diags := compileAnnotationSpec(t, "map", spec)
				requireErrorsAndWarnings(t, diags, nil)
				union, ok := kindNode(t, doc, componentID("M")).(*ir.Union)
				require.True(t, ok, "the body distributes over the union")
				require.Len(t, union.Variants, 2)
				m, ok := kindNode(t, doc, union.Variants[keptAt].Type.Target).(*ir.MapT)
				require.True(t, ok, "the variant keeping its branch is the map")
				assert.Equal(t, ir.TypeID("t/prim/string"), m.Value.Target)
				assert.Contains(t, m.Unmodeled, "openapi:oneOf/"+string(rune('0'+keptAt)))
				assert.Len(t, m.Unmodeled, 1, "the branch is all the map keeps")
				assert.Equal(t, map[string]any{"minProps": 1.0}, variantConstraints(t, doc, m.ID))
				composed, ok := kindNode(t, doc, union.Variants[1-keptAt].Type.Target).(*ir.Model)
				require.True(t, ok, "the variant over B composes it")
				require.NotNil(t, composed.Base)
				assert.Equal(t, componentID("B"), composed.Base.Target)
			})
		}
	}
}
