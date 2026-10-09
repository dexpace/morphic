package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func TestEnumMemberKey_EncodesEveryAdmissibleKind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		val  ir.Value
		want string
	}{
		{"string", ir.Value{Kind: ir.ValueString, Str: "a"}, "s:a"},
		{"symbol", ir.Value{Kind: ir.ValueSymbol, Str: "a"}, "y:a"},
		{"number", ir.Value{Kind: ir.ValueNumber, Num: "-1"}, "n:-1"},
		{"bool", ir.Value{Kind: ir.ValueBool, Bool: true}, "b:true"},
		{"bytes without a slash", ir.Value{Kind: ir.ValueBytes, Bytes: []byte{0xfb, 0xff, 0xfe}}, "x:-__-"},
		{"null", ir.Value{Kind: ir.ValueNull}, "z:"},
		{"introducer escaped", ir.Value{Kind: ir.ValueString, Str: "a#2"}, "s:a%232"},
		{"percent escaped", ir.Value{Kind: ir.ValueString, Str: "a%23"}, "s:a%2523"},
		{"separator escaped", ir.Value{Kind: ir.ValueString, Str: "a/b~"}, "s:a~1b~0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := enumMemberKey(tc.val)
			require.True(t, ok)
			assert.Equal(t, tc.want, got)
			assert.NotContains(t, got, "/", "a key is one ID segment")
		})
	}
}

func TestEnumMemberKey_IsInjectiveAcrossKinds(t *testing.T) {
	t.Parallel()
	vals := []ir.Value{
		{Kind: ir.ValueNumber, Num: "1"},
		{Kind: ir.ValueString, Str: "1"},
		{Kind: ir.ValueSymbol, Str: "1"},
		{Kind: ir.ValueNumber, Num: "-1"},
		{Kind: ir.ValueNumber, Num: "0"},
		{Kind: ir.ValueBool, Bool: true},
		{Kind: ir.ValueString, Str: "true"},
		{Kind: ir.ValueString, Str: "a#2"},
		{Kind: ir.ValueString, Str: "a%232"},
	}
	seen := map[string]int{}
	for i, v := range vals {
		key, ok := enumMemberKey(v)
		require.True(t, ok)
		prev, dup := seen[key]
		assert.False(t, dup, "value %d and %d share key %q", prev, i, key)
		seen[key] = i
	}
}

func TestEnumMemberKey_RefusesWhatNoEnumHolds(t *testing.T) {
	t.Parallel()
	for _, kind := range []ir.ValueKind{
		ir.ValueList, ir.ValueObject, ir.ValueRefKind, ir.ValueCtor, ir.ValueKind("undeclared"),
	} {
		key, ok := enumMemberKey(ir.Value{Kind: kind})
		assert.False(t, ok, "%q", kind)
		assert.Empty(t, key)
	}
}

func TestMemberID_FirstOccurrenceKeepsTheBareID(t *testing.T) {
	t.Parallel()
	enum := ir.TypeID("t/openapi/components/schemas/E")
	assert.Equal(t, ir.EnumMemberID("e/openapi/components/schemas/E/s:a"), memberID(enum, "s:a", 1))
	assert.Equal(t, ir.EnumMemberID("e/openapi/components/schemas/E/s:a#2"), memberID(enum, "s:a", 2))
	assert.Equal(t, ir.EnumMemberID("e/openapi/components/schemas/E/s:a#3"), memberID(enum, "s:a", 3))
}
