// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// TestVerify_UnreferencedAnonymousTypes pins ir/unreferenced-anonymous-type:
// anonymous types reached from named types, outside Types, UnmodeledEntry
// Targets, or transitively through other anonymous types pass, while orphan
// leaves, self-loops, and dead cycles are reported.
func TestVerify_UnreferencedAnonymousTypes(t *testing.T) {
	t.Parallel()
	const (
		namedID  ir.TypeID = "type/test/Root"
		liveProp ir.TypeID = "type/anonymous/prop"
		liveDeep ir.TypeID = "type/anonymous/deep"
		liveOp   ir.TypeID = "type/anonymous/op"
		liveUnm  ir.TypeID = "type/anonymous/unmodeled"
		liveDoc  ir.TypeID = "type/anonymous/doc-unmodeled"
		deadLeaf ir.TypeID = "type/anonymous/dead-leaf"
		deadSelf ir.TypeID = "type/anonymous/dead-self"
		deadCycA ir.TypeID = "type/anonymous/dead-cycle-a"
		deadCycB ir.TypeID = "type/anonymous/dead-cycle-b"
	)

	anon := func(id ir.TypeID, props ...ir.Property) *ir.Model {
		return &ir.Model{ID: id, Name: ir.Naming{Hint: "h"}, Anonymous: true, Properties: props}
	}
	prop := func(name string, target ir.TypeID) ir.Property {
		return ir.Property{ID: ir.PropertyID("property/test/" + name), Name: ir.NamingFor(name), WireName: name, Type: ir.TypeRef{Target: target}}
	}

	root := &ir.Model{
		ID:         namedID,
		Name:       ir.NamingFor("Root"),
		Properties: []ir.Property{prop("p", liveProp)},
		Unmodeled: ir.Unmodeled{
			"openapi:not": {Reason: ir.ReasonValidationOnly, Value: ir.RawValue(`{"$ref":"#/a"}`), Targets: []ir.TypeID{liveUnm}},
		},
	}

	doc := &ir.Document{
		IRVersion: ir.IRVersion,
		IDSpaces: map[string][]string{
			ir.IDKindType:     {"anonymous", "test"},
			ir.IDKindProperty: {"test"},
		},
		Unmodeled: ir.Unmodeled{
			"openapi:kept": {Reason: ir.ReasonDegradedLowering, Value: ir.RawValue(`{}`), Targets: []ir.TypeID{liveDoc}},
		},
		Services: []ir.Service{{
			ID:   "service/test/s",
			Name: ir.NamingFor("S"),
			Groups: []ir.OperationGroup{{
				ID:   "operation-group/test/g",
				Name: ir.NamingFor("G"),
				Operations: []ir.Operation{{
					ID:   "operation/test/op",
					Name: ir.NamingFor("Op"),
					Params: []ir.Parameter{{
						ID:   "parameter/test/p",
						Name: ir.NamingFor("p"),
						Type: ir.TypeRef{Target: liveOp},
					}},
				}},
			}},
		}},
		Types: ir.TypeRegistry{
			namedID:  root,
			liveProp: anon(liveProp, prop("deep", liveDeep), prop("dup", liveDeep)),
			liveDeep: anon(liveDeep, prop("back", liveProp)),
			liveOp:   anon(liveOp),
			liveUnm:  anon(liveUnm),
			liveDoc:  anon(liveDoc),
			deadLeaf: anon(deadLeaf),
			deadSelf: anon(deadSelf, prop("self", deadSelf)),
			deadCycA: anon(deadCycA, prop("b", deadCycB)),
			deadCycB: anon(deadCycB, prop("a", deadCycA)),
		},
	}

	var got []string
	for _, v := range irverify.Verify(doc) {
		if v.Code == "ir/unreferenced-anonymous-type" {
			got = append(got, v.Path)
		}
	}
	assert.Equal(t, []string{
		"types[type/anonymous/dead-cycle-a]",
		"types[type/anonymous/dead-cycle-b]",
		"types[type/anonymous/dead-leaf]",
		"types[type/anonymous/dead-self]",
	}, got)
}

func minimalValidDoc(types ir.TypeRegistry) *ir.Document {
	return &ir.Document{
		IRVersion: ir.IRVersion,
		IDSpaces: map[string][]string{
			ir.IDKindType:     {"anonymous", "test"},
			ir.IDKindProperty: {"test"},
			ir.IDKindService:  {"test"},
		},
		Sources:  []ir.SourceInfo{{Format: "openapi@3.1", Path: "spec.yaml", Hash: "0000000000000000000000000000000000000000000000000000000000000000"}},
		Services: []ir.Service{{ID: "service/test/s", Name: ir.NamingFor("S")}},
		Types:    types,
	}
}

