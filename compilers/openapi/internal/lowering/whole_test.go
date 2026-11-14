// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/runtimeexpr"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// wholeOf is a Whole over a document whose one service holds op in its one
// group, and the operation as Whole holds it, which is the one Params reads.
func wholeOf(op ir.Operation) (*lowering.Whole, *ir.Operation) {
	doc := &ir.Document{Services: []ir.Service{{Groups: []ir.OperationGroup{{Operations: []ir.Operation{op}}}}}}
	w := lowering.NewWhole(doc)
	return w, &doc.Services[0].Groups[0].Operations[0]
}

// prop is a member of a model with the ID id, the wire name wire and the type
// at target.
func prop(id ir.PropID, wire string, target ir.TypeID) ir.Property {
	return ir.Property{ID: id, WireName: wire, Type: ir.TypeRef{Target: target}}
}

// model is the model id declaring props, shaped further by mutate.
func model(id ir.TypeID, mutate func(*ir.Model), props ...ir.Property) *ir.Model {
	return irtest.ModelOf(id, func(m *ir.Model) {
		m.Properties = props
		if mutate != nil {
			mutate(m)
		}
	})
}

// TestWhole_ListsEveryOperationOnce pins the operations a document holds:
// those of every service and group, each once, a service's before the next's
// and a group's before those of the groups beside it, as pointers into the
// document; one in a group nested to ir.MaxGroupDepth among them, and none in a
// group nested deeper. The order within a group's nested groups is left open.
func TestWhole_ListsEveryOperationOnce(t *testing.T) {
	t.Parallel()
	op := func(id string) ir.Operation { return ir.Operation{ID: ir.OpID(id)} }
	var chain []ir.OperationGroup
	for level := ir.MaxGroupDepth + 1; level >= 1; level-- {
		chain = []ir.OperationGroup{{Operations: []ir.Operation{op(fmt.Sprintf("op/%d", level))}, Groups: chain}}
	}
	doc := &ir.Document{Services: []ir.Service{
		{Groups: []ir.OperationGroup{
			{Operations: []ir.Operation{op("op/a"), op("op/b")}},
			{Operations: []ir.Operation{op("op/c")}},
			{Groups: []ir.OperationGroup{
				{Operations: []ir.Operation{op("op/d")}},
				{Groups: []ir.OperationGroup{{Operations: []ir.Operation{op("op/e")}}}},
			}},
		}},
		{Groups: []ir.OperationGroup{{Operations: []ir.Operation{op("op/f")}}, {Groups: chain}}},
	}}
	w := lowering.NewWhole(doc)

	got := make([]ir.OpID, 0, len(w.Ops))
	seen := map[ir.OpID]bool{}
	for _, o := range w.Ops {
		got = append(got, o.ID)
		assert.False(t, seen[o.ID], "%s is listed once", o.ID)
		seen[o.ID] = true
	}
	above := []ir.OpID{"op/a", "op/b", "op/c", "op/d", "op/e", "op/f"}
	assert.Equal(t, []ir.OpID{"op/a", "op/b", "op/c"}, got[:3], "a group's operations, then those of the groups beside it")
	for _, id := range above[:5] {
		assert.Less(t, slices.Index(got, id), slices.Index(got, "op/f"), "%s, of the first service, before the second's", id)
	}
	for _, id := range above {
		assert.True(t, seen[id], "%s is listed", id)
	}
	assert.Len(t, w.Ops, len(above)+ir.MaxGroupDepth, "those above, and one per level of the chain to the cap")
	assert.True(t, seen[ir.OpID(fmt.Sprintf("op/%d", ir.MaxGroupDepth))], "the group at the cap is read")
	assert.False(t, seen[ir.OpID(fmt.Sprintf("op/%d", ir.MaxGroupDepth+1))], "the group past it is not")

	w.Ops[0].Pagination = &ir.Pagination{}
	assert.NotNil(t, doc.Services[0].Groups[0].Operations[0].Pagination, "an operation is read in place")
}

