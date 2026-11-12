// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/ctxtest"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
	"github.com/dexpace/morphic/pass"
)

// str is the string value s.
func str(s string) ir.Value { return ir.Value{Kind: ir.ValueString, Str: s} }

// lit is the anonymous string Literal at id holding value, as a compiler
// lowers a const branch.
func lit(id ir.TypeID, value string) *ir.Literal {
	return &ir.Literal{ID: id, Anonymous: true, Name: hinted("variant"),
		Value: str(value), ValueType: ir.PrimString}
}

// pinned is the anonymous closed Enum at id holding values, as a compiler lowers
// an enum branch.
func pinned(id ir.TypeID, values ...string) *ir.Enum {
	e := &ir.Enum{ID: id, Anonymous: true, Name: hinted("variant"),
		ValueType: ir.PrimString, Closed: true}
	for _, v := range values {
		e.Members = append(e.Members, member(id, v))
	}
	return e
}

// member is the member of the enum at enum holding value, under the ID and the
// name a compiler gives it.
func member(enum ir.TypeID, value string) ir.EnumMember {
	key, _ := ir.EnumMemberKey(str(value))
	id, _ := ir.EnumMemberIDFor(enum, key, 1 /* occurrence */)
	return ir.EnumMember{ID: id, Name: ir.NamingFor(value), Value: str(value)}
}

// union is the closed, exclusive union at id whose variants name targets.
func union(id ir.TypeID, targets ...ir.TypeID) *ir.Union {
	u := &ir.Union{ID: id, Name: named("U"), Docs: ir.Docs{Summary: "A union."},
		Exclusive: true, Closed: true}
	for _, target := range targets {
		u.Variants = append(u.Variants, ir.Variant{Name: hinted("variant"), Type: ir.TypeRef{Target: target}})
	}
	return u
}

// anonymousUnion is the union at id whose variants name targets, as a
// compiler lowers an inline oneOf: anonymous, named by its position alone.
func anonymousUnion(id ir.TypeID, targets ...ir.TypeID) *ir.Union {
	u := union(id, targets...)
	u.Anonymous, u.Name, u.Docs = true, hinted("variant"), ir.Docs{}
	return u
}

// enumFor is the Enum pass.ConstUnions makes of u, holding members.
func enumFor(u *ir.Union, members ...ir.EnumMember) *ir.Enum {
	return &ir.Enum{TypeCommon: u.TypeCommon, ValueType: ir.PrimString, Members: members, Closed: true}
}

// constDoc is a document declaring the namespace x for types and enum members,
// holding types.
func constDoc(types ...ir.TypeDef) *ir.Document {
	doc := withTypes(types...)
	doc.IRVersion = ir.IRVersion
	doc.IDSpaces = map[string][]string{ir.IDKindType: {"x"}, ir.IDKindEnumMember: {"x"}}
	return doc
}

// collapsed runs pass.ConstUnions over doc, failing t on any diagnostic or a
// change to doc, and returns what it made.
func collapsed(t *testing.T, doc *ir.Document) *ir.Document {
	t.Helper()
	before := irtest.Encoded(t, doc)
	out, diags := pass.ConstUnions{}.Run(t.Context(), doc)
	require.Empty(t, diags)
	require.Equal(t, before, irtest.Encoded(t, doc), "the pass changed its input")
	return out
}

