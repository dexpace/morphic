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
// and its position keep with one value: one entry and one report, the
// principal's for a keyword of the census, one writing of it, and the
// position's for another reader's; a number spelt another way is another
// value, kept apart.
func TestPrincipalKept_AKeywordAlikeIsKeptOnce(t *testing.T) {
	t.Parallel()
	assertKept(t, []keptCase{
		{name: "Census", schema: "{allOf: [{type: string, minimum: 2}], minimum: 2}",
			kept: map[string]string{"openapi:minimum": "2 [degraded_lowering]"}, atEntry: 1},
		{name: "Content", schema: "{allOf: [{oneOf: [{type: string}, {type: integer}], contentEncoding: a}], contentEncoding: a}",
			kept: map[string]string{"openapi:contentEncoding": `"a" [no_ir_home]`}},
		{name: "Spelling", schema: "{allOf: [{type: string, minimum: 1}], minimum: 1.0}",
			kept:    map[string]string{"openapi:allOf/0": `{"minimum":1} [degraded_lowering]`, "openapi:minimum": "1.0 [degraded_lowering]"},
			atEntry: 1, atSelf: 1},
		{name: "Members", schema: "{allOf: [{type: string, properties: {a: {}, b: {}}}], properties: {b: {}, a: {}}}",
			kept: map[string]string{"openapi:properties": `{"a":{},"b":{}} [degraded_lowering]`}, atEntry: 1},
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

// draft07 opens an allOf entry read in JSON Schema draft 7, which defines
// `dependencies`.
const draft07 = "$id: 'https://example.com/e', $schema: 'http://json-schema.org/draft-07/schema#'"

// TestPrincipalKept_ADraftsDependenciesBesideAKeyNoDialectDefines pins the one
// key two readers spell alike: an entry naming a draft keeps its
// `dependencies` as validation logic, the position its own as a key its
// dialect does not define, both under openapi:dependencies. Written otherwise,
// the plain key holds the position's, the principal's is kept under its
// entry's key, and another entry whole; written alike, it is kept once.
func TestPrincipalKept_ADraftsDependenciesBesideAKeyNoDialectDefines(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Principal: {allOf: [{"+draft07+", type: string, dependencies: {a: [z]}}], dependencies: {b: [z]}}\n"+
			"    Entry: {allOf: [{type: string}, {"+draft07+", dependencies: {a: [z]}}], dependencies: {b: [z]}}\n"+
			"    Alike: {allOf: [{"+draft07+", type: string, dependencies: {a: [z]}}], dependencies: {a: [z]}}\n"+
			"    Alone: {allOf: [{"+draft07+", type: string, dependencies: {a: [z]}}]}\n"+
			"    Model: {type: object, allOf: [{"+draft07+", dependencies: {a: [z]}}], dependencies: {b: [z]}}\n"))
	assert.False(t, openapitest.HasDiag(diags, diag.UnknownKeyEntryTaken), "no value is refused its key")

	principal := keptOn(t, doc, "Principal")
	assert.Equal(t, `{"b":["z"]} [out_of_scope]`, principal["openapi:dependencies"])
	assert.Equal(t, `{"dependencies":{"a":["z"]}} [validation_only]`, principal["openapi:allOf/0"])
	assert.Len(t, reportsAt(diags, "/components/schemas/Principal/allOf/0"), 1)

	entry := keptOn(t, doc, "Entry")
	assert.Equal(t, `{"b":["z"]} [out_of_scope]`, entry["openapi:dependencies"])
	assert.Contains(t, entry["openapi:allOf/1"], `"dependencies":{"a":["z"]}`, "an entry that is no principal is kept whole")

	alike := keptOn(t, doc, "Alike")
	assert.Equal(t, `{"a":["z"]} [out_of_scope]`, alike["openapi:dependencies"])
	assert.NotContains(t, alike, "openapi:allOf/0")
	assert.Equal(t, `{"a":["z"]} [validation_only]`, keptOn(t, doc, "Alone")["openapi:dependencies"], "alone, it folds as written flat")

	model := keptOn(t, doc, "Model")
	assert.Equal(t, `{"b":["z"]} [out_of_scope]`, model["openapi:dependencies"])
	assert.Contains(t, model["openapi:allOf/0"], `"dependencies":{"a":["z"]}`, "a composition keeps its entry whole")
}

// TestPrincipalKept_APatternBesideThePrincipalsKeepsBoth pins two patterns, one
// on an inline principal whose node holds a string's bounds and one beside the
// allOf: a field holds one pattern, the position's, as the keywords beside an
// allOf stand, and the principal, disagreeing, is kept whole with its own,
// whatever node it builds. A node holding no such bound keeps both verbatim.
func TestPrincipalKept_APatternBesideThePrincipalsKeepsBoth(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Union: {allOf: [{oneOf: [{type: string}, {type: integer}], pattern: '^p$'}], pattern: '^b$'}\n"+
			"    TypeList: {allOf: [{type: [string, boolean], pattern: '^p$'}], pattern: '^b$'}\n"+
			"    Scalar: {allOf: [{type: string, format: byte, pattern: '^p$'}], pattern: '^b$'}\n"+
			"    Shared: {allOf: [{type: string, pattern: '^p$'}], pattern: '^b$'}\n"+
			"    Alike: {allOf: [{oneOf: [{type: string}, {type: integer}], pattern: '^p$'}], pattern: '^p$'}\n"+
			"    List: {allOf: [{type: array, items: {type: string}, pattern: '^p$'}], pattern: '^b$'}\n"))
	held := map[string]*ir.Constraints{
		"Union":    doc.Types[componentID("Union")].(*ir.Union).Constraints,
		"TypeList": doc.Types["t/anon/components/schemas/TypeList/type/0"].(*ir.Scalar).Constraints,
		"Scalar":   doc.Types[componentID("Scalar")].(*ir.Scalar).Constraints,
		"Shared":   doc.Types[componentID("Shared")].(*ir.Scalar).Constraints,
	}
	for name, cons := range held {
		require.NotNil(t, cons, name)
		assert.Equal(t, "^b$", cons.Pattern, "%s: the field holds the position's", name)
		kept := keptOn(t, doc, name)
		require.Len(t, kept, 1, name)
		assert.Contains(t, kept["openapi:allOf/0"], `"pattern":"^p$"`, "%s: the whole principal holds its own", name)
		assert.Len(t, reportsAt(diags, "/components/schemas/"+name+"/allOf/0"), 1, name)
		assert.Empty(t, reportsAt(diags, "/components/schemas/"+name), name)
	}
	assert.Equal(t, "^p$", doc.Types[componentID("Alike")].(*ir.Union).Constraints.Pattern)
	assert.Empty(t, keptOn(t, doc, "Alike"))
	assert.Equal(t, map[string]string{"openapi:allOf/0": `{"pattern":"^p$"} [degraded_lowering]`, "openapi:pattern": `"^b$" [degraded_lowering]`},
		keptOn(t, doc, "List"))
}

