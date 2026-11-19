// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"maps"
	"slices"
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
			"    Media: {allOf: [{type: string, contentMediaType: text/plain}], contentMediaType: text/html}\n"+
			"    TypeList: {allOf: [{type: [string, boolean], contentEncoding: a}], contentEncoding: a}\n"))
	for _, name := range []string{"Content", "ContentFlat", "Tag", "TagFlat", "TypeList"} {
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

// TestPrincipalKept_AWholeEntryThatCannotRenderIsKeptByKeyword pins an inline
// principal its position would keep whole, holding a value no JSON holds: it
// is not kept whole, and nothing of it is lost for that one value. What it
// keeps is held keyword by keyword against the position's, as any principal's
// is, and the one value is refused where it is written, as written flat.
func TestPrincipalKept_AWholeEntryThatCannotRenderIsKeptByKeyword(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Free: {allOf: [{type: string, minimum: 1, x-k: 1, x-q: "+unpreservableValue+"}], x-k: 2}\n"+
			"    Taken: {allOf: [{type: string, minimum: 1, x-k: 1, x-q: "+unpreservableValue+"}], x-k: 2, minimum: 2}\n"+
			"    Shared: {allOf: [{type: string, x-k: 1, x-q: "+unpreservableValue+"}], x-k: 2}\n"))
	assert.Equal(t, map[string]string{
		"openapi:allOf/0": `{"x-k":1} [vendor_extension]`,
		"openapi:minimum": "1 [degraded_lowering]",
		"openapi:x-k":     "2 [vendor_extension]",
	}, keptOn(t, doc, "Free"))
	assert.Equal(t, map[string]string{
		"openapi:allOf/0": `{"minimum":1,"x-k":1} [degraded_lowering]`,
		"openapi:minimum": "2 [degraded_lowering]",
		"openapi:x-k":     "2 [vendor_extension]",
	}, keptOn(t, doc, "Taken"))
	assert.Equal(t, map[string]string{"openapi:allOf/0": `{"x-k":1} [vendor_extension]`, "openapi:x-k": "2 [vendor_extension]"},
		keptOn(t, doc, "Shared"))
	for _, name := range []string{"Free", "Taken", "Shared"} {
		at := "/components/schemas/" + name + "/allOf/0"
		assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnpreservableConstruct, at+"/x-q"), "%s: the value is refused at its keyword", name)
		for _, msg := range reportsAt(diags, at) {
			assert.NotContains(t, msg, "in no form at all", "%s: the entry is in the IR", name)
		}
	}
	assert.Len(t, reportsAt(diags, "/components/schemas/Free/allOf/0"), 2, "the bound's own report and the extension held otherwise")
	assert.Len(t, reportsAt(diags, "/components/schemas/Taken/allOf/0"), 2, "the bound and the extension, each held otherwise")
}

// TestPrincipalKept_ThePositionNamesTheNodeAsItsPrincipalDoes pins the kind a
// report names where an inline principal's node stands at the position: the
// report of what the position keeps and the principal's own name it alike, as
// the same keywords written flat do, a shared primitive's alias a primitive.
func TestPrincipalKept_ThePositionNamesTheNodeAsItsPrincipalDoes(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, openapitest.ComponentSpec(
		"    Alias: {allOf: [{type: string, minimum: 1}], maxItems: 3}\n"+
			"    AliasFlat: {type: string, minimum: 1, maxItems: 3}\n"+
			"    Scalar: {allOf: [{type: string, format: byte, minimum: 1}], maxItems: 3}\n"+
			"    ScalarFlat: {type: string, format: byte, minimum: 1, maxItems: 3}\n"+
			"    List: {allOf: [{type: array, items: {type: string}, minimum: 1}], minLength: 3}\n"+
			"    ListFlat: {type: array, items: {type: string}, minimum: 1, minLength: 3}\n"))
	for name, kind := range map[string]string{"Alias": "primitive", "Scalar": "scalar", "List": "list"} {
		want := `this position lowered to a node of kind "` + kind + `"`
		flat := reportsAt(diags, "/components/schemas/"+name+"Flat")
		require.Len(t, flat, 1, name)
		assert.Contains(t, flat[0], want, name)
		for _, at := range []string{"", "/allOf/0"} {
			got := reportsAt(diags, "/components/schemas/"+name+at)
			require.Len(t, got, 1, name+at)
			assert.Contains(t, got[0], want, name+at)
		}
	}
}