// TestConstUnions_ReplacesAUnionOfStringsWithTheirEnum pins the Enum each union
// of strings becomes, at its own ID with its common part: a member for each
// value, in variant order, identified and named as an enum's would be, holding
// what its variant, its branch and the branch's member say of it. A branch that
// is a union is read as the Enum it becomes, in the same run, and one of several
// values, an Enum or a union, is taken whole where only this variant names it.
// The branch types go with the union.
func TestConstUnions_ReplacesAUnionOfStringsWithTheirEnum(t *testing.T) {
	t.Parallel()
	deprecated := &ir.Deprecation{Message: "gone"}
	available := &ir.Availability{Added: []string{"v2"}}
	examples := []ir.Example{{Value: new(str("red"))}}
	unmodeled := ir.Unmodeled{"openapi:x-a": {Reason: ir.ReasonVendorExtension}}
	tests := []struct {
		name  string
		types func() (*ir.Union, []ir.TypeDef)
		want  func(u *ir.Union) []ir.EnumMember
	}{
		{"string consts", func() (*ir.Union, []ir.TypeDef) {
			return union("t/x/U", "t/x/U/0", "t/x/U/1"), []ir.TypeDef{lit("t/x/U/0", "red"), lit("t/x/U/1", "green")}
		}, func(u *ir.Union) []ir.EnumMember {
			return []ir.EnumMember{member(u.ID, "red"), member(u.ID, "green")}
		}},
		{"one-member enums of an anyOf", func() (*ir.Union, []ir.TypeDef) {
			u := union("t/x/U", "t/x/U/0", "t/x/U/1")
			u.Exclusive = false
			return u, []ir.TypeDef{pinned("t/x/U/0", "low"), pinned("t/x/U/1", "high")}
		}, func(u *ir.Union) []ir.EnumMember {
			return []ir.EnumMember{member(u.ID, "low"), member(u.ID, "high")}
		}},
		{"what each says of a value", func() (*ir.Union, []ir.TypeDef) {
			u := union("t/x/U", "t/x/U/0", "t/x/U/1", "t/x/U/2")
			u.Variants[0].Docs = ir.Docs{Description: "Red."}
			u.Variants[2].Unmodeled = unmodeled
			red, green, blue := lit("t/x/U/0", "red"), pinned("t/x/U/1", "green"), pinned("t/x/U/2", "blue")
			red.Deprecation, green.Availability = deprecated, available
			green.Members[0].Examples, green.Members[0].WireName = examples, "GREEN"
			blue.Docs = ir.Docs{Summary: "Blue"}
			return u, []ir.TypeDef{red, green, blue}
		}, func(u *ir.Union) []ir.EnumMember {
			red, green, blue := member(u.ID, "red"), member(u.ID, "green"), member(u.ID, "blue")
			red.Docs, red.Deprecation = ir.Docs{Description: "Red."}, deprecated
			green.Availability, green.Examples, green.WireName = available, examples, "GREEN"
			blue.Docs, blue.Unmodeled = ir.Docs{Summary: "Blue"}, unmodeled
			return []ir.EnumMember{red, green, blue}
		}},
		{"a union of one value", func() (*ir.Union, []ir.TypeDef) {
			u := union("t/x/U", "t/x/U/0", "t/x/U/1")
			u.Variants[0].Docs = ir.Docs{Description: "Up."}
			return u, []ir.TypeDef{anonymousUnion("t/x/U/0", "t/x/U/0/0"), lit("t/x/U/0/0", "up"), lit("t/x/U/1", "down")}
		}, func(u *ir.Union) []ir.EnumMember {
			up := member(u.ID, "up")
			up.Docs = ir.Docs{Description: "Up."}
			return []ir.EnumMember{up, member(u.ID, "down")}
		}},
		{"branches of several values", func() (*ir.Union, []ir.TypeDef) {
			return union("t/x/U", "t/x/U/0", "t/x/U/1"),
				[]ir.TypeDef{pinned("t/x/U/0", "small", "large"), pinned("t/x/U/1", "")}
		}, func(u *ir.Union) []ir.EnumMember {
			return []ir.EnumMember{member(u.ID, "small"), member(u.ID, "large"), member(u.ID, "")}
		}},
		{"the one branch of several values", func() (*ir.Union, []ir.TypeDef) {
			return union("t/x/U", "t/x/U/0"), []ir.TypeDef{pinned("t/x/U/0", "s", "m", "l")}
		}, func(u *ir.Union) []ir.EnumMember {
			return []ir.EnumMember{member(u.ID, "s"), member(u.ID, "m"), member(u.ID, "l")}
		}},
		{"a union of several values", func() (*ir.Union, []ir.TypeDef) {
			return union("t/x/U", "t/x/U/0", "t/x/U/1"), []ir.TypeDef{
				anonymousUnion("t/x/U/0", "t/x/U/0/0", "t/x/U/0/1"), lit("t/x/U/0/0", "up"), pinned("t/x/U/0/1", "down"),
				lit("t/x/U/1", "left")}
		}, func(u *ir.Union) []ir.EnumMember {
			return []ir.EnumMember{member(u.ID, "up"), member(u.ID, "down"), member(u.ID, "left")}
		}},
		{"what the members of a branch of several values say", func() (*ir.Union, []ir.TypeDef) {
			many := pinned("t/x/U/0", "red", "green")
			many.Members[0].Docs, many.Members[1].WireName = ir.Docs{Description: "Red."}, "GREEN"
			return union("t/x/U", "t/x/U/0"), []ir.TypeDef{many}
		}, func(u *ir.Union) []ir.EnumMember {
			red, green := member(u.ID, "red"), member(u.ID, "green")
			red.Docs, green.WireName = ir.Docs{Description: "Red."}, "GREEN"
			return []ir.EnumMember{red, green}
		}},
		{"branches nested without end", func() (*ir.Union, []ir.TypeDef) {
			return union("t/x/U", "t/x/U/0", "t/x/U/1"), []ir.TypeDef{
				anonymousUnion("t/x/U/0", "t/x/U/0/0", "t/x/U/0/1"), anonymousUnion("t/x/U/0/0", "t/x/U/0/0/0", "t/x/U/0/0/1"),
				lit("t/x/U/0/0/0", "a"), lit("t/x/U/0/0/1", "b"), pinned("t/x/U/0/1", "c", "d"), pinned("t/x/U/1", "e", "f")}
		}, func(u *ir.Union) []ir.EnumMember {
			return []ir.EnumMember{member(u.ID, "a"), member(u.ID, "b"), member(u.ID, "c"), member(u.ID, "d"),
				member(u.ID, "e"), member(u.ID, "f")}
		}},
		{"values a key escapes or no word names", func() (*ir.Union, []ir.TypeDef) {
			return union("t/x/U", "t/x/U/0", "t/x/U/1"), []ir.TypeDef{lit("t/x/U/0", ""), lit("t/x/U/1", "a/b~c")}
		}, func(u *ir.Union) []ir.EnumMember {
			return []ir.EnumMember{
				{ID: "e/x/U/s:", Name: hinted(ir.EmptyNameHint), Value: str("")},
				{ID: "e/x/U/s:a~1b~0c", Name: named("a/b~c"), Value: str("a/b~c")},
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			u, branches := tt.types()
			out := collapsed(t, constDoc(append([]ir.TypeDef{u}, branches...)...))

			assert.Empty(t, cmp.Diff(ir.TypeRegistry{u.ID: enumFor(u, tt.want(u)...)}, out.Types))
		})
	}
}

// TestConstUnions_KeepsTheValueTypeEveryBranchShares pins the Enum's ValueType
// to the one every branch types its values with, a format's included, and a
// union whose branches type them apart stays.
func TestConstUnions_KeepsTheValueTypeEveryBranchShares(t *testing.T) {
	t.Parallel()
	u := union("t/x/U", "t/x/U/0", "t/x/U/1")
	day, other := lit("t/x/U/0", "2026-01-01"), pinned("t/x/U/1", "2026-01-02")
	day.ValueType, other.ValueType = ir.PrimDate, ir.PrimDate
	out := collapsed(t, constDoc(u, day, other))
	enum, ok := out.Types[u.ID].(*ir.Enum)
	require.True(t, ok)
	assert.Equal(t, ir.PrimDate, enum.ValueType)

	other.ValueType = ir.PrimString
	apart := constDoc(u, day, other)
	assert.Same(t, apart, collapsed(t, apart))
}

// several adds a second value to e, which makes it a branch of several values.
func several(e *ir.Enum) { e.Members = append(e.Members, member(e.ID, "blue")) }

// TestConstUnions_KeepsAUnionAnEnumCannotSay pins each union the pass leaves:
// one saying what an Enum has no field for, a variant or branch saying more
// than its value, a branch of another kind or none, a branch of several values
// that another node names or that says anything of all of them, values of
// different types or a value twice, and a fact two places say of one value,
// which a member holds one of. Each is a document the pass hands back
// unchanged.
func TestConstUnions_KeepsAUnionAnEnumCannotSay(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		write func(u *ir.Union, red *ir.Literal, green *ir.Enum, types ir.TypeRegistry)
	}{
		{"an open union", func(u *ir.Union, _ *ir.Literal, _ *ir.Enum, _ ir.TypeRegistry) { u.Closed = false }},
		{"a tagged union", func(u *ir.Union, _ *ir.Literal, _ *ir.Enum, _ ir.TypeRegistry) { u.WireTagged = true }},
		{"a discriminator", func(u *ir.Union, _ *ir.Literal, _ *ir.Enum, _ ir.TypeRegistry) {
			u.Discriminator = &ir.Discriminator{PropertyName: "kind"}
		}},
		{"an input-only union", func(u *ir.Union, _ *ir.Literal, _ *ir.Enum, _ ir.TypeRegistry) { u.InputOnly = true }},
		{"no variant", func(u *ir.Union, _ *ir.Literal, _ *ir.Enum, _ ir.TypeRegistry) { u.Variants = nil }},
		{"a nullable variant", func(u *ir.Union, _ *ir.Literal, _ *ir.Enum, _ ir.TypeRegistry) {
			u.Variants[0].Type.Nullable = true
		}},
		{"a variant's declared name", func(u *ir.Union, _ *ir.Literal, _ *ir.Enum, _ ir.TypeRegistry) {
			u.Variants[0].Name = ir.Naming{Source: "Red"}
		}},
		{"a variant's canonical name", func(u *ir.Union, _ *ir.Literal, _ *ir.Enum, _ ir.TypeRegistry) {
			u.Variants[0].Name = ir.Naming{Canonical: "red"}
		}},
		{"a variant's alias", func(u *ir.Union, _ *ir.Literal, _ *ir.Enum, _ ir.TypeRegistry) {
			u.Variants[1].Name = ir.Naming{Hint: "variant", Aliases: []string{"Green"}}
		}},
		{"a variant's wire name", func(u *ir.Union, _ *ir.Literal, _ *ir.Enum, _ ir.TypeRegistry) {
			u.Variants[1].WireName = "g"
		}},
		{"a named branch", func(_ *ir.Union, red *ir.Literal, _ *ir.Enum, _ ir.TypeRegistry) { red.Anonymous = false }},
		{"an anonymous branch's declared name", func(_ *ir.Union, red *ir.Literal, _ *ir.Enum, _ ir.TypeRegistry) {
			red.Name = named("Red")
		}},
		{"a branch's tags", func(_ *ir.Union, red *ir.Literal, _ *ir.Enum, _ ir.TypeRegistry) { red.Tags = []string{"t"} }},
		{"a sensitive branch", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			green.Sensitive = true
		}},
		{"a constraint a branch keeps", func(_ *ir.Union, red *ir.Literal, _ *ir.Enum, _ ir.TypeRegistry) {
			red.Unmodeled = ir.Unmodeled{"openapi:pattern": {Reason: ir.ReasonDegradedLowering}}
		}},
		{"a construct a variant keeps", func(u *ir.Union, _ *ir.Literal, _ *ir.Enum, _ ir.TypeRegistry) {
			u.Variants[1].Unmodeled = ir.Unmodeled{"openapi:x-a": {Reason: ir.ReasonVendorExtension},
				"openapi:b": {Reason: ir.ReasonNoIRHome}}
		}},
		{"an inferred branch", func(_ *ir.Union, red *ir.Literal, _ *ir.Enum, _ ir.TypeRegistry) {
			red.Provenance.Inferred = "a-heuristic"
		}},
		{"a number", func(_ *ir.Union, red *ir.Literal, _ *ir.Enum, _ ir.TypeRegistry) {
			red.Value = ir.Value{Kind: ir.ValueNumber, Num: "1"}
		}},
		{"literals no primitive types", func(u *ir.Union, red *ir.Literal, _ *ir.Enum, types ir.TypeRegistry) {
			untyped := lit(u.Variants[1].Type.Target, "green")
			red.ValueType, untyped.ValueType = "", ""
			types[untyped.ID] = untyped
		}},
		{"an open enum", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) { green.Closed = false }},
		{"a flags enum", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) { green.Flags = true }},
		{"an enum's fallback member", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			green.FallbackMember = "green"
		}},
		{"an empty enum", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) { green.Members = nil }},
		{"an enum of a bool", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			green.Members[0].Value = ir.Value{Kind: ir.ValueBool, Bool: true}
		}},
		{"enums no primitive types", func(u *ir.Union, _ *ir.Literal, green *ir.Enum, types ir.TypeRegistry) {
			untyped := pinned(u.Variants[0].Type.Target, "red")
			green.ValueType, untyped.ValueType = "", ""
			types[untyped.ID] = untyped
		}},
		{"a branch of another kind", func(u *ir.Union, _ *ir.Literal, _ *ir.Enum, types ir.TypeRegistry) {
			types[u.Variants[0].Type.Target] = &ir.Model{ID: "t/x/U/0", Anonymous: true}
		}},
		{"a branch naming itself", func(u *ir.Union, _ *ir.Literal, _ *ir.Enum, types ir.TypeRegistry) {
			types[u.Variants[0].Type.Target] = anonymousUnion("t/x/U/0", "t/x/U/0", "t/x/U/1")
		}},
		{"no branch", func(u *ir.Union, _ *ir.Literal, _ *ir.Enum, types ir.TypeRegistry) {
			delete(types, u.Variants[0].Type.Target)
		}},
		{"a nil literal", func(u *ir.Union, _ *ir.Literal, _ *ir.Enum, types ir.TypeRegistry) {
			types[u.Variants[0].Type.Target] = (*ir.Literal)(nil)
		}},
		{"a nil enum", func(u *ir.Union, _ *ir.Literal, _ *ir.Enum, types ir.TypeRegistry) {
			types[u.Variants[1].Type.Target] = (*ir.Enum)(nil)
		}},
		{"values typed apart", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			green.ValueType = ir.PrimDate
		}},
		{"a branch of several values typed apart", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			several(green)
			green.ValueType = ir.PrimDate
		}},
		{"a value twice", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			green.Members[0].Value = str("red")
		}},
		{"docs a variant and its branch say", func(u *ir.Union, red *ir.Literal, _ *ir.Enum, _ ir.TypeRegistry) {
			u.Variants[0].Docs, red.Docs = ir.Docs{Summary: "a"}, ir.Docs{Summary: "b"}
		}},
		{"a deprecation a branch and its member say", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			green.Deprecation, green.Members[0].Deprecation = &ir.Deprecation{}, &ir.Deprecation{}
		}},
		{"an availability a variant and its member say", func(u *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			u.Variants[1].Availability, green.Members[0].Availability = &ir.Availability{}, &ir.Availability{}
		}},
		{"examples a variant and its branch say", func(u *ir.Union, red *ir.Literal, _ *ir.Enum, _ ir.TypeRegistry) {
			u.Variants[0].Examples, red.Examples = []ir.Example{{Value: new(str("red"))}}, []ir.Example{{Value: new(str("red"))}}
		}},
		{"unmodeled entries a branch and its member say", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			green.Unmodeled = ir.Unmodeled{"openapi:x-a": {Reason: ir.ReasonVendorExtension}}
			green.Members[0].Unmodeled = ir.Unmodeled{"openapi:x-b": {Reason: ir.ReasonVendorExtension}}
		}},
		{"a branch of several values a property names", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, types ir.TypeRegistry) {
			several(green)
			types["t/x/M"] = &ir.Model{ID: "t/x/M", Properties: []ir.Property{{ID: "p/x/M/p", Type: ir.TypeRef{Target: green.ID}}}}
		}},
		{"a branch of several values a second union names", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, types ir.TypeRegistry) {
			several(green)
			types["t/x/V"] = union("t/x/V", green.ID)
		}},
		{"a branch of several values a doc ref names", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, types ir.TypeRegistry) {
			several(green)
			types["t/x/M"] = &ir.Model{ID: "t/x/M", Docs: ir.Docs{Refs: []ir.DocRef{{Type: green.ID}}}}
		}},
		{"a branch of several values another map's key names", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, types ir.TypeRegistry) {
			several(green)
			types["t/x/M"] = &ir.Model{ID: "t/x/M", Examples: []ir.Example{{Value: &ir.Value{
				Kind: ir.ValueRefKind, Ref: &ir.ValueRef{Type: green.ID, Member: "blue"}}}}}
		}},
		{"a branch of several values its union names twice", func(u *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			several(green)
			u.Variants = append(u.Variants, ir.Variant{Name: hinted("variant"), Type: ir.TypeRef{Target: green.ID}})
		}},
		{"a branch of several values that says docs of them all", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			several(green)
			green.Docs = ir.Docs{Summary: "Colors."}
		}},
		{"a branch of several values that is deprecated", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			several(green)
			green.Deprecation = &ir.Deprecation{Message: "gone"}
		}},
		{"a branch of several values with an availability", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			several(green)
			green.Availability = &ir.Availability{Added: []string{"v2"}}
		}},
		{"a branch of several values with examples", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			several(green)
			green.Examples = []ir.Example{{Value: new(str("green"))}}
		}},
		{"a branch of several values with an extension", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			several(green)
			green.Unmodeled = ir.Unmodeled{"openapi:x-a": {Reason: ir.ReasonVendorExtension}}
		}},
		{"docs a variant says of a branch of several values", func(u *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			several(green)
			u.Variants[1].Docs = ir.Docs{Description: "Colors."}
		}},
		{"a branch of several values that is open", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			several(green)
			green.Closed = false
		}},
		{"a branch of several values that is flags", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			several(green)
			green.Flags = true
		}},
		{"a branch of several values with a bool", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			several(green)
			green.Members[1].Value = ir.Value{Kind: ir.ValueBool, Bool: true}
		}},
		{"a branch union of several values that holds a number", func(u *ir.Union, _ *ir.Literal, _ *ir.Enum, types ir.TypeRegistry) {
			number := lit("t/x/U/1/0", "")
			number.Value = ir.Value{Kind: ir.ValueNumber, Num: "1"}
			types[u.Variants[1].Type.Target] = anonymousUnion("t/x/U/1", "t/x/U/1/0", "t/x/U/1/1")
			types[number.ID] = number
			types["t/x/U/1/1"] = lit("t/x/U/1/1", "green")
		}},
		{"a value a branch of several values and a literal share", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			several(green)
			green.Members[1] = member(green.ID, "red")
		}},
		{"a value twice in a branch of several values", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			several(green)
			green.Members[1] = member(green.ID, "green")
		}},
		{"a value two branches of several values share", func(u *ir.Union, _ *ir.Literal, green *ir.Enum, types ir.TypeRegistry) {
			several(green)
			types["t/x/U/2"] = pinned("t/x/U/2", "blue", "green", "teal")
			u.Variants = append(u.Variants, ir.Variant{Name: hinted("variant"), Type: ir.TypeRef{Target: "t/x/U/2"}})
		}},
		{"a value two smaller branches of several values share", func(u *ir.Union, _ *ir.Literal, green *ir.Enum, types ir.TypeRegistry) {
			several(green)
			types["t/x/U/2"] = pinned("t/x/U/2", "cyan", "teal", "navy", "gold")
			types["t/x/U/3"] = pinned("t/x/U/3", "blue", "plum")
			u.Variants = append(u.Variants, ir.Variant{Name: hinted("variant"), Type: ir.TypeRef{Target: "t/x/U/2"}},
				ir.Variant{Name: hinted("variant"), Type: ir.TypeRef{Target: "t/x/U/3"}})
		}},
		{"a union off its key", func(u *ir.Union, _ *ir.Literal, _ *ir.Enum, types ir.TypeRegistry) {
			delete(types, u.ID)
			types["t/x/W"] = u
		}},
		{"a nil union", func(u *ir.Union, _ *ir.Literal, _ *ir.Enum, types ir.TypeRegistry) {
			types[u.ID] = (*ir.Union)(nil)
		}},
		{"a union no member fits under", func(u *ir.Union, _ *ir.Literal, _ *ir.Enum, types ir.TypeRegistry) {
			delete(types, u.ID)
			u.ID = "U"
			types[u.ID] = u
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			u, red, green := union("t/x/U", "t/x/U/0", "t/x/U/1"), lit("t/x/U/0", "red"), pinned("t/x/U/1", "green")
			doc := constDoc(u, red, green)
			tt.write(u, red, green, doc.Types)

			assert.Same(t, doc, collapsed(t, doc), "the union is kept, and the document with it")
		})
	}
}

