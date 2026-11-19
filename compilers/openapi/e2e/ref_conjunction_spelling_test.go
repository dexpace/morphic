// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import "testing"

// TestRefConjunction_AUnionBesideARefToTheTopTypeStandsAtThePosition holds
// the four spellings of a union beside a $ref to a schema admitting every
// kind to one document, the union the node the position owns
// (docs/compilers/openapi.md#allof-refinement).
func TestRefConjunction_AUnionBesideARefToTheTopTypeStandsAtThePosition(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"T: {}", "T: true", "T: {properties: {a: {type: string}}}"} {
		requireOneDocument(t, conjSites, target, "oneOf: [{type: string}, {type: integer}]")
	}
}