// TestPrincipalKept_EachKeywordHasOneKeeper pins three readers of one inline
// principal at once, its census, its content reader and what the entry keeps
// as residue, each holding a keyword the position writes otherwise: one object
// under the entry's key holds all three, each keyword is named by one report,
// its reader's, and no reader keeps another's.
func TestPrincipalKept_EachKeywordHasOneKeeper(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Three: {allOf: [{type: integer, minLength: 1, contentEncoding: a, foo: 1}], minLength: 2, contentEncoding: b, foo: 2}\n"+
			"    Draft: {allOf: [{"+draft07+", type: integer, minLength: 1, dependencies: {a: [z]}, discriminator: {propertyName: a}}], "+
			"minLength: 2, dependencies: {b: [z]}, discriminator: {propertyName: b}}\n"))
	assert.Equal(t, map[string]string{
		"openapi:allOf/0":         `{"contentEncoding":"a","foo":1,"minLength":1} [degraded_lowering]`,
		"openapi:contentEncoding": `"b" [no_ir_home]`,
		"openapi:foo":             "2 [out_of_scope]",
		"openapi:minLength":       "2 [degraded_lowering]",
	}, keptOn(t, doc, "Three"))
	said := strings.Join(reportsAt(diags, "/components/schemas/Three/allOf/0"), "\n")
	assert.Len(t, reportsAt(diags, "/components/schemas/Three/allOf/0"), 3, "one report a reader")
	for _, keyword := range []string{"minLength", "contentEncoding", "foo"} {
		assert.Equal(t, 1, strings.Count(said, keyword), "%s is named once", keyword)
	}

	draft := keptOn(t, doc, "Draft")
	assert.Equal(t, `{"dependencies":{"a":["z"]},"discriminator":{"propertyName":"a"},"minLength":1} [degraded_lowering]`,
		draft["openapi:allOf/0"])
	said = strings.Join(reportsAt(diags, "/components/schemas/Draft/allOf/0"), "\n")
	for _, keyword := range []string{"minLength", "dependencies", "discriminator"} {
		assert.Equal(t, 1, strings.Count(said, keyword), "%s is named once", keyword)
	}
}

// keptAndSaid lists what the component name holds verbatim, bar vendor
// extensions, which no report names, an entry's key by its members, and what
// its reports say is kept: the keywords a report names as written otherwise or
// with no home, read off the two wordings the readers of a principal draw.
func keptAndSaid(t *testing.T, doc *ir.Document, diags []ir.Diagnostic, name string) (kept, said []string) {
	t.Helper()
	td := typeByName(doc, name)
	require.NotNil(t, td, name)
	for key, e := range td.Common().Unmodeled {
		switch {
		case e.Reason == ir.ReasonVendorExtension:
		case strings.HasPrefix(key, "openapi:allOf/"):
			var members map[string]jsontext.Value
			require.NoError(t, json.Unmarshal(e.Value, &members), key)
			for member := range members {
				kept = append(kept, "allOf:"+member)
			}
		default:
			kept = append(kept, strings.TrimPrefix(key, "openapi:"))
		}
	}
	at := "/components/schemas/" + name
	for _, d := range diags {
		pointer := string(d.Provenance.Pointer)
		if d.Severity == ir.SeverityError || !strings.HasPrefix(pointer, at) || !strings.Contains(d.Message, "kept verbatim") {
			continue
		}
		switch before, _, apart := strings.Cut(d.Message, " otherwise than this inline branch"); {
		case apart:
			_, list, _ := strings.Cut(before, " write ")
			for keyword := range strings.SplitSeq(list, ", ") {
				said = append(said, "allOf:"+keyword)
			}
		case strings.Contains(d.Message, "which has no home for "):
			_, list, _ := strings.Cut(d.Message, "which has no home for ")
			list, _, _ = strings.Cut(list, " declared beside it")
			said = append(said, strings.Split(list, ", ")...)
		default:
			said = append(said, pointer[strings.LastIndex(pointer, "/")+1:])
		}
	}
	slices.Sort(kept)
	slices.Sort(said)
	return kept, said
}

// TestPrincipalKept_AFailedKeepingLeavesWhatItsReportsName fails each keeping
// of an inline principal's keyword in turn, a value no JSON holds in each
// reader's hands and in the whole entry, a discriminator naming no schema, a
// kept object referring to none: what the node holds verbatim is what its
// reports name, no more and no less, and each failure draws its error.
func TestPrincipalKept_AFailedKeepingLeavesWhatItsReportsName(t *testing.T) {
	t.Parallel()
	bad := unpreservableValue
	cases := []struct {
		name, schema, failure string
		kept                  []string
	}{
		{"CensusApart", "{allOf: [{type: string, minimum: " + bad + ", maxItems: 1}], minimum: 2, maxItems: 2}", diag.UnpreservableConstruct,
			[]string{"allOf:maxItems", "maxItems", "minimum"}},
		{"CensusPlain", "{allOf: [{type: string, minimum: " + bad + ", maxItems: 1}], maxItems: 2}", diag.UnpreservableConstruct,
			[]string{"allOf:maxItems", "maxItems"}},
		{"TextApart", "{allOf: [{type: string, format: int64, minimum: " + bad + ", required: [a]}], minimum: 2, required: [b]}",
			diag.UnpreservableConstruct, []string{"allOf:required", "minimum", "required"}},
		{"ContentApart", "{allOf: [{type: integer, minLength: 1, contentSchema: {const: " + bad + "}}], minLength: 2, contentSchema: {const: 1}}",
			diag.UnpreservableConstruct, []string{"allOf:minLength", "contentSchema", "minLength"}},
		{"ResidueApart", "{allOf: [{type: string, minimum: 1, foo: " + bad + "}], minimum: 2, foo: 2}", diag.UnpreservableConstruct,
			[]string{"allOf:minimum", "foo", "minimum"}},
		{"Whole", "{allOf: [{type: string, minimum: 1, x-k: 1, x-q: " + bad + "}], minimum: 2, x-k: 2}", diag.UnpreservableConstruct,
			[]string{"allOf:minimum", "allOf:x-k", "minimum"}},
		{"RoutesApart", "{allOf: [{type: string, minimum: 1, discriminator: {propertyName: a, mapping: {x: Nope}}}], " +
			"minimum: 2, discriminator: {propertyName: b}}", diag.UnresolvedRef, []string{"allOf:discriminator", "allOf:minimum", "discriminator", "minimum"}},
		{"RoutesPlain", "{allOf: [{type: string, minimum: 1, discriminator: {propertyName: a, mapping: {x: Nope}}}], minimum: 2}",
			diag.UnresolvedRef, []string{"allOf:minimum", "discriminator", "minimum"}},
		{"RefInObject", "{allOf: [{type: string, minimum: 1, properties: {a: {$ref: '#/components/schemas/Nope'}}}], " +
			"minimum: 2, properties: {b: {type: string}}}", "", []string{"allOf:minimum", "allOf:properties", "minimum", "properties"}},
	}
	var lines strings.Builder
	for _, tc := range cases {
		lines.WriteString("    " + tc.name + ": " + tc.schema + "\n")
	}
	doc, diags := parseFull(t, openapitest.ComponentSpec(lines.String()))
	for _, tc := range cases {
		kept, said := keptAndSaid(t, doc, diags, tc.name)
		assert.Equal(t, tc.kept, kept, "%s: what the node holds", tc.name)
		assert.Equal(t, kept, said, "%s: what its reports name", tc.name)
		if tc.failure == "" {
			continue
		}
		failed := slices.ContainsFunc(diags, func(d ir.Diagnostic) bool {
			return d.Code == tc.failure && strings.HasPrefix(string(d.Provenance.Pointer), "/components/schemas/"+tc.name+"/allOf/0")
		})
		assert.True(t, failed, "%s: the failure is reported at the entry", tc.name)
	}
}