// TestConstUnions_KeepsAUnionWhoseMembersHaveNoNamespace pins the union a
// document declaring no member namespace for its own keeps: an Enum's members
// would take IDs in a namespace nobody declared, which the edit refuses, so the
// union stays and nothing is reported.
func TestConstUnions_KeepsAUnionWhoseMembersHaveNoNamespace(t *testing.T) {
	t.Parallel()
	for _, spaces := range []map[string][]string{nil, {ir.IDKindType: {"x"}, ir.IDKindEnumMember: {"y"}}} {
		doc := constDoc(union("t/x/U", "t/x/U/0"), lit("t/x/U/0", "red"))
		doc.IDSpaces = spaces
		assert.Same(t, doc, collapsed(t, doc), "member namespaces %v", spaces)
	}
}

// TestConstUnions_RemovesOnlyTheBranchesNothingElseReferences pins which
// branch types go with their union: those nothing else names, a union
// collapsed beside it included. A reference anywhere else keeps one, in a
// TypeRef, a DocRef, a value, another map's key, or a union that stays.
func TestConstUnions_RemovesOnlyTheBranchesNothingElseReferences(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		refs func(doc *ir.Document)
		kept bool
	}{
		{"nothing else", func(*ir.Document) {}, false},
		{"another union collapsed", func(doc *ir.Document) {
			doc.Types["t/x/V"] = union("t/x/V", "t/x/U/0")
		}, false},
		{"a property", func(doc *ir.Document) {
			doc.Types["t/x/M"] = &ir.Model{ID: "t/x/M",
				Properties: []ir.Property{{ID: "p/x/M/p", Type: ir.TypeRef{Target: "t/x/U/0"}}}}
		}, true},
		{"a doc ref", func(doc *ir.Document) {
			doc.Types["t/x/M"] = &ir.Model{ID: "t/x/M",
				Docs: ir.Docs{Refs: []ir.DocRef{{Type: "t/x/U/0"}}}}
		}, true},
		{"an example's value", func(doc *ir.Document) {
			doc.Types["t/x/M"] = &ir.Model{ID: "t/x/M", Examples: []ir.Example{{Value: &ir.Value{
				Kind: ir.ValueRefKind, Ref: &ir.ValueRef{Type: "t/x/U/0", Member: "red"}}}}}
		}, true},
		{"a rename's key", func(doc *ir.Document) {
			doc.Services = []ir.Service{{ID: "s/x", Renames: map[ir.TypeID]ir.Naming{"t/x/U/0": named("Red")}}}
		}, true},
		{"a union that stays", func(doc *ir.Document) {
			kept := union("t/x/V", "t/x/U/0")
			kept.WireTagged = true
			doc.Types["t/x/V"] = kept
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := constDoc(union("t/x/U", "t/x/U/0", "t/x/U/1"), lit("t/x/U/0", "red"), lit("t/x/U/1", "green"))
			tt.refs(doc)
			out := collapsed(t, doc)

			require.Equal(t, ir.KindEnum, out.Types["t/x/U"].Kind())
			assert.NotContains(t, out.Types, ir.TypeID("t/x/U/1"), "a branch nothing else names goes")
			_, kept := out.Types["t/x/U/0"]
			assert.Equal(t, tt.kept, kept, "the branch named elsewhere is kept")
		})
	}
}

