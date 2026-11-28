// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package typemap_test

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/emitters/typemap"
	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/ir"
)

const (
	shopspring    = "github.com/shopspring/decimal"
	runtimeModule = "example.com/sdk/runtime"
)

// builtInTable returns a new built-in table on each call, so no test shares a
// map or a pointer with another.
func builtInTable() typemap.Table {
	return typemap.Table{
		Primitives: map[ir.PrimitiveKind]map[string]typemap.Row{
			ir.PrimitiveString: {"": {Type: "string"}},
			ir.PrimitiveDecimal: {"": {
				Type:       "decimal.Decimal",
				Imports:    map[string]string{"decimal": shopspring},
				Dependency: &typemap.Dependency{Module: shopspring, MinVersion: "v1.4.0"},
			}},
			ir.PrimitiveBytes: {"base64url": {
				Type:    "[]byte",
				Codec:   "Base64URL",
				Imports: map[string]string{"runtime": runtimeModule},
			}},
		},
		Externals: map[string]typemap.Row{
			"erlang:pid": {Type: "Pid", Imports: map[string]string{"runtime": runtimeModule}},
		},
	}
}

// declaredKinds returns the primitive kinds ir declares, read through
// internal/decls, in byte order.
func declaredKinds(t *testing.T) []ir.PrimitiveKind {
	t.Helper()
	loadedPackage, err := decls.Load("github.com/dexpace/morphic/ir")
	require.NoError(t, err)
	consts, err := loadedPackage.StringConsts("PrimitiveKind")
	require.NoError(t, err)
	kinds := make([]ir.PrimitiveKind, 0, len(consts))
	for _, declaredConst := range consts {
		kinds = append(kinds, ir.PrimitiveKind(declaredConst.Value))
	}
	require.NotEmpty(t, kinds)
	slices.Sort(kinds)
	return kinds
}

func TestMerge_ReplacesARowWhole(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		user typemap.Table
		row  func(typemap.Table) typemap.Row
		want typemap.Row
	}{
		{
			name: "a default row keeps neither the built-in imports nor its dependency",
			user: typemap.Table{Primitives: map[ir.PrimitiveKind]map[string]typemap.Row{
				ir.PrimitiveDecimal: {"": {Type: "big.Float", Imports: map[string]string{"big": "math/big"}}},
			}},
			row:  func(table typemap.Table) typemap.Row { return table.Primitives[ir.PrimitiveDecimal][""] },
			want: typemap.Row{Type: "big.Float", Imports: map[string]string{"big": "math/big"}},
		},
		{
			name: "a row stating a type alone keeps neither the codec nor the imports",
			user: typemap.Table{Primitives: map[ir.PrimitiveKind]map[string]typemap.Row{
				ir.PrimitiveBytes: {"base64url": {Type: "Bytes"}},
			}},
			row:  func(table typemap.Table) typemap.Row { return table.Primitives[ir.PrimitiveBytes]["base64url"] },
			want: typemap.Row{Type: "Bytes"},
		},
		{
			name: "an external row",
			user: typemap.Table{Externals: map[string]typemap.Row{"erlang:pid": {Type: "Process"}}},
			row:  func(table typemap.Table) typemap.Row { return table.Externals["erlang:pid"] },
			want: typemap.Row{Type: "Process"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			merged := typemap.Merge(builtInTable(), tc.user)
			assert.Empty(t, cmp.Diff(tc.want, tc.row(merged)))
		})
	}
}

func TestMerge_AddsTheUsersRowsAndKeepsTheRest(t *testing.T) {
	t.Parallel()
	user := typemap.Table{
		Primitives: map[ir.PrimitiveKind]map[string]typemap.Row{
			ir.PrimitiveUUID:   {"": {Type: "uuid.UUID"}},
			ir.PrimitiveString: {"email": {Type: "Email"}},
		},
		Externals: map[string]typemap.Row{"erlang:ref": {Type: "Ref"}},
	}
	want := builtInTable()
	want.Primitives[ir.PrimitiveUUID] = map[string]typemap.Row{"": {Type: "uuid.UUID"}}
	want.Primitives[ir.PrimitiveString]["email"] = typemap.Row{Type: "Email"}
	want.Externals["erlang:ref"] = typemap.Row{Type: "Ref"}

	assert.Empty(t, cmp.Diff(want, typemap.Merge(builtInTable(), user)))
}

func TestMerge_HoldsNoEmptyMap(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		builtIn, user typemap.Table
		want          typemap.Table
	}{
		{name: "an empty user table", builtIn: builtInTable(), want: builtInTable()},
		{name: "a user kind holding no row",
			builtIn: builtInTable(),
			user: typemap.Table{Primitives: map[ir.PrimitiveKind]map[string]typemap.Row{
				ir.PrimitiveString: {}, ir.PrimitiveUUID: {},
			}, Externals: map[string]typemap.Row{}},
			want: builtInTable()},
		{name: "a built-in kind holding no row",
			builtIn: typemap.Table{Primitives: map[ir.PrimitiveKind]map[string]typemap.Row{ir.PrimitiveString: {}}},
			want:    typemap.Table{}},
		{name: "built-in externals holding no row",
			builtIn: typemap.Table{Externals: map[string]typemap.Row{}},
			want:    typemap.Table{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, cmp.Diff(tc.want, typemap.Merge(tc.builtIn, tc.user)))
		})
	}
}

