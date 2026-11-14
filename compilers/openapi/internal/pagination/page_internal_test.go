// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestResolve_ReadsAtMostTheBound pins the alias scalars a type is read
// through: a chain of maxAliasHops reaches what it stands over, and a longer
// one, a loop or a typed nil stands for no type.
func TestResolve_ReadsAtMostTheBound(t *testing.T) {
	t.Parallel()
	list := &ir.List{ID: "t/list"}
	chain := func(hops int) ir.TypeRegistry {
		reg := ir.TypeRegistry{"t/list": list}
		next := ir.TypeID("t/list")
		for i := range hops {
			id := ir.TypeID(fmt.Sprintf("t/alias%d", i))
			reg[id] = &ir.Scalar{ID: id, Base: &ir.TypeRef{Target: next}}
			next = id
		}
		reg["t/x"] = reg[next]
		return reg
	}
	var typedNil *ir.Model
	for name, tc := range map[string]struct {
		reg  ir.TypeRegistry
		want ir.TypeDef
	}{
		"no alias":         {chain(0), list},
		"within the bound": {chain(maxAliasHops), list},
		"past the bound":   {chain(maxAliasHops + 1), nil},
		"a loop": {ir.TypeRegistry{
			"t/x": &ir.Scalar{ID: "t/x", Base: &ir.TypeRef{Target: "t/y"}},
			"t/y": &ir.Scalar{ID: "t/y", Base: &ir.TypeRef{Target: "t/x"}},
		}, nil},
		"a typed nil":      {ir.TypeRegistry{"t/x": typedNil}, nil},
		"a type not there": {ir.TypeRegistry{}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, resolve(tc.reg, "t/x"))
		})
	}
}

// TestReader_ReadsEachBodyAndModelOnce pins the reader's memo: operations
// naming one body read its page once per walk they send, and a model several
// members hold, or several bodies, is read once.
func TestReader_ReadsEachBodyAndModelOnce(t *testing.T) {
	t.Parallel()
	info := &ir.Model{ID: "t/info", Properties: []ir.Property{
		{ID: "p/info/total_count", Name: ir.Naming{Source: "total_count", Canonical: "total_count"},
			Type: ir.TypeRef{Target: "t/prim/integer"}},
	}}
	body := func(id ir.TypeID) *ir.Model {
		return &ir.Model{ID: id, Properties: []ir.Property{
			{ID: ir.PropID(id + "/result"), Name: ir.Naming{Source: "result", Canonical: "result"},
				Type: ir.TypeRef{Target: "t/list"}},
			{ID: ir.PropID(id + "/a"), Name: ir.Naming{Source: "a", Canonical: "a"}, Type: ir.TypeRef{Target: "t/info"}},
			{ID: ir.PropID(id + "/b"), Name: ir.Naming{Source: "b", Canonical: "b"}, Type: ir.TypeRef{Target: "t/info"}},
		}}
	}
	r := newReader(ir.TypeRegistry{
		"t/list": &ir.List{ID: "t/list"}, "t/info": info, "t/one": body("t/one"), "t/two": body("t/two"),
		"t/prim/integer": &ir.Primitive{ID: "t/prim/integer", Prim: ir.PrimInteger},
	})
	for range 3 {
		for _, id := range []ir.TypeID{"t/one", "t/two"} {
			pg := r.page(pageKey{body: id})
			assert.Equal(t, []ir.PropID{ir.PropID(id + "/result")}, pg.items)
			assert.Nil(t, pg.totalCount, "a total two members hold counts neither")
		}
	}
	assert.Len(t, r.pages, 2, "a page per body")
	assert.Len(t, r.models, 3, "a reading per model: the two bodies and the one their members hold")
}