// TestConstUnions_WalksOnlyWhatCanNameAType holds the walk for references to
// what can hold a TypeID: a model whose property also carries provenance, kept
// constructs, a constraint and a name in every channel shows it no more values
// than one carrying none of them, while a doc ref, which can name a type, is
// shown.
func TestConstUnions_WalksOnlyWhatCanNameAType(t *testing.T) {
	t.Parallel()
	model := func() *ir.Model {
		return &ir.Model{ID: "t/x/M",
			Properties: []ir.Property{{ID: "p/x/M/p", Type: ir.TypeRef{Target: "t/x/U/0"}}}}
	}
	bare := pass.TypeRefVisits(withTypes(model()), "t/x/U/0")
	require.Positive(t, bare)

	full := model()
	full.Provenance = ir.Provenance{Pointer: "/components/schemas/M", Node: "n", Inferred: "a-heuristic"}
	full.Unmodeled = ir.Unmodeled{"openapi:x-a": {Reason: ir.ReasonVendorExtension}}
	full.Name = ir.Naming{Source: "M", Canonical: "m", Hint: "m", Aliases: []string{"N"}}
	full.Properties[0].Constraints = &ir.Constraints{Pattern: "^a$"}
	assert.Equal(t, bare, pass.TypeRefVisits(withTypes(full), "t/x/U/0"), "the walk entered what names no type")

	carrying := model()
	carrying.Docs.Refs = []ir.DocRef{{Type: "t/x/U/0"}}
	assert.Greater(t, pass.TypeRefVisits(withTypes(carrying), "t/x/U/0"), bare, "the walk passed over a doc ref")
}

