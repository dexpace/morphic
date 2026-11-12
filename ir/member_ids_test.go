// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestEnumMemberKey_EncodesEveryAdmissibleKind writes each key out: a tag for
// the kind of value, a colon, then the value as it is: the ID escapes the whole
// key as one pointer token.
func TestEnumMemberKey_EncodesEveryAdmissibleKind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		val  ir.Value
		want string
	}{
		{"string", ir.Value{Kind: ir.ValueString, Str: "a-b"}, "s:a-b"},
		{"string keeping a slash", ir.Value{Kind: ir.ValueString, Str: "a/b"}, "s:a/b"},
		{"string with % and #", ir.Value{Kind: ir.ValueString, Str: "100%#1"}, "s:100%#1"},
		{"empty string", ir.Value{Kind: ir.ValueString}, "s:"},
		{"symbol", ir.Value{Kind: ir.ValueSymbol, Str: "ok"}, "y:ok"},
		{"number", ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("-1.5")}, "n:-1.5e0"},
		{"number spelled with an exponent", ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("-15e-1")}, "n:-1.5e0"},
		{"number outside the decimal grammar", ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("1p4")}, "n:1p4"},
		{"bool", ir.Value{Kind: ir.ValueBool, Bool: true}, "b:true"},
		{"bytes", ir.Value{Kind: ir.ValueBytes, Bytes: []byte("hi?>")}, "x:aGk_Pg"},
		{"null", ir.Value{Kind: ir.ValueNull}, "z:"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			key, ok := ir.EnumMemberKey(tc.val)
			require.True(t, ok)
			assert.Equal(t, tc.want, key)
		})
	}
}

// TestEnumMemberKey_IsInjectiveAcrossKinds holds that two values of different
// kinds never share a key, whatever text they spell, and that no value's ID is
// the ID of a repeat: the string a/2 is not the second a.
func TestEnumMemberKey_IsInjectiveAcrossKinds(t *testing.T) {
	t.Parallel()
	values := []ir.Value{
		{Kind: ir.ValueString, Str: "1"},
		{Kind: ir.ValueSymbol, Str: "1"},
		{Kind: ir.ValueNumber, Num: ir.BigVal("1")},
		{Kind: ir.ValueString, Str: "true"},
		{Kind: ir.ValueBool, Bool: true},
		{Kind: ir.ValueString, Str: ""},
		{Kind: ir.ValueNull},
		{Kind: ir.ValueString, Str: "a/2"},
		{Kind: ir.ValueString, Str: "a"},
	}
	const enum = ir.TypeID("t/openapi/components/schemas/E")
	repeat, ok := ir.EnumMemberIDFor(enum, "s:a", 2)
	require.True(t, ok)
	seen := map[string]int{}
	for i, v := range values {
		key, ok := ir.EnumMemberKey(v)
		require.True(t, ok)
		require.NotContains(t, seen, key, "values %d and %d share a key", seen[key], i)
		seen[key] = i
		id, ok := ir.EnumMemberIDFor(enum, key, 1)
		require.True(t, ok)
		assert.NotEqual(t, repeat, id, "value %d spells the second a", i)
	}
}

// TestEnumMemberKey_RefusesWhatNoEnumHolds pins the kinds with no key. A list,
// an object, a reference, a constructor and a non-finite number are not
// members of an enum, so the lowering degrades the whole set to a union before
// it asks for one.
func TestEnumMemberKey_RefusesWhatNoEnumHolds(t *testing.T) {
	t.Parallel()
	for _, kind := range []ir.ValueKind{
		ir.ValueList, ir.ValueObject, ir.ValueRefKind, ir.ValueCtor,
		ir.ValueInf, ir.ValueNegInf, ir.ValueNaN,
		ir.ValueKind("a kind ir does not declare"),
	} {
		key, ok := ir.EnumMemberKey(ir.Value{Kind: kind})
		assert.False(t, ok, "%s has no key", kind)
		assert.Empty(t, key)
	}
}

// TestEnumMemberIDFor_LivesBeneathItsEnum pins a member's ID to its enum's: the
// enum's namespace, qualified or not, its path, then one escaped key, and the
// occurrence number for a repeat. A key spelling a slash and a number is not a
// repeat, and an enum whose ID is a namespace alone takes its key there.
func TestEnumMemberIDFor_LivesBeneathItsEnum(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		enum       ir.TypeID
		key        string
		occurrence int
		want       ir.EnumMemberID
	}{
		{"a component schema", "t/openapi/components/schemas/E", "s:a", 1, "e/openapi/components/schemas/E/s:a"},
		{"an inline schema", "t/anon/components/schemas/S/properties/p", "s:a", 1,
			"e/anon/components/schemas/S/properties/p/s:a"},
		{"a qualified namespace", "t/openapi@dir~1a~0b.yaml/components/schemas/User", "s:a", 1,
			"e/openapi@dir~1a~0b.yaml/components/schemas/User/s:a"},
		{"a namespace alone", "t/proto.v1", "s:a", 1, "e/proto.v1/s:a"},
		{"a key holding a slash", "t/openapi/components/schemas/E", "s:a/b", 1, "e/openapi/components/schemas/E/s:a~1b"},
		{"a key holding a tilde", "t/openapi/components/schemas/E", "s:a~b", 1, "e/openapi/components/schemas/E/s:a~0b"},
		{"a repeat", "t/openapi/components/schemas/E", "s:a", 2, "e/openapi/components/schemas/E/s:a/2"},
		{"a third occurrence", "t/openapi/components/schemas/E", "s:a", 3, "e/openapi/components/schemas/E/s:a/3"},
		{"a key spelling a repeat", "t/openapi/components/schemas/E", "s:a/2", 1,
			"e/openapi/components/schemas/E/s:a~12"},
	}
	seen := map[ir.EnumMemberID]string{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ir.EnumMemberIDFor(tc.enum, tc.key, tc.occurrence)
			require.True(t, ok)
			assert.Equal(t, tc.want, got)
			assert.True(t, ir.WellFormedID(ir.IDKindEnumMember, string(got)))
			memberSpace, _ := ir.IDSpace(ir.IDKindEnumMember, string(got))
			enumSpace, _ := ir.IDSpace(ir.IDKindType, string(tc.enum))
			assert.Equal(t, enumSpace, memberSpace, "a member lives in its enum's own namespace")
		})
		require.NotContains(t, seen, tc.want, "%s and %s share an ID", tc.name, seen[tc.want])
		seen[tc.want] = tc.name
	}
}

// TestEnumMemberIDFor_RefusesWhatPlacesNoMember pins the inputs no member ID
// follows from: an enum ID that is no well-formed type ID, and an occurrence
// below the first.
func TestEnumMemberIDFor_RefusesWhatPlacesNoMember(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		enum       ir.TypeID
		occurrence int
	}{
		{"no ID", "", 1},
		{"no kind", "openapi/components/schemas/E", 1},
		{"another kind", "e/openapi/components/schemas/E", 1},
		{"no namespace", "t//components/schemas/E", 1},
		{"a kind alone", "t/", 1},
		{"an empty path", "t/openapi/", 1},
		{"no occurrence", "t/openapi/components/schemas/E", 0},
		{"a negative occurrence", "t/openapi/components/schemas/E", -1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ir.EnumMemberIDFor(tc.enum, "s:a", tc.occurrence)
			assert.False(t, ok)
			assert.Empty(t, got)
		})
	}
}
