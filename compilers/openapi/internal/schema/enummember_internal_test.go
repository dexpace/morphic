package schema

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
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
			key, ok := enumMemberKey(tc.val)
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
	const enum = jsontext.Pointer("/components/schemas/E")
	repeat := ids.EnumMember(enum, "s:a", 2)
	seen := map[string]int{}
	for i, v := range values {
		key, ok := enumMemberKey(v)
		require.True(t, ok)
		require.NotContains(t, seen, key, "values %d and %d share a key", seen[key], i)
		seen[key] = i
		assert.NotEqual(t, repeat, ids.EnumMember(enum, key, 1), "value %d spells the second a", i)
	}
}

// TestEnumMemberKey_RefusesWhatNoEnumHolds pins the kinds with no key. A list,
// an object, a reference and a constructor are not members of an enum, so the
// lowering degrades the whole set to a union before it asks for one.
func TestEnumMemberKey_RefusesWhatNoEnumHolds(t *testing.T) {
	t.Parallel()
	for _, kind := range []ir.ValueKind{
		ir.ValueList, ir.ValueObject, ir.ValueRefKind, ir.ValueCtor,
		ir.ValueKind("a kind ir does not declare"),
	} {
		key, ok := enumMemberKey(ir.Value{Kind: kind})
		assert.False(t, ok, "%s has no key", kind)
		assert.Empty(t, key)
	}
}
