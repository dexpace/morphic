// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// memberRow is a registry to hold MemberReader to, and the declarations it
// must take that a reader of the flattening alone would not show: a wire name
// of a root and the property it must name.
type memberRow struct {
	types ir.TypeRegistry
	taken map[[2]string]ir.PropID
}

// memberRows are the registries the flattening's own tests build, and the
// shapes on which a reader of one member could part from it: an alias for a
// root, a name declared twice, a diamond and cycles.
func memberRows() map[string]memberRow {
	nilModel := irtest.DocOf(owning("m", mixing("nil", "x"), wired("m.a", "a")), owning("x", nil, wired("x.a", "a")))
	nilModel.Types["nil"] = (*ir.Model)(nil)
	return map[string]memberRow{
		"a root that is an alias of a model": {
			types: irtest.DocOf(
				irtest.ScalarOf("root", "alias"), irtest.ScalarOf("alias", "m"),
				owning("m", based("b"), wired("m.a", "a")),
				owning("b", nil, wired("b.a", "a"), wired("b.b", "b")),
			).Types,
			taken: map[[2]string]ir.PropID{{"root", "a"}: "m.a", {"root", "b"}: "b.b", {"alias", "a"}: "m.a"},
		},
		"a root that names no model": {
			types: irtest.DocOf(
				irtest.ScalarOf("opaque", ""), irtest.ScalarOf("toEnum", "enum"), &ir.Enum{ID: "enum"},
				irtest.ScalarOf("loop", "loop"), owning("m", nil, wired("m.a", "a")),
			).Types,
		},
		"a model declaring one wire name twice": {
			types: irtest.DocOf(owning("m", nil, wired("m.first", "n"), wired("m.later", "n"), wired("m.o", "o"))).Types,
			taken: map[[2]string]ir.PropID{{"m", "n"}: "m.later"},
		},
		"a diamond": {
			types: irtest.DocOf(
				owning("m", mixing("x", "y")),
				owning("x", mixing("z"), wired("x.n", "n")),
				owning("y", mixing("z")),
				owning("z", nil, wired("z.n", "n")),
			).Types,
			taken: map[[2]string]ir.PropID{{"m", "n"}: "x.n"},
		},
		"a cycle the root composes": {
			types: irtest.DocOf(
				owning("r", mixing("a", "z")),
				owning("a", mixing("r"), wired("a.n", "n")),
				owning("z", nil, wired("z.n", "n")),
			).Types,
			taken: map[[2]string]ir.PropID{{"r", "n"}: "a.n"},
		},
		"a cycle through a base and an alias": {
			types: irtest.DocOf(
				owning("a", based("b"), wired("a.p", "p")),
				owning("b", mixing("alias"), wired("b.q", "q"), wired("b.p", "p")),
				irtest.ScalarOf("alias", "a"),
				owning("self", based("self"), wired("self.s", "s")),
			).Types,
		},
		"a model on a cycle through the root": {
			types: irtest.DocOf(
				owning("r", func(m *ir.Model) { m.Implements = irtest.RefsTo("c"); m.Mixins = irtest.RefsTo("x") }),
				owning("c", based("r"), wired("c.k", "k")),
				owning("x", nil, wired("x.k", "k"), wired("x.j", "j")),
				owning("s", func(m *ir.Model) { m.Implements = irtest.RefsTo("a"); m.Mixins = irtest.RefsTo("x") }),
				owning("a", based("b"), wired("a.j", "j")),
				owning("b", based("s"), wired("b.k", "k")),
			).Types,
		},
		"a model read earlier that holds nothing back": {
			types: irtest.DocOf(
				owning("r", mixing("a", "b", "c")),
				owning("a", nil, wired("a.j", "j")),
				owning("b", based("a"), wired("b.k", "k")),
				owning("c", nil, wired("c.k", "k")),
			).Types,
			taken: map[[2]string]ir.PropID{{"r", "k"}: "c.k"},
		},
		"entries that name no model": {
			types: irtest.DocOf(
				owning("m", func(m *ir.Model) {
					m.Base = &ir.TypeRef{Target: "uuid"}
					m.Implements = []ir.TypeRef{{}, {Target: "u"}}
					m.Mixins = irtest.RefsTo("x", "enum", "ghost", "cycle1", "holey", "uuid")
				}, wired("m.a", "a")),
				irtest.ScalarOf("uuid", "string"), &ir.Primitive{ID: "string", Prim: ir.PrimString},
				&ir.Union{ID: "u"}, owning("x", mixing("deep"), wired("x.a", "a")), &ir.MapT{ID: "deep"},
				&ir.Enum{ID: "enum"}, irtest.ScalarOf("cycle1", "cycle2"), irtest.ScalarOf("cycle2", "cycle1"),
				&ir.Scalar{ID: "holey", Base: &ir.TypeRef{}},
			).Types,
		},
		"a model of no value": {types: nilModel.Types},
	}
}

