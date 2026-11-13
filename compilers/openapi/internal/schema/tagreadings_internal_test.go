// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"fmt"
	"maps"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering/loweringtest"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// newReadings returns a run's tagReadings over a context of its own, so no
// test shares what another read.
func newReadings(t *testing.T) *tagReadings {
	t.Helper()
	return readingsOf(loweringtest.Context(t, openapitest.EmptySpec, lowering.Policy{}))
}

// lit is a string Literal at id.
func lit(id ir.TypeID, s string) *ir.Literal {
	return &ir.Literal{ID: id, Value: ir.Value{Kind: ir.ValueString, Str: s}}
}

// unionOf is a closed union at id of the variants targets, exclusive as said.
func unionOf(id ir.TypeID, exclusive bool, targets ...ir.TypeRef) *ir.Union {
	variants := make([]ir.Variant, len(targets))
	for i, t := range targets {
		variants[i] = ir.Variant{Type: t}
	}
	return &ir.Union{ID: id, Variants: variants, Exclusive: exclusive, Closed: true}
}

// to is a non-nullable reference to id.
func to(id ir.TypeID) ir.TypeRef { return ir.TypeRef{Target: id} }

// valueOf returns what a value t names admits, as r reads a member declared
// by t alone.
func valueOf(r *tagReadings, reg ir.TypeRegistry, t ir.TypeRef) tagSet {
	return r.member(reg, ir.FlatProperty{Property: &ir.Property{Type: t}})
}

// TestTagReadings_ValueReadsAClosedSetOfStrings pins what a type must be to
// bound a tag to strings: a string Literal, a closed Enum marking no flags every
// member of which is a string, its members read sorted and once, or a closed
// union every variant of which bounds them, an exclusive one admitting what
// exactly one variant does, read through alias scalars. Any other node, a nil
// one included, admits any string, and null where a reference along the way
// is nullable.
func TestTagReadings_ValueReadsAClosedSetOfStrings(t *testing.T) {
	t.Parallel()
	str := func(s string) ir.EnumMember { return ir.EnumMember{Value: ir.Value{Kind: ir.ValueString, Str: s}} }
	num := ir.EnumMember{Value: ir.Value{Kind: ir.ValueNumber, Num: "1"}}
	nullable := func(id ir.TypeID) ir.TypeRef { return ir.TypeRef{Target: id, Nullable: true} }
	base := ir.TypeRegistry{"t/a": lit("t/a", "a"), "t/b": lit("t/b", "b"), "t/c": lit("t/c", "c"),
		"t/str": &ir.Primitive{ID: "t/str", Prim: ir.PrimString}}
	for name, tc := range map[string]struct {
		node ir.TypeDef
		want []string // nil where none is bounded
		null bool
	}{
		"literal":         {lit("t/x", "a"), []string{"a"}, false},
		"enum of one":     {&ir.Enum{Members: []ir.EnumMember{str("a")}, Closed: true}, []string{"a"}, false},
		"enum":            {&ir.Enum{Members: []ir.EnumMember{str("b"), str("a"), str("b")}, Closed: true}, []string{"a", "b"}, false},
		"nil literal":     {(*ir.Literal)(nil), nil, false},
		"nil enum":        {(*ir.Enum)(nil), nil, false},
		"nil union":       {(*ir.Union)(nil), nil, false},
		"open enum":       {&ir.Enum{Members: []ir.EnumMember{str("a")}}, nil, false},
		"flags":           {&ir.Enum{Members: []ir.EnumMember{str("a")}, Closed: true, Flags: true}, nil, false},
		"a number member": {&ir.Enum{Members: []ir.EnumMember{str("a"), num}, Closed: true}, nil, false},
		"number":          {&ir.Literal{Value: num.Value}, nil, false},
		"model":           {&ir.Model{}, nil, false},
		"through alias":   {&ir.Scalar{Base: &ir.TypeRef{Target: "t/a"}}, []string{"a"}, false},
		"a nullable alias": {&ir.Scalar{Base: &ir.TypeRef{Target: "t/a", Nullable: true}},
			[]string{"a"}, true},
		"anyOf":             {unionOf("t/x", false, to("t/b"), to("t/a"), to("t/b")), []string{"a", "b"}, false},
		"oneOf, one shared": {unionOf("t/x", true, to("t/a"), to("t/b"), to("t/a")), []string{"b"}, false},
		"a nullable variant": {unionOf("t/x", false, to("t/a"), nullable("t/b")),
			[]string{"a", "b"}, true},
		"oneOf, two nullable variants": {unionOf("t/x", true, nullable("t/a"), nullable("t/b")),
			[]string{"a", "b"}, false},
		"a union in turn": {unionOf("t/x", false, to("t/a"), to("t/inner")), []string{"a", "b", "c"}, false},
		"a union through an alias": {unionOf("t/x", false, to("t/a"), to("t/aliased")),
			[]string{"a", "b", "c"}, false},
		"a variant any string": {unionOf("t/x", false, to("t/a"), to("t/str")), nil, false},
		"open union":           {&ir.Union{Variants: []ir.Variant{{Type: to("t/a")}}}, nil, true},
		"empty union":          {unionOf("t/x", false), []string{}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reg := ir.TypeRegistry{"t/x": tc.node,
				"t/inner":   unionOf("t/inner", true, to("t/b"), to("t/c")),
				"t/aliased": &ir.Scalar{ID: "t/aliased", Base: &ir.TypeRef{Target: "t/inner"}}}
			maps.Copy(reg, base)
			got := valueOf(newReadings(t), reg, to("t/x"))
			assert.Equal(t, tc.want != nil, got.closed)
			if tc.want != nil {
				assert.Equal(t, tc.want, got.strings)
			}
			assert.Equal(t, tc.null, got.null)
			assert.Equal(t, len(tc.want) > 0 && !tc.null, got.pins())
		})
	}
}

