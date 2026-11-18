// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// keptCase is a component whose inline allOf principal and position both keep
// keywords: what its node holds verbatim, each entry as "value [reason]", and
// how many reports stand at its entry and at the schema itself.
type keptCase struct {
	name, schema    string
	kept            map[string]string
	atEntry, atSelf int
}

// keptOn returns what the component name holds verbatim, each entry as
// "value [reason]".
func keptOn(t *testing.T, doc *ir.Document, name string) map[string]string {
	t.Helper()
	td := typeByName(doc, name)
	require.NotNil(t, td, name)
	out := map[string]string{}
	for key, e := range td.Common().Unmodeled {
		out[key] = string(e.Value) + " [" + string(e.Reason) + "]"
	}
	return out
}

// reportsAt returns the messages of the findings drawn at pointer.
func reportsAt(diags []ir.Diagnostic, pointer string) []string {
	var out []string
	for _, d := range diags {
		if d.Provenance.Pointer == jsontext.Pointer(pointer) {
			out = append(out, d.Message)
		}
	}
	return out
}

// assertKept compiles cases as one document and holds each to what it keeps
// and to the number of reports at its first entry and at itself.
func assertKept(t *testing.T, cases []keptCase) []ir.Diagnostic {
	t.Helper()
	var lines strings.Builder
	for _, tc := range cases {
		lines.WriteString("    " + tc.name + ": " + tc.schema + "\n")
	}
	doc, diags := parseFull(t, openapitest.ComponentSpec(lines.String()))
	for _, tc := range cases {
		at := "/components/schemas/" + tc.name
		assert.Equal(t, tc.kept, keptOn(t, doc, tc.name), tc.name)
		assert.Len(t, reportsAt(diags, at+"/allOf/0"), tc.atEntry, "%s: reports at its entry: %v", tc.name, reportsAt(diags, at+"/allOf/0"))
		assert.Len(t, reportsAt(diags, at), tc.atSelf, "%s: reports at the schema: %v", tc.name, reportsAt(diags, at))
	}
	return diags
}

// TestPrincipalKept_EveryReaderKeepsUnderOneEntryKey pins what several readers
// of one inline principal keep that the position writes otherwise: one object
// under the entry's key holds each reader's, none overwriting another's, each
// reader naming its own in one report at the entry, and the reason is the one
// they share (docs/compilers/openapi.md#allof-inline-residue).
func TestPrincipalKept_EveryReaderKeepsUnderOneEntryKey(t *testing.T) {
	t.Parallel()
	diags := assertKept(t, []keptCase{
		{name: "TextAndCensus", schema: "{allOf: [{type: string, format: int64, minimum: 1, required: [a]}], minimum: 2, required: [b]}",
			kept: map[string]string{
				"openapi:allOf/0":  `{"minimum":1,"required":["a"]} [degraded_lowering]`,
				"openapi:minimum":  "2 [no_ir_home]",
				"openapi:required": `["b"] [degraded_lowering]`,
			}, atEntry: 2, atSelf: 1},
		{name: "TextAlone", schema: "{allOf: [{type: string, format: int64, minimum: 1}], minimum: 2}",
			kept: map[string]string{"openapi:allOf/0": `{"minimum":1} [no_ir_home]`, "openapi:minimum": "2 [no_ir_home]"}, atEntry: 1},
		{name: "ContentAndCensus", schema: "{allOf: [{type: integer, contentEncoding: a, required: [a]}], contentEncoding: b, required: [b]}",
			kept: map[string]string{
				"openapi:allOf/0":         `{"contentEncoding":"a","required":["a"]} [degraded_lowering]`,
				"openapi:contentEncoding": `"b" [no_ir_home]`,
				"openapi:required":        `["b"] [degraded_lowering]`,
			}, atEntry: 2, atSelf: 1},
		{name: "OneOfTwo", schema: "{allOf: [{type: string, minimum: 1, maxItems: 4}], minimum: 2}",
			kept: map[string]string{
				"openapi:allOf/0":  `{"minimum":1} [degraded_lowering]`,
				"openapi:maxItems": "4 [degraded_lowering]",
				"openapi:minimum":  "2 [degraded_lowering]",
			}, atEntry: 2, atSelf: 1},
	})
	entry := reportsAt(diags, "/components/schemas/TextAndCensus/allOf/0")
	require.Len(t, entry, 2)
	assert.Contains(t, entry[0]+entry[1], "write minimum otherwise")
	assert.Contains(t, entry[0]+entry[1], "write required otherwise")
}

