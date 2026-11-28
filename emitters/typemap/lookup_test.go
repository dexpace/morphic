// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package typemap_test

import (
	"maps"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/emitters/typemap"
	"github.com/dexpace/morphic/ir"
)

// fullTable returns a new table holding a default row for every kind ir
// declares, typed "default <kind>", and the keyed rows the lookup's cases
// read, each typed "<kind>/<encoding>".
func fullTable(t *testing.T) typemap.Table {
	t.Helper()
	table := typemap.Table{Primitives: map[ir.PrimitiveKind]map[string]typemap.Row{}}
	for _, kind := range declaredKinds(t) {
		table.Primitives[kind] = map[string]typemap.Row{"": {Type: "default " + string(kind)}}
	}
	for _, key := range []typemap.PrimitiveKey{
		{Primitive: ir.PrimitiveBytes, Encoding: "base64url"},
		{Primitive: ir.PrimitiveBytes, Encoding: "base64"},
		{Primitive: ir.PrimitiveDatetimeOffset, Encoding: "http-date"},
		{Primitive: ir.PrimitiveString, Encoding: "base64"},
		{Primitive: ir.PrimitiveString, Encoding: "base32"},
		{Primitive: ir.PrimitiveString, Encoding: "A.B.C.D/N"},
	} {
		table.Primitives[key.Primitive][key.Encoding] = typemap.Row{Type: string(key.Primitive) + "/" + key.Encoding}
	}
	return table
}

// encoding is the Encoding named name whose value travels as wireType.
func encoding(name string, wireType ir.PrimitiveKind) typemap.Encoding {
	return typemap.Encoding{Name: name, WireType: wireType}
}

func TestKey_RekeysTextToEncodeAsBytes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		primitive ir.PrimitiveKind
		encoding  typemap.Encoding
		want      typemap.PrimitiveKey
	}{
		{"text to encode", ir.PrimitiveString, encoding("base64", ir.PrimitiveString),
			typemap.PrimitiveKey{Primitive: ir.PrimitiveBytes, Encoding: "base64"}},
		{"a string travelling as another primitive", ir.PrimitiveString, encoding("base64", ir.PrimitiveInt64),
			typemap.PrimitiveKey{Primitive: ir.PrimitiveString, Encoding: "base64"}},
		{"encoded text already", ir.PrimitiveString, encoding("base64", ""),
			typemap.PrimitiveKey{Primitive: ir.PrimitiveString, Encoding: "base64"}},
		{"a string under no name", ir.PrimitiveString, encoding("", ir.PrimitiveString),
			typemap.PrimitiveKey{Primitive: ir.PrimitiveString}},
		{"bytes travelling as text", ir.PrimitiveBytes, encoding("base64", ir.PrimitiveString),
			typemap.PrimitiveKey{Primitive: ir.PrimitiveBytes, Encoding: "base64"}},
		{"a uuid travelling as text", ir.PrimitiveUUID, encoding("hex", ir.PrimitiveString),
			typemap.PrimitiveKey{Primitive: ir.PrimitiveUUID, Encoding: "hex"}},
		{"no encoding", ir.PrimitiveInt64, typemap.Encoding{},
			typemap.PrimitiveKey{Primitive: ir.PrimitiveInt64}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, typemap.Key(tc.primitive, tc.encoding))
		})
	}
}

func TestKey_ChangesThePrimitiveOnlyForTextToEncode(t *testing.T) {
	t.Parallel()
	for _, kind := range declaredKinds(t) {
		for _, name := range []string{"", "x"} {
			for _, wireType := range []ir.PrimitiveKind{"", ir.PrimitiveString, ir.PrimitiveInt64} {
				textToEncode := kind == ir.PrimitiveString && name == "x" && wireType == ir.PrimitiveString
				key := typemap.Key(kind, encoding(name, wireType))
				assert.Equal(t, textToEncode, key.Primitive != kind,
					"Key(%s, {%q, %q}) is %+v", kind, name, wireType, key)
				assert.Equal(t, name, key.Encoding, "Key keeps the encoding's name")
			}
		}
	}
}

// lookupCase is one row of TestLookup_FindsTheRowAndSaysHowItWasFound: a
// value's primitive and encoding, the Type of the row Lookup must find, and
// how.
type lookupCase struct {
	name      string
	table     func(t *testing.T) typemap.Table
	primitive ir.PrimitiveKind
	encoding  typemap.Encoding
	wantType  string
	wantFound typemap.Found
	wantOK    bool
}

