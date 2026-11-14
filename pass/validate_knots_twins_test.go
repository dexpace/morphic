// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestValidate_ATypeWithAWayOutOfItsKnotHasAFiniteValue holds the types that
// look like a knot and are not: each leads back to itself, and each has a
// value that needs no other. A list with no bounds at all may be empty, a
// union with a variant that is null or names nothing the document declares
// has that variant for a value, and a map may be empty as well.
func TestValidate_ATypeWithAWayOutOfItsKnotHasAFiniteValue(t *testing.T) {
	t.Parallel()
	for name, def := range map[string]ir.TypeDef{
		"a list of itself with no bounds":          &ir.List{ID: "t/k", Elem: ref("t/k"), Provenance: provAt("t/k")},
		"a map of itself with no bounds":           &ir.MapT{ID: "t/k", Value: ref("t/k"), Provenance: provAt("t/k")},
		"a map of itself that may be empty":        &ir.MapT{ID: "t/k", Value: ref("t/k"), Constraints: &ir.Constraints{MinProps: new(int64(0))}, Provenance: provAt("t/k")},
		"a union with a variant that is nullable":  &ir.Union{ID: "t/k", Variants: []ir.Variant{{Type: ref("t/k")}, {Type: maybe("t/prim/string")}}, Provenance: provAt("t/k")},
		"a union with a variant nothing declares":  &ir.Union{ID: "t/k", Variants: []ir.Variant{{Type: ref("t/k")}, {Type: ref("t/ghost")}}, Provenance: provAt("t/k")},
		"a union of nothing":                       &ir.Union{ID: "t/k", Provenance: provAt("t/k")},
		"a model requiring itself, where nullable": composed("t/k", nil, needs("p/k/0", maybe("t/k"))),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, found(t, knotDoc(def), finiteCode))
		})
	}

	forced := &ir.MapT{ID: "t/k", Value: ref("t/k"), Constraints: &ir.Constraints{MinProps: new(int64(1))}, Provenance: provAt("t/k")}
	assert.Equal(t, []finding{{"t/k", "no finite value: each needs its value, t/k, which has none either"}},
		found(t, knotDoc(forced), finiteCode), "a map that must hold an entry")
}
