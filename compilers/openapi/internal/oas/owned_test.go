// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas_test

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/ir"
)

// TestCheck_HandsTheValuesItLeavesToOwned pins what the check hands
// Options.Owned and leaves unreported: each value of a field whose shape
// another package owns, in either shape, and an `items` written as a list;
// not one items schema, a null, nor a multipleOf out of range, which it
// reports itself. Each names the schema writing it under Root, and a finding
// about it is placed as the check's own, naming where it was read.
func TestCheck_HandsTheValuesItLeavesToOwned(t *testing.T) {
	var n yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("openapi: 3.1.0\ninfo: {title: t, version: v}\ncomponents:\n  schemas:\n"+
		"    S: {minimum: abc, exclusiveMinimum: true, maximum: 3, items: [{}], multipleOf: 0, exclusiveMaximum: null}\n"+
		"    T: {items: {}}\n"+
		"    U: {properties: {a: &v {exclusiveMinimum: true}, b: *v}}\n"), &n))
	anchor := strings.NewReplacer("/properties/b/", "/properties/a/")
	locate := func(p jsontext.Pointer) ir.Provenance {
		return ir.Provenance{Pointer: "/x-doc" + jsontext.Pointer(anchor.Replace(string(p)))}
	}
	var owned []oas.Owned
	diags := oas.NewChecker().Check(t.Context(), &n, "3.1.0", oas.Options{Locate: locate, ReadAt: true, Root: "/x-doc",
		Owned: func(o oas.Owned) { owned = append(owned, o) }})
	got := map[string]jsontext.Pointer{}
	for _, o := range owned {
		got[string(o.Schema)+" "+o.Field] = o.Schema
	}
	assert.Equal(t, map[string]jsontext.Pointer{
		"/x-doc/components/schemas/S minimum":                       "/x-doc/components/schemas/S",
		"/x-doc/components/schemas/S exclusiveMinimum":              "/x-doc/components/schemas/S",
		"/x-doc/components/schemas/S maximum":                       "/x-doc/components/schemas/S",
		"/x-doc/components/schemas/S items":                         "/x-doc/components/schemas/S",
		"/x-doc/components/schemas/U/properties/a exclusiveMinimum": "/x-doc/components/schemas/U/properties/a",
		"/x-doc/components/schemas/U/properties/b exclusiveMinimum": "/x-doc/components/schemas/U/properties/b",
	}, got)
	reported := make([]string, 0, len(diags))
	for _, d := range diags {
		reported = append(reported, string(d.Provenance.Pointer))
	}
	assert.ElementsMatch(t, []string{"/x-doc/components/schemas/S/multipleOf", "/x-doc/components/schemas/S/exclusiveMaximum"},
		reported, "what it reports itself, and nothing it handed over")

	for _, o := range owned {
		d := o.Finding("openapi/x", "%s is %s", o.Field, "wrong")
		assert.Equal(t, ir.SeverityError, d.Severity)
		assert.Equal(t, o.Field+" is wrong", oas.AsPlaced(d).Message)
		readElsewhere := strings.HasPrefix(string(o.Schema), "/x-doc/components/schemas/U/properties/b")
		assert.Equal(t, readElsewhere, strings.Contains(d.Message, "; read at "+string(o.Schema)), d.Message)
	}
}

// TestSchemaRules_FormatFindingJudgesAnIDInItsDialect pins the format of an
// `$id`, which the check leaves to its caller: 2020-12's and 2019-09's name no
// fragment, draft 7's may name one alone, declaring a plain name, and draft 4
// has no `$id` to judge.
func TestSchemaRules_FormatFindingJudgesAnIDInItsDialect(t *testing.T) {
	var n yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("openapi: 3.1.0\ninfo: {title: t, version: v}\ncomponents:\n  schemas:\n"+
		"    S: {$id: '#frag'}\n"), &n))
	var owned []oas.Owned
	oas.NewChecker().Check(t.Context(), &n, "3.1.0", oas.Options{
		Locate: func(p jsontext.Pointer) ir.Provenance { return ir.Provenance{Pointer: p} },
		Owned:  func(o oas.Owned) { owned = append(owned, o) },
	})
	require.Len(t, owned, 1)
	for dialect, refused := range map[string]bool{
		"": true, draft2020: true, draft2019: true, draft07: false, draft04: false,
	} {
		d, bad := rulesOf("3.1.0", dialect).FormatFinding(owned[0])
		assert.Equal(t, refused, bad, dialect)
		if bad {
			assert.Equal(t, "openapi/validation/validation-invalid-format", d.Code)
			assert.Equal(t, jsontext.Pointer("/components/schemas/S/$id"), d.Provenance.Pointer)
		}
	}
}

