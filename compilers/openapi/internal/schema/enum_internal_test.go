// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// TestEnumMemberForm_ClassifiesEveryValueKind pins the classification arm by
// arm, including the kinds OpenAPI cannot produce. ValueKind is IR-wide, and
// the exhaustive linter requires every member of it to be named here — this is
// what says the naming is a decision rather than a formality.
func TestEnumMemberForm_ClassifiesEveryValueKind(t *testing.T) {
	t.Parallel()
	admissible := []struct {
		name      string
		val       ir.Value
		primitive ir.PrimitiveKind
		text      string
	}{
		{"string", ir.Value{Kind: ir.ValueString, String: "s"}, ir.PrimitiveString, "s"},
		{"symbol", ir.Value{Kind: ir.ValueSymbol, String: "ok"}, ir.PrimitiveString, "ok"},
		{"number", ir.Value{Kind: ir.ValueNumber, Number: ir.BigVal("1.5")}, ir.PrimitiveNumber, "1.5"},
		{"bool", ir.Value{Kind: ir.ValueBool, Bool: true}, ir.PrimitiveBool, "true"},
		{"bytes", ir.Value{Kind: ir.ValueBytes, Bytes: []byte("hi")}, ir.PrimitiveBytes, "aGk="},
	}
	for _, tc := range admissible {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			primitive, text, ok := enumMemberForm(tc.val)
			require.True(t, ok, "%s is admissible as an enum member", tc.name)
			assert.Equal(t, tc.primitive, primitive)
			assert.Equal(t, tc.text, text)
		})
	}

	refused := []ir.ValueKind{
		ir.ValueNull, ir.ValueList, ir.ValueObject, ir.ValueRefKind, ir.ValueConstructor,
		ir.ValueInfinity, ir.ValueNegativeInfinity, ir.ValueNaN,
		ir.ValueKind("a kind ir does not declare"),
	}
	for _, kind := range refused {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			primitive, text, ok := enumMemberForm(ir.Value{Kind: kind})
			assert.False(t, ok, "%q has no place in an Enum", kind)
			assert.Empty(t, primitive, "a refused kind asserts no primitive")
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

	members, _, ok := enumMembers(readEnum(nodes), "type/openapi/components/schemas/E")
	require.True(t, ok)
	assert.Equal(t, []ir.EnumMember{{ID: "enum-member/openapi/components/schemas/E/string:a", Name: ir.NamingFor("a"),
		Value: ir.Value{Kind: ir.ValueString, String: "a"}}}, members)

	members, primitive, ok := enumMembers(readEnum(nodes), "openapi/components/schemas/E")
	assert.False(t, ok, "no member is placed beneath an ID that is no type ID")
	assert.Empty(t, members)
	assert.Empty(t, primitive)
}