// TestConstUnions_ReadsEachGoTypeOnce pins what the walk's pruning costs:
// whether a Go type can hold a TypeID is read once, however many values have
// it, so a document of many models reads no more types than one of one.
func TestConstUnions_ReadsEachGoTypeOnce(t *testing.T) {
	t.Parallel()
	models := make([]ir.TypeDef, 0, 50)
	for i := range cap(models) {
		models = append(models, &ir.Model{ID: ir.TypeID(fmt.Sprintf("t/x/M%d", i)),
			Properties: []ir.Property{{ID: ir.PropID(fmt.Sprintf("p/x/M%d/p", i)), Type: ir.TypeRef{Target: "t/x/U/0"}}}})
	}
	one := pass.TypeReads(withTypes(models[0]), "t/x/U/0")
	require.Positive(t, one)

	assert.Equal(t, one, pass.TypeReads(withTypes(models...), "t/x/U/0"))
}

// TestConstUnions_RemovesNothingPastTheWalksReach pins that a walk the depth
// cap cut short, which may not have reached every reference, removes no
// branch: the union is still replaced, its branches stay.
func TestConstUnions_RemovesNothingPastTheWalksReach(t *testing.T) {
	t.Parallel()
	doc := constDoc(union("t/x/U", "t/x/U/0"), lit("t/x/U/0", "red"))
	deep := ir.OperationGroup{ID: "g/x/deep"}
	for range ir.MaxWalkDepth {
		deep = ir.OperationGroup{ID: "g/x/deep", Groups: []ir.OperationGroup{deep}}
	}
	doc.Services = []ir.Service{{ID: "s/x", Groups: []ir.OperationGroup{deep}}}
	out := collapsed(t, doc)

	assert.Equal(t, ir.KindEnum, out.Types["t/x/U"].Kind())
	assert.Contains(t, out.Types, ir.TypeID("t/x/U/0"))
}

