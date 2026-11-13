// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"fmt"
	"maps"
	"math"
	"slices"
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
// what its variant, its branch and the branch's member say of it. A branch
// holding several values that only the union names gives a member for each,
// holding what that value's member says, and a branch that is a union is read
// as the values it holds, in the same run. The branch types go with the union.
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
			return union("t/x/U", "t/x/U/0", "t/x/U/1"),
				[]ir.TypeDef{anonymousUnion("t/x/U/0", "t/x/U/0/0"), lit("t/x/U/0/0", "up"), lit("t/x/U/1", "down")}
		}, func(u *ir.Union) []ir.EnumMember {
			return []ir.EnumMember{member(u.ID, "up"), member(u.ID, "down")}
		}},
		{"an enum of several values only the union names", func() (*ir.Union, []ir.TypeDef) {
			u := union("t/x/U", "t/x/U/0", "t/x/U/1")
			u.Exclusive = false
			return u, []ir.TypeDef{pinned("t/x/U/0", "small", "large"), pinned("t/x/U/1", "")}
		}, func(u *ir.Union) []ir.EnumMember {
			return []ir.EnumMember{member(u.ID, "small"), member(u.ID, "large"), member(u.ID, "")}
		}},
		{"a lone branch of several values", func() (*ir.Union, []ir.TypeDef) {
			return union("t/x/U", "t/x/U/0"), []ir.TypeDef{pinned("t/x/U/0", "s", "m", "l")}
		}, func(u *ir.Union) []ir.EnumMember {
			return []ir.EnumMember{member(u.ID, "s"), member(u.ID, "m"), member(u.ID, "l")}
		}},
		{"what each member of a branch of several says", func() (*ir.Union, []ir.TypeDef) {
			several := pinned("t/x/U/1", "green", "blue")
			several.Members[0].Docs, several.Members[1].Deprecation = ir.Docs{Summary: "Green"}, deprecated
			several.Members[1].Unmodeled = unmodeled
			return union("t/x/U", "t/x/U/0", "t/x/U/1"), []ir.TypeDef{lit("t/x/U/0", "red"), several}
		}, func(u *ir.Union) []ir.EnumMember {
			green, blue := member(u.ID, "green"), member(u.ID, "blue")
			green.Docs, blue.Deprecation, blue.Unmodeled = ir.Docs{Summary: "Green"}, deprecated, unmodeled
			return []ir.EnumMember{member(u.ID, "red"), green, blue}
		}},
		{"a union of several values only the union names", func() (*ir.Union, []ir.TypeDef) {
			return union("t/x/U", "t/x/U/0", "t/x/U/1"), []ir.TypeDef{
				lit("t/x/U/0", "left"), anonymousUnion("t/x/U/1", "t/x/U/1/0", "t/x/U/1/1"),
				lit("t/x/U/1/0", "up"), pinned("t/x/U/1/1", "down", "out"),
			}
		}, func(u *ir.Union) []ir.EnumMember {
			return []ir.EnumMember{member(u.ID, "left"), member(u.ID, "up"), member(u.ID, "down"), member(u.ID, "out")}
		}},
		{"two unions of several values only the union names", func() (*ir.Union, []ir.TypeDef) {
			return union("t/x/U", "t/x/U/0", "t/x/U/1"), []ir.TypeDef{
				anonymousUnion("t/x/U/0", "t/x/U/0/0", "t/x/U/0/1"), lit("t/x/U/0/0", "a"), lit("t/x/U/0/1", "b"),
				anonymousUnion("t/x/U/1", "t/x/U/1/0", "t/x/U/1/1"), lit("t/x/U/1/0", "c"), pinned("t/x/U/1/1", "d", "e"),
			}
		}, func(u *ir.Union) []ir.EnumMember {
			return []ir.EnumMember{member(u.ID, "a"), member(u.ID, "b"), member(u.ID, "c"), member(u.ID, "d"),
				member(u.ID, "e")}
		}},
		{"a union of one value saying something of it", func() (*ir.Union, []ir.TypeDef) {
			nested := anonymousUnion("t/x/U/0", "t/x/U/0/0")
			nested.Docs = ir.Docs{Summary: "Up"}
			return union("t/x/U", "t/x/U/0", "t/x/U/1"), []ir.TypeDef{nested, lit("t/x/U/0/0", "up"), lit("t/x/U/1", "down")}
		}, func(u *ir.Union) []ir.EnumMember {
			up := member(u.ID, "up")
			up.Docs = ir.Docs{Summary: "Up"}
			return []ir.EnumMember{up, member(u.ID, "down")}
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

// TestConstUnions_KeepsAUnionAnEnumCannotSay pins each union the pass leaves:
// one saying what an Enum has no field for, a variant or branch saying more
// than its value, a branch of another kind or none, values of different types
// or a value twice, a fact two places say of one value, and a branch of
// several values that another node, or a member of it, names, or that it or
// its variant says something of them all. Each sits beside a union the pass
// rewrites, so the document is read, and stays as it was but for that one.
func TestConstUnions_KeepsAUnionAnEnumCannotSay(t *testing.T) {
	t.Parallel()
	several := func(e *ir.Enum) { e.Members = append(e.Members, member(e.ID, "blue")) }
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
		{"an enum of several values another node names", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, types ir.TypeRegistry) {
			several(green)
			types["t/x/M"] = &ir.Model{ID: "t/x/M",
				Properties: []ir.Property{{ID: "p/x/M/p", Type: ir.TypeRef{Target: green.ID}}}}
		}},
		{"an enum of several values another union names", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, types ir.TypeRegistry) {
			several(green)
			types["t/x/V"] = union("t/x/V", green.ID)
		}},
		{"an enum of several values a member of which another node names", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, types ir.TypeRegistry) {
			several(green)
			types["t/x/M"] = &ir.Model{ID: "t/x/M",
				Deprecation: &ir.Deprecation{ReplacedBy: []ir.EntityRef{{Member: green.Members[1].ID}}}}
		}},
		{"an enum of several values its variant says something of", func(u *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			several(green)
			u.Variants[1].Docs = ir.Docs{Summary: "Cool colours."}
		}},
		{"an enum of several values that says something of them all", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			several(green)
			green.Deprecation = &ir.Deprecation{}
		}},
		{"an enum of several values a vendor extension marks", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			several(green)
			green.Unmodeled = ir.Unmodeled{"openapi:x-a": {Reason: ir.ReasonVendorExtension}}
		}},
		{"an enum of several values holding another branch's value", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			green.Members = append(green.Members, member(green.ID, "red"))
		}},
		{"an enum of several values typed apart", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			several(green)
			green.ValueType = ir.PrimDate
		}},
		{"an enum of several values one of which is no string", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			several(green)
			green.Members[1].Value = ir.Value{Kind: ir.ValueBool, Bool: true}
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
			kept := maps.Clone(doc.Types)
			doc.Types["t/x/R"], doc.Types["t/x/R/0"] = union("t/x/R", "t/x/R/0"), lit("t/x/R/0", "beside")
			out := collapsed(t, doc)

			for id, td := range kept {
				assert.Same(t, td, out.Types[id], "%s is kept", id)
			}
			assert.Equal(t, ir.KindEnum, out.Types["t/x/R"].Kind(), "the union beside it is read and rewritten")
			assert.Len(t, out.Types, len(kept)+1, "only the branch of the union beside it goes")
		})
	}
}