// declaredWires returns every wire name a model of types declares, and one it does
// not, in order.
func declaredWires(types ir.TypeRegistry) []string {
	seen := map[string]bool{"absent": true}
	for _, def := range types {
		m, ok := def.(*ir.Model)
		if !ok || m == nil {
			continue
		}
		for i := range m.Properties {
			seen[m.Properties[i].EffectiveWireName()] = true
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// TestMemberReader_AgreesWithFlattenedProperties holds Member to the
// declaration the flattening of the root takes for a wire name, and to false
// where the flattening has none, for every root and every name any model
// declares. A reader is asked twice: fresh for each question, and one reader
// asked every question in each order of roots, since what it keeps from one
// root must not change what it answers for another.
func TestMemberReader_AgreesWithFlattenedProperties(t *testing.T) {
	t.Parallel()
	for name, row := range memberRows() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			roots := []ir.TypeID{"", "missing"}
			for id := range row.types {
				roots = append(roots, id)
			}
			slices.Sort(roots)
			names := declaredWires(row.types)

			for _, root := range roots {
				flat := ir.FlattenedProperties(row.types, root)
				want := map[string]ir.PropertyDecl{}
				for _, p := range flat.Properties {
					want[p.Property.EffectiveWireName()] = p.PropertyDecl
				}
				for _, wire := range names {
					got, ok := ir.NewMemberReader(row.types).Member(root, wire)
					decl, held := want[wire]
					require.Equal(t, held, ok, "root %q, name %q", root, wire)
					assert.Equal(t, decl.Owner, got.Owner, "root %q, name %q", root, wire)
					assert.Same(t, decl.Property, got.Property, "root %q, name %q", root, wire)
				}
			}
			backward := slices.Clone(roots)
			slices.Reverse(backward)
			for _, order := range [][]ir.TypeID{roots, backward} {
				shared := ir.NewMemberReader(row.types)
				for _, root := range order {
					flat := ir.FlattenedProperties(row.types, root)
					for _, p := range flat.Properties {
						wire := p.Property.EffectiveWireName()
						got, ok := shared.Member(root, wire)
						require.True(t, ok, "root %q, name %q", root, wire)
						assert.Same(t, p.Property, got.Property, "shared reader, root %q, name %q", root, wire)
					}
					for _, wire := range names {
						if !slices.ContainsFunc(flat.Properties, func(p ir.FlatProperty) bool {
							return p.Property.EffectiveWireName() == wire
						}) {
							_, ok := shared.Member(root, wire)
							assert.False(t, ok, "shared reader, root %q, name %q", root, wire)
						}
					}
				}
			}
			for key, id := range row.taken {
				got, ok := ir.NewMemberReader(row.types).Member(ir.TypeID(key[0]), key[1])
				require.True(t, ok, "%v", key)
				assert.Equal(t, id, got.Property.ID, "the declaration the row names for %v", key)
			}
		})
	}
}

// TestMemberReader_CostsTheCompositionNotTheWidth holds a read of one member
// to the entries it walks: a model of P members that a root composes costs the
// same at P, 2P and 4P once another read has met that model, so a read neither
// scans the model's properties nor reads the flattening it takes from.
func TestMemberReader_CostsTheCompositionNotTheWidth(t *testing.T) {
	t.Parallel()
	cost := make([]int, 0, 3)
	for _, width := range []int{100, 200, 400} {
		props := []ir.Property{wired("wide.a", "a")}
		for i := range width {
			props = append(props, wired(ir.PropID(fmt.Sprintf("wide.p%d", i)), fmt.Sprintf("p%d", i)))
		}
		types := irtest.DocOf(
			owning("wide", nil, props...),
			owning("bj", mixing("wide")),
			owning("bi", mixing("wide")),
		).Types
		r := ir.NewMemberReader(types)
		_, ok := r.Member("bj", "a")
		require.True(t, ok)

		before := r.Steps()
		got, ok := r.Member("bi", "a")
		require.True(t, ok)
		assert.Equal(t, ir.PropID("wide.a"), got.Property.ID)
		cost = append(cost, r.Steps()-before)
	}
	assert.Positive(t, cost[0], "a read takes steps")
	assert.Equal(t, []int{cost[0], cost[0], cost[0]}, cost, "steps of one read at 100, 200 and 400 members")
}