// TestConstUnions_IsIdempotent pins that what the pass returns has no union
// left for it: a second run hands its input back, where a union names a union
// of one value, which the first run rewrites with it, one of several values,
// which it takes whole in the same run, and a branch of several values another
// union names too, which it leaves, alone or beside a union it does rewrite. A
// removed branch whose docs name a branch of several values is no reference
// that goes: its docs, and the reference, move onto its member.
func TestConstUnions_IsIdempotent(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		doc      *ir.Document
		rewrites bool
	}{
		{"flat", constDoc(union("t/x/U", "t/x/U/0", "t/x/U/1"), lit("t/x/U/0", "red"), pinned("t/x/U/1", "green")), true},
		{"a union of one value", constDoc(union("t/x/U", "t/x/U/0", "t/x/U/1"), anonymousUnion("t/x/U/0", "t/x/U/0/0"),
			lit("t/x/U/0/0", "red"), pinned("t/x/U/1", "green")), true},
		{"a union of several values", constDoc(union("t/x/U", "t/x/U/0", "t/x/U/1"),
			anonymousUnion("t/x/U/0", "t/x/U/0/0", "t/x/U/0/1"), lit("t/x/U/0/0", "red"), lit("t/x/U/0/1", "blue"),
			pinned("t/x/U/1", "green")), true},
		{"a branch of several values a second union names", constDoc(union("t/x/U", "t/x/U/0", "t/x/U/1"),
			lit("t/x/U/0", "red"), pinned("t/x/U/1", "green", "blue"), union("t/x/V", "t/x/U/1")), false},
		{"a branch of several values a second union names beside a union rewritten", constDoc(
			union("t/x/U", "t/x/U/0", "t/x/U/1"), lit("t/x/U/0", "red"), pinned("t/x/U/1", "green", "blue"),
			union("t/x/V", "t/x/U/1"), union("t/x/W", "t/x/W/0"), lit("t/x/W/0", "up")), true},
		{"a removed branch whose docs name a branch of several values", docNaming(), true},
	} {
		once := collapsed(t, tt.doc)
		if tt.rewrites {
			require.NotSame(t, tt.doc, once, "%s: the first run rewrites a union", tt.name)
		} else {
			require.Same(t, tt.doc, once, "%s: the first run rewrites none", tt.name)
		}
		assert.Same(t, once, collapsed(t, once), tt.name)
	}
}

// docNaming is a document whose union V names the branch of several values M
// beside a literal, and whose union U rewrites with a branch of one value that
// has docs naming M.
func docNaming() *ir.Document {
	described := lit("t/x/U/0", "red")
	described.Docs = ir.Docs{Refs: []ir.DocRef{{Type: "t/x/M"}}}
	return constDoc(union("t/x/U", "t/x/U/0"), described,
		union("t/x/V", "t/x/M", "t/x/V/0"), pinned("t/x/M", "a", "b"), lit("t/x/V/0", "c"))
}

// TestConstUnions_KeepsAUnionWhoseNestedBranchRepeatsAValue pins that the values
// a branch took of its own are values of the branch: a union naming it beside a
// value one of them holds stays, however deep the branch took it, and the branch
// is the Enum of its own.
func TestConstUnions_KeepsAUnionWhoseNestedBranchRepeatsAValue(t *testing.T) {
	t.Parallel()
	root, mid, deep := union("t/x/R", "t/x/M", "t/x/R/0"), anonymousUnion("t/x/M", "t/x/D", "t/x/M/0"),
		anonymousUnion("t/x/D", "t/x/D/0", "t/x/D/1", "t/x/D/2")
	repeat := lit("t/x/R/0", "e")
	out := collapsed(t, constDoc(root, repeat, mid, lit("t/x/M/0", "e"), deep,
		lit("t/x/D/0", "a"), lit("t/x/D/1", "b"), lit("t/x/D/2", "c")))

	assert.Empty(t, cmp.Diff(ir.TypeRegistry{root.ID: root, repeat.ID: repeat,
		mid.ID: enumFor(mid, member(mid.ID, "a"), member(mid.ID, "b"), member(mid.ID, "c"), member(mid.ID, "e"))}, out.Types))
}

// TestConstUnions_CountsTheValuesOfAUnionNotItsVariants pins that a union of one
// variant naming several values is a branch of several, whose docs a variant
// would say of them all: the union stays, and the branch is the Enum of its own.
func TestConstUnions_CountsTheValuesOfAUnionNotItsVariants(t *testing.T) {
	t.Parallel()
	u, branch := union("t/x/U", "t/x/B"), anonymousUnion("t/x/B", "t/x/B/0")
	u.Variants[0].Docs = ir.Docs{Description: "Colors."}
	out := collapsed(t, constDoc(u, branch, pinned("t/x/B/0", "green", "blue")))

	assert.Empty(t, cmp.Diff(ir.TypeRegistry{u.ID: u,
		branch.ID: enumFor(branch, member(branch.ID, "green"), member(branch.ID, "blue"))}, out.Types))
}

// TestConstUnions_CopiesAUnionOfOneValueToEveryUnionNamingIt pins that a union
// of one value, which an Enum of several would be copied into each union naming
// it, is copied here whoever names it: a value costs no more than a reference.
func TestConstUnions_CopiesAUnionOfOneValueToEveryUnionNamingIt(t *testing.T) {
	t.Parallel()
	u, v := union("t/x/U", "t/x/I"), union("t/x/V", "t/x/I", "t/x/V/0")
	out := collapsed(t, constDoc(u, v, anonymousUnion("t/x/I", "t/x/I/0"), lit("t/x/I/0", "up"), lit("t/x/V/0", "down")))

	assert.Empty(t, cmp.Diff(ir.TypeRegistry{u.ID: enumFor(u, member(u.ID, "up")),
		v.ID: enumFor(v, member(v.ID, "up"), member(v.ID, "down"))}, out.Types))
}