// TestValueKey_IsInjectiveAcrossNesting holds that values a flat encoding would
// confuse keep keys of their own: one element against two that concatenate to
// it, a member name or value spelling a key, and one member against two.
func TestValueKey_IsInjectiveAcrossNesting(t *testing.T) {
	t.Parallel()
	stringValue := func(s string) ir.Value { return ir.Value{Kind: ir.ValueString, String: s} }
	list := func(vs ...ir.Value) ir.Value { return ir.Value{Kind: ir.ValueList, List: vs} }
	obj := func(fs ...ir.Field) ir.Value { return ir.Value{Kind: ir.ValueObject, Object: fs} }
	values := []ir.Value{
		list(stringValue("a,b")),
		list(stringValue("a"), stringValue("b")),
		list(stringValue("as:b")),
		list(list(stringValue("a")), stringValue("b")),
		list(),
		obj(),
		obj(ir.Field{Name: "a", Value: stringValue("b")}),
		obj(ir.Field{Name: `a":"s:b`, Value: list()}),
		obj(ir.Field{Name: "a", Value: stringValue("b")}, ir.Field{Name: "c", Value: stringValue("d")}),
		obj(ir.Field{Name: "a", Value: stringValue("bs:c")}),
		obj(ir.Field{Name: "a", Value: stringValue("b")}, ir.Field{Name: "", Value: stringValue("c")}),
		stringValue("l:[]"),
	}
	seen := map[string]int{}
	for i, value := range values {
		key, ok := valueKey(value, 0)
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
	for name, value := range map[string]ir.Value{
		"a reference":           ref,
		"a constructor":         {Kind: ir.ValueConstructor},
		"infinity":              {Kind: ir.ValueInfinity},
		"negative infinity":     {Kind: ir.ValueNegativeInfinity},
		"not a number":          {Kind: ir.ValueNaN},
		"a kind ir lacks":       {Kind: ir.ValueKind("a kind ir does not declare")},
		"a list holding one":    {Kind: ir.ValueList, List: []ir.Value{ref}},
		"an object holding one": {Kind: ir.ValueObject, Object: []ir.Field{{Name: "r", Value: ref}}},
		"past the depth bound":  deep,
	} {
		key, ok := valueKey(value, 0)
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
	deep := ir.Value{Kind: ir.ValueString, String: `"\`}
	for range depth {
		deep = ir.Value{Kind: ir.ValueList, List: []ir.Value{deep}}
	}
	key, ok := valueKey(deep, 0)
	require.True(t, ok)
	assert.Less(t, len(key), 8*depth, "%d bytes", len(key))
}

// TestTypesAdmit_ReadsEachTypesKinds pins the type keyword's reading of a
// value, by the kinds the admission reading gives each: a type no schema
// defines judges nothing, and no type declared admits every value.
func TestTypesAdmit_ReadsEachTypesKinds(t *testing.T) {
	t.Parallel()
	stringValue, one, half := ir.Value{Kind: ir.ValueString, String: "1"}, ir.Value{Kind: ir.ValueNumber, Number: "1.0"},
		ir.Value{Kind: ir.ValueNumber, Number: "0.5"}
	null, yes := ir.Value{Kind: ir.ValueNull}, ir.Value{Kind: ir.ValueBool, Bool: true}
	obj, list := ir.Value{Kind: ir.ValueObject}, ir.Value{Kind: ir.ValueList}
	bin := ir.Value{Kind: ir.ValueBytes, Bytes: []byte("hi")}
	for _, tc := range []struct {
		types  []statement.JSONSchemaTypeName
		admits []ir.Value
		not    []ir.Value
	}{
		{nil, []ir.Value{stringValue, null, obj}, nil},
		{[]statement.JSONSchemaTypeName{statement.KindString}, []ir.Value{stringValue, bin}, []ir.Value{one, null}},
		{[]statement.JSONSchemaTypeName{statement.KindInteger}, []ir.Value{one}, []ir.Value{half, stringValue}},
		{[]statement.JSONSchemaTypeName{statement.KindNumber}, []ir.Value{one, half}, []ir.Value{stringValue}},
		{[]statement.JSONSchemaTypeName{statement.KindBoolean}, []ir.Value{yes}, []ir.Value{stringValue}},
		{[]statement.JSONSchemaTypeName{statement.KindObject}, []ir.Value{obj}, []ir.Value{list}},
		{[]statement.JSONSchemaTypeName{statement.KindArray}, []ir.Value{list}, []ir.Value{obj}},
		{[]statement.JSONSchemaTypeName{statement.KindInteger, statement.KindBoolean}, []ir.Value{one, yes}, []ir.Value{stringValue}},
		{[]statement.JSONSchemaTypeName{statement.KindInteger, "file"}, []ir.Value{stringValue, one, null}, nil},
	} {
		for _, value := range tc.admits {
			assert.True(t, typesAdmit(tc.types, value), "%v admits %+v", tc.types, value)
		}
		for _, value := range tc.not {
			assert.False(t, typesAdmit(tc.types, value), "%v admits no %+v", tc.types, value)
		}
	}
}

// TestDescribeValue_SpellsEachKind pins how a member is named in a message.
func TestDescribeValue_SpellsEachKind(t *testing.T) {
	t.Parallel()
	for value, want := range map[*ir.Value]string{
		{Kind: ir.ValueString, String: "a"}:     `"a"`,
		{Kind: ir.ValueSymbol, String: "b"}:     `"b"`,
		{Kind: ir.ValueNumber, Number: "1.5"}:   "1.5",
		{Kind: ir.ValueBool}:                    "false",
		{Kind: ir.ValueNull}:                    "null",
		{Kind: ir.ValueBytes}:                   "of kind bytes",
		{Kind: ir.ValueKind("a kind ir lacks")}: "of kind a kind ir lacks",
	} {
		assert.Equal(t, want, describeValue(*value))
	}
}