// TestPrincipalKept_AKeywordTheNodeReadsIsNotAlsoKept pins a keyword an inline
// principal's node reads into a field, written beside the allOf too: alike, it
// is one writing, in the field alone, as written flat; a content keyword
// written otherwise is kept with a report saying whose the Encoding is, never
// that the node has no such field.
func TestPrincipalKept_AKeywordTheNodeReadsIsNotAlsoKept(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Content: {allOf: [{type: string, contentEncoding: base64}], contentEncoding: base64}\n"+
			"    ContentFlat: {type: string, contentEncoding: base64}\n"+
			"    Tag: {allOf: [{oneOf: [{type: string}, {type: integer}], discriminator: {propertyName: k}}], discriminator: {propertyName: k}}\n"+
			"    TagFlat: {oneOf: [{type: string}, {type: integer}], discriminator: {propertyName: k}}\n"+
			"    Media: {allOf: [{type: string, contentMediaType: text/plain}], contentMediaType: text/html}\n"))
	for _, name := range []string{"Content", "ContentFlat", "Tag", "TagFlat"} {
		assert.Empty(t, keptOn(t, doc, name), name)
		assert.Empty(t, reportsAt(diags, "/components/schemas/"+name), name)
	}
	assert.Equal(t, doc.Types[componentID("ContentFlat")].(*ir.Scalar).Encoding, doc.Types[componentID("Content")].(*ir.Scalar).Encoding)
	assert.Equal(t, doc.Types[componentID("TagFlat")].(*ir.Union).Discriminator.PropertyName,
		doc.Types[componentID("Tag")].(*ir.Union).Discriminator.PropertyName)

	assert.Equal(t, map[string]string{"openapi:contentMediaType": `"text/html" [no_ir_home]`}, keptOn(t, doc, "Media"))
	media := reportsAt(diags, "/components/schemas/Media/contentMediaType")
	require.Len(t, media, 1)
	assert.Contains(t, media[0], "holds what its inline allOf branch writes")
	assert.NotContains(t, media[0], "no Encoding field")
}