// TestTagReadings_AUnionOnACycleAdmitsAnything pins that a union reaching a
// cycle of unions, or on one, admits any value, null among them, whichever
// union the reading meets first, so no answer turns on the order settlements
// ask in; a union beside the cycle is read as if it were not there.
func TestTagReadings_AUnionOnACycleAdmitsAnything(t *testing.T) {
	t.Parallel()
	reg := ir.TypeRegistry{
		"t/a": lit("t/a", "a"), "t/b": lit("t/b", "b"),
		// p and q name each other; r reaches them; s is beside them.
		"t/p": unionOf("t/p", true, to("t/q"), ir.TypeRef{Target: "t/a", Nullable: true}),
		"t/q": unionOf("t/q", true, to("t/p"), to("t/b")),
		"t/r": unionOf("t/r", false, to("t/a"), to("t/p")),
		"t/s": unionOf("t/s", false, to("t/a"), to("t/b")),
		// l names itself.
		"t/l": unionOf("t/l", false, to("t/a"), to("t/l")),
	}
	ids := []ir.TypeID{"t/p", "t/q", "t/r", "t/s", "t/l"}
	read := func(order []ir.TypeID) map[ir.TypeID]tagSet {
		r := newReadings(t)
		out := map[ir.TypeID]tagSet{}
		for _, id := range order {
			out[id] = valueOf(r, reg, to(id))
		}
		return out
	}
	want := read(ids)
	for _, id := range []ir.TypeID{"t/p", "t/q", "t/r", "t/l"} {
		assert.Equal(t, anything(), want[id], id)
	}
	assert.Equal(t, tagSet{strings: []string{"a", "b"}, closed: true}, want["t/s"])
	for _, order := range [][]ir.TypeID{
		{"t/q", "t/p", "t/r", "t/s", "t/l"}, {"t/r", "t/s", "t/q", "t/l", "t/p"}, {"t/s", "t/l", "t/p", "t/r", "t/q"},
	} {
		assert.Equal(t, want, read(order), "read in the order %v", order)
	}
}