func TestLookup_FindsTheRowAndSaysHowItWasFound(t *testing.T) {
	t.Parallel()
	for _, tc := range lookupCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			row, found, ok := typemap.Lookup(tc.table(t), tc.primitive, tc.encoding)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.wantFound, found)
			assert.Equal(t, tc.wantOK, found != 0, "the zero Found is none, which only a miss returns")
			assert.Empty(t, cmp.Diff(typemap.Row{Type: tc.wantType}, row))
		})
	}
}

// lookupCases are the lookup's rows. Those named "no meaning" are encodings
// the IR gives no meaning, which pin that Lookup answers them, not that the
// answer is right.
func lookupCases() []lookupCase {
	exact, travelsAs, primitive, none := typemap.Exact, typemap.TravelsAs, typemap.Primitive, typemap.Found(0)
	uuidDefaultOnly := func(*testing.T) typemap.Table {
		return typemap.Table{Primitives: map[ir.PrimitiveKind]map[string]typemap.Row{ir.PrimitiveUUID: {"": {Type: "U"}}}}
	}
	uuidHexOnly := func(*testing.T) typemap.Table {
		return typemap.Table{Primitives: map[ir.PrimitiveKind]map[string]typemap.Row{ir.PrimitiveUUID: {"hex": {Type: "H"}}}}
	}
	undeclaredKindRow := func(*testing.T) typemap.Table {
		return typemap.Table{Primitives: map[ir.PrimitiveKind]map[string]typemap.Row{"int65": {"": {Type: "I"}}}}
	}
	empty := func(*testing.T) typemap.Table { return typemap.Table{} }
	return []lookupCase{
		{"1 a keyed row", fullTable, ir.PrimitiveBytes, encoding("base64url", ir.PrimitiveString), "bytes/base64url", exact, true},
		{"2 an encoding with no row", fullTable, ir.PrimitiveString, encoding("email", ""), "default string", primitive, true},
		{"3 a timestamp travelling as int64", fullTable, ir.PrimitiveDatetimeOffset, encoding("unixTimestamp", ir.PrimitiveInt64), "default int64", travelsAs, true},
		{"3 a timestamp travelling as integer", fullTable, ir.PrimitiveDatetimeOffset, encoding("unixTimestamp", ir.PrimitiveInteger), "default integer", travelsAs, true},
		{"3 a timestamp travelling as number", fullTable, ir.PrimitiveDatetimeOffset, encoding("unixTimestamp", ir.PrimitiveNumber), "default number", travelsAs, true},
		{"3 a timestamp travelling as string", fullTable, ir.PrimitiveDatetimeOffset, encoding("unixTimestamp", ir.PrimitiveString), "default string", travelsAs, true},
		{"4 a string-spelled kind under another spelling", fullTable, ir.PrimitiveUUID, encoding("hex", ""), "default string", travelsAs, true},
		{"5 no encoding", fullTable, ir.PrimitiveInt64, typemap.Encoding{}, "default int64", exact, true},
		{"6 text to encode takes the bytes row", fullTable, ir.PrimitiveString, encoding("base64", ir.PrimitiveString), "bytes/base64", exact, true},
		{"7 text to encode with no bytes row", fullTable, ir.PrimitiveString, encoding("base32", ir.PrimitiveString), "default string", travelsAs, true},
		{"8 encoded text already", fullTable, ir.PrimitiveString, encoding("base64", ""), "string/base64", exact, true},
		{"8 encoded text with no row", fullTable, ir.PrimitiveString, encoding("base58", ""), "default string", primitive, true},
		{"9 bytes travelling as text", fullTable, ir.PrimitiveBytes, encoding("base32", ir.PrimitiveString), "default string", travelsAs, true},
		{"10 an unknown date encoding", fullTable, ir.PrimitiveDatetimeOffset, encoding("x-unknown", ""), "default string", travelsAs, true},
		{"10 a known date encoding", fullTable, ir.PrimitiveDatetimeOffset, encoding("http-date", ""), "datetime_offset/http-date", exact, true},
		{"11 an int64 as text", fullTable, ir.PrimitiveInt64, encoding("string", ir.PrimitiveString), "default string", travelsAs, true},
		{"11 a decimal as text", fullTable, ir.PrimitiveDecimal, encoding("string", ir.PrimitiveString), "default string", travelsAs, true},
		{"12 a kind that is not string-spelled", fullTable, ir.PrimitiveInteger, encoding("double", ""), "default integer", primitive, true},
		{"13 the wire type before the string rule", fullTable, ir.PrimitiveUUID, encoding("x", ir.PrimitiveInt64), "default int64", travelsAs, true},
		{"14 no meaning: a wire type equal to the primitive", fullTable, ir.PrimitiveBytes, encoding("weird", ir.PrimitiveBytes), "default bytes", primitive, true},
		{"15 no meaning: an int64 under a string wire type with no name", fullTable, ir.PrimitiveInt64, encoding("", ir.PrimitiveString), "default int64", exact, true},
		{"15 no meaning: a string wire type with no name", fullTable, ir.PrimitiveString, encoding("", ir.PrimitiveString), "default string", exact, true},
		{"16 no third probe", uuidDefaultOnly, ir.PrimitiveUUID, encoding("hex", ""), "", none, false},
		{"17 a keyed row alone", uuidHexOnly, ir.PrimitiveUUID, encoding("hex", ""), "H", exact, true},
		{"18 an undeclared kind", fullTable, "int65", typemap.Encoding{}, "", none, false},
		{"18 an undeclared wire type", fullTable, ir.PrimitiveUUID, encoding("x", "int65"), "", none, false},
		{"18 an undeclared kind the table holds a row for", undeclaredKindRow, "int65", typemap.Encoding{}, "I", exact, true},
		{"18 an undeclared wire type the table holds a row for", undeclaredKindRow, ir.PrimitiveUUID, encoding("x", "int65"), "I", travelsAs, true},
		{"19 keys compare exactly, as text", fullTable, ir.PrimitiveBytes, encoding("Base64URL", ir.PrimitiveString), "default string", travelsAs, true},
		{"19 keys compare exactly", fullTable, ir.PrimitiveBytes, encoding("Base64URL", ""), "default string", travelsAs, true},
		{"20 a slash in an encoding name", fullTable, ir.PrimitiveString, encoding("A.B.C.D/N", ""), "string/A.B.C.D/N", exact, true},
		{"21 an empty table", empty, ir.PrimitiveInt64, typemap.Encoding{}, "", none, false},
		{"22 no meaning: a string under another wire type", fullTable, ir.PrimitiveString, encoding("base32", ir.PrimitiveInt64), "string/base32", exact, true},
		{"23 an undeclared kind under a name", fullTable, "int65", encoding("x", ""), "", none, false},
		{"23 an undeclared kind under a name the table holds a default row for", undeclaredKindRow, "int65", encoding("x", ""), "I", primitive, true},
	}
}