// TestPrincipalKept_AKeywordAlikeIsKeptOnce pins a keyword an inline principal
// and its position keep with one value: one entry, the position's, and one
// report, whichever reader keeps the principal's; a number spelt another way is
// another value, kept apart.
func TestPrincipalKept_AKeywordAlikeIsKeptOnce(t *testing.T) {
	t.Parallel()
	assertKept(t, []keptCase{
		{name: "Census", schema: "{allOf: [{type: string, minimum: 2}], minimum: 2}",
			kept: map[string]string{"openapi:minimum": "2 [degraded_lowering]"}, atSelf: 1},
		{name: "Content", schema: "{allOf: [{oneOf: [{type: string}, {type: integer}], contentEncoding: a}], contentEncoding: a}",
			kept: map[string]string{"openapi:contentEncoding": `"a" [no_ir_home]`}},
		{name: "Spelling", schema: "{allOf: [{type: string, minimum: 1}], minimum: 1.0}",
			kept:    map[string]string{"openapi:allOf/0": `{"minimum":1} [degraded_lowering]`, "openapi:minimum": "1.0 [degraded_lowering]"},
			atEntry: 1, atSelf: 1},
		{name: "Members", schema: "{allOf: [{type: string, properties: {a: {}, b: {}}}], properties: {b: {}, a: {}}}",
			kept: map[string]string{"openapi:properties": `{"a":{},"b":{}} [degraded_lowering]`}, atSelf: 1},
	})
}

// TestPrincipalKept_AKeyNoCensusKeepsIsKeptUnderTheEntryKey pins a keyword
// outside the census an inline principal and its position keep otherwise, a
// key no dialect defines: the principal is lowered, so it is not kept whole
// for it, as another entry is.
func TestPrincipalKept_AKeyNoCensusKeepsIsKeptUnderTheEntryKey(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Principal: {allOf: [{type: string, foo: 1}], foo: 2}\n"+
			"    Entry: {allOf: [{type: string}, {foo: 1}], foo: 2}\n"+
			"    Among: {allOf: [{type: string, foo: 1}, {foo: 3}]}\n"))
	assert.Equal(t, map[string]string{"openapi:allOf/0": `{"foo":1} [out_of_scope]`, "openapi:foo": "2 [out_of_scope]"},
		keptOn(t, doc, "Principal"))
	assert.Len(t, reportsAt(diags, "/components/schemas/Principal/allOf/0"), 1)
	assert.Equal(t, map[string]string{"openapi:allOf/1": `{"foo":1} [out_of_scope]`, "openapi:foo": "2 [out_of_scope]"},
		keptOn(t, doc, "Entry"), "an entry that is no principal stays whole")
	assert.Equal(t, map[string]string{"openapi:allOf/1": `{"foo":3} [out_of_scope]`, "openapi:foo": "1 [out_of_scope]"},
		keptOn(t, doc, "Among"), "a key the position leaves free holds the principal's")
}

// TestPrincipalKept_ADiscriminatorKeptPlainHoldsItsKeysOnce pins a
// discriminator the principal's census keeps after the position read the keys
// inside it: the entry kept whole holds them, and the node none beside it.
func TestPrincipalKept_ADiscriminatorKeptPlainHoldsItsKeysOnce(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, openapitest.ComponentSpec(
		"    D: {allOf: [{type: string, discriminator: {propertyName: a, x-foo: 1}}]}\n"+
			"    F: {type: string, discriminator: {propertyName: a, x-foo: 1}}\n"))
	flat := keptOn(t, doc, "F")
	assert.Equal(t, map[string]string{"openapi:discriminator": `{"propertyName":"a","x-foo":1} [degraded_lowering]`}, flat)
	assert.Equal(t, flat, keptOn(t, doc, "D"))
}

// TestPrincipalKept_AValueThatCannotRenderIsRefusedAsWrittenFlat pins a
// keyword of the principal no JSON holds, beside the position's own: its
// keeper refuses it at the keyword, as it refuses one written flat, and
// nothing is kept under the entry's key or announced.
func TestPrincipalKept_AValueThatCannotRenderIsRefusedAsWrittenFlat(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Census: {allOf: [{type: string, minimum: "+unpreservableValue+"}], minimum: 2}\n"+
			"    Content: {allOf: [{type: integer, contentSchema: {const: "+unpreservableValue+"}}], contentSchema: {const: 1}}\n"))
	assert.Equal(t, map[string]string{"openapi:minimum": "2 [degraded_lowering]"}, keptOn(t, doc, "Census"))
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnpreservableConstruct, "/components/schemas/Census/allOf/0/minimum"))
	assert.Empty(t, reportsAt(diags, "/components/schemas/Census/allOf/0"))
	assert.Equal(t, map[string]string{"openapi:contentSchema": `{"const":1} [no_ir_home]`}, keptOn(t, doc, "Content"))
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnpreservableConstruct, "/components/schemas/Content/allOf/0/contentSchema"))
}