// TestConstUnions_KeepsABranchOfSeveralValuesNamedTwice pins that a branch of
// several values two variants name is taken by neither, whether it is an Enum
// or a union, which reads as an Enum alone: copying it into both is the
// growth taking it only where it is named once avoids.
func TestConstUnions_KeepsABranchOfSeveralValuesNamedTwice(t *testing.T) {
	t.Parallel()
	t.Run("an enum", func(t *testing.T) {
		t.Parallel()
		doc := constDoc(union("t/x/U", "t/x/B"), union("t/x/V", "t/x/B", "t/x/V/0"), lit("t/x/V/0", "c"),
			pinned("t/x/B", "a", "b"))

		assert.Same(t, doc, collapsed(t, doc))
	})
	t.Run("a union", func(t *testing.T) {
		t.Parallel()
		b := anonymousUnion("t/x/B", "t/x/B/0", "t/x/B/1")
		u, v := union("t/x/U", "t/x/B"), union("t/x/V", "t/x/B", "t/x/V/0")
		out := collapsed(t, constDoc(u, v, lit("t/x/V/0", "c"), b, lit("t/x/B/0", "a"), lit("t/x/B/1", "b")))

		assert.Empty(t, cmp.Diff(ir.TypeRegistry{u.ID: u, v.ID: v, "t/x/V/0": lit("t/x/V/0", "c"),
			b.ID: enumFor(b, member(b.ID, "a"), member(b.ID, "b"))}, out.Types), "the branch is an Enum of its own, and both unions stay")
	})
}

// TestConstUnions_ReadsEachUnionOnce pins the cost of reading the unions: a
// union two others name as a branch, and each reads first, is read once.
func TestConstUnions_ReadsEachUnionOnce(t *testing.T) {
	t.Parallel()
	doc := constDoc(anonymousUnion("t/x/I", "t/x/I/0", "t/x/I/1"), lit("t/x/I/0", "up"), lit("t/x/I/1", "down"),
		union("t/x/A", "t/x/I", "t/x/A/0"), lit("t/x/A/0", "left"),
		union("t/x/B", "t/x/I", "t/x/B/0"), lit("t/x/B/0", "right"))

	assert.Equal(t, 3, pass.UnionReads(doc))
}

// TestConstUnions_ReturnsItsInputOnceItsContextIsDone cancels a Run at every
// check of its context a whole one makes, its walks' included, those that count
// the references to a branch of several values among them, and requires the
// document it was handed back with nothing reported.
func TestConstUnions_ReturnsItsInputOnceItsContextIsDone(t *testing.T) {
	t.Parallel()
	for name, doc := range map[string]*ir.Document{
		"consts": constDoc(union("t/x/U", "t/x/U/0", "t/x/U/1"), lit("t/x/U/0", "red"), lit("t/x/U/1", "green")),
		"a branch of several values": constDoc(union("t/x/U", "t/x/U/0", "t/x/U/1"), lit("t/x/U/0", "red"),
			pinned("t/x/U/1", "green", "blue")),
	} {
		whole := &ctxtest.Countdown{Context: t.Context(), Left: math.MaxInt}
		out, _ := pass.ConstUnions{}.Run(whole, doc)
		require.NotSame(t, doc, out, "%s: an uncancelled run replaces the union", name)
		require.Greater(t, whole.Calls, 1, "%s: the walk asks too", name)

		for left := range whole.Calls {
			ctx := &ctxtest.Countdown{Context: t.Context(), Left: left}

			out, diags := pass.ConstUnions{}.Run(ctx, doc)

			assert.Same(t, doc, out, "%s: check %d", name, left+1)
			assert.Nil(t, diags, "%s: check %d", name, left+1)
		}
	}
}

// TestConstUnions_WalksOnlyWhereItCounts pins the walks a run makes: none for a
// document with no union to rewrite, the one that finds the branches nothing
// references for consts, and one more to count the references to a branch of
// several values only where a union names one, rewritten or not.
func TestConstUnions_WalksOnlyWhereItCounts(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		doc   *ir.Document
		walks int
	}{
		{"no union", constDoc(&ir.Model{ID: "t/x/M"}), 0},
		{"consts", constDoc(union("t/x/U", "t/x/U/0", "t/x/U/1"), lit("t/x/U/0", "red"), lit("t/x/U/1", "green")), 1},
		{"branches of one value", constDoc(union("t/x/U", "t/x/U/0", "t/x/U/1"), pinned("t/x/U/0", "red"),
			pinned("t/x/U/1", "green")), 1},
		{"a union of one value", constDoc(union("t/x/U", "t/x/U/0"), anonymousUnion("t/x/U/0", "t/x/U/0/0"),
			lit("t/x/U/0/0", "red")), 2},
		{"a branch of several values", constDoc(union("t/x/U", "t/x/U/0", "t/x/U/1"), lit("t/x/U/0", "red"),
			pinned("t/x/U/1", "green", "blue")), 2},
		{"a union of several values", constDoc(union("t/x/U", "t/x/U/0"), anonymousUnion("t/x/U/0", "t/x/U/0/0", "t/x/U/0/1"),
			lit("t/x/U/0/0", "red"), lit("t/x/U/0/1", "blue")), 2},
		{"a branch of several values no union takes", constDoc(union("t/x/U", "t/x/B"), union("t/x/V", "t/x/B"),
			pinned("t/x/B", "red", "blue")), 1},
		{"an enum of several values no union names", constDoc(union("t/x/U", "t/x/U/0"), lit("t/x/U/0", "red"),
			pinned("t/x/E", "green", "blue")), 1},
		{"a named enum of several values", constDoc(union("t/x/U", "t/x/B"), named3(pinned("t/x/B", "red", "blue"))), 0},
		{"a union off its key", offKey(constDoc(union("t/x/U", "t/x/B"), anonymousUnion("t/x/B", "t/x/B/0"),
			lit("t/x/B/0", "red"))), 0},
	} {
		assert.Equal(t, tt.walks, pass.ConstWalks(t, tt.doc), tt.name)
	}
}

// named3 is e as a named type, which a union cannot take the values of.
func named3(e *ir.Enum) *ir.Enum {
	e.Anonymous, e.Name = false, named("B")
	return e
}

// offKey is doc with its union B, keyed as it is, holding another ID.
func offKey(doc *ir.Document) *ir.Document {
	b, _ := doc.Types["t/x/B"].(*ir.Union)
	b.ID = "t/x/Other"
	return doc
}

