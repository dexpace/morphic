// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapi_test

import (
	"context"
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

// TestPruneUnreferencedTypes_RemovesOnlyUnreachableAnonymousNodes pins the
// reachability rules of pruneUnreferencedTypes: live anonymous types (reached
// from named types, outside Types, extra roots, or transitively through other
// anonymous types) survive, while unreachable ones (single nodes, chains, self
// loops, and dead cycles) are removed. Non-anonymous types and nil definitions
// are left untouched.
func TestPruneUnreferencedTypes_RemovesOnlyUnreachableAnonymousNodes(t *testing.T) {
	t.Parallel()
	const (
		liveFrom  ir.TypeID = "type/anonymous/from-named"
		liveDeep  ir.TypeID = "type/anonymous/deep"
		liveOp    ir.TypeID = "type/anonymous/from-op"
		liveRoot  ir.TypeID = "type/anonymous/extra-root"
		liveViaR  ir.TypeID = "type/anonymous/via-extra-root"
		liveDisc  ir.TypeID = "type/anonymous/via-disc"
		deadLeaf  ir.TypeID = "type/anonymous/dead-leaf"
		deadSelf  ir.TypeID = "type/anonymous/dead-self"
		deadCycA  ir.TypeID = "type/anonymous/dead-cycle-a"
		deadCycB  ir.TypeID = "type/anonymous/dead-cycle-b"
		primStr   ir.TypeID = "type/primitive/string"
		nilEntry  ir.TypeID = "type/anonymous/nil-entry"
		absentExt ir.TypeID = "type/anonymous/not-in-registry"
	)

	root := rootModel(prop("a", liveFrom), prop("s", primStr))
	root.Discriminator = &ir.Discriminator{Mapping: map[string]ir.TypeID{"d": liveDisc}}

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
			liveRoot:    anonModel(liveRoot, prop("r", liveViaR)),
			liveViaR:    anonModel(liveViaR),
			liveDisc:    anonModel(liveDisc),
			deadLeaf:    anonModel(deadLeaf, prop("s", primStr)),
			deadSelf:    anonModel(deadSelf, prop("self", deadSelf)),
			deadCycA:    anonModel(deadCycA, prop("b", deadCycB)),
			deadCycB:    anonModel(deadCycB, prop("a", deadCycA)),
			nilEntry:    (*ir.Model)(nil),
		},
	}

	pruned, diags := openapi.PruneUnreferencedTypesWith(t.Context(), doc, []ir.TypeID{liveRoot, absentExt}, ir.WalkValuesContext)
	assert.Empty(t, diags)
	for _, want := range []ir.TypeID{primStr, rootModelID, liveFrom, liveDeep, liveOp, liveRoot, liveViaR, liveDisc, nilEntry} {
		assert.Contains(t, pruned.Types, want, "live or non-anonymous type %s stays", want)
	}
	for _, gone := range []ir.TypeID{deadLeaf, deadSelf, deadCycA, deadCycB} {
		assert.NotContains(t, pruned.Types, gone, "unreachable anonymous type %s is removed", gone)
	}
}

// TestPruneUnreferencedTypes_FastPathsAndStoppedWalks pins the early returns
// when no anonymous type exists, when every anonymous type is reachable, and
// when either walk truncates or stops on cancellation.
func TestPruneUnreferencedTypes_FastPathsAndStoppedWalks(t *testing.T) {
	t.Parallel()
	const anonID ir.TypeID = "type/anonymous/orphan"

	t.Run("no anonymous types", func(t *testing.T) {
		t.Parallel()
		doc := &ir.Document{Types: ir.TypeRegistry{rootModelID: rootModel()}}
		got, diags := openapi.PruneUnreferencedTypesWith(t.Context(), doc, nil, ir.WalkValuesContext)
		assert.Same(t, doc, got)
		assert.Empty(t, diags)
	})

	t.Run("all anonymous types reachable", func(t *testing.T) {
		t.Parallel()
		doc := &ir.Document{Types: ir.TypeRegistry{
			rootModelID: rootModel(prop("a", anonID)),
			anonID:      anonModel(anonID),
		}}
		got, diags := openapi.PruneUnreferencedTypesWith(t.Context(), doc, nil, ir.WalkValuesContext)
		assert.Same(t, doc, got)
		assert.Empty(t, diags)
	})

	t.Run("truncated outer walk removes nothing", func(t *testing.T) {
		t.Parallel()
		doc := &ir.Document{Types: ir.TypeRegistry{anonID: anonModel(anonID)}}
		stub := func(context.Context, any, string, ...ir.Visitor) (bool, error) { return true, nil }
		got, diags := openapi.PruneUnreferencedTypesWith(t.Context(), doc, nil, stub)
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
		got, diags := openapi.PruneUnreferencedTypesWith(t.Context(), doc, nil, stub)
		assert.Same(t, doc, got)
		assert.Empty(t, diags)
		assert.Contains(t, got.Types, anonID)
	})

	t.Run("cancelled context removes nothing", func(t *testing.T) {
		t.Parallel()
		doc := &ir.Document{Types: ir.TypeRegistry{anonID: anonModel(anonID)}}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		got, diags := openapi.PruneUnreferencedTypesWith(ctx, doc, nil, ir.WalkValuesContext)
		assert.Same(t, doc, got)
		assert.Empty(t, diags)
		assert.Contains(t, got.Types, anonID)
	})
}

// TestPruneUnreferencedTypes_ValueCarriers pins that anonymous types
// referenced through ir.Value are treated as live.
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
	got, diags := openapi.PruneUnreferencedTypesWith(t.Context(), doc, nil, ir.WalkValuesContext)
	require.Empty(t, diags)
	assert.Contains(t, got.Types, valID)
}
