// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapi_test

import (
	"context"
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

const rootModelID ir.TypeID = "type/openapi/components/schemas/Root"

func anonModel(id ir.TypeID, props ...ir.Property) *ir.Model {
	return &ir.Model{
		ID:         id,
		Anonymous:  true,
		Properties: props,
	}
}

func rootModel(props ...ir.Property) *ir.Model {
	return &ir.Model{
		ID:         rootModelID,
		Properties: props,
	}
}

func prop(wire string, target ir.TypeID) ir.Property {
	return ir.Property{WireName: wire, Type: ir.TypeRef{Target: target}}
}

// TestPruneUnreferencedTypes_RemovesOnlySeededUnreachableAnonymousNodes pins
// the seeded reachability rules of pruneUnreferencedTypes: live anonymous
// types (even if seeded as discarded) survive, seeded unreachable nodes and
// their unreachable anonymous descendants (including self-loops and cycles) are
// removed, and unseeded unreachable orphans stay in Document.Types so irverify
// reports accidental compiler leaks.
func TestPruneUnreferencedTypes_RemovesOnlySeededUnreachableAnonymousNodes(t *testing.T) {
	t.Parallel()
	const (
		liveFrom  ir.TypeID = "type/anonymous/from-named"
		liveDeep  ir.TypeID = "type/anonymous/deep"
		liveOp    ir.TypeID = "type/anonymous/from-op"
		liveUnm   ir.TypeID = "type/anonymous/from-unmodeled"
		liveViaU  ir.TypeID = "type/anonymous/via-unmodeled"
		liveDisc  ir.TypeID = "type/anonymous/via-disc"
		deadLeaf  ir.TypeID = "type/anonymous/dead-leaf"
		deadChild ir.TypeID = "type/anonymous/dead-child"
		deadSelf  ir.TypeID = "type/anonymous/dead-self"
		deadCycA  ir.TypeID = "type/anonymous/dead-cycle-a"
		deadCycB  ir.TypeID = "type/anonymous/dead-cycle-b"
		unseeded  ir.TypeID = "type/anonymous/unseeded-orphan"
		primStr   ir.TypeID = "type/primitive/string"
		nilEntry  ir.TypeID = "type/anonymous/nil-entry"
	)

	root := rootModel(prop("a", liveFrom), prop("s", primStr))
	root.Discriminator = &ir.Discriminator{Mapping: map[string]ir.TypeID{"d": liveDisc}}
	root.Unmodeled = ir.Unmodeled{
		"openapi:not": {
			Reason:  ir.ReasonValidationOnly,
			Value:   ir.RawValue(`{"$ref":"#/a"}`),
			Targets: map[jsontext.Pointer]ir.TypeID{"/$ref": liveUnm},
		},
	}

	doc := &ir.Document{
		Services: []ir.Service{{
			Groups: []ir.OperationGroup{{
				Operations: []ir.Operation{{
					Params: []ir.Parameter{{Type: ir.TypeRef{Target: liveOp}}},
				}},
			}},
		}},
		Types: ir.TypeRegistry{
			primStr:     &ir.Primitive{ID: primStr, PrimitiveKind: ir.PrimitiveString},
			rootModelID: root,
			liveFrom:    anonModel(liveFrom, prop("deep", liveDeep), prop("again", liveDeep)),
			liveDeep:    anonModel(liveDeep, prop("back", liveFrom)),
			liveOp:      anonModel(liveOp),
			liveUnm:     anonModel(liveUnm, prop("r", liveViaU)),
			liveViaU:    anonModel(liveViaU),
			liveDisc:    anonModel(liveDisc),
			deadLeaf:    anonModel(deadLeaf, prop("child", deadChild), prop("live", liveDeep), prop("s", primStr)),
			deadChild:   anonModel(deadChild),
			deadSelf:    anonModel(deadSelf, prop("self", deadSelf)),
			deadCycA:    anonModel(deadCycA, prop("b", deadCycB)),
			deadCycB:    anonModel(deadCycB, prop("a", deadCycA)),
			unseeded:    anonModel(unseeded),
			nilEntry:    (*ir.Model)(nil),
		},
	}

	discarded := map[ir.TypeID]bool{
		liveFrom: true, // still reachable from root: must stay
		deadLeaf: true, // unreachable: deadLeaf and deadChild are pruned; liveDeep stays
		deadSelf: true,
		deadCycA: true, // deadCycB is reachable from deadCycA so both are pruned
		primStr:  true, // non-anonymous: ignored
	}

	pruned, diags := openapi.PruneUnreferencedTypesWith(t.Context(), doc, discarded, ir.WalkValuesContext)
	assert.Empty(t, diags)
	for _, want := range []ir.TypeID{primStr, rootModelID, liveFrom, liveDeep, liveOp, liveUnm, liveViaU, liveDisc, unseeded, nilEntry} {
		assert.Contains(t, pruned.Types, want, "live, non-anonymous, or unseeded type %s stays", want)
	}
	for _, gone := range []ir.TypeID{deadLeaf, deadChild, deadSelf, deadCycA, deadCycB} {
		assert.NotContains(t, pruned.Types, gone, "seeded unreachable anonymous type %s is removed", gone)
	}
}

// TestPruneUnreferencedTypes_FastPathsAndStoppedWalks pins the early returns
// when no discarded seeds exist, when no seed is an anonymous type, when every
// seeded type is reachable, and when either walk truncates or stops on
// cancellation.
func TestPruneUnreferencedTypes_FastPathsAndStoppedWalks(t *testing.T) {
	t.Parallel()
	const anonID ir.TypeID = "type/anonymous/orphan"
	seed := map[ir.TypeID]bool{anonID: true}

	t.Run("no discarded seeds", func(t *testing.T) {
		t.Parallel()
		doc := &ir.Document{Types: ir.TypeRegistry{rootModelID: rootModel(), anonID: anonModel(anonID)}}
		got, diags := openapi.PruneUnreferencedTypesWith(t.Context(), doc, nil, ir.WalkValuesContext)
		assert.Same(t, doc, got)
		assert.Empty(t, diags)
		assert.Contains(t, got.Types, anonID)
	})

	t.Run("no anonymous seeds", func(t *testing.T) {
		t.Parallel()
		doc := &ir.Document{Types: ir.TypeRegistry{rootModelID: rootModel()}}
		got, diags := openapi.PruneUnreferencedTypesWith(t.Context(), doc, map[ir.TypeID]bool{rootModelID: true}, ir.WalkValuesContext)
		assert.Same(t, doc, got)
		assert.Empty(t, diags)
	})

	t.Run("all seeded anonymous types reachable", func(t *testing.T) {
		t.Parallel()
		doc := &ir.Document{Types: ir.TypeRegistry{
			rootModelID: rootModel(prop("a", anonID)),
			anonID:      anonModel(anonID),
		}}
		got, diags := openapi.PruneUnreferencedTypesWith(t.Context(), doc, seed, ir.WalkValuesContext)
		assert.Same(t, doc, got)
		assert.Empty(t, diags)
	})

	t.Run("truncated outer walk removes nothing", func(t *testing.T) {
		t.Parallel()
		doc := &ir.Document{Types: ir.TypeRegistry{anonID: anonModel(anonID)}}
		stub := func(context.Context, any, string, ...ir.Visitor) (bool, error) { return true, nil }
		got, diags := openapi.PruneUnreferencedTypesWith(t.Context(), doc, seed, stub)
		assert.Same(t, doc, got)
		assert.Empty(t, diags)
		assert.Contains(t, got.Types, anonID)
	})

	t.Run("truncated types walk removes nothing", func(t *testing.T) {
		t.Parallel()
		doc := &ir.Document{Types: ir.TypeRegistry{anonID: anonModel(anonID)}}
		calls := 0
		stub := func(ctx context.Context, root any, path string, visitors ...ir.Visitor) (bool, error) {
			calls++
			if calls == 2 {
				return true, nil
			}
			return ir.WalkValuesContext(ctx, root, path, visitors...)
		}
		got, diags := openapi.PruneUnreferencedTypesWith(t.Context(), doc, seed, stub)
		assert.Same(t, doc, got)
		assert.Empty(t, diags)
		assert.Contains(t, got.Types, anonID)
	})

	t.Run("cancelled context removes nothing", func(t *testing.T) {
		t.Parallel()
		doc := &ir.Document{Types: ir.TypeRegistry{anonID: anonModel(anonID)}}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		got, diags := openapi.PruneUnreferencedTypesWith(ctx, doc, seed, ir.WalkValuesContext)
		assert.Same(t, doc, got)
		assert.Empty(t, diags)
		assert.Contains(t, got.Types, anonID)
	})
}

// TestPruneUnreferencedTypes_ValueCarriers pins that anonymous types
// referenced through ir.Value are treated as live even when seeded.
func TestPruneUnreferencedTypes_ValueCarriers(t *testing.T) {
	t.Parallel()
	const valID ir.TypeID = "type/anonymous/from-value"
	root := rootModel()
	root.Examples = []ir.Example{{Value: &ir.Value{
		Kind:        ir.ValueConstructor,
		Constructor: &ir.ConstructorValue{Scalar: valID, Name: "now"},
	}}}
	doc := &ir.Document{Types: ir.TypeRegistry{
		rootModelID: root,
		valID:       anonModel(valID),
	}}
	got, diags := openapi.PruneUnreferencedTypesWith(t.Context(), doc, map[ir.TypeID]bool{valID: true}, ir.WalkValuesContext)
	require.Empty(t, diags)
	assert.Contains(t, got.Types, valID)
}