// TestConstUnions_BuildsEachMemberOnce pins what reading a chain of unions
// costs, each naming the next once beside a value of its own: the Enum of the
// first holds every value, and no other is built, so the members built grow
// with the chain's length, as the unions read do, and not with its square, as
// they would if each union built the Enum of the one below.
func TestConstUnions_BuildsEachMemberOnce(t *testing.T) {
	t.Parallel()
	built := func(links int) int {
		doc := chain(links)
		out := collapsed(t, doc)

		enum, ok := out.Types["t/x/X0"].(*ir.Enum)
		require.True(t, ok, "the first union is an Enum")
		assert.Len(t, enum.Members, links+1)
		assert.Len(t, out.Types, 1, "no other union is left")
		assert.Equal(t, links, pass.UnionReads(doc))
		return pass.ConstBuilt(t, doc)
	}
	const links = 64
	one, two, four := built(links), built(2*links), built(4*links)

	assert.Equal(t, links+1, one)
	assert.LessOrEqual(t, four-two, (two-one)*5/2, "the marginal growth is constant: %d, %d, %d", one, two, four)
}

// chain is the document of the unions X0 to Xlinks-1, each naming the next once
// beside a string of its own, the last holding two, so that the first holds
// links+1 values.
func chain(links int) *ir.Document {
	types := make([]ir.TypeDef, 0, 2*links+1)
	for i := range links {
		id, own := ir.TypeID(fmt.Sprintf("t/x/X%d", i)), ir.TypeID(fmt.Sprintf("t/x/X%d/own", i))
		targets := []ir.TypeID{own, own + "/more"}
		if i < links-1 {
			targets = []ir.TypeID{ir.TypeID(fmt.Sprintf("t/x/X%d", i+1)), own}
		}
		u := anonymousUnion(id, targets...)
		if i == 0 {
			u = union(id, targets...)
		}
		types = append(types, u, lit(own, fmt.Sprintf("v%d", i)))
	}
	last := ir.TypeID(fmt.Sprintf("t/x/X%d/own/more", links-1))
	return constDoc(append(types, lit(last, "last"))...)
}

// TestConstUnions_AddsEachKeyOnce pins what checking the values of a chain of
// unions distinct costs, each naming the next beside a branch of several values
// of its own: a union takes over the largest set of keys a part holds and adds
// the others to it, so the keys added grow with the chain's length, and not with
// its square, as they would if it added the largest to a smaller.
func TestConstUnions_AddsEachKeyOnce(t *testing.T) {
	t.Parallel()
	moved := func(links int) int {
		types := make([]ir.TypeDef, 0, 2*links)
		for i := range links {
			id, own := ir.TypeID(fmt.Sprintf("t/x/X%d", i)), ir.TypeID(fmt.Sprintf("t/x/X%d/own", i))
			targets := []ir.TypeID{ir.TypeID(fmt.Sprintf("t/x/X%d", i+1)), own}
			if i == links-1 {
				targets = []ir.TypeID{own}
			}
			u := anonymousUnion(id, targets...)
			if i == 0 {
				u = union(id, targets...)
			}
			types = append(types, u, pinned(own, fmt.Sprintf("a%d", i), fmt.Sprintf("b%d", i)))
		}
		doc := constDoc(types...)
		require.Len(t, collapsed(t, doc).Types, 1, "every link is taken")
		return pass.ConstMoved(t, doc)
	}
	const links = 64
	one, two, four := moved(links), moved(2*links), moved(4*links)

	assert.LessOrEqual(t, four-two, (two-one)*5/2, "the marginal growth is constant: %d, %d, %d", one, two, four)
}

// TestConstUnions_BuildsNoMemberForABranchManyName pins the growth a branch of
// several values many unions name would cost if each were its Enum: none is
// rewritten and no member is built, however many name the one branch.
func TestConstUnions_BuildsNoMemberForABranchManyName(t *testing.T) {
	t.Parallel()
	const unions, values = 40, 25
	branch := pinned("t/x/B", "v0")
	for i := 1; i < values; i++ {
		branch.Members = append(branch.Members, member(branch.ID, fmt.Sprintf("v%d", i)))
	}
	types := make([]ir.TypeDef, 0, 1+unions)
	types = append(types, branch)
	for i := range unions {
		types = append(types, union(ir.TypeID(fmt.Sprintf("t/x/U%d", i)), branch.ID))
	}
	doc := constDoc(types...)

	assert.Same(t, doc, collapsed(t, doc), "every union stays")
	assert.Zero(t, pass.ConstBuilt(t, doc))
}

// TestConstUnions_TakesNoBranchPastTheWalksReach pins that a walk the depth
// cap cut short, whose count of the references to a branch of several values
// may be too low, takes none: the document is handed back, where the same
// document it did not cut short has the branch's union rewritten.
func TestConstUnions_TakesNoBranchPastTheWalksReach(t *testing.T) {
	t.Parallel()
	doc := constDoc(union("t/x/U", "t/x/U/0"), pinned("t/x/U/0", "red", "green"))
	rewritten := collapsed(t, doc)
	require.Equal(t, ir.KindEnum, rewritten.Types["t/x/U"].Kind())

	deep := ir.OperationGroup{ID: "g/x/deep"}
	for range ir.MaxWalkDepth {
		deep = ir.OperationGroup{ID: "g/x/deep", Groups: []ir.OperationGroup{deep}}
	}
	doc.Services = []ir.Service{{ID: "s/x", Groups: []ir.OperationGroup{deep}}}

	assert.Same(t, doc, collapsed(t, doc))
}

// TestConstUnions_ReportsAMemberNoIDFits pins the Enum built under a root ID no
// member can take: it reports so, where a root the pass reads has one.
func TestConstUnions_ReportsAMemberNoIDFits(t *testing.T) {
	t.Parallel()
	members, ok := pass.MembersUnder("t/x/U", "a", "b")
	require.True(t, ok)
	assert.Equal(t, []ir.EnumMember{
		{ID: "e/x/U/s:a", Value: str("a")}, {ID: "e/x/U/s:b", Value: str("b")}}, members)

	_, ok = pass.MembersUnder("U", "a")
	assert.False(t, ok)
}

// TestConstUnions_HandsBackNoDocument pins the nil document, which holds no
// union to replace.
func TestConstUnions_HandsBackNoDocument(t *testing.T) {
	t.Parallel()
	out, diags := pass.ConstUnions{}.Run(t.Context(), nil)
	assert.Nil(t, out)
	assert.Nil(t, diags)
}

func TestConstUnions_NameIsStable(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "const-unions", pass.ConstUnions{}.Name())
	assert.Equal(t, pass.ConstUnionsName, pass.ConstUnions{}.Name())
}
