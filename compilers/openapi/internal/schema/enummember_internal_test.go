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

// TestValueKey_IsInjectiveAcrossNesting holds that values a flat encoding would
// confuse keep keys of their own: one element against two that concatenate to
// it, a member name or value spelling a key, and one member against two.
func TestValueKey_IsInjectiveAcrossNesting(t *testing.T) {
	t.Parallel()
	str := func(s string) ir.Value { return ir.Value{Kind: ir.ValueString, Str: s} }
	list := func(vs ...ir.Value) ir.Value { return ir.Value{Kind: ir.ValueList, List: vs} }
	obj := func(fs ...ir.Field) ir.Value { return ir.Value{Kind: ir.ValueObject, Object: fs} }
	values := []ir.Value{
		list(str("a,b")),
		list(str("a"), str("b")),
		list(str("as:b")),
		list(list(str("a")), str("b")),
		list(),
		obj(),
		obj(ir.Field{Name: "a", Value: str("b")}),
		obj(ir.Field{Name: `a":"s:b`, Value: list()}),
		obj(ir.Field{Name: "a", Value: str("b")}, ir.Field{Name: "c", Value: str("d")}),
		obj(ir.Field{Name: "a", Value: str("bs:c")}),
		obj(ir.Field{Name: "a", Value: str("b")}, ir.Field{Name: "", Value: str("c")}),
		str("l:[]"),
	}
	seen := map[string]int{}
	for i, v := range values {
		key, ok := valueKey(v, 0)
		require.True(t, ok, "value %d", i)
		require.NotContains(t, seen, key, "values %d and %d share a key", seen[key], i)
		seen[key] = i
	}
}

// TestValueKey_RefusesWhatNoSourceEnumHolds pins the values with no key: a
// reference, a constructor, anything nested in one, and a value nested past the
// depth bound.
func TestValueKey_RefusesWhatNoSourceEnumHolds(t *testing.T) {
	t.Parallel()
	ref := ir.Value{Kind: ir.ValueRefKind}
	deep := ir.Value{Kind: ir.ValueNull}
	for range maxValueKeyDepth + 1 {
		deep = ir.Value{Kind: ir.ValueList, List: []ir.Value{deep}}
	}
	for name, v := range map[string]ir.Value{
		"a reference":           ref,
		"a constructor":         {Kind: ir.ValueCtor},
		"a kind ir lacks":       {Kind: ir.ValueKind("a kind ir does not declare")},
		"a list holding one":    {Kind: ir.ValueList, List: []ir.Value{ref}},
		"an object holding one": {Kind: ir.ValueObject, Object: []ir.Field{{Name: "r", Value: ref}}},
		"past the depth bound":  deep,
	} {
		key, ok := valueKey(v, 0)
		assert.False(t, ok, name)
		assert.Empty(t, key, name)
	}
	_, ok := valueKey(deep.List[0], 0)
	assert.True(t, ok, "a value at the bound itself has a key")
}

// TestValueKey_GrowsByAFixedAmountPerLevel holds a nested key to a size linear
// in its depth. An encoding that escaped each nested key again, as quoting does,
// doubled the key at every level. Twenty levels tell the two apart by a factor
// of thousands, where the depth bound would exhaust memory before failing.
func TestValueKey_GrowsByAFixedAmountPerLevel(t *testing.T) {
	t.Parallel()
	const depth = 20
	deep := ir.Value{Kind: ir.ValueString, Str: `"\`}
	for range depth {
		deep = ir.Value{Kind: ir.ValueList, List: []ir.Value{deep}}
	}
	key, ok := valueKey(deep, 0)
	require.True(t, ok)
	assert.Less(t, len(key), 8*depth, "%d bytes", len(key))
}
