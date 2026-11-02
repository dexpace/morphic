// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// TestKeepLosingDeclaration_RecordsTheDiscardedDeclaration pins what the fix for
// GitHub #424 adds: the redeclaration whose type loses is kept verbatim beside
// the winner, so a consumer reading the document — not the diagnostic stream —
// still sees what the second declaration said.
//
// The reference is asserted whole rather than by its target alone: Nullable is
// half of what a redeclaration says about a field, and an entry that kept only
// the ID would lose it while still looking like a preservation.
func TestKeepLosingDeclaration_RecordsTheDiscardedDeclaration(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/str": &ir.Primitive{ID: "t/str", Prim: ir.PrimString},
		"t/int": &ir.Primitive{ID: "t/int", Prim: ir.PrimInt32},
	})
	dst := ir.Property{
		WireName:   "id",
		Type:       ir.TypeRef{Target: "t/str"},
		Provenance: ir.Provenance{Source: 2, Pointer: "/components/schemas/S/allOf/0/properties/id"},
	}
	src := ir.Property{
		WireName:   "id",
		Type:       ir.TypeRef{Target: "t/int", Nullable: true},
		Provenance: ir.Provenance{Source: 2, Pointer: "/components/schemas/S/allOf/1/properties/id"},
	}

	dst = foldTwo(g, dst, src, written(`{"type":"integer","nullable":true}`))

	require.Len(t, *recorded, 1, "the conflict is still diagnosed")
	assert.Equal(t, ir.TypeID("t/str"), dst.Type.Target, "the first declaration still wins the shape")

	key := "openapi:conflicting-redeclaration/components/schemas/S/allOf/1/properties/id"
	entry, ok := dst.Unmodeled[key]
	require.True(t, ok, "the losing declaration is kept under a pointer-namespaced key; got %v", dst.Unmodeled)
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.JSONEq(t, `{"type":"integer","nullable":true}`, string(entry.Value),
		"the declaration is kept as the document wrote it, nullability included")
	assert.Equal(t, ir.Provenance{Source: 2, Pointer: "/components/schemas/S/allOf/1/properties/id"},
		entry.Provenance, "the entry locates the losing declaration, not the merged property")
}

// TestKeepLosingDeclaration_EveryLoserSurvivesItsSiblings pins the key's namespacing.
// A field three branches type three incompatible ways discards two
// declarations, and a fixed key would leave only whichever branch ran last —
// the same silent overwrite the entry exists to prevent.
func TestKeepLosingDeclaration_EveryLoserSurvivesItsSiblings(t *testing.T) {
	t.Parallel()
	reg := map[ir.TypeID]ir.TypeDef{
		"t/str":  &ir.Primitive{ID: "t/str", Prim: ir.PrimString},
		"t/int":  &ir.Primitive{ID: "t/int", Prim: ir.PrimInt32},
		"t/bool": &ir.Primitive{ID: "t/bool", Prim: ir.PrimBool},
	}
	g, _ := stubMerger(reg)
	m := &ir.Model{}
	idx, settle := indexOver(m, g)

	idx.MergeProperty(ir.Property{WireName: "id", Type: ir.TypeRef{Target: "t/str"}, Provenance: ptrAt("/a")}, unread(t), "")
	idx.MergeProperty(ir.Property{WireName: "id", Type: ir.TypeRef{Target: "t/int"}, Provenance: ptrAt("/b")}, written(`{"type":"integer"}`), "")
	idx.MergeProperty(ir.Property{WireName: "id", Type: ir.TypeRef{Target: "t/bool"}, Provenance: ptrAt("/c")}, written(`{"type":"boolean"}`), "")
	settle()

	require.Len(t, m.Properties, 1, "three declarations still reconcile to one property")
	assert.Equal(t, ir.Unmodeled{
		"openapi:conflicting-redeclaration/b": {
			Reason:     ir.ReasonDegradedLowering,
			Value:      ir.RawValue(`{"type":"integer"}`),
			Provenance: ir.Provenance{Pointer: "/b"},
		},
		"openapi:conflicting-redeclaration/c": {
			Reason:     ir.ReasonDegradedLowering,
			Value:      ir.RawValue(`{"type":"boolean"}`),
			Provenance: ir.Provenance{Pointer: "/c"},
		},
	}, m.Properties[0].Unmodeled)
}

// TestKeepLosingDeclaration_AgreeingDeclarationsKeepNothing pins the side the
// entry must not appear on: agreeing declarations discard nothing, so an entry
// would be noise. A keyword no one value states is the other side: the merge
// keeps the first declaration's and drops the redeclaration's, which is a loss
// like any other, and the entry records what the merge dropped rather than
// deciding what it should have kept.
func TestKeepLosingDeclaration_AgreeingDeclarationsKeepNothing(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/str": &ir.Primitive{ID: "t/str", Prim: ir.PrimString},
	})
	agreeing := ir.Property{WireName: "id", Type: ir.TypeRef{Target: "t/str"}}

	agreeing = foldTwo(g, agreeing, ir.Property{WireName: "id", Type: ir.TypeRef{Target: "t/str"}, Provenance: ptrAt("/b")}, unread(t))

	assert.Empty(t, *recorded, "agreeing declarations are not a conflict")
	assert.Empty(t, agreeing.Unmodeled, "and nothing was discarded to keep")

	bounded := ir.Property{
		WireName: "code", Type: ir.TypeRef{Target: "t/str"},
		Constraints: &ir.Constraints{Pattern: "^a$"},
	}
	bounded = foldTwo(g, bounded, ir.Property{
		WireName: "code", Type: ir.TypeRef{Target: "t/str"},
		Constraints: &ir.Constraints{Pattern: "^b$"},
		Provenance:  ptrAt("/b"),
	}, written(`{"type":"string","pattern":"^b$"}`))

	require.Len(t, *recorded, 1, "the constraint conflict is still diagnosed")
	assert.Equal(t, diag.ConflictingRedecl, (*recorded)[0].Code)
	assert.Equal(t, ir.Unmodeled{
		"openapi:conflicting-redeclaration/b": {
			Reason:     ir.ReasonDegradedLowering,
			Value:      ir.RawValue(`{"type":"string","pattern":"^b$"}`),
			Provenance: ir.Provenance{Pointer: "/b"},
		},
	}, bounded.Unmodeled, "the keyword the merge dropped is kept with the declaration that wrote it")
}
