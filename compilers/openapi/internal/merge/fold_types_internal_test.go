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
	stubbed, recorded := stubMerger(map[ir.TypeID]ir.TypeDef{
		"type/str": &ir.Primitive{ID: "type/str", PrimitiveKind: ir.PrimitiveString},
		"type/int": &ir.Primitive{ID: "type/int", PrimitiveKind: ir.PrimitiveInt32},
	})
	dst := ir.Property{
		WireName:    "id",
		Type:        ir.TypeRef{Target: "type/str"},
		Default:     &ir.Value{Kind: ir.ValueString, String: "abc"},
		Constraints: &ir.Constraints{MaxLength: new(int64(10))},
		Examples:    []ir.Example{{Value: &ir.Value{Kind: ir.ValueString, String: "abc"}}},
	}
	src := ir.Property{WireName: "id", Type: ir.TypeRef{Target: "type/int"}}

	dst = foldTwo(stubbed, dst, src, written(`{"type":"integer"}`))

	require.Len(t, *recorded, 1, "the conflict is diagnosed")
	assert.Equal(t, ir.TypeID("type/int"), dst.Type.Target, "the later declaration wins the shape")
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
		"type/str": &ir.Primitive{ID: "type/str", PrimitiveKind: ir.PrimitiveString},
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
			stubbed, recorded := stubMerger(reg)
			dst := ir.Property{WireName: "x", Type: ir.TypeRef{Target: "type/str", Nullable: tc.dstNull}}
			src := ir.Property{WireName: "x", Type: ir.TypeRef{Target: "type/str", Nullable: tc.srcNull}}

			dst = foldTwo(stubbed, dst, src, unread(t))

			assert.Equal(t, tc.want, dst.Type.Nullable, "the merged field admits null only where both branches do")
			assert.Empty(t, *recorded, "an intersection the IR can express is not a conflict")
		})
	}
}