// TestPrincipalKept_ADiscriminatorBesideAUnionsOwnSaysWhoseItHolds pins a
// discriminator written beside an allOf whose inline principal's Union holds
// its own: it is kept verbatim, its routes resolved, and reported as kept
// beside the discriminator the Union holds, never for want of a home, which a
// Union has; a keyword with none is reported apart, and beside a Union holding
// none the discriminator is not said to be beside one.
func TestPrincipalKept_ADiscriminatorBesideAUnionsOwnSaysWhoseItHolds(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Tag: {allOf: [{oneOf: [{type: string}, {type: integer}], discriminator: {propertyName: a}}], discriminator: {propertyName: b}}\n"+
			"    Two: {allOf: [{oneOf: [{type: string}, {type: integer}], discriminator: {propertyName: a}}], "+
			"discriminator: {propertyName: b, mapping: {x: Nope}}, maxItems: 3}\n"+
			"    None: {allOf: [{oneOf: [{type: string}, {type: integer}]}], discriminator: {propertyName: b}}\n"))
	assert.Equal(t, map[string]string{"openapi:discriminator": `{"propertyName":"b"} [degraded_lowering]`}, keptOn(t, doc, "Tag"))
	assert.Equal(t, "a", doc.Types[componentID("Tag")].(*ir.Union).Discriminator.PropertyName)
	said := reportsAt(diags, "/components/schemas/Tag")
	require.Len(t, said, 1)
	assert.Contains(t, said[0], "holds the discriminator its inline allOf branch writes")
	assert.NotContains(t, said[0], "no home")

	said = reportsAt(diags, "/components/schemas/Two")
	require.Len(t, said, 2)
	assert.Contains(t, said[0], "which has no home for maxItems declared beside it")
	assert.Contains(t, said[1], "holds the discriminator its inline allOf branch writes")
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnresolvedRef, "/components/schemas/Two/discriminator/mapping/x"),
		"the kept discriminator's mapping is resolved")
	said = reportsAt(diags, "/components/schemas/None")
	require.Len(t, said, 1)
	assert.NotContains(t, said[0], "holds the discriminator", "a Union holding none is not said to hold its branch's")
}

