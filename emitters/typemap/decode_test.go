// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package typemap_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"slices"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/emitters/options"
	"github.com/dexpace/morphic/emitters/typemap"
	"github.com/dexpace/morphic/ir"
)

func TestDecode_ReadsATable(t *testing.T) {
	t.Parallel()

	empty, err := typemap.Decode([]byte(`{}`))
	require.NoError(t, err)
	assert.Empty(t, cmp.Diff(typemap.Table{}, empty), "an empty object holds no map")

	primitivesOnly, err := typemap.Decode([]byte(`{"primitives":{}}`))
	require.NoError(t, err)
	assert.NotNil(t, primitivesOnly.Primitives)
	assert.Empty(t, primitivesOnly.Primitives)
	assert.Nil(t, primitivesOnly.Externals)

	full, err := typemap.Decode([]byte(`{"primitives":{
		"decimal":{"":{"type":"decimal.Decimal","imports":{"decimal":"github.com/shopspring/decimal"},
			"dependency":{"module":"github.com/shopspring/decimal","minVersion":"v1.4.0"}}},
		"string":{"A.B.C.D/N":{"type":"netip.Prefix","imports":{"netip":"net/netip"}}},
		"bytes":{"base64url":{"type":"[]byte","codec":""}}},
		"externals":{"erlang:pid":{"type":"Pid","imports":{}},"":{"type":"Opaque"}}}`))
	require.NoError(t, err)
	want := typemap.Table{
		Primitives: map[ir.PrimitiveKind]map[string]typemap.Row{
			ir.PrimitiveDecimal: {"": {
				Type:       "decimal.Decimal",
				Imports:    map[string]string{"decimal": "github.com/shopspring/decimal"},
				Dependency: &typemap.Dependency{Module: "github.com/shopspring/decimal", MinVersion: "v1.4.0"},
			}},
			ir.PrimitiveString: {"A.B.C.D/N": {Type: "netip.Prefix", Imports: map[string]string{"netip": "net/netip"}}},
			ir.PrimitiveBytes:  {"base64url": {Type: "[]byte"}},
		},
		Externals: map[string]typemap.Row{
			"erlang:pid": {Type: "Pid", Imports: map[string]string{}},
			"":           {Type: "Opaque"},
		},
	}
	assert.Empty(t, cmp.Diff(want, full))
}

// refusal is one row of TestDecode_Refuses: a table holding one fault, the
// error it is refused with, and its twin, the same table without the fault.
type refusal struct {
	name string
	raw  string
	twin string
	// want is the sentinel the error wraps; nil asks only for an error that
	// is no null refusal.
	want error
	// contains is what the error names the faulty member by, empty where json
	// names it in words of its own.
	contains string
}

// nullAt is what Decode's error names a member written null by.
func nullAt(pointer string) string {
	return "type table: " + strconv.Quote(pointer) + ": member written null"
}

// faultAt is what Decode's error names a member breaking typemap's own rules by.
func faultAt(pointer string) string {
	return "type table " + pointer + ": "
}

// rowIn wraps a row's members in a table holding it under string's default.
func rowIn(members string) string {
	return `{"primitives":{"string":{"":{` + members + `}}}}`
}

// dependencyIn wraps a dependency's members in a table holding it under
// decimal's default row.
func dependencyIn(members string) string {
	return `{"primitives":{"decimal":{"":{"type":"d","dependency":{` + members + `}}}}}`
}