// TestTagReadings_ACycleOfCompositionReadsAlikeFromAnyEntry pins what a
// cycle of unions and the objects composing them reads, the shape of a union
// listing subtypes that compose it: each subtype its own pin, the union what
// any of them admits, and so a holder composing it, every answer the same
// whichever reading the walk enters first.
func TestTagReadings_ACycleOfCompositionReadsAlikeFromAnyEntry(t *testing.T) {
	t.Parallel()
	subtype := func(id, tag ir.TypeID) *ir.Model {
		return &ir.Model{ID: id, Mixins: []ir.TypeRef{to("t/shape")},
			Properties: []ir.Property{{ID: ir.PropID("p/" + id), Name: ir.Naming{Source: "kind"}, Type: to(tag)}}}
	}
	reg := ir.TypeRegistry{
		"t/c": lit("t/c", "circle"), "t/s": lit("t/s", "square"),
		"t/shape":  unionOf("t/shape", true, to("t/circle"), to("t/square")),
		"t/circle": subtype("t/circle", "t/c"), "t/square": subtype("t/square", "t/s"),
		"t/holder": &ir.Model{ID: "t/holder", Mixins: []ir.TypeRef{to("t/shape")}},
	}
	read := func(order []ir.TypeID) map[string]tagSet {
		r := newReadings(t)
		out := map[string]tagSet{}
		for _, id := range order {
			flat := ir.FlattenedProperties(reg, id)
			p, declared := flatMember(flat, "kind")
			out[string(id)] = r.object(reg, flat, p, declared, "kind")
			out["fold "+string(id)] = r.fold(reg, readKey{id: id, name: "kind", object: true})
		}
		return out
	}
	want := read([]ir.TypeID{"t/circle", "t/square", "t/shape", "t/holder"})
	assert.Equal(t, tagSet{strings: []string{"circle"}, closed: true}, want["t/circle"])
	assert.Equal(t, tagSet{strings: []string{"square"}, closed: true}, want["t/square"])
	assert.Equal(t, tagSet{strings: []string{"circle", "square"}, closed: true}, want["t/holder"],
		"a holder composing the union admits what any of its variants does")
	for _, order := range [][]ir.TypeID{
		{"t/shape", "t/circle", "t/holder", "t/square"}, {"t/holder", "t/square", "t/circle", "t/shape"},
	} {
		assert.Equal(t, want, read(order), "read in the order %v", order)
	}
}

// TestTagReadings_SweepsACycleAtMostTheBound pins the sweeps a cycle of
// unions and the objects composing them is read in: a pin carried back a step
// a sweep reaches the union it is read from on the last sweep allowed, and
// the model it is read from on the sweep after, past the bound, so any value.
func TestTagReadings_SweepsACycleAtMostTheBound(t *testing.T) {
	t.Parallel()
	// chain is a cycle of n unions of one model each, every model composing the
	// next union and the last the first, pinning kind to a: the pin reaches the
	// first union on sweep 2n and its model on sweep 2n-1.
	chain := func(n int) ir.TypeRegistry {
		reg := ir.TypeRegistry{"t/a": lit("t/a", "a")}
		for i := range n {
			u, m, next := fmt.Sprintf("t/u%d", i), fmt.Sprintf("t/m%d", i), fmt.Sprintf("t/u%d", (i+1)%n)
			reg[ir.TypeID(u)] = unionOf(ir.TypeID(u), true, to(ir.TypeID(m)))
			model := &ir.Model{ID: ir.TypeID(m), Mixins: []ir.TypeRef{to(ir.TypeID(next))}}
			if i == n-1 {
				model.Properties = []ir.Property{{ID: "p/m", Name: ir.Naming{Source: "kind"}, Type: to("t/a")}}
			}
			reg[ir.TypeID(m)] = model
		}
		return reg
	}
	for _, c := range []struct {
		n      int
		root   ir.TypeID
		closed bool
	}{
		{maxComponentSweeps / 2, "t/u0", true},
		{maxComponentSweeps/2 + 1, "t/m0", false},
	} {
		got := newReadings(t).fold(chain(c.n), readKey{id: c.root, name: "kind", object: true})
		assert.Equal(t, c.closed, got.closed, "%s on a cycle of %d unions", c.root, c.n)
	}
}