// TestPrincipalKept_WhatABuildKeepsIsHeldAgainstThePositions pins the keywords
// an inline principal's build keeps on the node itself, beside the field it
// could not fill, before any reader runs: a format beside a contentEncoding,
// the union keyword a Union passes over, a prefixItems beside a listed tuple.
// The position's own keyword takes the plain key, the principal's is kept
// under its entry's key, and each is reported once, the build's as written
// flat. Written alike it is one writing, and a whole principal holds it, its
// reason reading it.
func TestPrincipalKept_WhatABuildKeepsIsHeldAgainstThePositions(t *testing.T) {
	t.Parallel()
	union := "oneOf: [{type: string}, {type: integer}], anyOf: [{type: string}, {type: boolean}]"
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Format: {allOf: [{type: string, format: byte, contentEncoding: base64url}], format: uuid}\n"+
			"    Union: {allOf: [{"+union+"}], anyOf: [{type: number}, {type: string}]}\n"+
			"    Prefix: {allOf: [{"+draft07+", type: array, items: [{type: string}], prefixItems: [{type: integer}]}], "+
			"prefixItems: [{type: boolean}]}\n"+
			"    Alike: {allOf: [{type: string, format: byte, contentEncoding: base64url}], format: byte}\n"+
			"    UnionAlike: {allOf: [{"+union+"}], anyOf: [{type: string}, {type: boolean}]}\n"+
			"    Whole: {allOf: [{"+union+", x-k: 1}], anyOf: [{type: number}, {type: string}], x-k: 2}\n"+
			"    Free: {allOf: [{type: string, format: byte, contentEncoding: base64url}]}\n"+
			"    WholeFree: {allOf: [{type: string, format: byte, contentEncoding: base64url, x-k: 1}], x-k: 2}\n"))
	for _, tc := range []struct{ name, keyword, entry, plain, flat string }{
		{"Format", "format", `{"format":"byte"} [no_ir_home]`, `"uuid" [degraded_lowering]`, "format is kept verbatim under Unmodeled"},
		{"Union", "anyOf", `{"anyOf":[{"type":"string"},{"type":"boolean"}]} [degraded_lowering]`,
			`[{"type":"number"},{"type":"string"}] [degraded_lowering]`, "with anyOf kept verbatim under Unmodeled"},
		{"Prefix", "prefixItems", `{"prefixItems":[{"type":"integer"}]} [degraded_lowering]`, `[{"type":"boolean"}] [degraded_lowering]`,
			"prefixItems is kept verbatim: this schema's dialect"},
	} {
		kept := keptOn(t, doc, tc.name)
		assert.Equal(t, tc.entry, kept["openapi:allOf/0"], "%s: the principal's under its entry's key", tc.name)
		assert.Equal(t, tc.plain, kept["openapi:"+tc.keyword], "%s: the position's under the keyword", tc.name)
		at := "/components/schemas/" + tc.name
		said := strings.Join(slices.Concat(reportsAt(diags, at+"/allOf/0"), reportsAt(diags, at+"/allOf/0/"+tc.keyword)), "\n")
		assert.Equal(t, 1, strings.Count(said, tc.flat), "%s: the build's report, as written flat, once: %s", tc.name, said)
		assert.NotContains(t, said, "otherwise than this inline branch", "%s: and no second report of it", tc.name)
		assert.Len(t, reportsAt(diags, at), 1, "%s: the position's once", tc.name)
	}
	assert.Equal(t, map[string]string{"openapi:format": `"byte" [no_ir_home]`}, keptOn(t, doc, "Alike"))
	assert.Empty(t, reportsAt(diags, "/components/schemas/Alike"), "one writing, reported at the entry alone")
	assert.Equal(t, map[string]string{"openapi:anyOf": `[{"type":"string"},{"type":"boolean"}] [degraded_lowering]`}, keptOn(t, doc, "UnionAlike"))
	assert.Empty(t, reportsAt(diags, "/components/schemas/UnionAlike"))
	whole := keptOn(t, doc, "Whole")
	assert.Contains(t, whole["openapi:allOf/0"], `"anyOf":[{"type":"string"},{"type":"boolean"}]`)
	assert.True(t, strings.HasSuffix(whole["openapi:allOf/0"], " [degraded_lowering]"),
		"the whole entry's reason reads the anyOf the build kept beside the x-k: %s", whole["openapi:allOf/0"])
	assert.Contains(t, whole["openapi:allOf/0"], `"x-k":1`, "the position's anyOf leaves the whole entry whole")
	assert.Equal(t, `[{"type":"number"},{"type":"string"}] [degraded_lowering]`, whole["openapi:anyOf"])
	free := keptOn(t, doc, "WholeFree")
	assert.Len(t, free, 2, "the whole entry holds the format the build kept, and the position's x-k: %v", free)
	assert.Contains(t, free["openapi:allOf/0"], `"format":"byte"`)
	assert.Equal(t, keptOn(t, doc, "Alike"), keptOn(t, doc, "Free"), "alone, it is kept as written flat")
}

// TestPrincipalKept_AVoicesBuildKeepStaysThePositions pins what the build of
// a principal's node keeps of the position's own keywords, read beside the
// principal: a 3.0 exclusive modifier with no bound beside it. It is the
// position's, held back with nothing of the principal's, and one the principal
// writes alike is kept once.
func TestPrincipalKept_AVoicesBuildKeepStaysThePositions(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, openapitest.ComponentSpecVer("3.0.3",
		"    Beside: {allOf: [{type: number, format: fixed}], exclusiveMinimum: true}\n"+
			"    Alike: {allOf: [{type: number, format: fixed, exclusiveMinimum: true}], exclusiveMinimum: true}\n"))
	assert.Equal(t, map[string]string{"openapi:exclusiveMinimum": "true [degraded_lowering]"}, keptOn(t, doc, "Beside"))
	assert.Equal(t, map[string]string{"openapi:exclusiveMinimum": "true [degraded_lowering]"}, keptOn(t, doc, "Alike"))
}