// TestConstUnions_RewritesAUnionOfSeveralValuesItsUnionCannotTake pins a union
// naming a union of several values it cannot take, as another node names that
// union or it says something of all its values: the union stays, naming the
// Enum the union it names becomes, whose branches go, and a second run finds
// nothing left to rewrite.
func TestConstUnions_RewritesAUnionOfSeveralValuesItsUnionCannotTake(t *testing.T) {
	t.Parallel()
	for name, write := range map[string]func(doc *ir.Document, nested *ir.Union){
		"another node names it": func(doc *ir.Document, nested *ir.Union) {
			doc.Types["t/x/M"] = &ir.Model{ID: "t/x/M", Docs: ir.Docs{Refs: []ir.DocRef{{Type: nested.ID}}}}
		},
		"it says something of them all": func(_ *ir.Document, nested *ir.Union) {
			nested.Docs = ir.Docs{Description: "Either way."}
		},
	} {
		nested := anonymousUnion("t/x/U/1", "t/x/U/1/0", "t/x/U/1/1")
		doc := constDoc(union("t/x/U", "t/x/U/0", "t/x/U/1"), lit("t/x/U/0", "left"),
			nested, lit("t/x/U/1/0", "up"), lit("t/x/U/1/1", "down"))
		write(doc, nested)
		once := collapsed(t, doc)

		assert.Same(t, doc.Types["t/x/U"], once.Types["t/x/U"], "%s: the union naming it stays", name)
		assert.Empty(t, cmp.Diff(enumFor(nested, member(nested.ID, "up"), member(nested.ID, "down")),
			once.Types[nested.ID]), name)
		assert.NotContains(t, once.Types, ir.TypeID("t/x/U/1/0"), name)
		assert.Same(t, once, collapsed(t, once), "%s: a second run rewrites nothing", name)
	}
}

