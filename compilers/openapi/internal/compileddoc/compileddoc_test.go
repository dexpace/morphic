// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package compileddoc_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/compileddoc"
	"github.com/dexpace/morphic/compilers/openapi/internal/pagination"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/compilers/openapi/internal/runtimeexpr"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// wholeOf is a Document over a document whose one service holds operation in its one
// group, and the operation as it holds it, which is the one Params reads.
func wholeOf(operation ir.Operation) (*compileddoc.CompiledDocumentView, *ir.Operation) {
	doc := &ir.Document{Services: []ir.Service{{Groups: []ir.OperationGroup{{Operations: []ir.Operation{operation}}}}}}
	compiledDocumentView := compileddoc.New(doc)
	return compiledDocumentView, &doc.Services[0].Groups[0].Operations[0]
}

// property is a member of a model with the ID id, the wire name wire and the type
// at target.
func property(id ir.PropertyID, wire string, target ir.TypeID) ir.Property {
	return ir.Property{ID: id, WireName: wire, Type: ir.TypeRef{Target: target}}
}

// model is the model id declaring properties, shaped further by mutate.
func model(id ir.TypeID, mutate func(*ir.Model), properties ...ir.Property) *ir.Model {
	return irtest.ModelOf(id, func(model *ir.Model) {
		model.Properties = properties
		if mutate != nil {
			mutate(model)
		}
	})
}

// TestDocument_ListsEveryOperationOnce pins the operations a document holds:
// those of every service and group, each once, a service's before the next's
// and a group's before those of the groups beside it, as pointers into the
// document; one in a group nested to ir.MaxGroupDepth among them, and none in a
// group nested deeper. The order within a group's nested groups is left open.
func TestDocument_ListsEveryOperationOnce(t *testing.T) {
	t.Parallel()
	operationWithID := func(id string) ir.Operation { return ir.Operation{ID: ir.OperationID(id)} }
	var chain []ir.OperationGroup
	for level := ir.MaxGroupDepth + 1; level >= 1; level-- {
		chain = []ir.OperationGroup{{Operations: []ir.Operation{operationWithID(fmt.Sprintf("operation/%d", level))}, Groups: chain}}
	}
	doc := &ir.Document{Services: []ir.Service{
		{Groups: []ir.OperationGroup{
			{Operations: []ir.Operation{operationWithID("operation/a"), operationWithID("operation/b")}},
			{Operations: []ir.Operation{operationWithID("operation/c")}},
			{Groups: []ir.OperationGroup{
				{Operations: []ir.Operation{operationWithID("operation/d")}},
				{Groups: []ir.OperationGroup{{Operations: []ir.Operation{operationWithID("operation/e")}}}},
			}},
		}},
		{Groups: []ir.OperationGroup{{Operations: []ir.Operation{operationWithID("operation/f")}}, {Groups: chain}}},
	}}
	compiledDocumentView := compileddoc.New(doc)

	got := make([]ir.OperationID, 0, len(compiledDocumentView.Operations))
	seen := map[ir.OperationID]bool{}
	for _, operation := range compiledDocumentView.Operations {
		got = append(got, operation.ID)
		assert.False(t, seen[operation.ID], "%s is listed once", operation.ID)
		seen[operation.ID] = true
	}
	above := []ir.OperationID{"operation/a", "operation/b", "operation/c", "operation/d", "operation/e", "operation/f"}
	assert.Equal(t, []ir.OperationID{"operation/a", "operation/b", "operation/c"}, got[:3], "a group's operations, then those of the groups beside it")
	for _, id := range above[:5] {
		assert.Less(t, slices.Index(got, id), slices.Index(got, "operation/f"), "%s, of the first service, before the second's", id)
	}
	for _, id := range above {
		assert.True(t, seen[id], "%s is listed", id)
	}
	assert.Len(t, compiledDocumentView.Operations, len(above)+ir.MaxGroupDepth, "those above, and one per level of the chain to the cap")
	assert.True(t, seen[ir.OperationID(fmt.Sprintf("operation/%d", ir.MaxGroupDepth))], "the group at the cap is read")
	assert.False(t, seen[ir.OperationID(fmt.Sprintf("operation/%d", ir.MaxGroupDepth+1))], "the group past it is not")

	compiledDocumentView.Operations[0].Pagination = &ir.Pagination{}
	assert.NotNil(t, doc.Services[0].Groups[0].Operations[0].Pagination, "an operation is read in place")
}