// TestPrincipalKept_ANestedPrincipalKeepsEachLevelOnce pins an entry naming no
// type whose own allOf holds the inline principal: a wrapper the position
// lowers from, its keywords read as written beside the principal's allOf. Each
// level's keyword is kept once: the outermost writer's under the keyword, each
// writer inside it under its own entry's key, two allOfs deep and three; a
// wrapper kept whole for another cause holds the principal, whose readers then
// keep nothing; and no report names a conjunct that writes nothing.
func TestPrincipalKept_ANestedPrincipalKeepsEachLevelOnce(t *testing.T) {
	t.Parallel()
	inner := "allOf: [{type: string, minimum: 3}]"
	cases := []keptCase{
		{name: "Three", schema: "{allOf: [{" + inner + ", minimum: 2}], minimum: 1}", kept: map[string]string{
			"openapi:minimum":         "1 [degraded_lowering]",
			"openapi:allOf/0":         `{"minimum":2} [degraded_lowering]`,
			"openapi:allOf/0/allOf/0": `{"minimum":3} [degraded_lowering]`,
		}, atEntry: 1, atSelf: 1},
		{name: "Two", schema: "{allOf: [{" + inner + ", minimum: 2}]}", kept: map[string]string{
			"openapi:minimum":         "2 [degraded_lowering]",
			"openapi:allOf/0/allOf/0": `{"minimum":3} [degraded_lowering]`,
		}, atEntry: 1},
		{name: "Alike", schema: "{allOf: [{" + inner + ", minimum: 3}]}", kept: map[string]string{
			"openapi:minimum": "3 [degraded_lowering]",
		}, atEntry: 1},
		{name: "Beside", schema: "{allOf: [{" + inner + ", maxItems: 2}], maxItems: 1}", kept: map[string]string{
			"openapi:maxItems": "1 [degraded_lowering]",
			"openapi:allOf/0":  `{"maxItems":2} [degraded_lowering]`,
			"openapi:minimum":  "3 [degraded_lowering]",
		}, atEntry: 1, atSelf: 1},
		{name: "Whole", schema: "{allOf: [{" + inner + ", minimum: 2, x-k: 1}], x-k: 2}", kept: map[string]string{
			"openapi:allOf/0": `{"allOf":[{"minimum":3,"type":"string"}],"minimum":2,"x-k":1} [degraded_lowering]`,
			"openapi:x-k":     "2 [vendor_extension]",
		}, atEntry: 1},
		{name: "Deep", schema: "{allOf: [{allOf: [{allOf: [{type: string, minimum: 4}], minimum: 3}], minimum: 2}], minimum: 1}",
			kept: map[string]string{
				"openapi:minimum":                 "1 [degraded_lowering]",
				"openapi:allOf/0":                 `{"minimum":2} [degraded_lowering]`,
				"openapi:allOf/0/allOf/0":         `{"minimum":3} [degraded_lowering]`,
				"openapi:allOf/0/allOf/0/allOf/0": `{"minimum":4} [degraded_lowering]`,
			}, atEntry: 1, atSelf: 1},
		{name: "Entry", schema: "{allOf: [{" + inner + ", minimum: 2}, {minimum: 5}]}", kept: map[string]string{
			"openapi:minimum":         "2 [degraded_lowering]",
			"openapi:allOf/0/allOf/0": `{"minimum":3} [degraded_lowering]`,
			"openapi:allOf/1":         `{"minimum":5} [degraded_lowering]`,
		}, atEntry: 1},
		{name: "DeepAlone", schema: "{allOf: [{allOf: [{allOf: [{type: string, minimum: 4}], minimum: 3}], minimum: 2}]}",
			kept: map[string]string{
				"openapi:minimum":                 "2 [degraded_lowering]",
				"openapi:allOf/0/allOf/0":         `{"minimum":3} [degraded_lowering]`,
				"openapi:allOf/0/allOf/0/allOf/0": `{"minimum":4} [degraded_lowering]`,
			}, atEntry: 1},
		{name: "WholeAround", schema: "{allOf: [{allOf: [{type: string, foo: 3, minimum: 3}], foo: 2, x-k: 1}], x-k: 2}",
			kept: map[string]string{
				"openapi:allOf/0": `{"allOf":[{"foo":3,"minimum":3,"type":"string"}],"foo":2,"x-k":1} [degraded_lowering]`,
				"openapi:x-k":     "2 [vendor_extension]",
			}, atEntry: 1},
		{name: "Text", schema: "{allOf: [{allOf: [{type: string, format: int64, minimum: 3}], minimum: 2, foo: 2}], minimum: 1, foo: 1}",
			kept: map[string]string{
				"openapi:minimum":         "1 [no_ir_home]",
				"openapi:foo":             "1 [out_of_scope]",
				"openapi:allOf/0":         `{"foo":2,"minimum":2} [degraded_lowering]`,
				"openapi:allOf/0/allOf/0": `{"minimum":3} [no_ir_home]`,
			}, atEntry: 1},
		{name: "TextBound", schema: "{allOf: [{allOf: [{type: string, format: int64, minimum: 3}], minimum: 2}], minimum: 1}",
			kept: map[string]string{
				"openapi:minimum":         "1 [no_ir_home]",
				"openapi:allOf/0":         `{"minimum":2} [no_ir_home]`,
				"openapi:allOf/0/allOf/0": `{"minimum":3} [no_ir_home]`,
			}, atEntry: 1},
		{name: "Key", schema: "{allOf: [{allOf: [{type: string, foo: 3}], foo: 2}], foo: 1}", kept: map[string]string{
			"openapi:foo":             "1 [out_of_scope]",
			"openapi:allOf/0":         `{"foo":2} [out_of_scope]`,
			"openapi:allOf/0/allOf/0": `{"foo":3} [out_of_scope]`,
		}, atEntry: 1},
	}
	diags := assertKept(t, cases)
	for _, tc := range cases {
		at := "/components/schemas/" + tc.name
		for _, msg := range slices.Concat(reportsAt(diags, at+"/allOf/0"), reportsAt(diags, at+"/allOf/0/allOf/0")) {
			assert.NotContains(t, msg, "another conjunct", "%s: no entry is kept whole for a keyword the position lowers from", tc.name)
		}
		if tc.name != "Alike" && tc.name != "Whole" && tc.name != "WholeAround" {
			assert.Len(t, reportsAt(diags, at+"/allOf/0/allOf/0"), 1, "%s: the principal's keyword is named once, at its entry", tc.name)
		}
	}
	whole := reportsAt(diags, "/components/schemas/Whole/allOf/0")
	require.Len(t, whole, 1)
	assert.Contains(t, whole[0], "the branch (minimum, x-k)", "the whole wrapper names what the principal's readers said, once")
	assert.Empty(t, reportsAt(diags, "/components/schemas/Whole/allOf/0/allOf/0"))
	for _, d := range diags {
		assert.False(t, strings.HasPrefix(string(d.Provenance.Pointer), "/components/schemas/WholeAround/allOf/0/allOf/0"),
			"what a whole wrapper holds is reported by it alone: %s", d.Message)
	}
}

