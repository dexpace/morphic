// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// TestFold_AConflictingTypeTakesNothingWithIt pins that a
// discarded declaration is discarded whole. The type conflict is diagnosed and
// the loser's type kept under Unmodeled, but the fold below it used to run to
// completion regardless, adopting the loser's default, constraints and examples
// onto the winner. That leaves the document asserting two contradictory things
// about one field — an integer carrying a string default — and nothing in
// pass/validate or irverify compares a Value's kind to its property's type, so
// an emitter renders it into non-compiling code.
func TestFold_AConflictingTypeTakesNothingWithIt(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/str": &ir.Primitive{ID: "t/str", Prim: ir.PrimString},
		"t/int": &ir.Primitive{ID: "t/int", Prim: ir.PrimInt32},
	})
	dst := ir.Property{WireName: "id", Type: ir.TypeRef{Target: "t/int"}}
	src := ir.Property{
		WireName:    "id",
		Type:        ir.TypeRef{Target: "t/str"},
		Default:     &ir.Value{Kind: ir.ValueString, Str: "abc"},
		Constraints: &ir.Constraints{MaxLength: new(int64(10))},
		Examples:    []ir.Example{{Value: &ir.Value{Kind: ir.ValueString, Str: "abc"}}},
	}

	dst = foldTwo(g, dst, src, written(`{"type":"string"}`))

	require.Len(t, *recorded, 1, "the conflict is diagnosed")
	assert.Equal(t, ir.TypeID("t/int"), dst.Type.Target, "the first declaration wins the shape")
	assert.Nil(t, dst.Default, "a string default does not belong to an integer field")
	assert.Nil(t, dst.Constraints, "maxLength constrains the string that was discarded")
	assert.Empty(t, dst.Examples, "the examples are of the discarded shape")
}

// TestFold_NullabilityIntersectsWhenTargetsAgree pins the case
// the judgment settles before it reads Nullable: one branch admits null and
// the other does not, so the intersection forbids it. Neither reconciled nor
// preserved before, and the answer flipped with branch order — the same source
// compiled to nullable: true or false depending on which branch was written
// first. foldNullVerdicts already states the conjunction rule for the
// single-schema case; this is the same rule across a redeclaration.
func TestFold_NullabilityIntersectsWhenTargetsAgree(t *testing.T) {
	t.Parallel()
	reg := map[ir.TypeID]ir.TypeDef{
		"t/str": &ir.Primitive{ID: "t/str", Prim: ir.PrimString},
	}
	cases := []struct {
		name                   string
		dstNull, srcNull, want bool
	}{
		{"nullable then not", true, false, false},
		{"not then nullable", false, true, false},
		{"both nullable", true, true, true},
		{"neither nullable", false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g, recorded := stubMerger(reg)
			dst := ir.Property{WireName: "x", Type: ir.TypeRef{Target: "t/str", Nullable: tc.dstNull}}
			src := ir.Property{WireName: "x", Type: ir.TypeRef{Target: "t/str", Nullable: tc.srcNull}}

			dst = foldTwo(g, dst, src, unread(t))

			assert.Equal(t, tc.want, dst.Type.Nullable, "the merged field admits null only where both branches do")
			assert.Empty(t, *recorded, "an intersection the IR can express is not a conflict")
		})
	}
}

