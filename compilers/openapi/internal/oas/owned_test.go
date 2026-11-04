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
// `$id`, which the check leaves to its caller: 2020-12's names no fragment,
// draft 7's may name one alone, declaring a plain name, and draft 4 has no
// `$id` to judge.
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
		"": true, draft2020: true, "http://json-schema.org/draft-07/schema#": false, draft04: false,
	} {
		d, bad := rulesOf("3.1.0", dialect).FormatFinding(owned[0])
		assert.Equal(t, refused, bad, dialect)
		if bad {
			assert.Equal(t, "openapi/validation/validation-invalid-format", d.Code)
			assert.Equal(t, jsontext.Pointer("/components/schemas/S/$id"), d.Provenance.Pointer)
		}
	}
}
