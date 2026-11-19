// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/compilers/openapi/internal/service/servicetest"
	"github.com/dexpace/morphic/ir"
)

// newReadings returns a run's tagReadings over a context of its own, so no
// test shares what another read.
func newReadings(t *testing.T) *tagReadings {
	t.Helper()
	return readingsOf(servicetest.Context(t, openapitest.EmptySpec, service.Policy{}).Ctx)
}

// lit is a string Literal at id.
func lit(id ir.TypeID, s string) *ir.Literal {
	return &ir.Literal{ID: id, Value: ir.Value{Kind: ir.ValueString, String: s}}
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
// union every variant of which bounds them, a oneOf admitting what any variant
// does, as an anyOf does, read through alias scalars. Any other node, a nil one
// included, admits any string, and null where a reference along the way is
// nullable.
func TestTagReadings_ValueReadsAClosedSetOfStrings(t *testing.T) {
	t.Parallel()
	str := func(s string) ir.EnumMember { return ir.EnumMember{Value: ir.Value{Kind: ir.ValueString, String: s}} }
	num := ir.EnumMember{Value: ir.Value{Kind: ir.ValueNumber, Number: "1"}}
	nullable := func(id ir.TypeID) ir.TypeRef { return ir.TypeRef{Target: id, Nullable: true} }
	base := ir.TypeRegistry{"type/a": lit("type/a", "a"), "type/b": lit("type/b", "b"), "type/c": lit("type/c", "c"),
		"type/str": &ir.Primitive{ID: "type/str", PrimitiveKind: ir.PrimitiveString}}
	for name, tc := range map[string]struct {
		node ir.TypeDef
		want []string // nil where none is bounded
		null bool
	}{
		"literal":         {lit("type/x", "a"), []string{"a"}, false},
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
		"through alias":   {&ir.Scalar{Base: &ir.TypeRef{Target: "type/a"}}, []string{"a"}, false},
		"a nullable alias": {&ir.Scalar{Base: &ir.TypeRef{Target: "type/a", Nullable: true}},
			[]string{"a"}, true},
		"anyOf":             {unionOf("type/x", false, to("type/b"), to("type/a"), to("type/b")), []string{"a", "b"}, false},
		"oneOf, one shared": {unionOf("type/x", true, to("type/a"), to("type/b"), to("type/a")), []string{"a", "b"}, false},
		"a nullable variant": {unionOf("type/x", false, to("type/a"), nullable("type/b")),
			[]string{"a", "b"}, true},
		"oneOf, two nullable variants": {unionOf("type/x", true, nullable("type/a"), nullable("type/b")),
			[]string{"a", "b"}, true},
		"a union in turn": {unionOf("type/x", false, to("type/a"), to("type/inner")), []string{"a", "b", "c"}, false},
		"a union through an alias": {unionOf("type/x", false, to("type/a"), to("type/aliased")),
			[]string{"a", "b", "c"}, false},
		"a variant any string": {unionOf("type/x", false, to("type/a"), to("type/str")), nil, false},
		"open union":           {&ir.Union{Variants: []ir.Variant{{Type: to("type/a")}}}, nil, true},
		"empty union":          {unionOf("type/x", false), []string{}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reg := ir.TypeRegistry{"type/x": tc.node,
				"type/inner":   unionOf("type/inner", true, to("type/b"), to("type/c")),
				"type/aliased": &ir.Scalar{ID: "type/aliased", Base: &ir.TypeRef{Target: "type/inner"}}}
			maps.Copy(reg, base)
			got := valueOf(newReadings(t), reg, to("type/x"))
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
		"type/a": lit("type/a", "a"), "type/b": lit("type/b", "b"),
		// p and q name each other; r reaches them; s is beside them.
		"type/p": unionOf("type/p", true, to("type/q"), ir.TypeRef{Target: "type/a", Nullable: true}),
		"type/q": unionOf("type/q", true, to("type/p"), to("type/b")),
		"type/r": unionOf("type/r", false, to("type/a"), to("type/p")),
		"type/s": unionOf("type/s", false, to("type/a"), to("type/b")),
		// l names itself.
		"type/l": unionOf("type/l", false, to("type/a"), to("type/l")),
	}
	ids := []ir.TypeID{"type/p", "type/q", "type/r", "type/s", "type/l"}
	read := func(order []ir.TypeID) map[ir.TypeID]tagSet {
		r := newReadings(t)
		out := map[ir.TypeID]tagSet{}
		for _, id := range order {
			out[id] = valueOf(r, reg, to(id))
		}
		return out
	}
	want := read(ids)
	for _, id := range []ir.TypeID{"type/p", "type/q", "type/r", "type/l"} {
		assert.Equal(t, anything(), want[id], id)
	}
	assert.Equal(t, tagSet{strings: []string{"a", "b"}, closed: true}, want["type/s"])
	for _, order := range [][]ir.TypeID{
		{"type/q", "type/p", "type/r", "type/s", "type/l"}, {"type/r", "type/s", "type/q", "type/l", "type/p"}, {"type/s", "type/l", "type/p", "type/r", "type/q"},
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
		return &ir.Model{ID: id, Mixins: []ir.TypeRef{to("type/shape")},
			Properties: []ir.Property{{ID: ir.PropertyID("property/" + id), Name: ir.Naming{Source: "kind"}, Type: to(tag)}}}
	}
	reg := ir.TypeRegistry{
		"type/c": lit("type/c", "circle"), "type/s": lit("type/s", "square"),
		"type/shape":  unionOf("type/shape", true, to("type/circle"), to("type/square")),
		"type/circle": subtype("type/circle", "type/c"), "type/square": subtype("type/square", "type/s"),
		"type/holder": &ir.Model{ID: "type/holder", Mixins: []ir.TypeRef{to("type/shape")}},
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
	want := read([]ir.TypeID{"type/circle", "type/square", "type/shape", "type/holder"})
	assert.Equal(t, tagSet{strings: []string{"circle"}, closed: true}, want["type/circle"])
	assert.Equal(t, tagSet{strings: []string{"square"}, closed: true}, want["type/square"])
	assert.Equal(t, tagSet{strings: []string{"circle", "square"}, closed: true}, want["type/holder"],
		"a holder composing the union admits what any of its variants does")
	for _, order := range [][]ir.TypeID{
		{"type/shape", "type/circle", "type/holder", "type/square"}, {"type/holder", "type/square", "type/circle", "type/shape"},
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
		reg := ir.TypeRegistry{"type/a": lit("type/a", "a")}
		for i := range n {
			u, m, next := fmt.Sprintf("type/u%d", i), fmt.Sprintf("type/m%d", i), fmt.Sprintf("type/u%d", (i+1)%n)
			reg[ir.TypeID(u)] = unionOf(ir.TypeID(u), true, to(ir.TypeID(m)))
			model := &ir.Model{ID: ir.TypeID(m), Mixins: []ir.TypeRef{to(ir.TypeID(next))}}
			if i == n-1 {
				model.Properties = []ir.Property{{ID: "property/m", Name: ir.Naming{Source: "kind"}, Type: to("type/a")}}
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
		{maxComponentSweeps / 2, "type/u0", true},
		{maxComponentSweeps/2 + 1, "type/m0", false},
	} {
		got := newReadings(t).fold(chain(c.n), readKey{id: c.root, name: "kind", object: true})
		assert.Equal(t, c.closed, got.closed, "%s on a cycle of %d unions", c.root, c.n)
	}
}

// TestTagReadings_SweepsACycleUntilItsNullsSettle pins how a cycle reads null,
// from any value down: where every model on it lets its tag hold null, so
// does each, and where all but the last do, the last's exclusion of null
// reaches the first on a sweep changing nothing else, which is not the last.
func TestTagReadings_SweepsACycleUntilItsNullsSettle(t *testing.T) {
	t.Parallel()
	const n = 3
	for _, lastNullable := range []bool{true, false} {
		reg := ir.TypeRegistry{"type/a": lit("type/a", "a")}
		for i := range n {
			u, m, next := fmt.Sprintf("type/u%d", i), fmt.Sprintf("type/m%d", i), fmt.Sprintf("type/u%d", (i+1)%n)
			reg[ir.TypeID(u)] = unionOf(ir.TypeID(u), false, to(ir.TypeID(m)))
			reg[ir.TypeID(m)] = &ir.Model{ID: ir.TypeID(m), Mixins: []ir.TypeRef{to(ir.TypeID(next))},
				Properties: []ir.Property{{ID: ir.PropertyID("property/" + m), Name: ir.Naming{Source: "kind"},
					Type: ir.TypeRef{Target: "type/a", Nullable: i < n-1 || lastNullable}}}}
		}
		got := newReadings(t).fold(reg, readKey{id: "type/m0", name: "kind", object: true})
		assert.Equal(t, tagSet{strings: []string{"a"}, closed: true, null: lastNullable}, got,
			"the last model's tag nullable: %t", lastNullable)
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
		"type/a": lit("type/a", "a"), "type/b": lit("type/b", "b"),
		"type/ab": &ir.Enum{ID: "type/ab", Closed: true, Members: []ir.EnumMember{
			{Value: ir.Value{Kind: ir.ValueString, String: "a"}}, {Value: ir.Value{Kind: ir.ValueString, String: "b"}},
		}},
	}
	decl := func(target ir.TypeID, nullable bool) ir.PropertyDecl {
		return ir.PropertyDecl{Owner: "type/m", Property: &ir.Property{Name: ir.Naming{Source: "kind"},
			Type: ir.TypeRef{Target: target, Nullable: nullable}}}
	}
	ref := func(target ir.TypeID) ir.PropertyDecl { return decl(target, false /* nullable */) }
	refOrNull := func(target ir.TypeID) ir.PropertyDecl { return decl(target, true /* nullable */) }
	member := func(taken ir.PropertyDecl, overridden ...ir.PropertyDecl) ir.FlatProperty {
		return ir.FlatProperty{PropertyDecl: taken, Overridden: overridden}
	}
	const str = "type/primitive/string"
	for name, tc := range map[string]struct {
		p    ir.FlatProperty
		want []string // nil for no pin
	}{
		"taken":                         {member(ref("type/a")), []string{"a"}},
		"overridden":                    {member(ref(str), ref("type/a")), []string{"a"}},
		"agreeing":                      {member(ref("type/a"), ref(str), refOrNull("type/a")), []string{"a"}},
		"a set met":                     {member(ref("type/ab"), ref("type/b")), []string{"b"}},
		"a set and a string":            {member(ref("type/ab"), ref(str)), []string{"a", "b"}},
		"disagreeing":                   {member(ref("type/a"), ref("type/b")), nil},
		"disagreeing, each admits null": {member(refOrNull("type/a"), refOrNull("type/b")), nil},
		"a null another excludes":       {member(refOrNull("type/a"), ref(str)), []string{"a"}},
		"a null every one admits":       {member(refOrNull("type/a"), refOrNull(str)), nil},
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
		return &ir.Model{ID: id, Properties: []ir.Property{{ID: ir.PropertyID("property/" + id), Name: ir.Naming{Source: "kind"},
			Type: to(tag)}}}
	}
	reg := ir.TypeRegistry{
		"type/a": lit("type/a", "a"), "type/b": lit("type/b", "b"), "type/c": lit("type/c", "c"), "type/str": &ir.Primitive{ID: "type/str", PrimitiveKind: ir.PrimitiveString},
		"type/va": pinned("type/va", "type/a"), "type/vb": pinned("type/vb", "type/b"), "type/vc": pinned("type/vc", "type/c"), "type/vs": pinned("type/vs", "type/str"),
		"type/none": &ir.Model{ID: "type/none"},
	}
	for name, tc := range map[string]struct {
		own     ir.TypeID // the object's own declaration's type, "" for none
		union   *ir.Union
		want    []string // nil for any
		through bool     // the object composes an alias over the union
	}{
		"its variants pin":            {"", unionOf("type/u", true, to("type/va"), to("type/vb")), []string{"a", "b"}, false},
		"met with its own":            {"type/a", unionOf("type/u", true, to("type/va"), to("type/vb")), []string{"a"}, false},
		"through an alias":            {"", unionOf("type/u", true, to("type/va"), to("type/vc")), []string{"a", "c"}, true},
		"a variant declares none":     {"", unionOf("type/u", true, to("type/va"), to("type/none")), nil, false},
		"a variant any string":        {"", unionOf("type/u", true, to("type/va"), to("type/vs")), nil, false},
		"a variant no model":          {"", unionOf("type/u", true, to("type/va"), to("type/b")), nil, false},
		"open":                        {"", &ir.Union{ID: "type/u", Variants: []ir.Variant{{Type: to("type/va")}}}, nil, false},
		"own, the union open":         {"type/a", &ir.Union{ID: "type/u", Variants: []ir.Variant{{Type: to("type/vb")}}}, []string{"a"}, false},
		"a variant composing a union": {"", unionOf("type/u", true, to("type/vb"), to("type/composing")), []string{"a", "b", "c"}, false},
		"a variant a union in turn":   {"", unionOf("type/u", true, to("type/va"), to("type/inner")), []string{"a", "b", "c"}, false},
		"a variant through an alias":  {"", unionOf("type/u", true, to("type/va"), to("type/avc")), []string{"a", "c"}, false},
		"a variant on a cycle":        {"", unionOf("type/u", true, to("type/va"), to("type/looping")), nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := ir.TypeRegistry{"type/u": tc.union, "type/au": &ir.Scalar{ID: "type/au", Base: &ir.TypeRef{Target: "type/u"}},
				"type/inner":     unionOf("type/inner", false, to("type/vb"), to("type/vc")),
				"type/innerac":   unionOf("type/innerac", true, to("type/va"), to("type/vc")),
				"type/composing": &ir.Model{ID: "type/composing", Mixins: []ir.TypeRef{to("type/innerac")}},
				"type/looping":   &ir.Model{ID: "type/looping", Mixins: []ir.TypeRef{to("type/u")}},
				"type/avc":       &ir.Scalar{ID: "type/avc", Base: &ir.TypeRef{Target: "type/vc"}}}
			maps.Copy(r, reg)
			composed := ir.TypeID("type/u")
			if tc.through {
				composed = "type/au"
			}
			m := &ir.Model{ID: "type/m", Mixins: []ir.TypeRef{to(composed)}}
			if tc.own != "" {
				m.Properties = []ir.Property{{ID: "property/m", Name: ir.Naming{Source: "kind"}, Type: to(tc.own)}}
			}
			r["type/m"] = m
			flat := ir.FlattenedProperties(r, "type/m")
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
					variants[i] = to(ir.TypeID(fmt.Sprintf("type/lit%d", i)))
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
		id, tag := ir.TypeID(fmt.Sprintf("type/v%d", i)), ir.TypeID(fmt.Sprintf("type/lit%d", i))
		reg[tag] = lit(tag, fmt.Sprintf("v%d", i))
		reg[id] = &ir.Model{ID: id, Properties: []ir.Property{{ID: ir.PropertyID("property/" + id), Name: ir.Naming{Source: "kind"}, Type: to(tag)}}}
		variants[i] = to(id)
	}
	reg["type/u"] = unionOf("type/u", true, variants...)
	reg["type/m"] = &ir.Model{ID: "type/m", Mixins: []ir.TypeRef{to("type/u")}}
	r := newReadings(t)
	flat := ir.FlattenedProperties(reg, "type/m")
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
		id := ir.TypeID(fmt.Sprintf("type/lit%d", i))
		reg[id] = lit(id, fmt.Sprintf("v%d", i))
		members[i] = ir.EnumMember{Value: ir.Value{Kind: ir.ValueString, String: fmt.Sprintf("v%d", i)}}
	}
	reg["type/u"] = set("type/u", members)
	r := newReadings(t)
	valueOf(r, reg, to("type/u"))
	_, kept := r.read[readKey{id: "type/u"}]
	assert.True(t, kept, "the reading is kept for the next to ask")
	fewest := math.Inf(1)
	for range 20 {
		fewest = min(fewest, testing.AllocsPerRun(1, func() { valueOf(r, reg, to("type/u")) }))
	}
	return fewest
}

// TestReadingsOf_SharesOneStorePerRun pins that every settlement of a run reads
// through one tagReadings, so what one reads another does not read again,
// while another run's starts empty.
func TestReadingsOf_SharesOneStorePerRun(t *testing.T) {
	t.Parallel()
	c := servicetest.Context(t, openapitest.EmptySpec, service.Policy{})
	first, again := readingsOf(c.Ctx), readingsOf(c.Ctx)
	assert.Same(t, first, again)
	assert.NotSame(t, readingsOf(c.Ctx), readingsOf(servicetest.Context(t, openapitest.EmptySpec, service.Policy{}).Ctx))
}

// TestModelFields_ReportsAFoldFollowingAStoredReading pins the order the tag
// readings rest on (tagReadings): a fold, which rewrites a member's type, runs
// before any reading is stored, and reports nothing. A reading stored ahead of
// it, as one would be were the fold deferred behind a union's tags, leaves
// the fold reported as a compiler bug at its model, and the fold still taken.
func TestModelFields_ReportsAFoldFollowingAStoredReading(t *testing.T) {
	t.Parallel()
	const folded = "/components/schemas/Folded"
	spec := openapitest.ComponentSpecVer("3.1.0", "    Folded:\n      type: object\n"+
		"      allOf: [{type: object, properties: {kind: {type: string}}}]\n"+
		"      properties: {kind: {const: folded}}\n")
	models := make([]ir.TypeDef, 0, 2)
	for _, early := range []bool{false, true} {
		l, loaded := LoweredFor(t, spec)
		require.Empty(t, loaded)
		l.Diags.AppendAll(LowerComponentSchemas(t.Context(), l.Ctx, l.Types, l.Schemas()))
		if early {
			readingsOf(l.Ctx).refOf(ir.TypeRegistry{}, to("type/elsewhere"))
		}
		doc := l.Document(t)
		diags := l.Diags.List()
		assert.Equal(t, early, openapitest.HasDiagCodeAt(diags, diag.InternalInvariant, folded), "early: %v", early)
		openapitest.RequireNoErrorDiags(t, slices.DeleteFunc(diags, func(d ir.Diagnostic) bool {
			return d.Code == diag.InternalInvariant && d.Provenance.Pointer == folded
		}))
		models = append(models, doc.Types["type/openapi"+folded])
	}
	assert.Empty(t, cmp.Diff(models[0], models[1]), "the fold is taken either way")
}

// TestTagReadings_ReadAModelAsTheMapItSwapsFor pins what lets a reading stored
// before a settlement swaps a model for its map (besideUnionDiscriminator)
// answer as one taken after: a model holding only a catch-all, of a pinned
// type, admits any value at a property, as a variant or a conjunct, and any
// string as a value, as its map does.
func TestTagReadings_ReadAModelAsTheMapItSwapsFor(t *testing.T) {
	t.Parallel()
	reg := ir.TypeRegistry{
		"type/lit": lit("type/lit", "x"),
		"type/m":   &ir.Model{ID: "type/m", AdditionalProperties: &ir.AdditionalProperties{Value: &ir.TypeRef{Target: "type/lit"}}},
		"type/u":   unionOf("type/u", true, to("type/m")),
		"type/s":   &ir.Model{ID: "type/s", Mixins: []ir.TypeRef{to("type/u"), to("type/m")}},
	}
	read := func() []tagSet {
		r := newReadings(t)
		flat := ir.FlattenedProperties(reg, "type/s")
		p, declared := flatMember(flat, "kind")
		return []tagSet{r.object(reg, flat, p, declared, "kind"), valueOf(r, reg, to("type/m"))}
	}
	before := read()
	m, ok := reg["type/m"].(*ir.Model)
	require.True(t, ok)
	swapped, isMap := ir.AsMap(m)
	require.True(t, isMap, "the model is its map")
	reg["type/m"] = swapped

	sets := cmp.AllowUnexported(tagSet{})
	assert.Empty(t, cmp.Diff([]tagSet{anything(), anyString()}, before, sets))
	assert.Empty(t, cmp.Diff(before, read(), sets), "the swap changes no reading")
}