// TestConstUnions_KeepsAUnionWhoseUnionsOfSeveralValuesShareOne pins a union
// naming two unions of several values, each named nowhere else, that hold one
// value between them: the union stays, as it would hold the value twice, and
// each union it names becomes the Enum of its own values, read as it is
// whichever is the larger.
func TestConstUnions_KeepsAUnionWhoseUnionsOfSeveralValuesShareOne(t *testing.T) {
	t.Parallel()
	for _, sets := range [][2][]string{{{"a", "b", "c"}, {"d", "a"}}, {{"d", "a"}, {"a", "b", "c"}}} {
		u := union("t/x/U", "t/x/U/0", "t/x/U/1")
		doc := constDoc(u)
		for i, values := range sets {
			id := u.Variants[i].Type.Target
			doc.Types[id] = anonymousUnion(id, id+"/0", id+"/1")
			doc.Types[id+"/0"], doc.Types[id+"/1"] = lit(id+"/0", values[0]), pinned(id+"/1", values[1:]...)
		}
		out := collapsed(t, doc)

		assert.Same(t, u, out.Types[u.ID], "%v", sets)
		for i, values := range sets {
			nested, ok := doc.Types[u.Variants[i].Type.Target].(*ir.Union)
			require.True(t, ok)
			members := make([]ir.EnumMember, 0, len(values))
			for _, v := range values {
				members = append(members, member(nested.ID, v))
			}
			assert.Empty(t, cmp.Diff(enumFor(nested, members...), out.Types[nested.ID]), "%v", sets)
		}
	}
}

// TestConstUnions_CopiesAUnionOfOneValueAnotherNodeNames pins a union naming a
// union of one value that another node names too: one value copies into each
// Enum naming it at no cost a document can multiply, so the union becomes the
// Enum of its values, and the union it names its own Enum, which stays.
func TestConstUnions_CopiesAUnionOfOneValueAnotherNodeNames(t *testing.T) {
	t.Parallel()
	u, nested := union("t/x/U", "t/x/U/0", "t/x/U/1"), anonymousUnion("t/x/U/0", "t/x/U/0/0")
	doc := constDoc(u, nested, lit("t/x/U/0/0", "up"), lit("t/x/U/1", "down"))
	doc.Types["t/x/M"] = &ir.Model{ID: "t/x/M",
		Properties: []ir.Property{{ID: "p/x/M/p", Type: ir.TypeRef{Target: nested.ID}}}}
	out := collapsed(t, doc)

	assert.Empty(t, cmp.Diff(enumFor(u, member(u.ID, "up"), member(u.ID, "down")), out.Types[u.ID]))
	assert.Empty(t, cmp.Diff(enumFor(nested, member(nested.ID, "up")), out.Types[nested.ID]))
	assert.NotContains(t, out.Types, ir.TypeID("t/x/U/0/0"))
	assert.NotContains(t, out.Types, ir.TypeID("t/x/U/1"))
}