// TestFold_ADroppedTypeIsKeptEvenWhenItDoesNotConflict pins the
// gap between what the merge drops and what it calls a conflict. The judgment
// does not guess about two composites of one kind or an opaque scalar, but the
// fold keeps the winner's type regardless, so the losing declaration once
// vanished with neither a diagnostic nor an entry: #424's own failure, a
// consumer diffing two versions seeing no change. It is kept, and reported as a
// pair the merge does not compare, an info rather than a conflict: the field
// keeps the later declaration's type, which follows declaration order, and only
// the report says so.
func TestFold_ADroppedTypeIsKeptEvenWhenItDoesNotConflict(t *testing.T) {
	t.Parallel()
	reg := map[ir.TypeID]ir.TypeDef{
		"type/list":  &ir.List{ID: "type/list", ElementType: ir.TypeRef{Target: "type/str"}},
		"type/tuple": &ir.Tuple{ID: "type/tuple", ElementTypes: []ir.TypeRef{{Target: "type/str"}}},
		"type/any":   &ir.Any{ID: "type/any"},
		"type/str":   &ir.Primitive{ID: "type/str", PrimitiveKind: ir.PrimitiveString},
		"type/opq":   &ir.Scalar{ID: "type/opq"},
	}
	cases := []struct {
		name                 string
		dstTarget, srcTarget ir.TypeID
	}{
		{"a list and a tuple", "type/list", "type/tuple"},
		{"an opaque scalar keeps the position", "type/opq", "type/str"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stubbed, recorded := stubMerger(reg)
			dst := ir.Property{WireName: "id", Type: ir.TypeRef{Target: tc.dstTarget},
				Provenance: ir.Provenance{Source: 1, Pointer: "/p/allOf/0/properties/id"}}
			src := ir.Property{
				WireName:   "id",
				Type:       ir.TypeRef{Target: tc.srcTarget},
				Provenance: ir.Provenance{Source: 1, Pointer: "/p/allOf/1/properties/id"},
			}

			dst = foldTwo(stubbed, dst, src, written(`{"type":"object"}`))

			assert.Equal(t, tc.srcTarget, dst.Type.Target, "the later declaration wins")
			require.Len(t, *recorded, 1)
			assert.Equal(t, ir.SeverityInfo, (*recorded)[0].Severity, "a drop not called a conflict is no warning")
			assert.Equal(t, diag.DegradedConstruct, (*recorded)[0].Code)
			assert.Contains(t, (*recorded)[0].Message, "that the merge does not compare")
			entry, ok := dst.Unmodeled[losingDeclarationKey+"/p/allOf/0/properties/id"]
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
	stubbed, recorded := stubMerger(map[ir.TypeID]ir.TypeDef{
		"type/A":   declaring("type/A"),
		"type/opq": &ir.Scalar{ID: "type/opq"},
	})
	dst := ir.Property{
		WireName:    "x",
		Type:        ir.TypeRef{Target: "type/opq"},
		Default:     &ir.Value{Kind: ir.ValueObject},
		Constraints: &ir.Constraints{MinProperties: new(int64(1))},
		Examples:    []ir.Example{{Value: &ir.Value{Kind: ir.ValueObject}}},
		Provenance:  ptrAt("/allOf/0/properties/x"),
	}
	src := ir.Property{WireName: "x", Type: ir.TypeRef{Target: "type/A"}, Provenance: ptrAt("/allOf/1/properties/x")}

	dst = foldTwo(stubbed, dst, src, written(`{"type":"object"}`))

	require.Len(t, *recorded, 1)
	assert.Equal(t, diag.DegradedConstruct, (*recorded)[0].Code, "a model and an opaque scalar are not called a conflict")
	assert.Nil(t, dst.Default, "the default describes the shape that was dropped")
	assert.Nil(t, dst.Constraints, "so does minProperties")
	assert.Empty(t, dst.Examples, "and so do the examples")
	assert.Contains(t, dst.Unmodeled, losingDeclarationKey+"/allOf/0/properties/x", "the whole declaration is kept instead")
}

// TestFold_TheJudgmentsVerdictIsWhatIsReported pins the three
// paths of the fold: a disjoint pair warns, an overlapping pair draws an info,
// and one target declared twice judges null alone and adopts a default.
func TestFold_TheJudgmentsVerdictIsWhatIsReported(t *testing.T) {
	t.Parallel()
	value := stringValue("x")
	for _, tc := range []struct {
		name      string
		a, b      ir.TypeID
		dflt      *ir.Value
		severity  ir.Severity
		code      string
		keptEntry bool
	}{
		{"a literal against a kind it is disjoint from", "type/lit-x", "type/primitive/integer", nil,
			ir.SeverityWarning, diag.ConflictingRedecl, true},
		{"lists whose elements overlap", "type/list-i32", "type/list-u32", nil,
			ir.SeverityInfo, diag.DegradedConstruct, true},
		{"one target declared twice", "type/primitive/integer", "type/primitive/integer", &value, "", "", false},
	} {
		for i, order := range [][2]ir.TypeID{{tc.a, tc.b}, {tc.b, tc.a}} {
			stubbed, recorded := stubMerger(judgeRegistry())
			dst := ir.Property{WireName: "x", Type: ref(order[0]), Provenance: ptrAt("/allOf/0/properties/x")}
			src := ir.Property{WireName: "x", Type: ref(order[1]), Default: tc.dflt,
				Provenance: ptrAt("/allOf/1/properties/x")}
			if i == 1 {
				dst.Default, src.Default = src.Default, nil
			}

			dst = foldTwo(stubbed, dst, src, written(`{"type":"x"}`))

			if tc.keptEntry {
				require.Len(t, *recorded, 1, "%s (order %d)", tc.name, i)
				assert.Equal(t, tc.severity, (*recorded)[0].Severity, "%s (order %d)", tc.name, i)
				assert.Equal(t, tc.code, (*recorded)[0].Code, "%s (order %d)", tc.name, i)
				assert.Contains(t, dst.Unmodeled, losingDeclarationKey+"/allOf/0/properties/x",
					"the earlier declaration is kept: %s (order %d)", tc.name, i)
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
func reconcileBothOrders(t *testing.T, a, b ir.PrimitiveKind) (dsts [2]ir.Property, diags [2][]ir.Diagnostic) {
	t.Helper()
	for i, order := range [][2]ir.PrimitiveKind{{a, b}, {b, a}} {
		stubbed, recorded := stubMerger(narrowingRegistry())
		dst := ir.Property{WireName: "x", Type: primitiveRef(order[0]), Provenance: ptrAt("/allOf/0/properties/x")}
		src := ir.Property{WireName: "x", Type: primitiveRef(order[1]), Provenance: ptrAt("/allOf/1/properties/x")}

		dst = foldTwo(stubbed, dst, src, written(`{"type":"x"}`))

		dsts[i], diags[i] = dst, *recorded
	}
	return dsts, diags
}

// TestFold_OverlappingKindsAreKeptAndReportedAsInfo pins that
// kinds sharing values with neither holding the other keep the later
// declaration's kind, draw an info, and keep the earlier one, in both orders.
func TestFold_OverlappingKindsAreKeptAndReportedAsInfo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		a, b ir.PrimitiveKind
	}{
		{"int32 and uint32", ir.PrimitiveInt32, ir.PrimitiveUint32},
		{"float32 and integer", ir.PrimitiveFloat32, ir.PrimitiveInteger},
		{"int64 and float64", ir.PrimitiveInt64, ir.PrimitiveFloat64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dsts, diags := reconcileBothOrders(t, tc.a, tc.b)
			for i, later := range []ir.PrimitiveKind{tc.b, tc.a} {
				assert.Equal(t, primitiveRef(later).Target, dsts[i].Type.Target, "the later declaration's kind stays (order %d)", i)
				require.Len(t, diags[i], 1, "exactly one diagnostic (order %d)", i)
				assert.Equal(t, diag.DegradedConstruct, diags[i][0].Code, "order %d", i)
				assert.Equal(t, ir.SeverityInfo, diags[i][0].Severity, "order %d", i)
				assert.Contains(t, dsts[i].Unmodeled, losingDeclarationKey+"/allOf/0/properties/x",
					"the earlier declaration is kept (order %d)", i)
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
		a, b ir.PrimitiveKind
	}{
		{"string and integer", ir.PrimitiveString, ir.PrimitiveInteger},
		{"bool and number", ir.PrimitiveBool, ir.PrimitiveNumber},
		{"uuid and date-time", ir.PrimitiveUUID, ir.PrimitiveDatetimeOffset},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dsts, diags := reconcileBothOrders(t, tc.a, tc.b)
			for i, later := range []ir.PrimitiveKind{tc.b, tc.a} {
				assert.Equal(t, primitiveRef(later).Target, dsts[i].Type.Target, "the later declaration wins (order %d)", i)
				require.Len(t, diags[i], 1, "exactly one diagnostic (order %d)", i)
				assert.Equal(t, diag.ConflictingRedecl, diags[i][0].Code, "order %d", i)
				assert.Equal(t, ir.SeverityWarning, diags[i][0].Severity, "order %d", i)
				assert.Contains(t, dsts[i].Unmodeled, losingDeclarationKey+"/allOf/0/properties/x", "order %d", i)
			}
		})
	}
}

// enumRegistry adds two integer enums and two string enums to narrowingRegistry,
// each with a member the pairs below judge, and the uuid and bytes Scalar nodes
// that stand for a kind the same way a bare reference does.
func enumRegistry() map[ir.TypeID]ir.TypeDef {
	reg := narrowingRegistry()
	reg["type/enum/a"] = &ir.Enum{ID: "type/enum/a", ValueType: ir.PrimitiveString, Members: []ir.EnumMember{{Value: stringValue("a")}}}
	reg["type/enum/b"] = &ir.Enum{ID: "type/enum/b", ValueType: ir.PrimitiveString, Members: []ir.EnumMember{{Value: stringValue("b")}}}
	reg["type/enum/big"] = &ir.Enum{ID: "type/enum/big", ValueType: ir.PrimitiveInteger, Members: []ir.EnumMember{{Value: number("5000000000")}}}
	reg["type/enum/small"] = &ir.Enum{ID: "type/enum/small", ValueType: ir.PrimitiveInteger, Members: []ir.EnumMember{{Value: number("1")}}}
	uuid, bytes := primitiveRef(ir.PrimitiveUUID), primitiveRef(ir.PrimitiveBytes)
	reg["type/scalar/uuid"] = &ir.Scalar{ID: "type/scalar/uuid", Base: &uuid}
	reg["type/scalar/bytes"] = &ir.Scalar{ID: "type/scalar/bytes", Base: &bytes}
	return reg
}

// TestFold_EnumPairsAreJudgedByKind pins, in both orders, what an
// enum side draws: an enum over a bare kind or a Scalar node it equals or
// narrows wins; a bare kind or a Scalar node that admits one of its members
// overlaps it, which is info and keeps the later declaration, as does a kind
// with no judge; a kind that admits none of them warns, as does another
// family, and another enum sharing none of them.
func TestFold_EnumPairsAreJudgedByKind(t *testing.T) {
	t.Parallel()
	enum := func(id ir.TypeID) ir.TypeRef { return ir.TypeRef{Target: id} }
	cases := []struct {
		name   string
		a, b   ir.TypeRef
		want   ir.Severity // "" = none
		winner ir.TypeID   // "" = the later declaration's
	}{
		{"enum of string and uuid", enum("type/enum/a"), primitiveRef(ir.PrimitiveUUID), ir.SeverityWarning, ""},
		{"enum of integer and int32", enum("type/enum/big"), primitiveRef(ir.PrimitiveInt32), ir.SeverityWarning, ""},
		{"enum admitted by int32", enum("type/enum/small"), primitiveRef(ir.PrimitiveInt32), ir.SeverityInfo, ""},
		{"enum of string and bytes", enum("type/enum/a"), primitiveRef(ir.PrimitiveBytes), ir.SeverityInfo, ""},
		{"enum of string and a uuid scalar", enum("type/enum/a"), enum("type/scalar/uuid"), ir.SeverityWarning, ""},
		{"enum of string and a bytes scalar", enum("type/enum/a"), enum("type/scalar/bytes"), ir.SeverityInfo, ""},
		{"enum of string and integer", enum("type/enum/a"), primitiveRef(ir.PrimitiveInteger), ir.SeverityWarning, ""},
		{"enum of string and another sharing no member", enum("type/enum/a"), enum("type/enum/b"), ir.SeverityWarning, ""},
		{"enum of string and bare string", enum("type/enum/a"), primitiveRef(ir.PrimitiveString), "", "type/enum/a"},
		{"enum of integer and number", enum("type/enum/big"), primitiveRef(ir.PrimitiveNumber), "", "type/enum/big"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for i, order := range [][2]ir.TypeRef{{tc.a, tc.b}, {tc.b, tc.a}} {
				stubbed, recorded := stubMerger(enumRegistry())
				dst := ir.Property{WireName: "x", Type: order[0], Provenance: ptrAt("/allOf/0/properties/x")}
				src := ir.Property{WireName: "x", Type: order[1], Provenance: ptrAt("/allOf/1/properties/x")}

				dst = foldTwo(stubbed, dst, src, written(`{"type":"x"}`))

				wantTarget := order[1].Target
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
				assert.Contains(t, dst.Unmodeled, losingDeclarationKey+"/allOf/0/properties/x",
					"the earlier declaration is kept (order %d)", i)
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
			out = append(out, ir.EnumMember{ID: ir.EnumMemberID(prefix + v), Value: stringValue(v)})
		}
		return out
	}
	enum := func(id ir.TypeID, ms []ir.EnumMember, desc string) *ir.Enum {
		enum := &ir.Enum{ID: id, ValueType: ir.PrimitiveString, Members: ms}
		enum.Docs.Description = desc
		return enum
	}
	reg["type/enum/x1"] = enum("type/enum/x1", members("x1/", "a", "b"), "")
	reg["type/enum/x2"] = enum("type/enum/x2", members("x2/", "a", "b"), "")
	reg["type/enum/x-described"] = enum("type/enum/x-described", members("xd/", "a", "b"), "some")
	reg["type/enum/x-reordered"] = enum("type/enum/x-reordered", members("xr/", "b", "a"), "")
	for _, id := range []ir.TypeID{"type/enum/x-ext1", "type/enum/x-ext2"} {
		irEnum := enum(id, members(string(id)+"/", "a", "b"), "")
		irEnum.Unmodeled = ir.Unmodeled{"openapi:x-foo": {
			Reason:     ir.ReasonVendorExtension,
			Value:      ir.RawValue("1"),
			Provenance: ptrAt(jsontext.Pointer("/at/" + string(id) + "/x-foo")),
		}}
		reg[id] = irEnum
	}
	valued := enum("type/enum/x-ext-valued", members("xv/", "a", "b"), "")
	valued.Unmodeled = ir.Unmodeled{"openapi:x-foo": {
		Reason:     ir.ReasonVendorExtension,
		Value:      ir.RawValue("2"),
		Provenance: ptrAt("/at/t/enum/x-ext1/x-foo"),
	}}
	reg["type/enum/x-ext-valued"] = valued
	reg["type/lit/1"] = &ir.Literal{ID: "type/lit/1", Value: stringValue("a")}
	reg["type/lit/2"] = &ir.Literal{ID: "type/lit/2", Value: stringValue("a")}
	stringType := primitiveRef(ir.PrimitiveString)
	minLen := int64(2)
	reg["type/hex/1"] = &ir.Scalar{ID: "type/hex/1", Base: &stringType}
	reg["type/hex/2"] = &ir.Scalar{ID: "type/hex/2", Base: &stringType}
	reg["type/hex/min"] = &ir.Scalar{ID: "type/hex/min", Base: &stringType, Constraints: &ir.Constraints{MinLength: &minLen}}
	for _, td := range reg {
		switch td.(type) {
		case *ir.Enum, *ir.Literal, *ir.Scalar:
			td.Common().Anonymous = true
		}
	}
	reg["type/enum/x-named"] = enum("type/enum/x-named", members("xn/", "a", "b"), "")
	reg["type/enum/x-named2"] = enum("type/enum/x-named2", members("xn2/", "a", "b"), "")
	return reg
}

// TestFold_IdenticalNodesAreOneType pins, in both orders, that two
// hoisted Enum, Literal or Scalar nodes equal but for what names their site are
// one type, the one whose ID sorts first standing for both: no diagnostic and
// nothing kept aside. A named node wins over a hoisted copy of itself, and two
// named ones, or a pair that differs in a field, member order included, are an
// overlap reported as info, the later kept.
func TestFold_IdenticalNodesAreOneType(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		a, b ir.TypeID
		same bool
		win  ir.TypeID // the type that stands in both orders, or "" for the rule's
	}{
		{"two identical enums", "type/enum/x1", "type/enum/x2", true, ""},
		{"two identical literals", "type/lit/1", "type/lit/2", true, ""},
		{"two identical scalars", "type/hex/1", "type/hex/2", true, ""},
		{"enums that differ in the provenance of an extension", "type/enum/x-ext1", "type/enum/x-ext2", true, ""},
		{"a named enum against a hoisted copy", "type/enum/x-named", "type/enum/x1", true, "type/enum/x-named"},
		{"two named enums declared alike", "type/enum/x-named", "type/enum/x-named2", false, ""},
		{"enums that differ in an extension's value", "type/enum/x-ext1", "type/enum/x-ext-valued", false, ""},
		{"enums that differ in a description", "type/enum/x1", "type/enum/x-described", false, ""},
		{"enums that differ in member order", "type/enum/x1", "type/enum/x-reordered", false, ""},
		{"scalars that differ in a constraint", "type/hex/1", "type/hex/min", true, "type/hex/min"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for i, order := range [][2]ir.TypeID{{tc.a, tc.b}, {tc.b, tc.a}} {
				reg := twinRegistry()
				stubbed, recorded := stubMerger(reg)
				dst := ir.Property{WireName: "x", Type: ir.TypeRef{Target: order[0]}, Provenance: ptrAt("/allOf/0/properties/x")}
				src := ir.Property{WireName: "x", Type: ir.TypeRef{Target: order[1]}, Provenance: ptrAt("/allOf/1/properties/x")}
				for _, property := range []ir.Property{dst, src} {
					if typeCommon := reg[property.Type.Target].Common(); typeCommon.Anonymous {
						typeCommon.Provenance = property.Provenance
					}
				}

				dst = foldTwo(stubbed, dst, src, written(`{"type":"x"}`))

				want := tc.win
				switch {
				case want != "":
				case tc.same:
					want = min(tc.a, tc.b)
				default:
					want = order[1]
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