// TestFold_ADroppedTypeIsKeptEvenWhenItDoesNotConflict pins the
// gap between what the merge drops and what it calls a conflict. The judgment
// does not guess about two composites of one kind or an opaque scalar, but the
// fold keeps the winner's type regardless, so the redeclaration once vanished
// with neither a diagnostic nor an entry: #424's own failure, a consumer diffing
// two versions seeing no change. It is kept, and reported as a pair the merge
// does not compare, an info rather than a conflict: which type the field keeps
// then follows declaration order, and only the report says so.
func TestFold_ADroppedTypeIsKeptEvenWhenItDoesNotConflict(t *testing.T) {
	t.Parallel()
	reg := map[ir.TypeID]ir.TypeDef{
		"t/A":   &ir.Model{ID: "t/A"},
		"t/map": &ir.MapT{ID: "t/map"},
		"t/any": &ir.Any{ID: "t/any"},
		"t/str": &ir.Primitive{ID: "t/str", Prim: ir.PrimString},
		"t/opq": &ir.Scalar{ID: "t/opq"},
	}
	cases := []struct {
		name                 string
		dstTarget, srcTarget ir.TypeID
	}{
		{"a model and a map", "t/A", "t/map"},
		{"an opaque scalar keeps the position", "t/opq", "t/str"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g, recorded := stubMerger(reg)
			dst := ir.Property{WireName: "id", Type: ir.TypeRef{Target: tc.dstTarget}}
			src := ir.Property{
				WireName:   "id",
				Type:       ir.TypeRef{Target: tc.srcTarget},
				Provenance: ir.Provenance{Source: 1, Pointer: "/p/allOf/1/properties/id"},
			}

			dst = foldTwo(g, dst, src, written(`{"type":"object"}`))

			assert.Equal(t, tc.dstTarget, dst.Type.Target, "the first declaration still wins")
			require.Len(t, *recorded, 1)
			assert.Equal(t, ir.SeverityInfo, (*recorded)[0].Severity, "a drop not called a conflict is no warning")
			assert.Equal(t, diag.DegradedConstruct, (*recorded)[0].Code)
			assert.Contains(t, (*recorded)[0].Message, "that the merge does not compare")
			entry, ok := dst.Unmodeled[losingDeclarationKey+"/p/allOf/1/properties/id"]
			require.True(t, ok, "the dropped declaration is kept; got %v", dst.Unmodeled)
			assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
		})
	}
}

// TestFold_ADroppedTypeTakesNothingWithItEvenUnjudged is
// TestFold_AConflictingTypeTakesNothingWithIt for the drops
// the judgment declines to classify. The skip was keyed on the conflict
// diagnostic rather than on the drop, so a pair never called a conflict still
// folded the loser's default and constraints onto a winner whose type could
// not hold them, beside an entry saying that type was dropped: the
// contradiction the skip exists to prevent, on the one path it did not cover.
func TestFold_ADroppedTypeTakesNothingWithItEvenUnjudged(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/A":   &ir.Model{ID: "t/A"},
		"t/map": &ir.MapT{ID: "t/map"},
	})
	dst := ir.Property{WireName: "x", Type: ir.TypeRef{Target: "t/A"}}
	src := ir.Property{
		WireName:    "x",
		Type:        ir.TypeRef{Target: "t/map"},
		Default:     &ir.Value{Kind: ir.ValueObject},
		Constraints: &ir.Constraints{MinProps: new(int64(1))},
		Examples:    []ir.Example{{Value: &ir.Value{Kind: ir.ValueObject}}},
		Provenance:  ptrAt("/allOf/1/properties/x"),
	}

	dst = foldTwo(g, dst, src, written(`{"type":"object","default":{},"minProperties":1}`))

	require.Len(t, *recorded, 1)
	assert.Equal(t, diag.DegradedConstruct, (*recorded)[0].Code, "a model and a map are still not called a conflict")
	assert.Nil(t, dst.Default, "the default describes the shape that was dropped")
	assert.Nil(t, dst.Constraints, "so does minProperties")
	assert.Empty(t, dst.Examples, "and so do the examples")
	assert.Contains(t, dst.Unmodeled, losingDeclarationKey+"/allOf/1/properties/x", "the whole declaration is kept instead")
}

