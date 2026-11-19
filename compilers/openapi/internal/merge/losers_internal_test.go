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
	stubbed, recorded := stubMerger(map[ir.TypeID]ir.TypeDef{
		"type/str": &ir.Primitive{ID: "type/str", PrimitiveKind: ir.PrimitiveString},
		"type/int": &ir.Primitive{ID: "type/int", PrimitiveKind: ir.PrimitiveInt32},
	})
	loser := ir.Property{
		WireName:   "id",
		Type:       ir.TypeRef{Target: "type/int", Nullable: true},
		Provenance: ir.Provenance{Source: 2, Pointer: "/components/schemas/S/allOf/0/properties/id"},
	}
	winner := ir.Property{
		WireName:   "id",
		Type:       ir.TypeRef{Target: "type/str"},
		Provenance: ir.Provenance{Source: 2, Pointer: "/components/schemas/S/allOf/1/properties/id"},
	}

	dst := stubbed.fold([]declaration{{prop: loser, source: written(`{"type":"integer","nullable":true}`)}, {prop: winner, source: unread(t)}})

	require.Len(t, *recorded, 1, "the conflict is still diagnosed")
	assert.Equal(t, ir.TypeID("type/str"), dst.Type.Target, "the later declaration wins the shape")

	key := "openapi:conflicting-redeclaration/components/schemas/S/allOf/0/properties/id"
	entry, ok := dst.Unmodeled[key]
	require.True(t, ok, "the losing declaration is kept under a pointer-namespaced key; got %v", dst.Unmodeled)
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.JSONEq(t, `{"type":"integer","nullable":true}`, string(entry.Value),
		"the declaration is kept as the document wrote it, nullability included")
	assert.Equal(t, ir.Provenance{Source: 2, Pointer: "/components/schemas/S/allOf/0/properties/id"},
		entry.Provenance, "the entry locates the losing declaration, not the merged property")
}

// TestKeepLosingDeclaration_EveryLoserSurvivesItsSiblings pins the key's namespacing.
// A field three branches type three incompatible ways discards two
// declarations, and a fixed key would leave only whichever branch ran last —
// the same silent overwrite the entry exists to prevent.
func TestKeepLosingDeclaration_EveryLoserSurvivesItsSiblings(t *testing.T) {
	t.Parallel()
	reg := map[ir.TypeID]ir.TypeDef{
		"type/str":  &ir.Primitive{ID: "type/str", PrimitiveKind: ir.PrimitiveString},
		"type/int":  &ir.Primitive{ID: "type/int", PrimitiveKind: ir.PrimitiveInt32},
		"type/bool": &ir.Primitive{ID: "type/bool", PrimitiveKind: ir.PrimitiveBool},
	}
	stubbed, _ := stubMerger(reg)
	model := &ir.Model{}
	idx, settle := indexOver(model, stubbed)

	idx.MergeProperty(ir.Property{WireName: "id", Type: ir.TypeRef{Target: "type/str"}, Provenance: ptrAt("/a")}, written(`{"type":"string"}`), "")
	idx.MergeProperty(ir.Property{WireName: "id", Type: ir.TypeRef{Target: "type/int"}, Provenance: ptrAt("/b")}, written(`{"type":"integer"}`), "")
	idx.MergeProperty(ir.Property{WireName: "id", Type: ir.TypeRef{Target: "type/bool"}, Provenance: ptrAt("/c")}, unread(t), "")
	settle()

	require.Len(t, model.Properties, 1, "three declarations still reconcile to one property")
	assert.Equal(t, ir.Unmodeled{
		"openapi:conflicting-redeclaration/a": {
			Reason:     ir.ReasonDegradedLowering,
			Value:      ir.RawValue(`{"type":"string"}`),
			Provenance: ir.Provenance{Pointer: "/a"},
		},
		"openapi:conflicting-redeclaration/b": {
			Reason:     ir.ReasonDegradedLowering,
			Value:      ir.RawValue(`{"type":"integer"}`),
			Provenance: ir.Provenance{Pointer: "/b"},
		},
	}, model.Properties[0].Unmodeled)
}

// TestKeepLosingDeclaration_AgreeingDeclarationsKeepNothing pins the side the
// entry must not appear on: agreeing declarations discard nothing, so an entry
// would be noise. A keyword no one value states is the other side: the merge
// keeps the later declaration's and drops the earlier one's, which is a loss
// like any other, and the entry records what the merge dropped rather than
// deciding what it should have kept.
func TestKeepLosingDeclaration_AgreeingDeclarationsKeepNothing(t *testing.T) {
	t.Parallel()
	stubbed, recorded := stubMerger(map[ir.TypeID]ir.TypeDef{
		"type/str": &ir.Primitive{ID: "type/str", PrimitiveKind: ir.PrimitiveString},
	})
	agreeing := ir.Property{WireName: "id", Type: ir.TypeRef{Target: "type/str"}}

	agreeing = foldTwo(stubbed, agreeing, ir.Property{WireName: "id", Type: ir.TypeRef{Target: "type/str"}, Provenance: ptrAt("/b")}, unread(t))

	assert.Empty(t, *recorded, "agreeing declarations are not a conflict")
	assert.Empty(t, agreeing.Unmodeled, "and nothing was discarded to keep")

	bounded := stubbed.fold([]declaration{
		{prop: ir.Property{WireName: "code", Type: ir.TypeRef{Target: "type/str"}, Constraints: &ir.Constraints{Pattern: "^a$"},
			Provenance: ptrAt("/a")}, source: written(`{"type":"string","pattern":"^a$"}`)},
		{prop: ir.Property{WireName: "code", Type: ir.TypeRef{Target: "type/str"}, Constraints: &ir.Constraints{Pattern: "^b$"},
			Provenance: ptrAt("/b")}, source: unread(t)},
	})

	require.Len(t, *recorded, 1, "the constraint conflict is still diagnosed")
	assert.Equal(t, diag.ConflictingRedecl, (*recorded)[0].Code)
	assert.Equal(t, "^b$", bounded.Constraints.Pattern)
	assert.Equal(t, ir.Unmodeled{
		"openapi:conflicting-redeclaration/a": {
			Reason:     ir.ReasonDegradedLowering,
			Value:      ir.RawValue(`{"type":"string","pattern":"^a$"}`),
			Provenance: ir.Provenance{Pointer: "/a"},
		},
	}, bounded.Unmodeled, "the keyword the merge dropped is kept with the declaration that wrote it")
}
