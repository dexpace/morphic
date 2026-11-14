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
// ValueType" means. A kind no enum holds (list, object, ref, ctor and the
// non-finite numbers) is left to the member ID check and is not asked here.
var admittedMemberKinds = func() map[ir.PrimKind][]ir.ValueKind {
	text := []ir.ValueKind{ir.ValueString, ir.ValueSymbol}
	number := []ir.ValueKind{ir.ValueNumber}
	rows := map[ir.PrimKind][]ir.ValueKind{
		ir.PrimBool:  {ir.ValueBool},
		ir.PrimBytes: {ir.ValueBytes},
		ir.PrimAny: {
			ir.ValueBool, ir.ValueString, ir.ValueSymbol, ir.ValueNumber, ir.ValueBytes, ir.ValueNull,
		},
	}
	for _, k := range []ir.PrimKind{
		ir.PrimString, ir.PrimDate, ir.PrimTime, ir.PrimDatetime, ir.PrimDatetimeOffset, ir.PrimDuration,
		ir.PrimURL, ir.PrimUUID,
	} {
		rows[k] = text
	}
	for _, k := range []ir.PrimKind{
		ir.PrimInt8, ir.PrimInt16, ir.PrimInt32, ir.PrimInt64, ir.PrimUint8, ir.PrimUint16, ir.PrimUint32,
		ir.PrimUint64, ir.PrimInteger, ir.PrimFloat32, ir.PrimFloat64, ir.PrimFloat, ir.PrimNumber,
		ir.PrimDecimal, ir.PrimDecimal128,
	} {
		rows[k] = number
	}
	return rows
}()

// askedMemberKinds are the kinds of value an enum member may be held to, each
// with a value that fills its payload.
var askedMemberKinds = []ir.Value{
	{Kind: ir.ValueNull},
	{Kind: ir.ValueBool, Bool: true},
	{Kind: ir.ValueString, Str: "a"},
	{Kind: ir.ValueSymbol, Str: "a"},
	{Kind: ir.ValueNumber, Num: "1"},
	{Kind: ir.ValueBytes, Bytes: []byte("a")},
}

// enumHolding returns a document of one enum of value type vt with one member
// holding value.
func enumHolding(vt ir.PrimKind, value ir.Value) *ir.Document {
	name := ir.Naming{Source: "e", Canonical: "e"}
	return &ir.Document{
		IRVersion: ir.IRVersion,
		Types: ir.TypeRegistry{
			"t/x/E": &ir.Enum{ID: "t/x/E", Name: name, ValueType: vt, Members: []ir.EnumMember{
				{ID: "e/x/E/m", Name: name, Value: value},
			}},
		},
	}
}

// TestMemberKinds_EveryPrimKindHasARow holds the table above to the PrimKind
// constants ir declares, both ways: a value type added to ir without a row
// would admit no kind or all of them, and a row for one ir does not declare is
// a test of nothing.
func TestMemberKinds_EveryPrimKindHasARow(t *testing.T) {
	t.Parallel()
	consts, err := irDecls(t).StringConsts("PrimKind")
	require.NoError(t, err)
	require.NotEmpty(t, consts)
	declared := map[ir.PrimKind]bool{}
	for _, c := range consts {
		kind := ir.PrimKind(c.Value)
		declared[kind] = true
		assert.Contains(t, admittedMemberKinds, kind, "ir.%s is a value type with no row of admitted member kinds", c.Name)
	}
	for kind := range admittedMemberKinds {
		assert.True(t, declared[kind], "a row names value type %q, which ir does not declare", kind)
	}
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
				for _, v := range Verify(enumHolding(vt, value)) {
					if v.Code == memberKindCode {
						got = append(got, v.Path)
					}
				}
				if slices.Contains(admitted, value.Kind) {
					assert.Empty(t, got, "a %s member of an enum of value type %s", value.Kind, vt)
					continue
				}
				assert.Equal(t, []string{"types[t/x/E].Members[0].Value"}, got,
					"a %s member of an enum of value type %s", value.Kind, vt)
			}
		})
	}
}

// TestVerify_EnumMemberKindNamesBothKinds pins the wording.
func TestVerify_EnumMemberKindNamesBothKinds(t *testing.T) {
	t.Parallel()
	var got []string
	for _, v := range Verify(enumHolding(ir.PrimInteger, ir.Value{Kind: ir.ValueString, Str: "a"})) {
		if v.Code == memberKindCode {
			got = append(got, v.Message)
		}
	}
	assert.Equal(t, []string{"enum of value type integer holds a string member"}, got)
}

// TestVerify_EnumMemberKindIsReadOnlyForADeclaredValueType pins that an enum of
// a value type no PrimKind declares draws ir/unknown-prim-kind alone: there is
// no row to hold its members to.
func TestVerify_EnumMemberKindIsReadOnlyForADeclaredValueType(t *testing.T) {
	t.Parallel()
	got := Verify(enumHolding("bogus", ir.Value{Kind: ir.ValueString, Str: "a"}))
	codes := make([]string, 0, len(got))
	for _, v := range got {
		codes = append(codes, v.Code)
	}
	assert.Contains(t, codes, "ir/unknown-prim-kind")
	assert.NotContains(t, codes, memberKindCode)
}