// TestTagReadings_MemberMeetsEveryDeclaration pins that a member's tag is read
// off every declaration it conjoins, as an instance meets them all: a set the
// taken declaration overrides stands, two sets meet, null admitted by one
// declaration is excluded by another, and a member every declaration lets hold
// null pins nothing.
func TestTagReadings_MemberMeetsEveryDeclaration(t *testing.T) {
	t.Parallel()
	reg := ir.TypeRegistry{
		"t/a": lit("t/a", "a"), "t/b": lit("t/b", "b"),
		"t/ab": &ir.Enum{ID: "t/ab", Closed: true, Members: []ir.EnumMember{
			{Value: ir.Value{Kind: ir.ValueString, Str: "a"}}, {Value: ir.Value{Kind: ir.ValueString, Str: "b"}},
		}},
	}
	decl := func(target ir.TypeID, nullable bool) ir.PropertyDecl {
		return ir.PropertyDecl{Owner: "t/m", Property: &ir.Property{Name: ir.Naming{Source: "kind"},
			Type: ir.TypeRef{Target: target, Nullable: nullable}}}
	}
	ref := func(target ir.TypeID) ir.PropertyDecl { return decl(target, false /* nullable */) }
	refOrNull := func(target ir.TypeID) ir.PropertyDecl { return decl(target, true /* nullable */) }
	member := func(taken ir.PropertyDecl, overridden ...ir.PropertyDecl) ir.FlatProperty {
		return ir.FlatProperty{PropertyDecl: taken, Overridden: overridden}
	}
	const str = "t/prim/string"
	for name, tc := range map[string]struct {
		p    ir.FlatProperty
		want []string // nil for no pin
	}{
		"taken":                         {member(ref("t/a")), []string{"a"}},
		"overridden":                    {member(ref(str), ref("t/a")), []string{"a"}},
		"agreeing":                      {member(ref("t/a"), ref(str), refOrNull("t/a")), []string{"a"}},
		"a set met":                     {member(ref("t/ab"), ref("t/b")), []string{"b"}},
		"a set and a string":            {member(ref("t/ab"), ref(str)), []string{"a", "b"}},
		"disagreeing":                   {member(ref("t/a"), ref("t/b")), nil},
		"disagreeing, each admits null": {member(refOrNull("t/a"), refOrNull("t/b")), nil},
		"a null another excludes":       {member(refOrNull("t/a"), ref(str)), []string{"a"}},
		"a null every one admits":       {member(refOrNull("t/a"), refOrNull(str)), nil},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := newReadings(t).member(reg, tc.p)
			assert.Equal(t, tc.want != nil, got.pins())
			if tc.want != nil {
				assert.Equal(t, tc.want, got.strings)
			}
		})
	}
}