// TestFold_TheJudgmentsVerdictIsWhatIsReported pins the three
// paths of the fold: a disjoint pair warns, an overlapping pair draws an info,
// and one target declared twice judges null alone and adopts a default.
func TestFold_TheJudgmentsVerdictIsWhatIsReported(t *testing.T) {
	t.Parallel()
	d := str("x")
	for _, tc := range []struct {
		name      string
		a, b      ir.TypeID
		dflt      *ir.Value
		severity  ir.Severity
		code      string
		keptEntry bool
	}{
		{"a literal against a kind it is disjoint from", "t/lit-x", "t/prim/integer", nil,
			ir.SeverityWarning, diag.ConflictingRedecl, true},
		{"lists whose elements overlap", "t/list-int", "t/list-i32-null", nil,
			ir.SeverityInfo, diag.DegradedConstruct, true},
		{"one target declared twice", "t/prim/integer", "t/prim/integer", &d, "", "", false},
	} {
		for i, order := range [][2]ir.TypeID{{tc.a, tc.b}, {tc.b, tc.a}} {
			g, recorded := stubMerger(judgeRegistry())
			dst := ir.Property{WireName: "x", Type: ref(order[0]), Provenance: ptrAt("/allOf/0/properties/x")}
			src := ir.Property{WireName: "x", Type: ref(order[1]), Default: tc.dflt,
				Provenance: ptrAt("/allOf/1/properties/x")}
			if i == 1 {
				dst.Default, src.Default = src.Default, nil
			}

			dst = foldTwo(g, dst, src, written(`{"type":"x"}`))

			if tc.keptEntry {
				require.Len(t, *recorded, 1, "%s (order %d)", tc.name, i)
				assert.Equal(t, tc.severity, (*recorded)[0].Severity, "%s (order %d)", tc.name, i)
				assert.Equal(t, tc.code, (*recorded)[0].Code, "%s (order %d)", tc.name, i)
				assert.Contains(t, dst.Unmodeled, losingDeclarationKey+"/allOf/1/properties/x",
					"%s (order %d)", tc.name, i)
				continue
			}
			assert.Empty(t, *recorded, "%s (order %d)", tc.name, i)
			assert.Equal(t, tc.dflt, dst.Default, "the default is adopted (%s, order %d)", tc.name, i)
			assert.Empty(t, dst.Unmodeled, "%s (order %d)", tc.name, i)
		}
	}
}

// reconcileBothOrders reconciles kinds a and b in both declaration orders and
// returns what each order recorded and merged.
func reconcileBothOrders(t *testing.T, a, b ir.PrimKind) (dsts [2]ir.Property, diags [2][]ir.Diagnostic) {
	t.Helper()
	for i, order := range [][2]ir.PrimKind{{a, b}, {b, a}} {
		g, recorded := stubMerger(narrowingRegistry())
		dst := ir.Property{WireName: "x", Type: primRef(order[0]), Provenance: ptrAt("/allOf/0/properties/x")}
		src := ir.Property{WireName: "x", Type: primRef(order[1]), Provenance: ptrAt("/allOf/1/properties/x")}

		dst = foldTwo(g, dst, src, written(`{"type":"x"}`))

		dsts[i], diags[i] = dst, *recorded
	}
	return dsts, diags
}

// TestFold_OverlappingKindsAreKeptAndReportedAsInfo pins that
// kinds sharing values with neither holding the other keep the first
// declaration's kind, draw an info, and keep the redeclaration, in both orders.
func TestFold_OverlappingKindsAreKeptAndReportedAsInfo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		a, b ir.PrimKind
	}{
		{"int32 and uint32", ir.PrimInt32, ir.PrimUint32},
		{"float32 and integer", ir.PrimFloat32, ir.PrimInteger},
		{"int64 and float64", ir.PrimInt64, ir.PrimFloat64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dsts, diags := reconcileBothOrders(t, tc.a, tc.b)
			for i, first := range []ir.PrimKind{tc.a, tc.b} {
				assert.Equal(t, primRef(first).Target, dsts[i].Type.Target, "the first declaration's kind stays (order %d)", i)
				require.Len(t, diags[i], 1, "exactly one diagnostic (order %d)", i)
				assert.Equal(t, diag.DegradedConstruct, diags[i][0].Code, "order %d", i)
				assert.Equal(t, ir.SeverityInfo, diags[i][0].Severity, "order %d", i)
				assert.Contains(t, dsts[i].Unmodeled, losingDeclarationKey+"/allOf/1/properties/x",
					"the redeclaration is kept (order %d)", i)
			}
		})
	}
}