// TestConstUnions_KeepsAUnionWhoseMembersHaveNoNamespace pins the union a
// document declaring no member namespace for its own keeps: an Enum's members
// would take IDs in a namespace nobody declared, which the edit refuses, so the
// union stays and nothing is reported. So does a union whose ID names no
// namespace, which gives its members none, even where the document declares
// the empty one.
func TestConstUnions_KeepsAUnionWhoseMembersHaveNoNamespace(t *testing.T) {
	t.Parallel()
	for _, spaces := range []map[string][]string{nil, {ir.IDKindType: {"x"}, ir.IDKindEnumMember: {"y"}}} {
		doc := constDoc(union("t/x/U", "t/x/U/0"), lit("t/x/U/0", "red"))
		doc.IDSpaces = spaces
		assert.Same(t, doc, collapsed(t, doc), "member namespaces %v", spaces)
	}
	doc := constDoc(union("U", "t/x/U/0"), lit("t/x/U/0", "red"))
	doc.IDSpaces[ir.IDKindEnumMember] = []string{"x", ""}
	assert.Same(t, doc, collapsed(t, doc), "a union whose ID names no namespace")
}

// TestConstUnions_RemovesOnlyTheBranchesNothingElseReferences pins which
// branch types go with their union: those nothing else names, a union
// collapsed beside it included. A reference anywhere else keeps one, in a
// TypeRef, a DocRef, a value, another map's key, a member's ID, or a union
// that stays.
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
		{"a member's ID", func(doc *ir.Document) {
			doc.Types["t/x/M"] = &ir.Model{ID: "t/x/M",
				Deprecation: &ir.Deprecation{ReplacedBy: []ir.EntityRef{{Member: member("t/x/U/0", "red").ID}}}}
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
			doc := constDoc(union("t/x/U", "t/x/U/0", "t/x/U/1"), pinned("t/x/U/0", "red"), lit("t/x/U/1", "green"))
			tt.refs(doc)
			out := collapsed(t, doc)

			require.Equal(t, ir.KindEnum, out.Types["t/x/U"].Kind())
			assert.NotContains(t, out.Types, ir.TypeID("t/x/U/1"), "a branch nothing else names goes")
			_, kept := out.Types["t/x/U/0"]
			assert.Equal(t, tt.kept, kept, "the branch named elsewhere is kept")
		})
	}
}

// TestConstUnions_ReadsAUnionTwoKeysHoldAsTwoReferences pins a union the
// registry holds at its own key and, through one pointer, at another, which a
// walk meets once: its branches are named twice, so none of them goes with its
// own key's Enum, and a branch of several values stays with the union.
func TestConstUnions_ReadsAUnionTwoKeysHoldAsTwoReferences(t *testing.T) {
	t.Parallel()
	u := union("t/x/U", "t/x/U/0", "t/x/U/1")
	doc := constDoc(u, lit("t/x/U/0", "red"), lit("t/x/U/1", "green"))
	doc.Types["t/x/W"] = u
	out := collapsed(t, doc)

	assert.Equal(t, ir.KindEnum, out.Types["t/x/U"].Kind())
	assert.Same(t, u, out.Types["t/x/W"])
	assert.Contains(t, out.Types, ir.TypeID("t/x/U/0"))
	assert.Contains(t, out.Types, ir.TypeID("t/x/U/1"))

	several := constDoc(u, lit("t/x/U/0", "red"), pinned("t/x/U/1", "green", "blue"))
	several.Types["t/x/W"] = u
	assert.Same(t, several, collapsed(t, several))
}