// TestDocument_IndexesParametersOnce pins Params to one index of an operation's
// parameters, made when it is first asked for and kept: a parameter and its
// binding appended after that are in no answer.
func TestDocument_IndexesParametersOnce(t *testing.T) {
	t.Parallel()
	compiledDocumentView, operation := wholeOf(ir.Operation{
		ID:     "operation/x",
		Params: []ir.Parameter{{ID: "parameter/q", Name: ir.Naming{Source: "q"}}},
		Bindings: ir.OperationBindings{HTTP: []ir.HTTPBinding{{
			ParamBindings: []ir.HTTPParamBinding{{Param: "parameter/q", Location: ir.HTTPLocationQuery}},
		}}},
	})
	first := compiledDocumentView.Params(operation)
	require.Equal(t, []ir.ParameterID{"parameter/q"}, first.Bound(ir.HTTPLocationQuery, "q"))

	operation.Params = append(operation.Params, ir.Parameter{ID: "parameter/r", Name: ir.Naming{Source: "r"}})
	operation.Bindings.HTTP[0].ParamBindings = append(operation.Bindings.HTTP[0].ParamBindings,
		ir.HTTPParamBinding{Param: "parameter/r", Location: ir.HTTPLocationQuery})
	second := compiledDocumentView.Params(operation)
	assert.Equal(t, []ir.ParameterID{"parameter/q"}, second.Bound(ir.HTTPLocationQuery, "q"), "what it indexed first")
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
	taken map[string]ir.PropertyID
	// none is set where the root stands for no model: a union, which has no
	// member of its own, or the head of more alias scalars than Stands reads.
	none bool
}

// aliasChain is length alias scalars, named name and a number from 000, each
// standing for the next and the last for to.
func aliasChain(name string, length int, to ir.TypeID) []ir.TypeDef {
	at := func(i int) ir.TypeID { return ir.TypeID(fmt.Sprintf("%s%03d", name, i)) }
	defs := make([]ir.TypeDef, 0, length)
	for i := range length - 1 {
		defs = append(defs, irtest.ScalarOf(at(i), at(i+1)))
	}
	return append(defs, irtest.ScalarOf(at(length-1), to))
}

// bodyShapes are the roots a body names: through an alias, composed, redeclared,
// as a union and through alias chains to the cap and past it.
func bodyShapes() map[string]bodyShape {
	chained := func(length int) ir.TypeRegistry {
		return irtest.DocOf(append(aliasChain("body", length, "m"), model("m", nil, property("m.a", "a", "string")))...).Types
	}
	nullable := func(id ir.PropertyID, wire string, target ir.TypeID) ir.Property {
		property := property(id, wire, target)
		property.Type.Nullable = true
		return property
	}
	child := model("child", nil, property("child.v", "v", "string"))
	return map[string]bodyShape{
		"an alias of a model": {
			types: irtest.DocOf(irtest.ScalarOf("body", "alias"), irtest.ScalarOf("alias", "m"),
				model("m", nil, property("m.a", "a", "string"), property("m.b", "b", "string"))).Types,
			root: "body",
		},
		"an allOf redeclaring a member": {
			types: irtest.DocOf(
				model("m", func(model *ir.Model) { model.Base = &ir.TypeRef{Target: "base"} }, property("m.n", "n", "string")),
				model("base", nil, property("base.n", "n", "string"), property("base.k", "k", "string"))).Types,
			root:  "m",
			taken: map[string]ir.PropertyID{"n": "m.n"},
		},
		"members that are references": {
			types: irtest.DocOf(model("m", nil, property("m.c", "c", "child"), property("m.d", "d", "child")), child).Types,
			root:  "m",
		},
		"a nullable body with nullable members": {
			types: irtest.DocOf(irtest.ScalarOf("body", "m"),
				model("m", nil, nullable("m.c", "c", "child"), nullable("m.s", "s", "string")), child).Types,
			root: "body",
		},
		"one base composed twice": {
			types: irtest.DocOf(
				model("m", func(model *ir.Model) { model.Mixins = irtest.RefsTo("x", "y") }, property("m.own", "own", "string")),
				model("x", func(model *ir.Model) { model.Base = &ir.TypeRef{Target: "z"} }, property("x.n", "n", "string")),
				model("y", func(model *ir.Model) { model.Base = &ir.TypeRef{Target: "z"} }),
				model("z", nil, property("z.n", "n", "string"), property("z.k", "k", "string"))).Types,
			root:  "m",
			taken: map[string]ir.PropertyID{"n": "x.n"},
		},
		"a oneOf body": {
			types: irtest.DocOf(&ir.Union{ID: "u", Exclusive: true, Variants: []ir.Variant{{Type: ir.TypeRef{Target: "m"}}}},
				model("m", nil, property("m.n", "n", "string"))).Types,
			root: "u", none: true,
		},
		"an anyOf body": {
			types: irtest.DocOf(&ir.Union{ID: "u", Variants: []ir.Variant{{Type: ir.TypeRef{Target: "m"}}}},
				model("m", nil, property("m.n", "n", "string"))).Types,
			root: "u", none: true,
		},
		"an alias chain to the cap": {types: chained(compileddoc.MaxAliasHops), root: "body000"},
		"an alias chain past the cap": {
			types: chained(compileddoc.MaxAliasHops + 1), root: "body000", none: true,
		},
	}
}