// TestWhole_IndexesParametersOnce pins Params to one index of an operation's
// parameters, made when it is first asked for and kept: a parameter and its
// binding appended after that are in no answer.
func TestWhole_IndexesParametersOnce(t *testing.T) {
	t.Parallel()
	w, op := wholeOf(ir.Operation{
		ID:     "op/x",
		Params: []ir.Parameter{{ID: "param/q", Name: ir.Naming{Source: "q"}}},
		Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
			ParamBindings: []ir.HTTPParamBinding{{Param: "param/q", Location: ir.HTTPLocationQuery}},
		}}},
	})
	first := w.Params(op)
	require.Equal(t, []ir.ParamID{"param/q"}, first.Bound(ir.HTTPLocationQuery, "q"))

	op.Params = append(op.Params, ir.Parameter{ID: "param/r", Name: ir.Naming{Source: "r"}})
	op.Bindings.HTTP[0].ParamBindings = append(op.Bindings.HTTP[0].ParamBindings,
		ir.HTTPParamBinding{Param: "param/r", Location: ir.HTTPLocationQuery})
	second := w.Params(op)
	assert.Equal(t, []ir.ParamID{"param/q"}, second.Bound(ir.HTTPLocationQuery, "q"), "what it indexed first")
	assert.Empty(t, second.Bound(ir.HTTPLocationQuery, "r"), "and not what was added after")
	assert.Empty(t, first.Bound(ir.HTTPLocationQuery, "r"))
}

// bodyShape is one registry whose root a request or response body may name, and
// what its members are.
type bodyShape struct {
	types ir.TypeRegistry
	root  ir.TypeID
	// taken is the declaration the root takes for a wire name where more than
	// one model declares it.
	taken map[string]ir.PropID
	// union is set where the root is a union, which has no member of its own.
	union bool
}

// bodyShapes are the roots a body names: through an alias, composed, redeclared
// and as a union.
func bodyShapes() map[string]bodyShape {
	nullable := func(id ir.PropID, wire string, target ir.TypeID) ir.Property {
		p := prop(id, wire, target)
		p.Type.Nullable = true
		return p
	}
	child := model("child", nil, prop("child.v", "v", "string"))
	return map[string]bodyShape{
		"an alias of a model": {
			types: irtest.DocOf(irtest.ScalarOf("body", "alias"), irtest.ScalarOf("alias", "m"),
				model("m", nil, prop("m.a", "a", "string"), prop("m.b", "b", "string"))).Types,
			root: "body",
		},
		"an allOf redeclaring a member": {
			types: irtest.DocOf(
				model("m", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "base"} }, prop("m.n", "n", "string")),
				model("base", nil, prop("base.n", "n", "string"), prop("base.k", "k", "string"))).Types,
			root:  "m",
			taken: map[string]ir.PropID{"n": "m.n"},
		},
		"members that are references": {
			types: irtest.DocOf(model("m", nil, prop("m.c", "c", "child"), prop("m.d", "d", "child")), child).Types,
			root:  "m",
		},
		"a nullable body with nullable members": {
			types: irtest.DocOf(irtest.ScalarOf("body", "m"),
				model("m", nil, nullable("m.c", "c", "child"), nullable("m.s", "s", "string")), child).Types,
			root: "body",
		},
		"one base composed twice": {
			types: irtest.DocOf(
				model("m", func(m *ir.Model) { m.Mixins = irtest.RefsTo("x", "y") }, prop("m.own", "own", "string")),
				model("x", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "z"} }, prop("x.n", "n", "string")),
				model("y", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "z"} }),
				model("z", nil, prop("z.n", "n", "string"), prop("z.k", "k", "string"))).Types,
			root:  "m",
			taken: map[string]ir.PropID{"n": "x.n"},
		},
		"a oneOf body": {
			types: irtest.DocOf(&ir.Union{ID: "u", Exclusive: true, Variants: []ir.Variant{{Type: ir.TypeRef{Target: "m"}}}},
				model("m", nil, prop("m.n", "n", "string"))).Types,
			root: "u", union: true,
		},
		"an anyOf body": {
			types: irtest.DocOf(&ir.Union{ID: "u", Variants: []ir.Variant{{Type: ir.TypeRef{Target: "m"}}}},
				model("m", nil, prop("m.n", "n", "string"))).Types,
			root: "u", union: true,
		},
	}
}

// TestWhole_MemberAgreesWithMembers holds Member to the flattening Members
// reads, for each shape of body: every wire name of the flattening names the
// property Members lists, a name outside it names none, and a union body has no
// member whatever its variants declare.
func TestWhole_MemberAgreesWithMembers(t *testing.T) {
	t.Parallel()
	for name, shape := range bodyShapes() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w := lowering.NewWhole(&ir.Document{Types: shape.types})
			members := w.Members(shape.root)
			if shape.union {
				assert.Empty(t, members)
				for _, wire := range []string{"n", "a", "absent"} {
					_, ok := w.Member(shape.root, wire)
					assert.False(t, ok, "a union has no member %q", wire)
				}
				return
			}
			require.NotEmpty(t, members)
			for _, m := range members {
				got, ok := w.Member(shape.root, m.EffectiveWireName())
				require.True(t, ok, m.EffectiveWireName())
				assert.Same(t, m, got, "the declaration of %q the flattening takes", m.EffectiveWireName())
			}
			for wire, id := range shape.taken {
				got, ok := w.Member(shape.root, wire)
				require.True(t, ok, wire)
				assert.Equal(t, id, got.ID, "the declaration of %q the row names", wire)
			}
			_, ok := w.Member(shape.root, "absent")
			assert.False(t, ok, "a name no model declares")
		})
	}
}