// TestFold_DisjointKindsStillWarn pins that kinds no value shares
// keep the conflicting-redeclaration warning, in both orders.
func TestFold_DisjointKindsStillWarn(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		a, b ir.PrimKind
	}{
		{"string and integer", ir.PrimString, ir.PrimInteger},
		{"bool and number", ir.PrimBool, ir.PrimNumber},
		{"uuid and date-time", ir.PrimUUID, ir.PrimDatetimeOffset},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dsts, diags := reconcileBothOrders(t, tc.a, tc.b)
			for i, first := range []ir.PrimKind{tc.a, tc.b} {
				assert.Equal(t, primRef(first).Target, dsts[i].Type.Target, "the first declaration still wins (order %d)", i)
				require.Len(t, diags[i], 1, "exactly one diagnostic (order %d)", i)
				assert.Equal(t, diag.ConflictingRedecl, diags[i][0].Code, "order %d", i)
				assert.Equal(t, ir.SeverityWarning, diags[i][0].Severity, "order %d", i)
				assert.Contains(t, dsts[i].Unmodeled, losingDeclarationKey+"/allOf/1/properties/x", "order %d", i)
			}
		})
	}
}

// enumRegistry adds two integer enums and two string enums to narrowingRegistry,
// each with a member the pairs below judge, and the uuid and bytes Scalar nodes
// that stand for a kind the same way a bare reference does.
func enumRegistry() map[ir.TypeID]ir.TypeDef {
	reg := narrowingRegistry()
	reg["t/enum/a"] = &ir.Enum{ID: "t/enum/a", ValueType: ir.PrimString, Members: []ir.EnumMember{{Value: str("a")}}}
	reg["t/enum/b"] = &ir.Enum{ID: "t/enum/b", ValueType: ir.PrimString, Members: []ir.EnumMember{{Value: str("b")}}}
	reg["t/enum/big"] = &ir.Enum{ID: "t/enum/big", ValueType: ir.PrimInteger, Members: []ir.EnumMember{{Value: num("5000000000")}}}
	reg["t/enum/small"] = &ir.Enum{ID: "t/enum/small", ValueType: ir.PrimInteger, Members: []ir.EnumMember{{Value: num("1")}}}
	uuid, bytes := primRef(ir.PrimUUID), primRef(ir.PrimBytes)
	reg["t/scalar/uuid"] = &ir.Scalar{ID: "t/scalar/uuid", Base: &uuid}
	reg["t/scalar/bytes"] = &ir.Scalar{ID: "t/scalar/bytes", Base: &bytes}
	return reg
}