// userTable returns a new user table holding a row with imports and a
// dependency on each call.
func userTable() typemap.Table {
	return typemap.Table{
		Primitives: map[ir.PrimitiveKind]map[string]typemap.Row{
			ir.PrimitiveBytes: {"base64url": {
				Type:       "Bytes",
				Imports:    map[string]string{"bytes": "example.com/bytes"},
				Dependency: &typemap.Dependency{Module: "example.com/bytes", MinVersion: "v1.0.0"},
			}},
		},
		Externals: map[string]typemap.Row{"erlang:ref": {Type: "Ref", Imports: map[string]string{"ref": "example.com/ref"}}},
	}
}

func TestMerge_WritesNeitherArgument(t *testing.T) {
	t.Parallel()
	builtIn, user := builtInTable(), userTable()
	merged := typemap.Merge(builtIn, user)

	merged.Primitives[ir.PrimitiveString]["x"] = typemap.Row{Type: "X"}
	merged.Primitives[ir.PrimitiveDecimal][""].Imports["y"] = "y"
	merged.Primitives[ir.PrimitiveDecimal][""].Dependency.MinVersion = "v9"
	merged.Primitives[ir.PrimitiveBytes]["base64url"].Imports["y"] = "y"
	merged.Primitives[ir.PrimitiveBytes]["base64url"].Dependency.MinVersion = "v9"
	merged.Externals["new"] = typemap.Row{Type: "New"}
	merged.Externals["erlang:pid"].Imports["y"] = "y"
	merged.Externals["erlang:ref"].Imports["y"] = "y"
	assert.Empty(t, cmp.Diff(builtInTable(), builtIn), "writing the merged table wrote the built-in one")
	assert.Empty(t, cmp.Diff(userTable(), user), "writing the merged table wrote the user's")

	before := typemap.Merge(builtInTable(), userTable())
	merged = typemap.Merge(builtIn, user)
	user.Primitives[ir.PrimitiveBytes]["base64url"].Imports["z"] = "z"
	user.Primitives[ir.PrimitiveBytes]["base64url"].Dependency.Module = "z"
	user.Externals["erlang:ref"].Imports["z"] = "z"
	assert.Empty(t, cmp.Diff(before, merged), "writing the user's table wrote the merged one")
}

func TestMoved_ListsABuiltInRowTheUserDoesNotReplace(t *testing.T) {
	t.Parallel()
	dependency := func(module, minVersion string) *typemap.Dependency {
		return &typemap.Dependency{Module: module, MinVersion: minVersion}
	}
	stated := typemap.Table{
		Primitives: map[ir.PrimitiveKind]map[string]typemap.Row{
			ir.PrimitiveString:   {"": {Type: "string"}},
			ir.PrimitiveDecimal:  {"": {Type: "decimal.Decimal"}},
			ir.PrimitiveBytes:    {"base64url": {Type: "[]byte", Codec: "A"}},
			ir.PrimitiveUUID:     {"": {Type: "UUID", Imports: map[string]string{"a": "1"}}},
			ir.PrimitiveDate:     {"": {Type: "Date", Dependency: dependency("m", "v1")}},
			ir.PrimitiveDatetime: {"": {Type: "DateTime", Dependency: dependency("m1", "v1")}},
			ir.PrimitiveTime:     {"": {Type: "Time"}},
			ir.PrimitiveNumber:   {"": {Type: "Number", Dependency: dependency("m", "v1")}},
			ir.PrimitiveFloat:    {"": {Type: "float64"}},
			ir.PrimitiveInt32:    {"": {Type: "int32"}},
			ir.PrimitiveDuration: {"iso8601": {Type: "Duration"}},
		},
		Externals: map[string]typemap.Row{
			"a": {Type: "A"}, "b": {Type: "B"}, "c": {Type: "C"}, "d": {Type: "D"},
		},
	}
	current := typemap.Table{
		Primitives: map[ir.PrimitiveKind]map[string]typemap.Row{
			ir.PrimitiveString:   {"": {Type: "string"}},
			ir.PrimitiveDecimal:  {"": {Type: "big.Float"}},
			ir.PrimitiveBytes:    {"base64url": {Type: "[]byte", Codec: "B"}},
			ir.PrimitiveUUID:     {"": {Type: "UUID", Imports: map[string]string{"a": "2"}}},
			ir.PrimitiveDate:     {"": {Type: "Date", Dependency: dependency("m", "v2")}},
			ir.PrimitiveDatetime: {"": {Type: "DateTime", Dependency: dependency("m2", "v1")}},
			ir.PrimitiveTime:     {"": {Type: "Time", Dependency: dependency("m", "v1")}},
			ir.PrimitiveNumber:   {"": {Type: "Number"}},
			ir.PrimitiveFloat:    {"": {Type: "float64", Imports: map[string]string{}}},
			ir.PrimitiveInt64:    {"": {Type: "int64"}},
			ir.PrimitiveDuration: {"iso8601": {Type: "time.Duration"}},
			ir.PrimitiveURL:      {"": {Type: "url.URL"}},
		},
		Externals: map[string]typemap.Row{
			"a": {Type: "A2"}, "c": {Type: "C2"}, "d": {Type: "D"}, "e": {Type: "E"},
		},
	}
	user := typemap.Table{
		Primitives: map[ir.PrimitiveKind]map[string]typemap.Row{
			ir.PrimitiveDuration: {"iso8601": {Type: "Mine"}},
			ir.PrimitiveURL:      {"": {Type: "url.URL"}},
		},
		Externals: map[string]typemap.Row{"c": {Type: "Mine"}},
	}
	want := typemap.MovedRows{
		Primitives: []typemap.PrimitiveKey{
			{Primitive: ir.PrimitiveBytes, Encoding: "base64url"},
			{Primitive: ir.PrimitiveDate},
			{Primitive: ir.PrimitiveDatetime},
			{Primitive: ir.PrimitiveDecimal},
			{Primitive: ir.PrimitiveInt32},
			{Primitive: ir.PrimitiveInt64},
			{Primitive: ir.PrimitiveNumber},
			{Primitive: ir.PrimitiveTime},
			{Primitive: ir.PrimitiveUUID},
		},
		Externals: []string{"a", "b", "e"},
	}
	assert.Empty(t, cmp.Diff(want, typemap.Moved(stated, current, user)))
}