// TestWhole_WalkNamesEachMemberFromTheRoot pins a walk to the declaration of
// each token's member, through the type of the member before it, and to nothing
// where a step names no member or reaches no model, however an alias scalar
// stands between the steps.
func TestWhole_WalkNamesEachMemberFromTheRoot(t *testing.T) {
	t.Parallel()
	types := irtest.DocOf(
		model("t/page", nil, prop("p/info", "info", "t/info"), prop("p/items", "items", "t/text"),
			prop("p/aliased", "aliased", "t/infoalias")),
		model("t/info", nil, prop("p/next", "next", "t/text")),
		irtest.ScalarOf("t/infoalias", "t/info"),
		irtest.ScalarOf("t/pagealias", "t/page"),
		&ir.Scalar{ID: "t/text"},
	).Types
	for name, tc := range map[string]struct {
		root   ir.TypeID
		tokens []string
		want   []ir.PropID
		ok     bool
	}{
		"no token":                     {root: "t/page", want: []ir.PropID{}, ok: true},
		"one member":                   {root: "t/page", tokens: []string{"items"}, want: []ir.PropID{"p/items"}, ok: true},
		"a chain":                      {root: "t/page", tokens: []string{"info", "next"}, want: []ir.PropID{"p/info", "p/next"}, ok: true},
		"a root that is an alias":      {root: "t/pagealias", tokens: []string{"info", "next"}, want: []ir.PropID{"p/info", "p/next"}, ok: true},
		"a member typed at an alias":   {root: "t/page", tokens: []string{"aliased", "next"}, want: []ir.PropID{"p/aliased", "p/next"}, ok: true},
		"a member the model lacks":     {root: "t/page", tokens: []string{"gone"}},
		"a step through a scalar":      {root: "t/page", tokens: []string{"items", "next"}},
		"a root that is no model":      {root: "t/text", tokens: []string{"next"}},
		"a root the registry lacks":    {root: "t/gone", tokens: []string{"next"}},
		"a member of the wrong model":  {root: "t/info", tokens: []string{"items"}},
		"a member named by its own ID": {root: "t/page", tokens: []string{"p/items"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := lowering.NewWhole(&ir.Document{Types: types}).Walk(tc.root, tc.tokens)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestWhole_StandsReadsThroughAliasScalarsToTheCap pins Stands to the type an
// alias scalar stands for: the same type for one that is none, the model at the
// end of as many as lowering.MaxAliasHops alias scalars, and nothing past that,
// for a cycle of them, or for an alias of a type the registry lacks or holds as
// a typed nil.
func TestWhole_StandsReadsThroughAliasScalarsToTheCap(t *testing.T) {
	t.Parallel()
	chain := func(name string, length int) []ir.TypeDef {
		at := func(i int) ir.TypeID { return ir.TypeID(fmt.Sprintf("%s%03d", name, i)) }
		defs := make([]ir.TypeDef, 0, length)
		for i := range length - 1 {
			defs = append(defs, irtest.ScalarOf(at(i), at(i+1)))
		}
		return append(defs, irtest.ScalarOf(at(length-1), "m"))
	}
	defs := slices.Concat(
		[]ir.TypeDef{
			model("m", nil, prop("m.a", "a", "string")),
			irtest.ScalarOf("loop1", "loop2"), irtest.ScalarOf("loop2", "loop1"),
			irtest.ScalarOf("dangling", "gone"), irtest.ScalarOf("toNil", "nil"),
		},
		chain("ok", lowering.MaxAliasHops),
		chain("long", lowering.MaxAliasHops+1),
	)
	w := lowering.NewWhole(&ir.Document{Types: irtest.DocOf(defs...).Types})
	w.Types["nil"] = (*ir.Model)(nil)

	assert.Equal(t, w.Types["m"], w.Stands("m"), "a model stands for itself")
	assert.Equal(t, w.Types["m"], w.Stands("ok000"), "the head of as many alias scalars as the cap allows")
	assert.Nil(t, w.Stands("long000"), "the head of one more")
	assert.Nil(t, w.Stands("loop1"), "a cycle of aliases")
	assert.Nil(t, w.Stands("dangling"), "an alias of no type")
	assert.Nil(t, w.Stands("toNil"), "an alias of a typed nil")
	assert.Nil(t, w.Stands("gone"), "a type the registry lacks")
}

// TestWhole_HeaderMatchesTheNameWithoutCase pins a header to its wire name as
// HTTP compares one, and to every header a response declares under it, and a
// response's headers to the first read of its key.
func TestWhole_HeaderMatchesTheNameWithoutCase(t *testing.T) {
	t.Parallel()
	headers := []ir.Header{
		{Property: prop("h/rate", "X-Rate", "t/text")},
		{Property: prop("h/next", "Link", "t/text")},
		{Property: prop("h/again", "x-rate", "t/text")},
	}
	w := lowering.NewWhole(&ir.Document{})
	at := lowering.ResponseKey{Op: "op/x"}
	assert.Equal(t, []ir.PropID{"h/rate", "h/again"}, w.Header(at, headers, "X-RATE"))
	assert.Equal(t, []ir.PropID{"h/next"}, w.Header(at, headers, "link"))
	assert.Empty(t, w.Header(at, headers, "ETag"))

	other := []ir.Header{{Property: prop("h/other", "X-Rate", "t/text")}}
	assert.Equal(t, []ir.PropID{"h/rate", "h/again"}, w.Header(at, other, "x-rate"), "a key's first read is kept")
	for _, key := range []lowering.ResponseKey{
		{Op: "op/x", List: lowering.ListErrors}, {Op: "op/x", Index: 1}, {Op: "op/y"},
	} {
		assert.Equal(t, []ir.PropID{"h/other"}, w.Header(key, other, "x-rate"), "%+v is a response of its own", key)
	}
}

// TestWhole_ReadsOnceAndAllocatesNothingAfter pins what makes resolving grow
// with the expressions and what they read rather than with the product of the
// two: a model, a parameter list and a response's headers are read by the first
// question of them, and a later question of the same builds nothing. It does
// not run in parallel, since the count is process-wide.
func TestWhole_ReadsOnceAndAllocatesNothingAfter(t *testing.T) {
	w := lowering.NewWhole(&ir.Document{
		Types:    irtest.DocOf(model("t/page", nil, prop("p/info", "info", "t/info"))).Types,
		Services: []ir.Service{{Groups: []ir.OperationGroup{{Operations: []ir.Operation{{ID: "op/x"}}}}}},
	})
	op := w.Ops[0]
	headers := []ir.Header{{Property: prop("h/rate", "X-Rate", "t/text")}}
	reads := map[string]func(){
		"a model's member":   func() { w.Member("t/page", "info") },
		"a model's members":  func() { w.Members("t/page") },
		"a parameter index":  func() { w.Params(op) },
		"a response headers": func() { w.Header(lowering.ResponseKey{Op: "op/x"}, headers, "x-rate") },
	}
	for name, read := range reads {
		read()
		assert.Zero(t, testing.AllocsPerRun(10, read), "%s: a second read builds nothing", name)
	}
}

// TestWhole_ABodyReadFlattensNoModel pins what a body read costs: BodyPath over
// a payload of K contents each naming a model that composes the one model,
// takes one declaration of the first member from each, and flattens no model,
// at K of 4 and of 16. The control is a flattening Members does make.
func TestWhole_ABodyReadFlattensNoModel(t *testing.T) {
	t.Parallel()
	for _, k := range []int{4, 16} {
		t.Run(fmt.Sprint(k), func(t *testing.T) {
			t.Parallel()
			defs := make([]ir.TypeDef, 0, 1+k)
			defs = append(defs, model("a", nil, prop("a.a", "a", "string")))
			payload := &ir.Payload{}
			for i := range k {
				id := ir.TypeID(fmt.Sprintf("b%02d", i))
				defs = append(defs, model(id, func(m *ir.Model) { m.Mixins = irtest.RefsTo("a") }))
				payload.Contents = append(payload.Contents,
					ir.Content{MediaType: fmt.Sprintf("application/x-%02d", i), Type: ir.TypeRef{Target: id}})
			}
			w := lowering.NewWhole(&ir.Document{Types: irtest.DocOf(defs...).Types})
			flattenings := lowering.CountFlattenings(w)

			got, why, ok := runtimeexpr.BodyPath(w, payload, []string{"a"}, "request")
			require.True(t, ok, why)
			assert.Equal(t, []ir.PropID{"a.a"}, got)
			assert.Zero(t, *flattenings, "no model of the %d contents is flattened", k)

			w.Members("b00")
			assert.Equal(t, 1, *flattenings, "the count sees a flattening that is made")
		})
	}
}
