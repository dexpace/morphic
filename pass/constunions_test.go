// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
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
// is a union of one value is read as the Enum it becomes, in the same run. The
// branch types go with the union.
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
// than its value, a branch of another kind, of several values or none, values
// of different types or a value twice, and a fact two places say of one value,
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
		{"a literal no primitive types", func(_ *ir.Union, red *ir.Literal, _ *ir.Enum, _ ir.TypeRegistry) {
			red.ValueType = ""
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
		{"an enum no primitive types", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			green.ValueType = ""
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
		{"an enum of several values", func(_ *ir.Union, _ *ir.Literal, green *ir.Enum, _ ir.TypeRegistry) {
			green.Members = append(green.Members, member(green.ID, "blue"))
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
// of one value, which the first run rewrites with it, and where it names one of
// several, which the first run rewrites alone.
func TestConstUnions_IsIdempotent(t *testing.T) {
	t.Parallel()
	for name, doc := range map[string]*ir.Document{
		"flat": constDoc(union("t/x/U", "t/x/U/0", "t/x/U/1"), lit("t/x/U/0", "red"), pinned("t/x/U/1", "green")),
		"a union of one value": constDoc(union("t/x/U", "t/x/U/0", "t/x/U/1"), anonymousUnion("t/x/U/0", "t/x/U/0/0"),
			lit("t/x/U/0/0", "red"), pinned("t/x/U/1", "green")),
		"a union of several values": constDoc(union("t/x/U", "t/x/U/0", "t/x/U/1"),
			anonymousUnion("t/x/U/0", "t/x/U/0/0", "t/x/U/0/1"), lit("t/x/U/0/0", "red"), lit("t/x/U/0/1", "blue"),
			pinned("t/x/U/1", "green")),
	} {
		once := collapsed(t, doc)
		require.NotSame(t, doc, once, "%s: the first run rewrites a union", name)
		assert.Same(t, once, collapsed(t, once), name)
	}
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
	require.Greater(t, whole.Calls, 1, "the walk asks too")

	for left := range whole.Calls {
		ctx := &ctxtest.Countdown{Context: t.Context(), Left: left}

		out, diags := pass.ConstUnions{}.Run(ctx, doc)

		assert.Same(t, doc, out, "check %d", left+1)
		assert.Nil(t, diags, "check %d", left+1)
	}
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