// TestDocument_MemberAgreesWithMembers holds Member to the flattening Members
// reads, for each shape of body: every wire name of the flattening names the
// property Members lists, a name outside it names none, and a body standing
// for no model, a union or a chain of aliases past the cap, has no member
// whatever the types it names declare.
func TestDocument_MemberAgreesWithMembers(t *testing.T) {
	t.Parallel()
	for name, shape := range bodyShapes() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			compiledDocumentView := compileddoc.New(&ir.Document{Types: shape.types})
			members := compiledDocumentView.Members(shape.root)
			if shape.none {
				assert.Empty(t, members)
				for _, wire := range []string{"n", "a", "absent"} {
					_, ok := compiledDocumentView.Member(shape.root, wire)
					assert.False(t, ok, "a body standing for no model has no member %q", wire)
				}
				return
			}
			require.NotEmpty(t, members)
			for _, property := range members {
				got, ok := compiledDocumentView.Member(shape.root, property.EffectiveWireName())
				require.True(t, ok, property.EffectiveWireName())
				assert.Same(t, property, got, "the declaration of %q the flattening takes", property.EffectiveWireName())
			}
			for wire, id := range shape.taken {
				got, ok := compiledDocumentView.Member(shape.root, wire)
				require.True(t, ok, wire)
				assert.Equal(t, id, got.ID, "the declaration of %q the row names", wire)
			}
			_, ok := compiledDocumentView.Member(shape.root, "absent")
			assert.False(t, ok, "a name no model declares")
		})
	}
}

