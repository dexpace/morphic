// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/ir"
)

// TestCompile_ABranchBesideABodyThePolicyMadeAnObjectLives holds that a branch
// admitting no object, beside object keywords with no type that the
// kind-inference policy made an object, is a variant: a string satisfies
// `properties`, which reads objects alone, so the union keeps no branch and
// the schema draws no warning (docs/compilers/openapi.md#union-beside-structure),
// under oneOf and anyOf, in both orders of the branches.
func TestCompile_ABranchBesideABodyThePolicyMadeAnObjectLives(t *testing.T) {
	t.Parallel()
	for _, keyword := range []string{"oneOf", "anyOf"} {
		for _, branches := range []string{"[{required: [x]}, {$ref: '#/components/schemas/Str'}]",
			"[{$ref: '#/components/schemas/Str'}, {required: [x]}]"} {
			spec := admissionSpec("3.1.0", "", "Str: {type: string}",
				fmt.Sprintf("P: {properties: {p: {type: string}}, %s: %s}", keyword, branches))
			t.Run(keyword+"/"+branches, func(t *testing.T) {
				t.Parallel()
				res := harness.Check(t.Context(), "spec.yaml", []byte(spec))
				require.Equal(t, harness.OutcomeOK, res.Outcome, res.Detail)
				doc, diags := compileAnnotationSpec(t, "inferred", spec)
				requireErrorsAndWarnings(t, diags, nil)
				union, ok := typeByName(doc, "P").(*ir.Union)
				require.True(t, ok, "the union distributes over the body the policy made an object")
				assert.Len(t, union.Variants, 2, "the string branch is a variant")
				assert.Empty(t, keptOn(union), "the union keeps no branch")
			})
		}
	}
}
