// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

//go:build pending

package e2e_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestVariantBounds_AStringBoundBesideAnArrayAndAStringBranchGoesToTheString
// holds the shape the bound is reported for: a union of an array and a string
// beside object keywords and a string bound is a union of the two, the
// string's variant holding the bound, the array's none, and nothing kept for
// it, in either order of the branches. Today the position is the top type,
// keeping the union and the members (GitHub #2046).
func TestVariantBounds_AStringBoundBesideAnArrayAndAStringBranchGoesToTheString(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, order := range [][]string{{"Arr", "Str"}, {"Str", "Arr"}} {
			t.Run(fmt.Sprintf("%s/%v", version, order), func(t *testing.T) {
				t.Parallel()
				schema := "{properties: {a: {type: string}}, minLength: 1, oneOf: [" +
					"{$ref: '#/components/schemas/" + order[0] + "'}, {$ref: '#/components/schemas/" + order[1] + "'}]}"
				doc, _ := compileAnnotationSpec(t, "issue", conjunctionSpec(version, schema,
					variantComponents["Str"], variantComponents["Arr"]))
				for _, at := range conjunctionPositions {
					union := variantUnionAt(t, doc, string(at), 2)
					assert.NotContains(t, keptOn(union), "openapi:minLength", "nothing is kept for the bound at %s", at)
					for j, name := range order {
						want := map[string]any{}
						if name == "Str" {
							want = map[string]any{"minLength": 1.0}
						}
						assert.Equal(t, want, variantConstraints(t, doc, union.Variants[j].Type.Target), "variant over %s at %s", name, at)
					}
				}
			})
		}
	}
}