// TestVerify_UnreferencedAnonymousTypes_AllDirectlyReachedAndOnlyDead pins the
// fast paths: every anonymous type directly reached from a named root, every
// anonymous type reached transitively, and an anonymous type with no roots at
// all.
func TestVerify_UnreferencedAnonymousTypes_AllDirectlyReachedAndOnlyDead(t *testing.T) {
	t.Parallel()
	const (
		namedID ir.TypeID = "type/test/Root"
		anonA   ir.TypeID = "type/anonymous/a"
		anonB   ir.TypeID = "type/anonymous/b"
	)

	t.Run("all directly reached", func(t *testing.T) {
		t.Parallel()
		doc := minimalValidDoc(ir.TypeRegistry{
			namedID: &ir.Model{ID: namedID, Name: ir.NamingFor("Root"), Properties: []ir.Property{
				{ID: "property/test/a", Name: ir.NamingFor("a"), WireName: "a", Type: ir.TypeRef{Target: anonA}},
			}},
			anonA: &ir.Model{ID: anonA, Name: ir.Naming{Hint: "a"}, Anonymous: true},
		})
		assert.Empty(t, irverify.Verify(doc))
	})

	t.Run("all reached transitively", func(t *testing.T) {
		t.Parallel()
		doc := minimalValidDoc(ir.TypeRegistry{
			namedID: &ir.Model{ID: namedID, Name: ir.NamingFor("Root"), Properties: []ir.Property{
				{ID: "property/test/a", Name: ir.NamingFor("a"), WireName: "a", Type: ir.TypeRef{Target: anonA}},
			}},
			anonA: &ir.Model{ID: anonA, Name: ir.Naming{Hint: "a"}, Anonymous: true, Properties: []ir.Property{
				{ID: "property/test/b", Name: ir.NamingFor("b"), WireName: "b", Type: ir.TypeRef{Target: anonB}},
			}},
			anonB: &ir.Model{ID: anonB, Name: ir.Naming{Hint: "b"}, Anonymous: true},
		})
		assert.Empty(t, irverify.Verify(doc))
	})

	t.Run("no roots at all", func(t *testing.T) {
		t.Parallel()
		doc := minimalValidDoc(ir.TypeRegistry{
			anonA: &ir.Model{ID: anonA, Name: ir.Naming{Hint: "a"}, Anonymous: true},
		})
		vs := irverify.Verify(doc)
		assert.Len(t, vs, 1)
		assert.Equal(t, "ir/unreferenced-anonymous-type", vs[0].Code)
		assert.Equal(t, "types[type/anonymous/a]", vs[0].Path)
	})
}

// TestVerify_UnmodeledTargets_CanonicalAndResolved pins that UnmodeledEntry
// Targets must be sorted without duplicates and resolve in Document.Types.
func TestVerify_UnmodeledTargets_CanonicalAndResolved(t *testing.T) {
	t.Parallel()
	const (
		rootID ir.TypeID = "type/test/Root"
		aID    ir.TypeID = "type/anonymous/a"
		bID    ir.TypeID = "type/anonymous/b"
	)

	makeDoc := func(targets []ir.TypeID) *ir.Document {
		return minimalValidDoc(ir.TypeRegistry{
			rootID: &ir.Model{
				ID:   rootID,
				Name: ir.NamingFor("Root"),
				Unmodeled: ir.Unmodeled{
					"openapi:not": {Reason: ir.ReasonValidationOnly, Value: ir.RawValue(`{}`), Targets: targets},
				},
			},
			aID: &ir.Model{ID: aID, Name: ir.Naming{Hint: "a"}, Anonymous: true},
			bID: &ir.Model{ID: bID, Name: ir.Naming{Hint: "b"}, Anonymous: true},
		})
	}

	codesOf := func(vs []irverify.Violation) []string {
		codes := make([]string, 0, len(vs))
		for _, v := range vs {
			codes = append(codes, v.Code)
		}
		return codes
	}

	assert.Empty(t, irverify.Verify(makeDoc([]ir.TypeID{aID, bID})))
	assert.Contains(t, codesOf(irverify.Verify(makeDoc([]ir.TypeID{bID, aID}))), "ir/unmodeled-targets-not-canonical")
	assert.Contains(t, codesOf(irverify.Verify(makeDoc([]ir.TypeID{aID, aID}))), "ir/unmodeled-targets-not-canonical")
	assert.Contains(t, codesOf(irverify.Verify(makeDoc([]ir.TypeID{aID, bID, "type/anonymous/missing"}))), "ir/dangling-type-ref")
}
