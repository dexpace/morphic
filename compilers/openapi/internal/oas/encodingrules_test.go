// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
)

// TestEncodingRules_AnswerAsTheTableDoes pins the default contentType of each
// case at every minor the table covers, "" where a minor gives the case none,
// and at versions it does not cover and a table with no Encoding Object, where
// no default is stated.
func TestEncodingRules_AnswerAsTheTableDoes(t *testing.T) {
	t.Parallel()
	const (
		octet = "application/octet-stream"
		text  = "text/plain"
		json  = "application/json"
	)
	cases := []struct {
		k             oas.DefaultCase
		v30, v31, v32 string
		name          string
	}{
		{oas.DefaultUntyped, "", octet, octet, "a schema writing no type"},
		{oas.DefaultRawBinary, octet, octet, octet, "raw bytes"},
		{oas.DefaultByteFormat, octet, text, text, "format byte, which 3.1 gives no effect on the encoding"},
		{oas.DefaultEncodedByteFormat, octet, octet, octet, "format byte and contentEncoding, each a row of its own minors"},
		{oas.DefaultContentEncoded, text, octet, octet, "contentEncoding, which 3.0's table has no column for"},
		{oas.DefaultString, text, text, text, "a string"},
		{oas.DefaultScalar, text, text, text, "a number, an integer or a boolean"},
		{oas.DefaultObject, json, json, json, "an object"},
		{oas.DefaultNestedArray, "", "", json, "an array inside an array, which 3.2 gives a row"},
	}
	for _, tc := range cases {
		for version, want := range map[string]string{"3.0.3": tc.v30, "3.1.0": tc.v31, "3.2.0": tc.v32, "": "", "3.3.0": ""} {
			got, ok := oas.Table().EncodingRules(version).ContentType(tc.k)
			assert.Equal(t, want, got, "%s at %q", tc.name, version)
			assert.Equal(t, want != "", ok, "%s at %q", tc.name, version)
		}
	}
	_, ok := oas.Spec{}.EncodingRules("3.1.0").ContentType(oas.DefaultUntyped)
	assert.False(t, ok, "a table with no Encoding Object states no default")
	_, ok = oas.Table().EncodingRules("3.1.0").ContentType(0)
	assert.False(t, ok, "the zero case is none the table names")
}

// TestEncodingRules_TakesEffectWhereTheTextSays pins where each RFC6570-style
// field takes effect, replacing the contentType: a form body at every minor,
// multipart/form-data from 3.1, and no other multipart media type; headers
// under every multipart media type and no form; and a field the table limits
// to no media type under every one. Ignores answers the converse, and nothing
// for the zero rules, which state nothing.
func TestEncodingRules_TakesEffectWhereTheTextSays(t *testing.T) {
	t.Parallel()
	const (
		form = "application/x-www-form-urlencoded"
		data = "multipart/form-data"
	)
	for _, field := range []string{"style", "explode", "allowReserved"} {
		for version, want := range map[string][3]bool{
			"3.0.3": {true, false, false}, "3.1.0": {true, true, false}, "3.2.0": {true, true, false}, "3.3.0": {},
		} {
			rules := oas.Table().EncodingRules(version)
			got := [3]bool{rules.TakesEffect(field, form), rules.TakesEffect(field, data), rules.TakesEffect(field, "multipart/mixed")}
			assert.Equal(t, want, got, "%s at %q under a form, multipart/form-data and multipart/mixed", field, version)
		}
	}
	for _, version := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		rules := oas.Table().EncodingRules(version)
		got := [3]bool{rules.TakesEffect("headers", form), rules.TakesEffect("headers", data), rules.TakesEffect("headers", "multipart/mixed")}
		assert.Equal(t, [3]bool{false, true, true}, got, "headers at %q under a form, multipart/form-data and multipart/mixed", version)
		assert.Equal(t, [3]bool{true, false, false},
			[3]bool{rules.Ignores("headers", form), rules.Ignores("headers", data), rules.Ignores("contentType", form)},
			"Ignores is the converse at %q", version)
	}
	assert.True(t, oas.Table().EncodingRules("3.0.3").TakesEffect("contentType", "multipart/mixed"),
		"a field the table limits to no media type takes effect under any")
	assert.False(t, oas.Spec{}.EncodingRules("3.1.0").TakesEffect("contentType", form),
		"the zero rules answer false")
	assert.False(t, oas.Spec{}.EncodingRules("3.1.0").Ignores("headers", form),
		"and say no field is ignored")
}