func TestMoved_SortsByPrimitiveThenEncoding(t *testing.T) {
	t.Parallel()
	added := typemap.Table{
		Primitives: map[ir.PrimitiveKind]map[string]typemap.Row{
			ir.PrimitiveString: {"b": {Type: "S"}, "": {Type: "S"}},
			ir.PrimitiveBool:   {"z": {Type: "B"}, "": {Type: "B"}, "a": {Type: "B"}},
			ir.PrimitiveBytes:  {"base64": {Type: "Y"}, "": {Type: "Y"}},
			ir.PrimitiveInt64:  {"x": {Type: "I"}, "": {Type: "I"}},
			ir.PrimitiveUUID:   {"hex": {Type: "U"}, "": {Type: "U"}},
			ir.PrimitiveDate:   {"y": {Type: "D"}, "": {Type: "D"}},
			ir.PrimitiveAny:    {"": {Type: "A"}},
			ir.PrimitiveURL:    {"": {Type: "L"}},
		},
		Externals: map[string]typemap.Row{"z": {Type: "Z"}, "a": {Type: "A"}, "m": {Type: "M"}},
	}
	key := func(primitive ir.PrimitiveKind, encoding string) typemap.PrimitiveKey {
		return typemap.PrimitiveKey{Primitive: primitive, Encoding: encoding}
	}
	want := typemap.MovedRows{
		Primitives: []typemap.PrimitiveKey{
			key(ir.PrimitiveAny, ""), key(ir.PrimitiveBool, ""), key(ir.PrimitiveBool, "a"),
			key(ir.PrimitiveBool, "z"), key(ir.PrimitiveBytes, ""), key(ir.PrimitiveBytes, "base64"),
			key(ir.PrimitiveDate, ""), key(ir.PrimitiveDate, "y"), key(ir.PrimitiveInt64, ""),
			key(ir.PrimitiveInt64, "x"), key(ir.PrimitiveString, ""), key(ir.PrimitiveString, "b"),
			key(ir.PrimitiveURL, ""), key(ir.PrimitiveUUID, ""), key(ir.PrimitiveUUID, "hex"),
		},
		Externals: []string{"a", "m", "z"},
	}
	for range 32 {
		assert.Empty(t, cmp.Diff(want, typemap.Moved(typemap.Table{}, added, typemap.Table{})))
	}
}

func TestMoved_IsEmptyBetweenATableAndItself(t *testing.T) {
	t.Parallel()
	assert.Empty(t, cmp.Diff(typemap.MovedRows{}, typemap.Moved(builtInTable(), builtInTable(), typemap.Table{})))
}

func TestMissing_ListsEachKindWithNoDefaultRow(t *testing.T) {
	t.Parallel()
	kinds := declaredKinds(t)
	assert.Equal(t, kinds, typemap.Missing(typemap.Table{}), "an empty table misses every declared kind, sorted")

	everyKind := typemap.Table{Primitives: map[ir.PrimitiveKind]map[string]typemap.Row{}}
	for _, kind := range kinds {
		everyKind.Primitives[kind] = map[string]typemap.Row{"": {Type: string(kind)}}
	}
	everyKind.Primitives["int65"] = map[string]typemap.Row{"": {Type: "int65"}}
	assert.Empty(t, typemap.Missing(everyKind), "a default row for every kind misses none")

	everyKind.Primitives[ir.PrimitiveUUID] = map[string]typemap.Row{"hex": {Type: "UUID"}}
	assert.Equal(t, []ir.PrimitiveKind{ir.PrimitiveUUID}, typemap.Missing(everyKind),
		"a kind only keyed rows name has no default row")
}