// TestFold_EnumPairsAreJudgedByKind pins, in both orders, what an
// enum side draws: an enum over a bare kind or a Scalar node it equals or
// narrows wins; a bare kind or a Scalar node that admits one of its members
// overlaps it, which is info and keeps the first declaration, as does a kind
// with no judge; a kind that admits none of them warns, as does another
// family; and two enums of one kind, each with members, overlap.
func TestFold_EnumPairsAreJudgedByKind(t *testing.T) {
	t.Parallel()
	enum := func(id ir.TypeID) ir.TypeRef { return ir.TypeRef{Target: id} }
	cases := []struct {
		name   string
		a, b   ir.TypeRef
		want   ir.Severity // "" = none
		winner ir.TypeID   // "" = the first declaration's
	}{
		{"enum of string and uuid", enum("t/enum/a"), primRef(ir.PrimUUID), ir.SeverityWarning, ""},
		{"enum of integer and int32", enum("t/enum/big"), primRef(ir.PrimInt32), ir.SeverityWarning, ""},
		{"enum admitted by int32", enum("t/enum/small"), primRef(ir.PrimInt32), ir.SeverityInfo, ""},
		{"enum of string and bytes", enum("t/enum/a"), primRef(ir.PrimBytes), ir.SeverityInfo, ""},
		{"enum of string and a uuid scalar", enum("t/enum/a"), enum("t/scalar/uuid"), ir.SeverityWarning, ""},
		{"enum of string and a bytes scalar", enum("t/enum/a"), enum("t/scalar/bytes"), ir.SeverityInfo, ""},
		{"enum of string and integer", enum("t/enum/a"), primRef(ir.PrimInteger), ir.SeverityWarning, ""},
		{"enum of string and another enum", enum("t/enum/a"), enum("t/enum/b"), ir.SeverityInfo, ""},
		{"enum of string and bare string", enum("t/enum/a"), primRef(ir.PrimString), "", "t/enum/a"},
		{"enum of integer and number", enum("t/enum/big"), primRef(ir.PrimNumber), "", "t/enum/big"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for i, order := range [][2]ir.TypeRef{{tc.a, tc.b}, {tc.b, tc.a}} {
				g, recorded := stubMerger(enumRegistry())
				dst := ir.Property{WireName: "x", Type: order[0], Provenance: ptrAt("/allOf/0/properties/x")}
				src := ir.Property{WireName: "x", Type: order[1], Provenance: ptrAt("/allOf/1/properties/x")}

				dst = foldTwo(g, dst, src, written(`{"type":"x"}`))

				wantTarget := order[0].Target
				if tc.winner != "" {
					wantTarget = tc.winner
				}
				assert.Equal(t, wantTarget, dst.Type.Target, "the winning target (order %d)", i)
				if tc.want == "" {
					assert.Empty(t, *recorded, "no diagnostic (order %d)", i)
					continue
				}
				require.Len(t, *recorded, 1, "exactly one diagnostic (order %d)", i)
				assert.Equal(t, tc.want, (*recorded)[0].Severity, "order %d", i)
				assert.Contains(t, dst.Unmodeled, losingDeclarationKey+"/allOf/1/properties/x",
					"the redeclaration is kept (order %d)", i)
			}
		})
	}
}

// twinRegistry holds, for each of Enum, Literal and Scalar, two hoisted nodes
// that differ only in ID and member IDs, and the variants that differ in a
// field. t/enum/x-named and t/enum/x-named2 are named copies of t/enum/x1.
func twinRegistry() map[ir.TypeID]ir.TypeDef {
	reg := narrowingRegistry()
	members := func(prefix string, vs ...string) []ir.EnumMember {
		out := make([]ir.EnumMember, 0, len(vs))
		for _, v := range vs {
			out = append(out, ir.EnumMember{ID: ir.EnumMemberID(prefix + v), Value: str(v)})
		}
		return out
	}
	enum := func(id ir.TypeID, ms []ir.EnumMember, desc string) *ir.Enum {
		e := &ir.Enum{ID: id, ValueType: ir.PrimString, Members: ms}
		e.Docs.Description = desc
		return e
	}
	reg["t/enum/x1"] = enum("t/enum/x1", members("x1/", "a", "b"), "")
	reg["t/enum/x2"] = enum("t/enum/x2", members("x2/", "a", "b"), "")
	reg["t/enum/x-described"] = enum("t/enum/x-described", members("xd/", "a", "b"), "some")
	reg["t/enum/x-reordered"] = enum("t/enum/x-reordered", members("xr/", "b", "a"), "")
	for _, id := range []ir.TypeID{"t/enum/x-ext1", "t/enum/x-ext2"} {
		e := enum(id, members(string(id)+"/", "a", "b"), "")
		e.Unmodeled = ir.Unmodeled{"openapi:x-foo": {
			Reason:     ir.ReasonVendorExtension,
			Value:      ir.RawValue("1"),
			Provenance: ptrAt(jsontext.Pointer("/at/" + string(id) + "/x-foo")),
		}}
		reg[id] = e
	}
	valued := enum("t/enum/x-ext-valued", members("xv/", "a", "b"), "")
	valued.Unmodeled = ir.Unmodeled{"openapi:x-foo": {
		Reason:     ir.ReasonVendorExtension,
		Value:      ir.RawValue("2"),
		Provenance: ptrAt("/at/t/enum/x-ext1/x-foo"),
	}}
	reg["t/enum/x-ext-valued"] = valued
	reg["t/lit/1"] = &ir.Literal{ID: "t/lit/1", Value: str("a")}
	reg["t/lit/2"] = &ir.Literal{ID: "t/lit/2", Value: str("a")}
	str := primRef(ir.PrimString)
	minLen := int64(2)
	reg["t/hex/1"] = &ir.Scalar{ID: "t/hex/1", Base: &str}
	reg["t/hex/2"] = &ir.Scalar{ID: "t/hex/2", Base: &str}
	reg["t/hex/min"] = &ir.Scalar{ID: "t/hex/min", Base: &str, Constraints: &ir.Constraints{MinLength: &minLen}}
	for _, td := range reg {
		switch td.(type) {
		case *ir.Enum, *ir.Literal, *ir.Scalar:
			td.Common().Anonymous = true
		}
	}
	reg["t/enum/x-named"] = enum("t/enum/x-named", members("xn/", "a", "b"), "")
	reg["t/enum/x-named2"] = enum("t/enum/x-named2", members("xn2/", "a", "b"), "")
	return reg
}