func TestDecode_Refuses(t *testing.T) {
	t.Parallel()
	fullDependency := `"module":"m","minVersion":"1"`
	rows := slices.Concat([]refusal{
		{name: "an unknown member at the top", raw: `{"rows":{}}`, twin: `{}`, want: json.ErrUnknownName},
		{name: "an unknown member in a row", raw: rowIn(`"type":"s","kind":"x"`), twin: rowIn(`"type":"s"`),
			want: json.ErrUnknownName},
		{name: "an unknown member in a dependency", raw: dependencyIn(fullDependency + `,"scope":"x"`),
			twin: dependencyIn(fullDependency), want: json.ErrUnknownName},
		{name: "a member name in another case", raw: `{"Primitives":{}}`, twin: `{"primitives":{}}`,
			want: json.ErrUnknownName},
		{name: "a row member in another case", raw: rowIn(`"Type":"s"`), twin: rowIn(`"type":"s"`),
			want: json.ErrUnknownName},
		{name: "a repeated kind", raw: `{"primitives":{"string":{},"string":{}}}`,
			twin: `{"primitives":{"string":{}}}`, want: jsontext.ErrDuplicateName},
		{name: "a repeated encoding", raw: `{"primitives":{"string":{"":{"type":"a"},"":{"type":"b"}}}}`,
			twin: `{"primitives":{"string":{"":{"type":"a"}}}}`, want: jsontext.ErrDuplicateName},
		{name: "a repeated type", raw: rowIn(`"type":"a","type":"b"`), twin: rowIn(`"type":"a"`),
			want: jsontext.ErrDuplicateName},
		{name: "null", raw: `null`, twin: `{}`, want: options.ErrNullMember, contains: nullAt("")},
		{name: "null primitives", raw: `{"primitives":null}`, twin: `{"primitives":{}}`,
			want: options.ErrNullMember, contains: nullAt("/primitives")},
		{name: "null externals", raw: `{"externals":null}`, twin: `{"externals":{}}`,
			want: options.ErrNullMember, contains: nullAt("/externals")},
		{name: "a null kind's map", raw: `{"primitives":{"string":null}}`, twin: `{"primitives":{"string":{}}}`,
			want: options.ErrNullMember, contains: nullAt("/primitives/string")},
		{name: "a null row", raw: `{"primitives":{"string":{"":null}}}`, twin: rowIn(`"type":"s"`),
			want: options.ErrNullMember, contains: nullAt("/primitives/string/")},
		{name: "a null type", raw: rowIn(`"type":null`), twin: rowIn(`"type":"s"`),
			want: options.ErrNullMember, contains: nullAt("/primitives/string//type")},
		{name: "a null codec", raw: rowIn(`"type":"s","codec":null`), twin: rowIn(`"type":"s","codec":"c"`),
			want: options.ErrNullMember, contains: nullAt("/primitives/string//codec")},
		{name: "null imports", raw: rowIn(`"type":"s","imports":null`), twin: rowIn(`"type":"s","imports":{}`),
			want: options.ErrNullMember, contains: nullAt("/primitives/string//imports")},
		{name: "a null import", raw: rowIn(`"type":"s","imports":{"a":null}`),
			twin: rowIn(`"type":"s","imports":{"a":"b"}`),
			want: options.ErrNullMember, contains: nullAt("/primitives/string//imports/a")},
		{name: "a null dependency", raw: `{"primitives":{"decimal":{"":{"type":"d","dependency":null}}}}`,
			twin: `{"primitives":{"decimal":{"":{"type":"d"}}}}`,
			want: options.ErrNullMember, contains: nullAt("/primitives/decimal//dependency")},
		{name: "a null module", raw: dependencyIn(`"module":null,"minVersion":"1"`), twin: dependencyIn(fullDependency),
			want: options.ErrNullMember, contains: nullAt("/primitives/decimal//dependency/module")},
	}, ownRefusals(fullDependency), malformedTables())
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := typemap.Decode([]byte(tc.raw))
			require.Error(t, err)
			if tc.want != nil {
				assert.ErrorIs(t, err, tc.want)
			} else {
				assert.NotErrorIs(t, err, options.ErrNullMember)
			}
			if tc.contains != "" {
				assert.ErrorContains(t, err, tc.contains)
			}
			assert.Empty(t, cmp.Diff(typemap.Table{}, got), "a refusal returns the zero table")

			_, err = typemap.Decode([]byte(tc.twin))
			assert.NoError(t, err, "the twin, which lacks the fault, must decode")
		})
	}
}

