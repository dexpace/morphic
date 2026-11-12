// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/ir"
)

// TestEnumMemberForm_ClassifiesEveryValueKind pins the classification arm by
// arm, including the kinds OpenAPI cannot produce. ValueKind is IR-wide, and
// the exhaustive linter requires every member of it to be named here — this is
// what says the naming is a decision rather than a formality.
func TestEnumMemberForm_ClassifiesEveryValueKind(t *testing.T) {
	t.Parallel()
	admissible := []struct {
		name string
		val  ir.Value
		prim ir.PrimKind
		text string
	}{
		{"string", ir.Value{Kind: ir.ValueString, Str: "s"}, ir.PrimString, "s"},
		{"symbol", ir.Value{Kind: ir.ValueSymbol, Str: "ok"}, ir.PrimString, "ok"},
		{"number", ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("1.5")}, ir.PrimNumber, "1.5"},
		{"bool", ir.Value{Kind: ir.ValueBool, Bool: true}, ir.PrimBool, "true"},
		{"bytes", ir.Value{Kind: ir.ValueBytes, Bytes: []byte("hi")}, ir.PrimBytes, "aGk="},
	}
	for _, tc := range admissible {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prim, text, ok := enumMemberForm(tc.val)
			require.True(t, ok, "%s is admissible as an enum member", tc.name)
			assert.Equal(t, tc.prim, prim)
			assert.Equal(t, tc.text, text)
		})
	}

	refused := []ir.ValueKind{
		ir.ValueNull, ir.ValueList, ir.ValueObject, ir.ValueRefKind, ir.ValueCtor,
		ir.ValueInf, ir.ValueNegInf, ir.ValueNaN,
		ir.ValueKind("a kind ir does not declare"),
	}
	for _, kind := range refused {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			prim, text, ok := enumMemberForm(ir.Value{Kind: kind})
			assert.False(t, ok, "%q has no place in an Enum", kind)
			assert.Empty(t, prim, "a refused kind asserts no primitive")
			assert.Empty(t, text)
		})
	}
}

// TestEnumMembers_KeepsNoneBeneathAnIDNoMemberFits pins the assertion on the
// enum ID a member is placed beneath: one that is no type ID gives no member a
// place, so the set keeps none rather than members carrying malformed IDs. The
// same nodes under a well-formed ID make members, so the refusal is the ID's.
func TestEnumMembers_KeepsNoneBeneathAnIDNoMemberFits(t *testing.T) {
	t.Parallel()
	nodes := []*yaml.Node{{Kind: yaml.ScalarNode, Tag: "!!str", Value: "a"}}

	members, _, ok := enumMembers(nodes, false, "t/openapi/components/schemas/E")
	require.True(t, ok)
	assert.Equal(t, []ir.EnumMember{{ID: "e/openapi/components/schemas/E/s:a", Name: ir.NamingFor("a"),
		Value: ir.Value{Kind: ir.ValueString, Str: "a"}}}, members)

	members, prim, ok := enumMembers(nodes, false, "openapi/components/schemas/E")
	assert.False(t, ok, "no member is placed beneath an ID that is no type ID")
	assert.Empty(t, members)
	assert.Empty(t, prim)
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
// reference, a constructor, a non-finite number, anything nested in one, and a
// value nested past the depth bound.
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
		"infinity":              {Kind: ir.ValueInf},
		"negative infinity":     {Kind: ir.ValueNegInf},
		"not a number":          {Kind: ir.ValueNaN},
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