// TestFold_IdenticalNodesAreOneType pins, in both orders, that two
// hoisted Enum, Literal or Scalar nodes equal but for what names their site are
// one type: no diagnostic and nothing kept aside. A named node wins over a
// hoisted copy of itself, and two named ones, or a pair that differs in a
// field, member order included, are an overlap reported as info.
func TestFold_IdenticalNodesAreOneType(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		a, b ir.TypeID
		same bool
		win  ir.TypeID // the type that stands in both orders, or "" for the first
	}{
		{"two identical enums", "t/enum/x1", "t/enum/x2", true, ""},
		{"two identical literals", "t/lit/1", "t/lit/2", true, ""},
		{"two identical scalars", "t/hex/1", "t/hex/2", true, ""},
		{"enums that differ in the provenance of an extension", "t/enum/x-ext1", "t/enum/x-ext2", true, ""},
		{"a named enum against a hoisted copy", "t/enum/x-named", "t/enum/x1", true, "t/enum/x-named"},
		{"two named enums declared alike", "t/enum/x-named", "t/enum/x-named2", false, ""},
		{"enums that differ in an extension's value", "t/enum/x-ext1", "t/enum/x-ext-valued", false, ""},
		{"enums that differ in a description", "t/enum/x1", "t/enum/x-described", false, ""},
		{"enums that differ in member order", "t/enum/x1", "t/enum/x-reordered", false, ""},
		{"scalars that differ in a constraint", "t/hex/1", "t/hex/min", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for i, order := range [][2]ir.TypeID{{tc.a, tc.b}, {tc.b, tc.a}} {
				reg := twinRegistry()
				g, recorded := stubMerger(reg)
				dst := ir.Property{WireName: "x", Type: ir.TypeRef{Target: order[0]}, Provenance: ptrAt("/allOf/0/properties/x")}
				src := ir.Property{WireName: "x", Type: ir.TypeRef{Target: order[1]}, Provenance: ptrAt("/allOf/1/properties/x")}
				for _, p := range []ir.Property{dst, src} {
					if c := reg[p.Type.Target].Common(); c.Anonymous {
						c.Provenance = p.Provenance
					}
				}

				dst = foldTwo(g, dst, src, written(`{"type":"x"}`))

				want := tc.win
				if want == "" {
					want = order[0]
				}
				assert.Equal(t, want, dst.Type.Target, "the type that stands (order %d)", i)
				if tc.same {
					assert.Empty(t, *recorded, "no diagnostic (order %d)", i)
					assert.Empty(t, dst.Unmodeled, "nothing is kept (order %d)", i)
					continue
				}
				require.Len(t, *recorded, 1, "exactly one diagnostic (order %d)", i)
				assert.Equal(t, ir.SeverityInfo, (*recorded)[0].Severity, "order %d", i)
				assert.Len(t, dst.Unmodeled, 1, "the redeclaration is kept (order %d)", i)
			}
		})
	}
}