// ownRefusals are the rows typemap refuses by its own rules rather than by
// options.DecodeLayer's.
func ownRefusals(fullDependency string) []refusal {
	fault := typemap.ErrInvalidTable
	return []refusal{
		{name: "an undeclared kind", raw: `{"primitives":{"int65":{"":{"type":"x"}}}}`,
			twin: `{"primitives":{"string":{"":{"type":"x"}}}}`, want: fault, contains: faultAt("/primitives/int65")},
		{name: "a kind in another case", raw: `{"primitives":{"String":{"":{"type":"x"}}}}`,
			twin: `{"primitives":{"string":{"":{"type":"x"}}}}`, want: fault, contains: faultAt("/primitives/String")},
		{name: "the empty kind", raw: `{"primitives":{"":{"":{"type":"x"}}}}`,
			twin: `{"primitives":{"string":{"":{"type":"x"}}}}`, want: fault, contains: faultAt("/primitives/")},
		{name: "an undeclared kind holding no row", raw: `{"primitives":{"int65":{}}}`,
			twin: `{"primitives":{"string":{}}}`, want: fault, contains: faultAt("/primitives/int65")},
		{name: "an empty type", raw: rowIn(`"type":""`), twin: rowIn(`"type":"s"`),
			want: fault, contains: faultAt("/primitives/string//type")},
		{name: "an absent type", raw: rowIn(`"codec":"c"`), twin: rowIn(`"type":"s","codec":"c"`),
			want: fault, contains: faultAt("/primitives/string//type")},
		{name: "an external's empty type", raw: `{"externals":{"erlang:pid":{"type":""}}}`,
			twin: `{"externals":{"erlang:pid":{"type":"Pid"}}}`, want: fault,
			contains: faultAt("/externals/erlang:pid/type")},
		{name: "a slash in an encoding name", raw: `{"primitives":{"string":{"A.B.C.D/N":{"type":""}}}}`,
			twin: `{"primitives":{"string":{"A.B.C.D/N":{"type":"netip.Prefix"}}}}`, want: fault,
			contains: faultAt("/primitives/string/A.B.C.D~1N/type")},
		{name: "a dependency with no minimum version", raw: dependencyIn(`"module":"m"`), twin: dependencyIn(fullDependency),
			want: fault, contains: faultAt("/primitives/decimal//dependency")},
		{name: "a dependency with no module", raw: dependencyIn(`"minVersion":"1"`), twin: dependencyIn(fullDependency),
			want: fault, contains: faultAt("/primitives/decimal//dependency")},
		{name: "a dependency with an empty module", raw: dependencyIn(`"module":"","minVersion":"1"`),
			twin: dependencyIn(fullDependency), want: fault, contains: faultAt("/primitives/decimal//dependency")},
		{name: "an empty dependency", raw: dependencyIn(``), twin: dependencyIn(fullDependency),
			want: fault, contains: faultAt("/primitives/decimal//dependency")},
		{name: "an external's half dependency", raw: `{"externals":{"x":{"type":"X","dependency":{"module":"m"}}}}`,
			twin: `{"externals":{"x":{"type":"X","dependency":{` + fullDependency + `}}}}`, want: fault,
			contains: faultAt("/externals/x/dependency")},
	}
}

// malformedTables are the rows that are no table at all, or hold a member of
// the wrong JSON type, which json refuses in words of its own.
func malformedTables() []refusal {
	return []refusal{
		{name: "empty input", raw: ``, twin: `{}`},
		{name: "an array", raw: `[]`, twin: `{}`},
		{name: "a string", raw: `"x"`, twin: `{}`},
		{name: "trailing data", raw: `{} {}`, twin: `{}`},
		{name: "an encoding name holding invalid UTF-8", raw: "{\"primitives\":{\"string\":{\"\xff\":{\"type\":\"s\"}}}}",
			twin: `{"primitives":{"string":{"x":{"type":"s"}}}}`},
		{name: "a type that is a number", raw: rowIn(`"type":1`), twin: rowIn(`"type":"s"`)},
		{name: "imports that are an array", raw: rowIn(`"type":"s","imports":[]`), twin: rowIn(`"type":"s","imports":{}`)},
	}
}

func TestDecode_RefusalNamesTheSameMemberEveryRun(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"externals":{"x":{"type":""}},"primitives":{"uuid":{"":{"type":""}},` +
		`"bool":{"b":{"type":""},"":{"type":""}}}}`)
	for range 64 {
		_, err := typemap.Decode(raw)
		require.ErrorIs(t, err, typemap.ErrInvalidTable)
		assert.ErrorContains(t, err, faultAt("/primitives/bool//type"))
	}
}