// TestPrincipalKept_ALevelAlikeWithOneAroundItIsKeptOnce pins a level of a
// nested principal writing a keyword as an entry around it does, or as the
// position does, where another level writes it otherwise: one writing, kept
// once under the outermost key holding that value, the plain key the
// position's, and reported once by its keeper. Levels written otherwise each
// keep their own under their entry's key; a value no JSON holds is alike to
// nothing.
func TestPrincipalKept_ALevelAlikeWithOneAroundItIsKeptOnce(t *testing.T) {
	t.Parallel()
	wrap := func(inner, minimum string) string { return "{allOf: [" + inner + "], minimum: " + minimum + "}" }
	str := func(minimum string) string { return "{type: string, minimum: " + minimum + "}" }
	at := func(level int) string { return "/allOf/0" + strings.Repeat("/allOf/0", level-1) }
	n := func(m string) string { return m + " [degraded_lowering]" }
	obj := func(m string) string { return `{"minimum":` + m + `} [degraded_lowering]` }
	cases := []struct {
		keptCase
		silent []int // levels that report nothing: their value is kept by another's writing
	}{
		{keptCase{name: "Two", schema: wrap(wrap(str("802"), "802"), "801"),
			kept: map[string]string{"openapi:minimum": n("801"), "openapi:allOf/0": obj("802")}, atEntry: 1, atSelf: 1}, []int{2}},
		{keptCase{name: "Skip", schema: wrap(wrap(wrap(str("5"), "6"), "5"), "1"),
			kept:    map[string]string{"openapi:minimum": n("1"), "openapi:allOf/0": obj("5"), "openapi:allOf/0/allOf/0": obj("6")},
			atEntry: 1, atSelf: 1}, []int{3}},
		{keptCase{name: "All", schema: wrap(wrap(wrap(str("5"), "5"), "5"), "1"),
			kept: map[string]string{"openapi:minimum": n("1"), "openapi:allOf/0": obj("5")}, atEntry: 1, atSelf: 1}, []int{2, 3}},
		{keptCase{name: "TwoInner", schema: wrap(wrap(str("802"), "801"), "802"),
			kept: map[string]string{"openapi:minimum": n("802"), "openapi:allOf/0": obj("801")}, atEntry: 1}, nil},
		{keptCase{name: "SkipInner", schema: wrap(wrap(wrap(str("5"), "6"), "7"), "5"),
			kept:    map[string]string{"openapi:minimum": n("5"), "openapi:allOf/0": obj("7"), "openapi:allOf/0/allOf/0": obj("6")},
			atEntry: 1}, nil},
		{keptCase{name: "AllInner", schema: wrap(wrap(wrap(str("5"), "6"), "6"), "5"),
			kept: map[string]string{"openapi:minimum": n("5"), "openapi:allOf/0": obj("6")}, atEntry: 1}, []int{2}},
		{keptCase{name: "Foo", schema: "{allOf: [{allOf: [{type: string, foo: 2}], foo: 2}], foo: 1}",
			kept: map[string]string{"openapi:foo": "1 [out_of_scope]", "openapi:allOf/0": `{"foo":2} [out_of_scope]`}, atEntry: 1}, []int{2}},
		{keptCase{name: "Build", schema: "{allOf: [{allOf: [{oneOf: [{type: string}, {type: integer}], anyOf: [{type: string}, {type: boolean}]}], " +
			"anyOf: [{type: string}, {type: boolean}]}], anyOf: [{type: string}, {type: number}]}",
			kept: map[string]string{
				"openapi:anyOf":   `[{"type":"string"},{"type":"number"}] [degraded_lowering]`,
				"openapi:allOf/0": `{"anyOf":[{"type":"string"},{"type":"boolean"}]} [degraded_lowering]`,
			}, atEntry: 1, atSelf: 1}, nil},
		{keptCase{name: "Refused", schema: "{allOf: [{allOf: [{type: string, foo: 2}], foo: 3}], foo: " + unpreservableValue + "}",
			kept: map[string]string{"openapi:foo": "3 [out_of_scope]", "openapi:allOf/0/allOf/0": `{"foo":2} [out_of_scope]`}}, nil},
	}
	keptCases := make([]keptCase, 0, len(cases))
	for _, tc := range cases {
		keptCases = append(keptCases, tc.keptCase)
	}
	diags := assertKept(t, keptCases)
	for _, tc := range cases {
		for _, level := range tc.silent {
			where := "/components/schemas/" + tc.name + at(level)
			assert.Empty(t, reportsAt(diags, where), "%s: level %d keeps nothing of its own", tc.name, level)
		}
	}
}

