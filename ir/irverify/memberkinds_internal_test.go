// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

const memberKindCode = "ir/enum-member-kind"

// admittedMemberKinds is, for each value type, the kinds of value an enum of it
// holds as a member: the table docs/ir/enums.md gives for what "matches
// ValueType" means. No row admits a list, an object, a reference, a
// constructor or a non-finite number, so a member of one is refused under
// every value type.
var admittedMemberKinds = func() map[ir.PrimitiveKind][]ir.ValueKind {
	text := []ir.ValueKind{ir.ValueString, ir.ValueSymbol}
	number := []ir.ValueKind{ir.ValueNumber}
	rows := map[ir.PrimitiveKind][]ir.ValueKind{
		ir.PrimitiveBool:  {ir.ValueBool},
		ir.PrimitiveBytes: {ir.ValueBytes},
		ir.PrimitiveAny: {
			ir.ValueBool, ir.ValueString, ir.ValueSymbol, ir.ValueNumber, ir.ValueBytes, ir.ValueNull,
		},
	}
	for _, primitiveKind := range []ir.PrimitiveKind{
		ir.PrimitiveString, ir.PrimitiveDate, ir.PrimitiveTime, ir.PrimitiveDatetime, ir.PrimitiveDatetimeOffset, ir.PrimitiveDuration,
		ir.PrimitiveURL, ir.PrimitiveUUID,
	} {
		rows[primitiveKind] = text
	}
	for _, primitiveKind := range []ir.PrimitiveKind{
		ir.PrimitiveInt8, ir.PrimitiveInt16, ir.PrimitiveInt32, ir.PrimitiveInt64, ir.PrimitiveUint8, ir.PrimitiveUint16, ir.PrimitiveUint32,
		ir.PrimitiveUint64, ir.PrimitiveInteger, ir.PrimitiveFloat32, ir.PrimitiveFloat64, ir.PrimitiveFloat, ir.PrimitiveNumber,
		ir.PrimitiveDecimal, ir.PrimitiveDecimal128,
	} {
		rows[primitiveKind] = number
	}
	return rows
}()

// askedMemberKinds are the kinds of value an enum member may be held to, each
// with a value that fills its payload.
var askedMemberKinds = []ir.Value{
	{Kind: ir.ValueNull},
	{Kind: ir.ValueBool, Bool: true},
	{Kind: ir.ValueString, String: "a"},
	{Kind: ir.ValueSymbol, String: "a"},
	{Kind: ir.ValueNumber, Number: "1"},
	{Kind: ir.ValueBytes, Bytes: []byte("a")},
	{Kind: ir.ValueList, List: []ir.Value{{Kind: ir.ValueNull}}},
	{Kind: ir.ValueObject, Object: []ir.Field{{Name: "a", Value: ir.Value{Kind: ir.ValueNull}}}},
	{Kind: ir.ValueRefKind, Ref: &ir.ValueRef{Type: "type/x/E", Member: "m"}},
	{Kind: ir.ValueConstructor, Constructor: &ir.ConstructorValue{Scalar: "type/x/E"}},
	{Kind: ir.ValueInfinity},
	{Kind: ir.ValueNegativeInfinity},
	{Kind: ir.ValueNaN},
}

// enumHolding returns a document of one enum of value type vt with one member
// holding value.
func enumHolding(vt ir.PrimitiveKind, value ir.Value) *ir.Document {
	name := ir.Naming{Source: "e", Canonical: "e"}
	return &ir.Document{
		IRVersion: ir.IRVersion,
		Types: ir.TypeRegistry{
			"type/x/E": &ir.Enum{ID: "type/x/E", Name: name, ValueType: vt, Members: []ir.EnumMember{
				{ID: "enum-member/x/E/m", Name: name, Value: value},
			}},
		},
	}
}

