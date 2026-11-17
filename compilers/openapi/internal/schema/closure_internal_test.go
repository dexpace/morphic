// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestCloserOf_NamesTheObjectWhoseClosureTypesTheModel pins which schema object
// of a composition closerOf names: the position where it writes a catch-all
// keyword and no entry declares a member, else the one inline entry writing
// one where no other object declares a member, else none. "own" names the
// position, "" none, and any other want the entry's path from it.
func TestCloserOf_NamesTheObjectWhoseClosureTypesTheModel(t *testing.T) {
	t.Parallel()
	const ref = "{$ref: '#/components/schemas/U'}"
	for _, tc := range []struct{ name, m, want string }{
		{"the position, no member declared", "{allOf: [" + ref + "], additionalProperties: false}", "own"},
		{"the position, declaring its own", "{properties: {a: {}}, additionalProperties: false, allOf: [" + ref + "]}", "own"},
		{"the position, patterns alone", "{allOf: [" + ref + "], patternProperties: {'^x': {}}}", "own"},
		{"the position, before an entry", "{allOf: [{additionalProperties: true}], additionalProperties: false}", "own"},
		{"the position, an entry declaring", "{allOf: [{properties: {b: {}}}], additionalProperties: false}", ""},
		{"patterns, an entry declaring", "{allOf: [{properties: {b: {}}}], patternProperties: {'^x': {}}}", ""},
		{"an entry, no member declared", "{allOf: [" + ref + ", {additionalProperties: false}]}", "allOf/1"},
		{"an entry, declaring its own", "{allOf: [{properties: {b: {}}, additionalProperties: false}, " + ref + "]}", "allOf/0"},
		{"an entry, an empty properties beside", "{allOf: [{properties: {}}, {additionalProperties: false}]}", "allOf/1"},
		{"an entry, a boolean beside", "{allOf: [true, {additionalProperties: false}]}", "allOf/1"},
		{"a wrapper", "{allOf: [{additionalProperties: false, allOf: [" + ref + "]}]}", "allOf/0"},
		{"an entry, another declaring", "{allOf: [{properties: {b: {}}}, {additionalProperties: false}]}", ""},
		{"an entry, the position declaring", "{properties: {a: {}}, allOf: [{additionalProperties: false}]}", ""},
		{"an entry, a $ref entry's own members", "{allOf: [{$ref: '#/components/schemas/U', properties: {b: {}}}, {additionalProperties: false}]}", ""},
		{"an entry, a $ref entry's own closure, a second", "{allOf: [{$ref: '#/components/schemas/U', additionalProperties: false}, {additionalProperties: false}]}", ""},
		{"two entries", "{allOf: [{additionalProperties: true}, {additionalProperties: false}]}", ""},
		{"none written", "{allOf: [" + ref + ", {properties: {b: {}}}]}", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l, _ := LoweredFor(t, openapitest.ComponentSpecVer("3.1.0", "    U: {type: object}\n    M: "+tc.m+"\n"))
			decl, ok := l.Doc.Components.Schemas.Get("M")
			require.True(t, ok)
			s := l.Ctx.Statements().Of(decl).Schema
			const at = jsontext.Pointer("/components/schemas/M")
			got := closerOf(s, allOfBranches(l.Ctx, s, at))
			switch tc.want {
			case "own":
				assert.True(t, got.own)
				assert.Same(t, s, got.schema)
				assert.Equal(t, at, got.at)
				assert.False(t, got.entryAt(at), "the position is no entry")
			case "":
				assert.False(t, got.own)
				assert.Nil(t, got.schema)
				assert.Empty(t, got.at)
			default:
				assert.False(t, got.own)
				assert.NotNil(t, got.schema)
				assert.Equal(t, at+"/"+jsontext.Pointer(tc.want), got.at)
				assert.True(t, got.entryAt(at+"/"+jsontext.Pointer(tc.want)))
				assert.False(t, got.entryAt(at), "the position is no entry")
			}
		})
	}
}

// TestKeepUnclosing_AClosureBesideAnEntrysMembersIsKeptOnce pins that an
// additionalProperties beside an allOf an entry of which declares members
// leaves the model open and is kept once under its own key, at the keyword,
// with one info at the position: a false entry closing the model keeps it no
// second time.
func TestKeepUnclosing_AClosureBesideAnEntrysMembersIsKeptOnce(t *testing.T) {
	t.Parallel()
	for name, allOf := range map[string]string{
		"an entry declaring":        "[{properties: {a: {type: string}}}]",
		"and a false entry closing": "[false, {properties: {a: {type: string}}}]",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := LowerSpec(t, openapitest.ComponentSpec("    M: {allOf: "+allOf+", additionalProperties: {type: string}}\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			m, ok := doc.Types["t/openapi/components/schemas/M"].(*ir.Model)
			require.True(t, ok, "M is a model")
			assert.Nil(t, m.AdditionalProps, "the model takes no catch-all")
			kept, ok := m.Unmodeled["openapi:additionalProperties"]
			require.True(t, ok, "the keyword is kept; got %+v", m.Unmodeled)
			assert.Equal(t, ir.ReasonDegradedLowering, kept.Reason)
			assert.Equal(t, jsontext.Pointer("/components/schemas/M/additionalProperties"), kept.Provenance.Pointer)
			assert.Len(t, openapitest.PointersOf(diags, diag.DegradedConstruct), 1, "one info, at the position")
			assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, "/components/schemas/M"),
				"also applies to the members an allOf entry here declares")
		})
	}
}