// TestSchemaRules_FormatFindingJudgesAnAnchorInItsDialect pins the grammar of
// an anchor's name, which the check leaves to its caller: 2020-12's, a letter
// or '_', then letters, digits, '-', '_' and '.', and 2019-09's `$anchor`'s, a
// letter, then letters, digits, '-', '_', ':' and '.'. A dialect defining no
// such keyword, 2019-09 no `$dynamicAnchor` and draft 7 no `$anchor`, judges
// one by 2020-12's, as it reads one. Draft 4's `$id`, read for nothing, is
// judged by none.
func TestSchemaRules_FormatFindingJudgesAnAnchorInItsDialect(t *testing.T) {
	for _, tc := range []struct {
		field, name, dialect string
		refused              bool
	}{
		{"$anchor", "_a1.b-c", "", false},
		{"$anchor", "1a", "", true},
		{"$anchor", "a:b", draft2020, true},
		{"$anchor", "a:b", draft2019, false},
		{"$anchor", "_x", draft2019, true},
		{"$dynamicAnchor", "a:b", "", true},
		{"$dynamicAnchor", "a:b", draft2019, true},
		{"$dynamicAnchor", "_x", draft2019, false},
		{"$anchor", "_x", draft07, false},
		{"$anchor", "1a", draft07, true},
		{"$id", "#a", draft04, false},
	} {
		var n yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte("openapi: 3.1.0\ninfo: {title: t, version: v}\ncomponents:\n  schemas:\n"+
			"    S: {"+tc.field+": '"+tc.name+"'}\n"), &n))
		var owned []oas.Owned
		oas.NewChecker().Check(t.Context(), &n, "3.1.0", oas.Options{Owned: func(o oas.Owned) { owned = append(owned, o) }})
		require.Len(t, owned, 1, "%s %s", tc.field, tc.name)
		_, bad := rulesOf("3.1.0", tc.dialect).FormatFinding(owned[0])
		assert.Equal(t, tc.refused, bad, "%s %q in %q", tc.field, tc.name, tc.dialect)
	}
}

// TestSchemaRules_FormatFindingSaysWhatADraftsIdentifierMustBe pins the
// finding about a draft 7 `$id` naming a fragment alone that is no plain name.
func TestSchemaRules_FormatFindingSaysWhatADraftsIdentifierMustBe(t *testing.T) {
	var n yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("openapi: 3.1.0\ninfo: {title: t, version: v}\ncomponents:\n  schemas:\n"+
		"    S: {$id: '#_x'}\n"), &n))
	var owned []oas.Owned
	oas.NewChecker().Check(t.Context(), &n, "3.1.0", oas.Options{Owned: func(o oas.Owned) { owned = append(owned, o) }})
	require.Len(t, owned, 1)
	d, bad := rulesOf("3.1.0", draft07).FormatFinding(owned[0])
	require.True(t, bad)
	assert.Equal(t, `Schema Object field "$id" must be a URI reference whose fragment, where written alone and no JSON `+
		`pointer, is a name of a letter, then letters, digits, '-', '_', ':' and '.' in this schema's dialect`,
		oas.AsPlaced(d).Message)
}

// TestSchemaRules_TupleRestFindingJudgesTheRestBesideAList pins the
// `additionalItems` beside an `items` list, which a dialect reading the list
// as a tuple reads as its rest: one no schema spells is an error at it, a null
// a warning that it is read as left out, as a null field is, and a schema, a
// boolean, an alias to one or none written draw nothing. A dialect reading no
// tuple there judges none, and a value of another field is no list.
func TestSchemaRules_TupleRestFindingJudgesTheRestBesideAList(t *testing.T) {
	for _, tc := range []struct {
		rest    string
		dialect string
		want    ir.Severity
	}{
		{", additionalItems: 5", draft07, ir.SeverityError},
		{", additionalItems: [{}]", draft04, ir.SeverityError},
		{", additionalItems: 'x'", draft2019, ir.SeverityError},
		{", additionalItems: null", draft06, ir.SeverityWarning},
		{", additionalItems: {type: string}", draft07, ""},
		{", additionalItems: false", draft07, ""},
		{", additionalItems: *f", draft07, ""},
		{"", draft07, ""},
		{", additionalItems: 5", draft2020, ""},
		{", additionalItems: 5", "", ""},
	} {
		var n yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte("openapi: 3.1.0\ninfo: {title: t, version: v}\nx-f: &f false\n"+
			"components:\n  schemas:\n    S: {items: [{}]"+tc.rest+"}\n"), &n))
		var owned []oas.Owned
		oas.NewChecker().Check(t.Context(), &n, "3.1.0", oas.Options{
			Locate: func(p jsontext.Pointer) ir.Provenance { return ir.Provenance{Pointer: p} },
			Owned:  func(o oas.Owned) { owned = append(owned, o) },
		})
		require.Len(t, owned, 1, tc.rest)
		d, bad := rulesOf("3.1.0", tc.dialect).TupleRestFinding(owned[0])
		require.Equal(t, tc.want != "", bad, "%s in %q", tc.rest, tc.dialect)
		if bad {
			assert.Equal(t, tc.want, d.Severity, tc.rest)
			assert.Equal(t, "openapi/validation/validation-type-mismatch", d.Code)
			assert.Equal(t, jsontext.Pointer("/components/schemas/S/additionalItems"), d.Provenance.Pointer)
		}
	}
	var n yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("openapi: 3.1.0\ninfo: {title: t, version: v}\ncomponents:\n  schemas:\n"+
		"    S: {items: {}, additionalItems: 5, minimum: 1}\n"), &n))
	var owned []oas.Owned
	oas.NewChecker().Check(t.Context(), &n, "3.1.0", oas.Options{Owned: func(o oas.Owned) { owned = append(owned, o) }})
	require.Len(t, owned, 1)
	_, bad := rulesOf("3.1.0", draft07).TupleRestFinding(owned[0])
	assert.False(t, bad, "a minimum is no items list")
}