// TestTagReadings_ObjectMeetsWhatAComposedUnionAdmits pins what an object
// composing a union admits at a property: what its own member admits, met with
// what any variant's member does, as the instance is one of them. A variant
// declaring none, one admitting any string, and a union not closed bound
// nothing, and an object declaring no such member admits what the union does.
func TestTagReadings_ObjectMeetsWhatAComposedUnionAdmits(t *testing.T) {
	t.Parallel()
	pinned := func(id, tag ir.TypeID) *ir.Model {
		return &ir.Model{ID: id, Properties: []ir.Property{{ID: ir.PropID("p/" + id), Name: ir.Naming{Source: "kind"},
			Type: to(tag)}}}
	}
	reg := ir.TypeRegistry{
		"t/a": lit("t/a", "a"), "t/b": lit("t/b", "b"), "t/c": lit("t/c", "c"), "t/str": &ir.Primitive{ID: "t/str", Prim: ir.PrimString},
		"t/va": pinned("t/va", "t/a"), "t/vb": pinned("t/vb", "t/b"), "t/vc": pinned("t/vc", "t/c"), "t/vs": pinned("t/vs", "t/str"),
		"t/none": &ir.Model{ID: "t/none"},
	}
	for name, tc := range map[string]struct {
		own     ir.TypeID // the object's own declaration's type, "" for none
		union   *ir.Union
		want    []string // nil for any
		through bool     // the object composes an alias over the union
	}{
		"its variants pin":            {"", unionOf("t/u", true, to("t/va"), to("t/vb")), []string{"a", "b"}, false},
		"met with its own":            {"t/a", unionOf("t/u", true, to("t/va"), to("t/vb")), []string{"a"}, false},
		"through an alias":            {"", unionOf("t/u", true, to("t/va"), to("t/vc")), []string{"a", "c"}, true},
		"a variant declares none":     {"", unionOf("t/u", true, to("t/va"), to("t/none")), nil, false},
		"a variant any string":        {"", unionOf("t/u", true, to("t/va"), to("t/vs")), nil, false},
		"a variant no model":          {"", unionOf("t/u", true, to("t/va"), to("t/b")), nil, false},
		"open":                        {"", &ir.Union{ID: "t/u", Variants: []ir.Variant{{Type: to("t/va")}}}, nil, false},
		"own, the union open":         {"t/a", &ir.Union{ID: "t/u", Variants: []ir.Variant{{Type: to("t/vb")}}}, []string{"a"}, false},
		"a variant composing a union": {"", unionOf("t/u", true, to("t/vb"), to("t/composing")), []string{"a", "b", "c"}, false},
		"a variant a union in turn":   {"", unionOf("t/u", true, to("t/va"), to("t/inner")), []string{"a", "b", "c"}, false},
		"a variant on a cycle":        {"", unionOf("t/u", true, to("t/va"), to("t/looping")), nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := ir.TypeRegistry{"t/u": tc.union, "t/au": &ir.Scalar{ID: "t/au", Base: &ir.TypeRef{Target: "t/u"}},
				"t/inner":     unionOf("t/inner", false, to("t/vb"), to("t/vc")),
				"t/innerac":   unionOf("t/innerac", true, to("t/va"), to("t/vc")),
				"t/composing": &ir.Model{ID: "t/composing", Mixins: []ir.TypeRef{to("t/innerac")}},
				"t/looping":   &ir.Model{ID: "t/looping", Mixins: []ir.TypeRef{to("t/u")}}}
			maps.Copy(r, reg)
			composed := ir.TypeID("t/u")
			if tc.through {
				composed = "t/au"
			}
			m := &ir.Model{ID: "t/m", Mixins: []ir.TypeRef{to(composed)}}
			if tc.own != "" {
				m.Properties = []ir.Property{{ID: "p/m", Name: ir.Naming{Source: "kind"}, Type: to(tc.own)}}
			}
			r["t/m"] = m
			flat := ir.FlattenedProperties(r, "t/m")
			p, declared := flatMember(flat, "kind")
			got := newReadings(t).object(r, flat, p, declared, "kind")
			assert.Equal(t, tc.want != nil, got.closed)
			if tc.want != nil {
				assert.Equal(t, tc.want, got.strings)
			}
		})
	}
}