func TestLookup_StringSpelledKindsTravelAsText(t *testing.T) {
	t.Parallel()
	stringSpelled := map[ir.PrimitiveKind]bool{
		ir.PrimitiveURL: true, ir.PrimitiveBytes: true, ir.PrimitiveUUID: true, ir.PrimitiveDate: true,
		ir.PrimitiveTime: true, ir.PrimitiveDatetime: true, ir.PrimitiveDatetimeOffset: true,
		ir.PrimitiveDuration: true,
		ir.PrimitiveAny:      false, ir.PrimitiveBool: false, ir.PrimitiveString: false,
		ir.PrimitiveInt8: false, ir.PrimitiveInt16: false, ir.PrimitiveInt32: false, ir.PrimitiveInt64: false,
		ir.PrimitiveUint8: false, ir.PrimitiveUint16: false, ir.PrimitiveUint32: false, ir.PrimitiveUint64: false,
		ir.PrimitiveInteger: false, ir.PrimitiveFloat32: false, ir.PrimitiveFloat64: false,
		ir.PrimitiveFloat: false, ir.PrimitiveNumber: false, ir.PrimitiveDecimal: false,
		ir.PrimitiveDecimal128: false,
	}
	kinds := declaredKinds(t)
	require.Equal(t, kinds, slices.Sorted(maps.Keys(stringSpelled)), "classify every kind ir declares")

	defaultsOnly := typemap.Table{Primitives: map[ir.PrimitiveKind]map[string]typemap.Row{}}
	for _, kind := range kinds {
		defaultsOnly.Primitives[kind] = map[string]typemap.Row{"": {Type: string(kind)}}
	}
	for _, kind := range kinds {
		row, found, ok := typemap.Lookup(defaultsOnly, kind, encoding("another-spelling", ""))
		require.True(t, ok, "%s", kind)
		if stringSpelled[kind] {
			assert.Equal(t, typemap.Row{Type: "string"}, row, "%s travels as text", kind)
			assert.Equal(t, typemap.TravelsAs, found, "%s", kind)
		} else {
			assert.Equal(t, typemap.Row{Type: string(kind)}, row, "%s travels as itself", kind)
			assert.Equal(t, typemap.Primitive, found, "%s", kind)
		}
	}
}