// TestConstUnions_WatchesOnlyTheBranchesOfAUnionThatMayBeAnEnum pins what
// spares a document the walk: no union at its own key that is closed, says
// nothing beside its variants and names an anonymous Literal, Enum or union at
// each. Such a union's branches are watched, and a union it names watches its
// own.
func TestConstUnions_WatchesOnlyTheBranchesOfAUnionThatMayBeAnEnum(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		write func(u *ir.Union, types ir.TypeRegistry)
		want  int
	}{
		{"a union that may be", func(*ir.Union, ir.TypeRegistry) {}, 2},
		{"one naming a union that may be", func(_ *ir.Union, types ir.TypeRegistry) {
			types["t/x/U/1"], types["t/x/U/1/0"] = anonymousUnion("t/x/U/1", "t/x/U/1/0"), lit("t/x/U/1/0", "green")
		}, 3},
		{"an open union", func(u *ir.Union, _ ir.TypeRegistry) { u.Closed = false }, 0},
		{"a tagged union", func(u *ir.Union, _ ir.TypeRegistry) { u.WireTagged = true }, 0},
		{"no variant", func(u *ir.Union, _ ir.TypeRegistry) { u.Variants = nil }, 0},
		{"a named branch", func(_ *ir.Union, types ir.TypeRegistry) { types["t/x/U/0"].Common().Anonymous = false }, 0},
		{"a branch of another kind", func(_ *ir.Union, types ir.TypeRegistry) {
			types["t/x/U/0"] = &ir.Model{ID: "t/x/U/0", Anonymous: true}
		}, 0},
		{"a nil branch", func(_ *ir.Union, types ir.TypeRegistry) { types["t/x/U/1"] = (*ir.Enum)(nil) }, 0},
		{"no branch", func(_ *ir.Union, types ir.TypeRegistry) { delete(types, "t/x/U/0") }, 0},
		{"a union off its key", func(u *ir.Union, types ir.TypeRegistry) {
			delete(types, u.ID)
			types["t/x/W"] = u
		}, 0},
		{"a nil union", func(u *ir.Union, types ir.TypeRegistry) { types[u.ID] = (*ir.Union)(nil) }, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			u := union("t/x/U", "t/x/U/0", "t/x/U/1")
			doc := constDoc(u, lit("t/x/U/0", "red"), pinned("t/x/U/1", "green"))
			tt.write(u, doc.Types)

			assert.Equal(t, tt.want, pass.Watched(doc))
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
// of one value or of several, which the first run rewrites with it, a chain of
// unions each naming the next, and a branch of several values another node
// names, which keeps its union on both runs as the first rewrites another.
func TestConstUnions_IsIdempotent(t *testing.T) {
	t.Parallel()
	shared := constDoc(union("t/x/U", "t/x/U/0", "t/x/U/1"), pinned("t/x/U/0", "red", "blue"), lit("t/x/U/1", "green"),
		union("t/x/V", "t/x/V/0"), lit("t/x/V/0", "up"))
	shared.Types["t/x/M"] = &ir.Model{ID: "t/x/M",
		Properties: []ir.Property{{ID: "p/x/M/p", Type: ir.TypeRef{Target: "t/x/U/0"}}}}
	for name, doc := range map[string]*ir.Document{
		"flat": constDoc(union("t/x/U", "t/x/U/0", "t/x/U/1"), lit("t/x/U/0", "red"), pinned("t/x/U/1", "green")),
		"a union of one value": constDoc(union("t/x/U", "t/x/U/0", "t/x/U/1"), anonymousUnion("t/x/U/0", "t/x/U/0/0"),
			lit("t/x/U/0/0", "red"), pinned("t/x/U/1", "green")),
		"a union of several values": constDoc(union("t/x/U", "t/x/U/0", "t/x/U/1"),
			anonymousUnion("t/x/U/0", "t/x/U/0/0", "t/x/U/0/1"), lit("t/x/U/0/0", "red"), lit("t/x/U/0/1", "blue"),
			pinned("t/x/U/1", "green")),
		"a chain":                     chain(4, false),
		"a branch another node names": shared,
	} {
		once := collapsed(t, doc)
		require.NotSame(t, doc, once, "%s: the first run rewrites a union", name)
		assert.Same(t, once, collapsed(t, once), name)
	}
	kept := collapsed(t, shared)
	assert.Same(t, shared.Types["t/x/U"], kept.Types["t/x/U"], "a branch another node names keeps its union")
}

// TestConstUnions_IsDeterministic pins the document a run returns to one
// encoding however Go ranges the maps the pass reads it through, over a chain
// of unions each beside a union of two values, one of which another node
// names, so the chain breaks into several Enums and a union that stays.
func TestConstUnions_IsDeterministic(t *testing.T) {
	t.Parallel()
	doc := caterpillar(6, false)
	doc.Types["t/x/M"] = &ir.Model{ID: "t/x/M",
		Properties: []ir.Property{{ID: "p/x/M/p", Type: ir.TypeRef{Target: "t/x/X3/v"}}}}
	want := irtest.Encoded(t, collapsed(t, doc))
	for range 8 {
		assert.Equal(t, want, irtest.Encoded(t, collapsed(t, doc)))
	}
}

// chain is a document of n unions, each naming the next and a value of its
// own, the value first where valueFirst, and the last naming a value of its
// own and the value last.
func chain(n int, valueFirst bool) *ir.Document {
	types := make([]ir.TypeDef, 0, 2*n+1)
	for i := range n {
		id, next := ir.TypeID(fmt.Sprintf("t/x/X%d", i)), ir.TypeID(fmt.Sprintf("t/x/X%d", i+1))
		value := id + "/v"
		u := union(id, next, value)
		if valueFirst {
			u = union(id, value, next)
		}
		if i > 0 {
			u.Anonymous, u.Name, u.Docs = true, hinted("variant"), ir.Docs{}
		}
		types = append(types, u, lit(value, fmt.Sprintf("a%d", i)))
	}
	return constDoc(append(types, lit(ir.TypeID(fmt.Sprintf("t/x/X%d", n)), "last"))...)
}

// caterpillar is chain(n, valueFirst) with each link's value a union of two
// values, so each link takes two unions of several values, the next link the
// larger.
func caterpillar(n int, valueFirst bool) *ir.Document {
	doc := chain(n, valueFirst)
	for i := range n {
		value := ir.TypeID(fmt.Sprintf("t/x/X%d/v", i))
		doc.Types[value] = anonymousUnion(value, value+"/0", value+"/1")
		doc.Types[value+"/0"], doc.Types[value+"/1"] = lit(value+"/0", fmt.Sprintf("b%d", i)), lit(value+"/1", fmt.Sprintf("c%d", i))
	}
	return doc
}

// TestConstUnions_TakesEveryLinkOfAChain pins a chain of unions, each naming
// the next once: the first becomes the Enum of every value the chain holds, in
// variant order, whichever side of each union the next link is on, and every
// other link and branch goes.
func TestConstUnions_TakesEveryLinkOfAChain(t *testing.T) {
	t.Parallel()
	const n = 4
	for _, valueFirst := range []bool{true, false} {
		doc := chain(n, valueFirst)
		out := collapsed(t, doc)

		values := make([]string, 0, n+1)
		for i := range n {
			values = append(values, fmt.Sprintf("a%d", i))
		}
		if valueFirst {
			values = append(values, "last")
		} else {
			values = append([]string{"last"}, values...)
			slices.Reverse(values[1:])
		}
		head, ok := doc.Types["t/x/X0"].(*ir.Union)
		require.True(t, ok)
		members := make([]ir.EnumMember, 0, len(values))
		for _, v := range values {
			members = append(members, member(head.ID, v))
		}
		assert.Empty(t, cmp.Diff(ir.TypeRegistry{head.ID: enumFor(head, members...)}, out.Types), "value first: %v", valueFirst)
	}
}

// TestConstUnions_ReadsAChainInWorkLinearInItsLength holds what reading a
// chain of unions costs to the values it holds, each link beside a value or a
// union of two: the keys set apart and the members built grow by about as much
// from 2n links to 4n as from n to 2n, where building each link's Enum from
// the next, or copying the larger union's keys into the one taking them rather
// than the smaller's, grows with the square of the chain.
func TestConstUnions_ReadsAChainInWorkLinearInItsLength(t *testing.T) {
	t.Parallel()
	const n = 200
	for name, shape := range map[string]func(int, bool) *ir.Document{"chain": chain, "caterpillar": caterpillar} {
		for _, valueFirst := range []bool{true, false} {
			w1, w2, w4 := pass.ReadingWork(shape(n, valueFirst)), pass.ReadingWork(shape(2*n, valueFirst)),
				pass.ReadingWork(shape(4*n, valueFirst))
			require.Positive(t, w2-w1)
			assert.LessOrEqual(t, float64(w4-w2), 2.5*float64(w2-w1), "%s, value first: %v: %d, %d, %d",
				name, valueFirst, w1, w2, w4)
		}
	}
}

// TestConstUnions_ReadsASetManyUnionsNameOnce holds what a branch of several
// values that many unions name costs to its size and to theirs, not to their
// product: the members watched and read grow by about as much from 2n to 4n
// as from n to 2n, where each of n unions reading the branch's n values, which
// none of them can take, would grow with the square of n.
func TestConstUnions_ReadsASetManyUnionsNameOnce(t *testing.T) {
	t.Parallel()
	const n = 100
	w1, w2, w4 := pass.ReadingWork(fanIn(n)), pass.ReadingWork(fanIn(2*n)), pass.ReadingWork(fanIn(4*n))
	require.Positive(t, w2-w1)
	assert.LessOrEqual(t, float64(w4-w2), 2.5*float64(w2-w1), "%d, %d, %d", w1, w2, w4)
}

// fanIn is a document of n unions, each naming one Enum of n values they all
// share and a value of its own.
func fanIn(n int) *ir.Document {
	values := make([]string, n)
	for i := range values {
		values[i] = fmt.Sprintf("s%d", i)
	}
	types := make([]ir.TypeDef, 0, 1+2*n)
	types = append(types, pinned("t/x/S", values...))
	for i := range n {
		id := ir.TypeID(fmt.Sprintf("t/x/U%d", i))
		types = append(types, union(id, "t/x/S", id+"/v"), lit(id+"/v", fmt.Sprintf("u%d", i)))
	}
	return constDoc(types...)
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
// check of its context a whole one makes, its walk's included, and requires the
// document it was handed back with nothing reported.
func TestConstUnions_ReturnsItsInputOnceItsContextIsDone(t *testing.T) {
	t.Parallel()
	doc := constDoc(union("t/x/U", "t/x/U/0", "t/x/U/1"), lit("t/x/U/0", "red"), lit("t/x/U/1", "green"))
	whole := &ctxtest.Countdown{Context: t.Context(), Left: math.MaxInt}
	out, _ := pass.ConstUnions{}.Run(whole, doc)
	require.NotSame(t, doc, out, "an uncancelled run replaces the union")
	require.Equal(t, 2, whole.Calls, "the walk asks once, and the run once more before it edits")

	for left := range whole.Calls {
		ctx := &ctxtest.Countdown{Context: t.Context(), Left: left}

		out, diags := pass.ConstUnions{}.Run(ctx, doc)

		assert.Same(t, doc, out, "check %d", left+1)
		assert.Nil(t, diags, "check %d", left+1)
	}
}

// TestConstUnions_AsksNoContextOfADocumentItNeedNotWalk pins that a document
// holding no union that may be an Enum is handed back unwalked: its context is
// never asked.
func TestConstUnions_AsksNoContextOfADocumentItNeedNotWalk(t *testing.T) {
	t.Parallel()
	u := union("t/x/U", "t/x/U/0")
	u.Closed = false
	doc := constDoc(u, lit("t/x/U/0", "red"))
	ctx := &ctxtest.Countdown{Context: t.Context(), Left: math.MaxInt}
	out, diags := pass.ConstUnions{}.Run(ctx, doc)

	assert.Same(t, doc, out)
	assert.Nil(t, diags)
	assert.Zero(t, ctx.Calls)
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