// TestPrincipalKept_AKeywordAlikeIsOneWritingForEveryReader pins a keyword an
// inline principal and its position write alike that no census reads: a key no
// dialect defines, a default, readOnly, a validation-only keyword, an
// extension. It is one writing: kept once and reported once, by the position
// where the principal is lowered, and inside the principal's entry, named by
// its report, where the position keeps that entry whole, but an extension the
// promotion policy reads, which stays beside it and is promoted.
func TestPrincipalKept_AKeywordAlikeIsOneWritingForEveryReader(t *testing.T) {
	t.Parallel()
	keywords := map[string]string{
		"foo": "1", "default": "a", "readOnly": "true", "dependentRequired": "{a: [z]}", "not": "{type: integer}", "x-v": "1",
		"if": "{minLength: 1}",
	}
	var lines strings.Builder
	for keyword, value := range keywords {
		name := strings.ToUpper(keyword[:1]) + strings.ReplaceAll(keyword[1:], "-", "")
		lines.WriteString("    " + name + ": {allOf: [{type: string, " + keyword + ": " + value + "}], " + keyword + ": " + value + "}\n")
		lines.WriteString("    Whole" + name + ": {allOf: [{type: string, " + keyword + ": " + value + ", x-k: 1}, {x-k: 2}], " +
			keyword + ": " + value + "}\n")
	}
	lines.WriteString("    Unrendered: {allOf: [{type: string, foo: 1, x-k: 1, x-q: " + unpreservableValue + "}, {x-k: 2}], foo: 1}\n")
	lines.WriteString("    Promoted: {allOf: [{type: string, x-sensitive: true, x-k: 1}, {x-k: 2}], x-sensitive: true}\n")
	doc, diags := parseFull(t, openapitest.ComponentSpec(lines.String()))
	for keyword := range keywords {
		name := strings.ToUpper(keyword[:1]) + strings.ReplaceAll(keyword[1:], "-", "")
		at := "/components/schemas/" + name
		kept := keptOn(t, doc, name)
		key := "openapi:" + keyword
		if keyword == "if" {
			key = "openapi:if-then-else" // the entry if, then and else fold into
		}
		require.Len(t, kept, 1, "%s: kept once: %v", name, kept)
		assert.Contains(t, kept, key, name)
		said := 0
		for _, d := range diags {
			if strings.HasPrefix(string(d.Provenance.Pointer), at) && (d.Provenance.Pointer == jsontext.Pointer(at) ||
				strings.HasPrefix(string(d.Provenance.Pointer), at+"/")) {
				said++
			}
		}
		want := 1
		if keyword == "x-v" {
			want = 0 // an extension is kept with no report
		}
		assert.Equal(t, want, said, "%s: reported once", name)

		whole := keptOn(t, doc, "Whole"+name)
		assert.NotContains(t, whole, key, "Whole%s: the whole entry holds the one writing", name)
		assert.Contains(t, whole["openapi:allOf/0"], `"`+keyword+`":`, "Whole%s", name)
		assert.Empty(t, reportsAt(diags, "/components/schemas/Whole"+name), "Whole%s: nothing is reported of the position's", name)
		assert.Empty(t, reportsAt(diags, "/components/schemas/Whole"+name+"/"+keyword), "Whole%s", name)
	}
	assert.Equal(t, "true [vendor_extension]", keptOn(t, doc, "Promoted")["openapi:x-sensitive"],
		"an extension the promotion policy reads stays where a promotion finds it, beside the whole entry")
	assert.True(t, doc.Types[componentID("Promoted")].Common().Sensitive, "and is promoted")
	assert.Equal(t, "1 [out_of_scope]", keptOn(t, doc, "Unrendered")["openapi:foo"],
		"an entry no JSON holds is not whole, so the position keeps its own")
}

// TestPrincipalKept_AnAlikeKeywordStandsWithItsFirstKeeper pins where a keyword
// written alike beside an inline principal is kept, once and reported once: a
// content keyword the node holds no field for is the position's, whose reading
// keeps it first; a union the principal's build keeps beside its Union is the
// principal's, at the entry, with the build's reason and report
// (docs/compilers/openapi.md#allof-refinement).
func TestPrincipalKept_AnAlikeKeywordStandsWithItsFirstKeeper(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    Content: {allOf: [{type: integer, contentMediaType: text/x}], contentMediaType: text/x}\n"+
			"    Shapeless: {allOf: [{type: string, oneOf: [{minLength: 1}, {maxLength: 9}], anyOf: [{maxLength: 3}]}], "+
			"anyOf: [{maxLength: 3}]}\n"+
			"    NullOnly: {allOf: [{oneOf: [{type: 'null'}], anyOf: [{type: 'null', description: q}]}], "+
			"anyOf: [{type: 'null', description: q}]}\n"))
	for _, tc := range []struct{ name, key, kept, at string }{
		{"Content", "openapi:contentMediaType", `"text/x" [no_ir_home]`, "/contentMediaType"},
		{"Shapeless", "openapi:anyOf", `[{"maxLength":3}] [validation_only]`, "/allOf/0/anyOf"},
		{"NullOnly", "openapi:anyOf", `[{"description":"q","type":"null"}] [degraded_lowering]`, "/allOf/0/anyOf"},
	} {
		at := "/components/schemas/" + tc.name
		assert.Equal(t, tc.kept, keptOn(t, doc, tc.name)[tc.key], tc.name)
		assert.Equal(t, jsontext.Pointer(at+tc.at), typeByName(doc, tc.name).Common().Unmodeled[tc.key].Provenance.Pointer, tc.name)
		var said []string
		for _, d := range diags {
			if strings.HasPrefix(string(d.Provenance.Pointer), at) && strings.Contains(d.Message, strings.TrimPrefix(tc.key, "openapi:")) {
				said = append(said, string(d.Provenance.Pointer)+": "+d.Message)
			}
		}
		assert.Len(t, said, 1, "%s: reported once: %v", tc.name, said)
	}
}