// TestDocument_WalkNamesEachMemberFromTheRoot pins a walk to the declaration of
// each token's member, through the type of the member before it, and to nothing
// where a step names no member or reaches no model, however an alias scalar
// stands between the steps.
func TestDocument_WalkNamesEachMemberFromTheRoot(t *testing.T) {
	t.Parallel()
	types := irtest.DocOf(
		model("type/page", nil, property("property/info", "info", "type/info"), property("property/items", "items", "type/text"),
			property("property/aliased", "aliased", "type/infoalias")),
		model("type/info", nil, property("property/next", "next", "type/text")),
		irtest.ScalarOf("type/infoalias", "type/info"),
		irtest.ScalarOf("type/pagealias", "type/page"),
		&ir.Scalar{ID: "type/text"},
	).Types
	for name, tc := range map[string]struct {
		root   ir.TypeID
		tokens []string
		want   []ir.PropertyID
		ok     bool
	}{
		"no token":                     {root: "type/page", want: []ir.PropertyID{}, ok: true},
		"one member":                   {root: "type/page", tokens: []string{"items"}, want: []ir.PropertyID{"property/items"}, ok: true},
		"a chain":                      {root: "type/page", tokens: []string{"info", "next"}, want: []ir.PropertyID{"property/info", "property/next"}, ok: true},
		"a root that is an alias":      {root: "type/pagealias", tokens: []string{"info", "next"}, want: []ir.PropertyID{"property/info", "property/next"}, ok: true},
		"a member typed at an alias":   {root: "type/page", tokens: []string{"aliased", "next"}, want: []ir.PropertyID{"property/aliased", "property/next"}, ok: true},
		"a member the model lacks":     {root: "type/page", tokens: []string{"gone"}},
		"a step through a scalar":      {root: "type/page", tokens: []string{"items", "next"}},
		"a root that is no model":      {root: "type/text", tokens: []string{"next"}},
		"a root the registry lacks":    {root: "type/gone", tokens: []string{"next"}},
		"a member of the wrong model":  {root: "type/info", tokens: []string{"items"}},
		"a member named by its own ID": {root: "type/page", tokens: []string{"property/items"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := compileddoc.New(&ir.Document{Types: types}).Walk(tc.root, tc.tokens)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestDocument_StandsReadsThroughAliasScalarsToTheCap pins Stands to the type
// an alias scalar stands for: the same type for one that is none, the model at
// the end of as many as compileddoc.MaxAliasHops alias scalars, and nothing
// past that, for a cycle of them, or for an alias of a type the registry lacks
// or holds as a typed nil.
func TestDocument_StandsReadsThroughAliasScalarsToTheCap(t *testing.T) {
	t.Parallel()
	defs := slices.Concat(
		[]ir.TypeDef{
			model("m", nil, property("m.a", "a", "string")),
			irtest.ScalarOf("loop1", "loop2"), irtest.ScalarOf("loop2", "loop1"),
			irtest.ScalarOf("dangling", "gone"), irtest.ScalarOf("toNil", "nil"),
		},
		aliasChain("ok", compileddoc.MaxAliasHops, "m"),
		aliasChain("long", compileddoc.MaxAliasHops+1, "m"),
	)
	compiledDocumentView := compileddoc.New(&ir.Document{Types: irtest.DocOf(defs...).Types})
	compiledDocumentView.Types["nil"] = (*ir.Model)(nil)
	// noType is no type at all, which a typed nil is not: assert.Nil accepts both.
	var noType ir.TypeDef

	assert.Equal(t, compiledDocumentView.Types["m"], compiledDocumentView.Stands("m"), "a model stands for itself")
	assert.Equal(t, compiledDocumentView.Types["m"], compiledDocumentView.Stands("ok000"), "the head of as many alias scalars as the cap allows")
	assert.Equal(t, noType, compiledDocumentView.Stands("long000"), "the head of one more")
	assert.Equal(t, noType, compiledDocumentView.Stands("loop1"), "a cycle of aliases")
	assert.Equal(t, noType, compiledDocumentView.Stands("dangling"), "an alias of no type")
	assert.Equal(t, noType, compiledDocumentView.Stands("toNil"), "an alias of a typed nil")
	assert.Equal(t, noType, compiledDocumentView.Stands("gone"), "a type the registry lacks")
}

// TestDocument_HeaderMatchesTheNameWithoutCase pins a header to its wire name as
// HTTP compares one, and to every header a response declares under it, and a
// response's headers to the first read of its key.
func TestDocument_HeaderMatchesTheNameWithoutCase(t *testing.T) {
	t.Parallel()
	headers := []ir.Header{
		{Property: property("h/rate", "X-Rate", "type/text")},
		{Property: property("h/next", "Link", "type/text")},
		{Property: property("h/again", "x-rate", "type/text")},
	}
	compiledDocumentView := compileddoc.New(&ir.Document{})
	at := compileddoc.ResponseKey{Operation: "operation/x"}
	assert.Equal(t, []ir.PropertyID{"h/rate", "h/again"}, compiledDocumentView.Header(at, headers, "X-RATE"))
	assert.Equal(t, []ir.PropertyID{"h/next"}, compiledDocumentView.Header(at, headers, "link"))
	assert.Empty(t, compiledDocumentView.Header(at, headers, "ETag"))

	other := []ir.Header{{Property: property("h/other", "X-Rate", "type/text")}}
	assert.Equal(t, []ir.PropertyID{"h/rate", "h/again"}, compiledDocumentView.Header(at, other, "x-rate"), "a key's first read is kept")
	for _, key := range []compileddoc.ResponseKey{
		{Operation: "operation/x", List: compileddoc.ListErrors}, {Operation: "operation/x", Index: 1}, {Operation: "operation/y"},
	} {
		assert.Equal(t, []ir.PropertyID{"h/other"}, compiledDocumentView.Header(key, other, "x-rate"), "%+v is a response of its own", key)
	}
}

// TestDocument_ReadsOnceAndAllocatesNothingAfter pins what makes resolving grow
// with the expressions and what they read rather than with the product of the
// two: a model, a parameter list and a response's headers are read by the first
// question of them, and a later question of the same builds nothing. It does
// not run in parallel, since the count is process-wide.
func TestDocument_ReadsOnceAndAllocatesNothingAfter(t *testing.T) {
	compiledDocumentView := compileddoc.New(&ir.Document{
		Types:    irtest.DocOf(model("type/page", nil, property("property/info", "info", "type/info"))).Types,
		Services: []ir.Service{{Groups: []ir.OperationGroup{{Operations: []ir.Operation{{ID: "operation/x"}}}}}},
	})
	operation := compiledDocumentView.Operations[0]
	headers := []ir.Header{{Property: property("h/rate", "X-Rate", "type/text")}}
	reads := map[string]func(){
		"a model's member":  func() { compiledDocumentView.Member("type/page", "info") },
		"a model's members": func() { compiledDocumentView.Members("type/page") },
		"a parameter index": func() { compiledDocumentView.Params(operation) },
		"a response headers": func() {
			compiledDocumentView.Header(compileddoc.ResponseKey{Operation: "operation/x"}, headers, "x-rate")
		},
	}
	for name, read := range reads {
		read()
		assert.Zero(t, testing.AllocsPerRun(10, read), "%s: a second read builds nothing", name)
	}
}

// TestDocument_ABodyReadFlattensNoModel pins what a body read costs: BodyPath over
// a payload of K contents each naming a model that composes the one model,
// takes one declaration of the first member from each, and flattens no model,
// at K of 4 and of 16. The control is a flattening Members does make.
func TestDocument_ABodyReadFlattensNoModel(t *testing.T) {
	t.Parallel()
	for _, k := range []int{4, 16} {
		t.Run(fmt.Sprint(k), func(t *testing.T) {
			t.Parallel()
			defs := make([]ir.TypeDef, 0, 1+k)
			defs = append(defs, model("a", nil, property("a.a", "a", "string")))
			payload := &ir.Payload{}
			for i := range k {
				id := ir.TypeID(fmt.Sprintf("b%02d", i))
				defs = append(defs, model(id, func(model *ir.Model) { model.Mixins = irtest.RefsTo("a") }))
				payload.Contents = append(payload.Contents,
					ir.Content{MediaType: fmt.Sprintf("application/x-%02d", i), Type: ir.TypeRef{Target: id}})
			}
			compiledDocumentView := compileddoc.New(&ir.Document{Types: irtest.DocOf(defs...).Types})
			flattenings := compileddoc.CountFlattenings(compiledDocumentView)

			got, why, ok := runtimeexpr.BodyPath(compiledDocumentView, payload, []string{"a"}, "request")
			require.True(t, ok, why)
			assert.Equal(t, []ir.PropertyID{"a.a"}, got)
			assert.Zero(t, *flattenings, "no model of the %d contents is flattened", k)

			compiledDocumentView.Members("b00")
			assert.Equal(t, 1, *flattenings, "the count sees a flattening that is made")
		})
	}
}

// TestDocument_APromotionNoMemberMarksFlattensNoModel pins what a
// pagination read through marks costs where no member of the document keeps
// the key its source reads: the promotion asks no model for its members, so
// the document flattens none.
func TestDocument_APromotionNoMemberMarksFlattensNoModel(t *testing.T) {
	t.Parallel()
	page := model("type/page", nil, property("property/items", "items", "type/list"), property("property/next", "next", "type/text"))
	operation := ir.Operation{
		ID: "operation/list",
		Responses: []ir.Response{{Payload: &ir.Payload{Contents: []ir.Content{
			{MediaType: "application/json", Type: ir.TypeRef{Target: "type/page"}},
		}}}},
	}
	compiledDocumentView := compileddoc.New(&ir.Document{
		Types:    irtest.DocOf(page, &ir.List{ID: "type/list", ElementType: ir.TypeRef{Target: "type/text"}}, &ir.Scalar{ID: "type/text"}).Types,
		Services: []ir.Service{{Groups: []ir.OperationGroup{{Operations: []ir.Operation{operation}}}}},
	})
	promoter := promotion.New(promotion.ExtensionPromotions{
		Targets: map[promotion.ExtensionTarget][]promotion.ExtensionSource{
			promotion.TargetPaginationItems: {{Key: "x-k", Member: "purpose", Values: []string{"items"}}},
		},
	})
	flattenings := compileddoc.CountFlattenings(compiledDocumentView)

	stated, diags := pagination.Promote(promoter, compiledDocumentView, nil)
	assert.Empty(t, stated, "no key is written")
	assert.Empty(t, diags)
	assert.Zero(t, *flattenings, "no member keeps the key, so no model is flattened")
}

// TestDocument_MembersFlattensAModelOnceWhateverNamesIt pins Members to one
// flattening of a model however many alias scalars name it: the model and two
// aliases of it give the same members, flattened once.
func TestDocument_MembersFlattensAModelOnceWhateverNamesIt(t *testing.T) {
	t.Parallel()
	compiledDocumentView := compileddoc.New(&ir.Document{Types: irtest.DocOf(
		model("m", nil, property("m.a", "a", "string")), irtest.ScalarOf("one", "m"), irtest.ScalarOf("two", "one"),
	).Types})
	flattenings := compileddoc.CountFlattenings(compiledDocumentView)
	want := compiledDocumentView.Members("m")
	require.Len(t, want, 1)
	for _, id := range []ir.TypeID{"one", "two"} {
		assert.Equal(t, want, compiledDocumentView.Members(id), "%s names the model", id)
	}
	assert.Equal(t, 1, *flattenings, "the model is flattened once")
}