// TestMemberKinds_EveryPrimKindHasARow holds the table above to the PrimitiveKind
// constants ir declares, both ways: a value type added to ir without a row
// would admit no kind or all of them, and a row for one ir does not declare is
// a test of nothing.
func TestMemberKinds_EveryPrimKindHasARow(t *testing.T) {
	t.Parallel()
	consts, err := irDecls(t).StringConsts("PrimitiveKind")
	require.NoError(t, err)
	require.NotEmpty(t, consts)
	declared := map[ir.PrimitiveKind]bool{}
	for _, declaredConst := range consts {
		kind := ir.PrimitiveKind(declaredConst.Value)
		declared[kind] = true
		assert.Contains(t, admittedMemberKinds, kind, "ir.%s is a value type with no row of admitted member kinds", declaredConst.Name)
	}
	for kind := range admittedMemberKinds {
		assert.True(t, declared[kind], "a row names value type %q, which ir does not declare", kind)
	}
}

// TestMemberKinds_AsksEveryValueKind holds askedMemberKinds to the ValueKind
// constants ir declares, so a kind ir adds is asked of every value type rather
// than left out of the test below.
func TestMemberKinds_AsksEveryValueKind(t *testing.T) {
	t.Parallel()
	consts, err := irDecls(t).StringConsts("ValueKind")
	require.NoError(t, err)
	declared := make([]ir.ValueKind, 0, len(consts))
	for _, declaredConst := range consts {
		declared = append(declared, ir.ValueKind(declaredConst.Value))
	}
	asked := make([]ir.ValueKind, 0, len(askedMemberKinds))
	for _, value := range askedMemberKinds {
		asked = append(asked, value.Kind)
	}
	assert.ElementsMatch(t, declared, asked)
}

// TestVerify_EnumMemberKindMatchesTheValueType asks every value type about
// every kind of member an enum may hold: a kind its row admits draws nothing,
// and any other draws the code at the member's value.
func TestVerify_EnumMemberKindMatchesTheValueType(t *testing.T) {
	t.Parallel()
	for vt, admitted := range admittedMemberKinds {
		t.Run(string(vt), func(t *testing.T) {
			t.Parallel()
			for _, value := range askedMemberKinds {
				var got []string
				for _, violation := range Verify(enumHolding(vt, value)) {
					if violation.Code == memberKindCode {
						got = append(got, violation.Path)
					}
				}
				if slices.Contains(admitted, value.Kind) {
					assert.Empty(t, got, "a %s member of an enum of value type %s", value.Kind, vt)
					continue
				}
				assert.Equal(t, []string{"types[type/x/E].Members[0].Value"}, got,
					"a %s member of an enum of value type %s", value.Kind, vt)
			}
		})
	}
}

// TestVerify_EnumMemberKindNamesBothKinds pins the wording.
func TestVerify_EnumMemberKindNamesBothKinds(t *testing.T) {
	t.Parallel()
	var got []string
	for _, violation := range Verify(enumHolding(ir.PrimitiveInteger, ir.Value{Kind: ir.ValueString, String: "a"})) {
		if violation.Code == memberKindCode {
			got = append(got, violation.Message)
		}
	}
	assert.Equal(t, []string{"enum of value type integer holds a string member"}, got)
}

// TestVerify_EnumMemberKindIsReadOnlyForADeclaredValueType pins that an enum of
// a value type no PrimitiveKind declares draws ir/unknown-prim-kind alone: there is
// no row to hold its members to.
func TestVerify_EnumMemberKindIsReadOnlyForADeclaredValueType(t *testing.T) {
	t.Parallel()
	got := Verify(enumHolding("bogus", ir.Value{Kind: ir.ValueString, String: "a"}))
	codes := make([]string, 0, len(got))
	for _, violation := range got {
		codes = append(codes, violation.Code)
	}
	assert.Contains(t, codes, "ir/unknown-prim-kind")
	assert.NotContains(t, codes, memberKindCode)
}