// TestPrincipalKept_ABoundATypeListVariantReadsIsKeptNowhere pins a bound
// written beside a type-list principal, or on another entry: where one of its
// variants reads it, a string variant a length or a pattern, a number variant
// a value bound, an array variant a collection bound, that variant's
// Constraints hold it, and nothing keeps or reports it, as written flat, a
// format another listed type pairs with leaving the string variant its text.
// A bound no variant reads, a string variant carrying a number as text, is
// kept once on the Union, with its report.
func TestPrincipalKept_ABoundATypeListVariantReadsIsKeptNowhere(t *testing.T) {
	t.Parallel()
	bounds := []struct{ keyword, value, field string }{
		{"minLength", "7", `"minLength":7`}, {"pattern", "'^p7$'", `"pattern":"^p7$"`},
		{"minimum", "17", `"min":"17"`}, {"multipleOf", "27", `"multipleOf":"27"`}, {"minItems", "37", `"minItems":37`},
	}
	var lines strings.Builder
	for _, b := range bounds {
		for _, format := range []string{"", ", format: int64"} {
			list := "{type: [string, integer, array], items: {type: string}" + format + "}"
			name := b.keyword + map[string]string{"": "", ", format: int64": "Text"}[format]
			lines.WriteString("    Beside" + name + ": {allOf: [" + list + "], " + b.keyword + ": " + b.value + "}\n")
			lines.WriteString("    Entry" + name + ": {allOf: [" + list + ", {" + b.keyword + ": " + b.value + "}]}\n")
		}
	}
	unread := "{type: [string, boolean], format: int64}"
	lines.WriteString("    Unread: {allOf: [" + unread + "], minLength: 7}\n    UnreadEntry: {allOf: [" + unread + ", {minLength: 7}]}\n")
	lines.WriteString("    Pair: {allOf: [{type: [string, integer], format: int64}], minLength: 2}\n" +
		"    PairBoth: {allOf: [{type: [string, integer], format: int64}], minLength: 2, minimum: 1}\n")
	doc, diags := parseFull(t, openapitest.ComponentSpec(lines.String()))
	nodes := func(name string) (held, kept []string) {
		at := "/components/schemas/" + name
		for _, td := range doc.Types {
			if p := string(td.Common().Provenance.Pointer); p == at || strings.HasPrefix(p, at+"/") {
				raw, err := json.Marshal(td, json.Deterministic(true))
				require.NoError(t, err)
				held = append(held, string(raw))
				kept = append(kept, slices.Collect(maps.Keys(td.Common().Unmodeled))...)
			}
		}
		return held, kept
	}
	said := func(name, keyword string) (n int) {
		at := "/components/schemas/" + name
		for _, d := range diags {
			if p := string(d.Provenance.Pointer); (p == at || strings.HasPrefix(p, at+"/")) && strings.Contains(d.Message, keyword) {
				n++
			}
		}
		return n
	}
	for _, b := range bounds {
		for _, suffix := range []string{"", "Text"} {
			for _, spelling := range []string{"Beside", "Entry"} {
				name := spelling + b.keyword + suffix
				held, kept := nodes(name)
				assert.Contains(t, strings.Join(held, "\n"), b.field, "%s: a variant reads it", name)
				assert.Empty(t, kept, "%s: nothing keeps it", name)
				assert.Zero(t, said(name, b.keyword), "%s: nothing reports it", name)
			}
		}
	}
	for _, name := range []string{"Pair", "PairBoth"} {
		held, kept := nodes(name)
		assert.Contains(t, strings.Join(held, "\n"), `"minLength":2`, "%s: the string variant reads it", name)
		assert.Empty(t, kept, name)
		assert.Zero(t, said(name, "minLength")+said(name, "minimum"), name)
	}
	assert.Contains(t, strings.Join(func() []string { h, _ := nodes("PairBoth"); return h }(), "\n"), `"min":"1"`)
	for _, name := range []string{"Unread", "UnreadEntry"} {
		assert.Equal(t, map[string]string{"openapi:minLength": "7 [no_ir_home]"}, keptOn(t, doc, name), "%s: kept once, on the Union", name)
		assert.Equal(t, 1, said(name, "minLength"), "%s: and reported once", name)
	}
}