// TestTagReadings_ReadsASharedTypeOnce pins what reading a type costs once a
// reading has asked of it: the same at two widths, so N subtypes composing one
// union of N variants, or N members typed by one union or Enum of N values,
// read it once a run rather than once each. It is not run in parallel:
// AllocsPerRun measures the process.
func TestTagReadings_ReadsASharedTypeOnce(t *testing.T) {
	for name, read := range map[string]func(*testing.T, int) float64{
		"a composed union": conjunctAllocs,
		"a union of values": func(t *testing.T, width int) float64 {
			t.Helper()
			return valueAllocs(t, width, func(id ir.TypeID, members []ir.EnumMember) ir.TypeDef {
				variants := make([]ir.TypeRef, len(members))
				for i := range members {
					variants[i] = to(ir.TypeID(fmt.Sprintf("t/lit%d", i)))
				}
				return unionOf(id, false, variants...)
			})
		},
		"an enum": func(t *testing.T, width int) float64 {
			t.Helper()
			return valueAllocs(t, width, func(id ir.TypeID, members []ir.EnumMember) ir.TypeDef {
				return &ir.Enum{ID: id, Members: members, Closed: true}
			})
		},
	} {
		narrow, wide := read(t, 2), read(t, 40)
		assert.Equal(t, narrow, wide, "%s is read once", name)
	}
}

// conjunctAllocs returns the allocations, the fewest of single runs, that
// reading what a model composing a union of width pinned variants admits
// takes, once another model composing it has been read.
func conjunctAllocs(t *testing.T, width int) float64 {
	t.Helper()
	reg := ir.TypeRegistry{}
	variants := make([]ir.TypeRef, width)
	for i := range variants {
		id, tag := ir.TypeID(fmt.Sprintf("t/v%d", i)), ir.TypeID(fmt.Sprintf("t/lit%d", i))
		reg[tag] = lit(tag, fmt.Sprintf("v%d", i))
		reg[id] = &ir.Model{ID: id, Properties: []ir.Property{{ID: ir.PropID("p/" + id), Name: ir.Naming{Source: "kind"}, Type: to(tag)}}}
		variants[i] = to(id)
	}
	reg["t/u"] = unionOf("t/u", true, variants...)
	reg["t/m"] = &ir.Model{ID: "t/m", Mixins: []ir.TypeRef{to("t/u")}}
	r := newReadings(t)
	flat := ir.FlattenedProperties(reg, "t/m")
	r.object(reg, flat, ir.FlatProperty{}, false, "kind")
	fewest := math.Inf(1)
	for range 20 {
		fewest = min(fewest, testing.AllocsPerRun(1, func() { r.object(reg, flat, ir.FlatProperty{}, false, "kind") }))
	}
	return fewest
}

// valueAllocs returns the allocations, the fewest of single runs, that reading
// what a reference to the type of width string values set builds admits takes,
// once it has been read.
func valueAllocs(t *testing.T, width int, set func(ir.TypeID, []ir.EnumMember) ir.TypeDef) float64 {
	t.Helper()
	reg := ir.TypeRegistry{}
	members := make([]ir.EnumMember, width)
	for i := range members {
		id := ir.TypeID(fmt.Sprintf("t/lit%d", i))
		reg[id] = lit(id, fmt.Sprintf("v%d", i))
		members[i] = ir.EnumMember{Value: ir.Value{Kind: ir.ValueString, Str: fmt.Sprintf("v%d", i)}}
	}
	reg["t/u"] = set("t/u", members)
	r := newReadings(t)
	valueOf(r, reg, to("t/u"))
	_, kept := r.read[readKey{id: "t/u"}]
	assert.True(t, kept, "the reading is kept for the next to ask")
	fewest := math.Inf(1)
	for range 20 {
		fewest = min(fewest, testing.AllocsPerRun(1, func() { valueOf(r, reg, to("t/u")) }))
	}
	return fewest
}

// TestReadingsOf_SharesOneStorePerRun pins that every settlement of a run reads
// through one tagReadings, so what one reads another does not read again,
// while another run's starts empty.
func TestReadingsOf_SharesOneStorePerRun(t *testing.T) {
	t.Parallel()
	c := loweringtest.Context(t, openapitest.EmptySpec, lowering.Policy{})
	first, again := readingsOf(c), readingsOf(c)
	assert.Same(t, first, again)
	assert.NotSame(t, readingsOf(c), readingsOf(loweringtest.Context(t, openapitest.EmptySpec, lowering.Policy{})))
}