// rowsWithImports returns a new table whose decimal and string default rows
// hold imports and a dependency.
func rowsWithImports() typemap.Table {
	row := func(name string) typemap.Row {
		return typemap.Row{
			Type:       name,
			Imports:    map[string]string{name: "example.com/" + name},
			Dependency: &typemap.Dependency{Module: "example.com/" + name, MinVersion: "v1.0.0"},
		}
	}
	return typemap.Table{
		Primitives: map[ir.PrimitiveKind]map[string]typemap.Row{
			ir.PrimitiveDecimal: {"": row("decimal")},
			ir.PrimitiveString:  {"": row("text")},
		},
		Externals: map[string]typemap.Row{"erlang:pid": row("pid")},
	}
}

func TestLookup_ReturnsACopy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		primitive    ir.PrimitiveKind
		encoding     typemap.Encoding
		wantFound    typemap.Found
		defaultRowOf ir.PrimitiveKind
	}{
		{"the key's own row", ir.PrimitiveDecimal, typemap.Encoding{}, typemap.Exact, ir.PrimitiveDecimal},
		{"the row the value travels as", ir.PrimitiveUUID, encoding("hex", ""), typemap.TravelsAs, ir.PrimitiveString},
		{"the key primitive's own default row", ir.PrimitiveDecimal, encoding("x", ""), typemap.Primitive,
			ir.PrimitiveDecimal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			table := rowsWithImports()
			want := rowsWithImports().Primitives[tc.defaultRowOf][""]
			row, found, ok := typemap.Lookup(table, tc.primitive, tc.encoding)
			require.True(t, ok)
			require.Equal(t, tc.wantFound, found)
			require.Empty(t, cmp.Diff(want, row), "Lookup returns the table's row")

			row.Imports["written"] = "written"
			row.Dependency.MinVersion = "v9"
			assert.Empty(t, cmp.Diff(rowsWithImports(), table), "writing the row Lookup returned wrote the table")

			kept, _, _ := typemap.Lookup(table, tc.primitive, tc.encoding)
			owned := table.Primitives[tc.defaultRowOf][""]
			owned.Imports["owner"] = "owner"
			owned.Dependency.MinVersion = "v8"
			assert.Empty(t, cmp.Diff(want, kept), "writing the table reached a row Lookup returned before")
		})
	}
}

func TestLookupExternal_FindsAnIdentitysRow(t *testing.T) {
	t.Parallel()
	table := rowsWithImports()

	row, ok := typemap.LookupExternal(table, "erlang:pid")
	require.True(t, ok)
	require.Empty(t, cmp.Diff(rowsWithImports().Externals["erlang:pid"], row), "LookupExternal returns the table's row")
	row.Imports["written"] = "written"
	row.Dependency.MinVersion = "v9"
	assert.Empty(t, cmp.Diff(rowsWithImports(), table), "writing the row LookupExternal returned wrote the table")

	kept, _ := typemap.LookupExternal(table, "erlang:pid")
	table.Externals["erlang:pid"].Imports["owner"] = "owner"
	table.Externals["erlang:pid"].Dependency.MinVersion = "v8"
	assert.Empty(t, cmp.Diff(rowsWithImports().Externals["erlang:pid"], kept),
		"writing the table reached a row LookupExternal returned before")

	for _, identity := range []string{"erlang:fun", ""} {
		row, ok := typemap.LookupExternal(table, identity)
		assert.False(t, ok, "%q has no row", identity)
		assert.Empty(t, cmp.Diff(typemap.Row{}, row), "%q has no row", identity)
	}
}
