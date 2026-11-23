// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/v2"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// againNotes are the notes a declaration of a member writes: as a keyword
// naming the value v, and as notesText reads it.
var againNotes = []struct{ name, write, typed string }{
	{"title", "title: %s", "title %s"},
	{"description", "description: %s", "description %s"},
	{"external docs", "externalDocs: {url: 'https://e.test/%s'}", "link https://e.test/%s"},
	{"deprecation", "deprecated: true, x-deprecated-reason: %s", "deprecated %s"},
	{"deprecation a key states", "x-fern-availability: deprecated, x-deprecated-reason: %s", "deprecated %s"},
}

// notesText is what docs, deprecation and examples hold, as one line.
func notesText(t *testing.T, docs ir.Docs, deprecation *ir.Deprecation, examples []ir.Example) string {
	t.Helper()
	var out []string
	if docs.Summary != "" {
		out = append(out, "title "+docs.Summary)
	}
	if docs.Description != "" {
		out = append(out, "description "+docs.Description)
	}
	for _, link := range docs.ExternalDocs {
		out = append(out, "link "+link.URL)
	}
	if deprecation != nil {
		out = append(out, strings.TrimSpace("deprecated "+deprecation.Message+deprecation.Since))
		for _, replacement := range deprecation.ReplacedBy {
			out = append(out, "replaced by "+string(replacement.Type))
		}
	}
	for _, example := range examples {
		raw, err := json.Marshal(example.Value, json.Deterministic(true))
		require.NoError(t, err)
		out = append(out, "example "+string(raw))
	}
	return strings.Join(out, ", ")
}

// typeText is the node id names, below model's pointer, with the notes it holds
// and the types it composes, as one line: what a reader of the type sees.
func typeText(t *testing.T, doc *ir.Document, id ir.TypeID) string {
	t.Helper()
	td, ok := doc.Types[id]
	require.True(t, ok, id)
	common := td.Common()
	name := strings.ReplaceAll(string(id), "components/schemas/", "")
	out := strings.TrimPrefix(strings.TrimPrefix(name, "type/anonymous/"), "type/") + " " + string(td.Kind())
	if enum, isEnum := td.(*ir.Enum); isEnum {
		for _, member := range enum.Members {
			out += " " + member.Value.String
		}
	}
	if notes := notesText(t, common.Docs, common.Deprecation, common.Examples); notes != "" {
		out += " [" + notes + "]"
	}
	if model, isModel := td.(*ir.Model); isModel && len(model.Mixins) > 0 {
		parts := make([]string, len(model.Mixins))
		for i, mixin := range model.Mixins {
			parts[i] = typeText(t, doc, mixin.Target)
		}
		out += " of (" + strings.Join(parts, "; ") + ")"
	}
	return out
}

// memberText is the member f of the model id names: the notes it holds, its
// type (typeText) and the declarations it keeps verbatim.
func memberText(t *testing.T, doc *ir.Document, id ir.TypeID) string {
	t.Helper()
	model, isModel := doc.Types[id].(*ir.Model)
	require.True(t, isModel, "%s is a model", id)
	property, ok := propertyByWire(model, "f")
	require.True(t, ok, "%s holds f", id)
	out := typeText(t, doc, property.Type.Target)
	if notes := notesText(t, property.Docs, property.Deprecation, property.Examples); notes != "" {
		out = "[" + notes + "] " + out
	}
	for _, key := range slices.Sorted(maps.Keys(property.Unmodeled)) {
		out += " | verbatim " + strings.TrimPrefix(key, "openapi:conflicting-redeclaration/components/schemas/")
	}
	return out
}

// TestAllOfFold_AMemberDeclaredAgainKeepsItsNotesOnItsNodes pins where the
// notes of a member stand that a model declares more than once itself: on the
// node each declaration's schema builds, whatever kind it is and whichever note
// it writes, the field holding none. A declaration building no node leaves its
// note with the field, which keeps the later. It holds however the model spells
// the two, two inline entries, its own properties beside one, an entry nested
// in one and the keywords beside an entry's $ref, and in every order of the
// entries and components (docs/compilers/openapi.md#allof-redeclared-property).
func TestAllOfFold_AMemberDeclaredAgainKeepsItsNotesOnItsNodes(t *testing.T) {
	t.Parallel()
	kinds := []struct {
		name, schema string
		noNode       bool
	}{
		{"a bare object", "type: object", false},
		{"an object with properties", "type: object, properties: {p: {type: string}}", false},
		{"a list", "type: array, items: {type: string}", false},
		{"an enum", "type: string, enum: [a, b]", false},
		{"a map", "type: object, additionalProperties: {type: string}", false},
		{"a union", "oneOf: [{type: string}, {type: integer}]", false},
		{"a tuple", "type: array, prefixItems: [{type: string}]", false},
		{"a scalar", "type: string, contentMediaType: image/png, contentEncoding: base64", false},
		{"a string", "type: string", true},
		{"an integer", "type: integer", true},
	}
	// Each spelling writes two declarations of f, and says where.
	spellings := []struct {
		name, spec string
		at         [2]string
	}{
		{"two entries", "{allOf: [{properties: {f: %s}}, {properties: {f: %s}}]}", [2]string{"/allOf/0", "/allOf/1"}},
		{"an entry and the model's own", "{allOf: [{properties: {f: %s}}], properties: {f: %s}}", [2]string{"/allOf/0", ""}},
		{"a nested entry and an entry", "{allOf: [{allOf: [{properties: {f: %s}}]}, {properties: {f: %s}}]}", [2]string{"/allOf/0/allOf/0", "/allOf/1"}},
		{"two entries nested in one", "{allOf: [{allOf: [{properties: {f: %s}}, {properties: {f: %s}}]}]}", [2]string{"/allOf/0/allOf/0", "/allOf/0/allOf/1"}},
		{"the keywords beside an entry's $ref", "{allOf: [{$ref: '#/components/schemas/Plain', properties: {f: %s}}, {properties: {f: %s}}]}",
			[2]string{"/allOf/0", "/allOf/1"}},
	}
	for _, kind := range kinds {
		for _, note := range againNotes {
			for i, spelling := range spellings {
				for layout := range 4 {
					values := []string{"v0", "v1"}
					if layout%2 == 1 {
						slices.Reverse(values)
					}
					declared := func(v string) string { return "{" + kind.schema + ", " + fmt.Sprintf(note.write, v) + "}" }
					coll := "    Coll: " + fmt.Sprintf(spelling.spec, declared(values[0]), declared(values[1])) + "\n"
					plain := "    Plain: {type: object, properties: {z: {type: string}}}\n"
					src := openapitest.ComponentSpec(coll + plain)
					if layout > 1 {
						src = openapitest.ComponentSpec(plain + coll)
					}
					label := fmt.Sprintf("%s, %s, %s, layout %d", kind.name, note.name, spelling.name, layout)
					doc, field, diags := notedField(t, src)

					requireErrorsAndWarnings(t, diags, nil)
					held := notesText(t, field.Docs, field.Deprecation, field.Examples)
					if kind.noNode {
						assert.Equal(t, fmt.Sprintf(note.typed, values[1]), held, "%s: the field keeps the later", label)
						assert.Contains(t, field.Unmodeled, "openapi:conflicting-redeclaration/components/schemas/Coll"+spellings[i].at[0]+"/properties/f", label)
						continue
					}
					assert.Empty(t, held, "%s: the field holds none", label)
					for k, at := range spelling.at {
						node, ok := doc.Types[ir.TypeID("type/anonymous/components/schemas/Coll"+at+"/properties/f")]
						require.True(t, ok, label)
						common := node.Common()
						assert.Equal(t, fmt.Sprintf(note.typed, values[k]), notesText(t, common.Docs, common.Deprecation, common.Examples),
							"%s: the node at %s holds its own", label, at)
					}
				}
			}
		}
	}
}

// foldedObjects is the allOf of a model declaring f in two entries, each an
// object with a property of its own and a description.
const foldedObjects = "allOf: [" +
	"{type: object, properties: {f: {type: object, properties: {p: {type: string}}, description: D0}}}, " +
	"{type: object, properties: {f: {type: object, properties: {q: {type: string}}, description: D1}}}]"

// TestAllOfFold_AMemberDeclaredAgainFoldsItsNotedNodes pins what a member a
// model declares more than once itself folds to: what its declarations' nodes
// do, each holding its notes. Each row is a shape a note moved off its node
// would lose: one no property has a slot for, one a node the fold builds
// adopts, one whose node stands apart by its note alone, and one written where
// a declaration building no node writes another
// (docs/compilers/openapi.md#allof-redeclared-property).
func TestAllOfFold_AMemberDeclaredAgainFoldsItsNotedNodes(t *testing.T) {
	t.Parallel()
	entry := func(schema string) string { return "{type: object, properties: {f: {" + schema + "}}}" }
	entries := func(schemas ...string) string {
		out := make([]string, len(schemas))
		for i, schema := range schemas {
			out[i] = entry(schema)
		}
		return "allOf: [" + strings.Join(out, ", ") + "]"
	}
	const maps0, mapsAB = "type: object, additionalProperties: {type: string}", "type: object, additionalProperties: {type: string, enum: [a, b]}"
	for _, tc := range []struct{ name, model, want string }{
		{"three enums meeting, each deprecated by keys",
			entries("type: string, enum: [a, b, c], deprecated: true, x-deprecated-reason: R0",
				"type: string, enum: [b, c, d], deprecated: true, x-deprecated-reason: R1",
				"type: string, enum: [b, c, e], deprecated: true, x-deprecated-since: '1.0'"),
			"M/allOf/2/properties/f enum b c e [deprecated 1.0] | verbatim M/allOf/0/properties/f | verbatim M/allOf/1/properties/f"},
		{"two enums meeting, one deprecated by a key",
			entries("type: string, enum: [a, b, c], deprecated: true, x-deprecated-reason: R1", "type: string, enum: [b, c, d], deprecated: true"),
			"M/allOf/1/properties/f enum b c d [deprecated] | verbatim M/allOf/0/properties/f"},
		{"two bare objects giving examples",
			entries("type: object, description: D0, examples: [{p: one}]", "type: object, description: D1, examples: [{p: two}]"),
			`composed/M/allOf/1/properties/f model of (` +
				`M/allOf/0/properties/f model [description D0, example {"kind":"object","object":[{"name":"p","value":{"kind":"string","string":"one"}}]}]; ` +
				`M/allOf/1/properties/f model [description D1, example {"kind":"object","object":[{"name":"p","value":{"kind":"string","string":"two"}}]}])`},
		{"a declaration building no node before an enum", entries("type: string, description: D0", "type: string, enum: [a, b], description: D1"),
			"[description D0] M/allOf/1/properties/f enum a b [description D1]"},
		{"a declaration building no node after an enum", entries("type: string, enum: [a, b], description: D1", "type: string, description: D0"),
			"[description D0] M/allOf/0/properties/f enum a b [description D1]"},
		{"an untyped declaration before a list", entries("description: D0", "type: array, items: {type: string}, description: D1"),
			"[description D0] M/allOf/1/properties/f list [description D1]"},
		{"maps and a bare object, each described",
			entries(maps0+", description: D0", mapsAB+", description: D1", "type: object, description: D2"),
			"composed/M/allOf/1/properties/f model of (M/allOf/0/properties/f map [description D0]; " +
				"M/allOf/1/properties/f map [description D1]; M/allOf/2/properties/f model [description D2])"},
		{"a titled union, a described bare object and a titled map",
			entries("anyOf: [{type: object}, {type: integer}], title: T0", "type: object, description: D1", maps0+", title: T2"),
			"composed/M/allOf/1/properties/f model of (M/allOf/1/properties/f model [description D1]; " +
				"M/allOf/2/properties/f map [title T2])"},
		{"a titled bare object and maps keeping extensions",
			entries("type: object, title: T0", mapsAB+", description: D1, x-foo: 1", maps0+", x-foo: 2"),
			"composed/M/allOf/1/properties/f model of (M/allOf/0/properties/f model [title T0]; " +
				"M/allOf/1/properties/f map [description D1]; M/allOf/2/properties/f map)"},
		{"maps no node holds together, beside a described bare object",
			entries("type: object, additionalProperties: {type: string, enum: [a, b]}, x-foo: 1", "type: object, additionalProperties: {type: string, enum: [b, c]}, x-foo: 1",
				"type: object, additionalProperties: {type: string, enum: [a, c]}, x-foo: 1", "type: object, description: D3"),
			"composed/M/allOf/1/properties/f model of (M/allOf/0/properties/f map; M/allOf/1/properties/f map; " +
				"M/allOf/2/properties/f map; M/allOf/3/properties/f model [description D3])"},
		{"lists meeting, two deprecated by keys",
			entries("type: array, items: {type: string}, minItems: 1, deprecated: true, x-deprecated-reason: R0",
				"type: array, items: {type: string}, maxItems: 9, deprecated: true, x-deprecated-since: '1.0'",
				"type: array, items: {type: string}, uniqueItems: true"),
			"composed/M/allOf/1/properties/f list [deprecated 1.0] | verbatim M/allOf/0/properties/f"},
	} {
		plain := "    Plain: {type: object}\n"
		model := "    M: {" + tc.model + "}\n"
		for _, spec := range []string{model + plain, plain + model} {
			doc, diags := parseVerified(t, openapitest.ComponentSpec(spec))
			requireErrorsAndWarnings(t, diags, nil)
			assert.Equal(t, tc.want, memberText(t, doc, componentID("M")), tc.name)
		}
	}
}

// TestAllOfFold_AReferenceToAnEntryMovesNoNoteOfAMemberDeclaredAgain pins that
// naming one allOf entry of a model, under a keyword a lowering reads or one
// nothing does, changes nothing of what the model's member folds to but its
// identity, whichever entry is named: three maps and bare objects compose, and
// two objects giving examples hold each on its node. The named entry's own
// member names the same noted node.
func TestAllOfFold_AReferenceToAnEntryMovesNoNoteOfAMemberDeclaredAgain(t *testing.T) {
	t.Parallel()
	namers := map[string]string{
		"a property": "properties: {h: %s}", "an allOf": "allOf: [%s, {properties: {own: {type: string}}}]",
		"a union's branch": "oneOf: [%s, {type: integer}]", "items": "type: array, items: %s",
		"not": "type: object, not: %s", "not's anyOf": "type: object, not: {anyOf: [%s]}", "if": "type: object, if: %s",
		"then": "type: object, then: %s", "else": "type: object, else: %s", "$defs": "type: object, $defs: {x: %s}",
		"dependentSchemas": "type: object, dependentSchemas: {x: %s}", "propertyNames": "type: object, propertyNames: %s",
		"contains": "type: object, contains: %s", "unevaluatedProperties": "type: object, unevaluatedProperties: %s",
		"unevaluatedItems": "type: object, unevaluatedItems: %s", "an extension": "type: object, x-foo: %s",
	}
	models := []struct {
		name, model, want string
		entries           []string
	}{
		{"objects with properties", foldedObjects, "composed/M/allOf/1/properties/f model of " +
			"(M/allOf/0/properties/f model [description D0]; M/allOf/1/properties/f model [description D1])",
			[]string{"M/allOf/0/properties/f model [description D0]", "M/allOf/1/properties/f model [description D1]"}},
		{"maps around a bare object", "allOf: [" +
			"{type: object, properties: {f: {type: object, additionalProperties: {type: string}, description: D0}}}, " +
			"{type: object, properties: {f: {type: object, description: D1}}}, " +
			"{type: object, properties: {f: {type: object, additionalProperties: {type: string}, description: D2}}}]",
			"composed/M/allOf/1/properties/f model of (M/allOf/0/properties/f map [description D0]; " +
				"M/allOf/1/properties/f model [description D1]; M/allOf/2/properties/f map [description D2])",
			[]string{"M/allOf/0/properties/f map [description D0]", "M/allOf/1/properties/f model [description D1]", "M/allOf/2/properties/f map [description D2]"}},
		{"bare objects giving examples", "allOf: [" +
			"{type: object, properties: {f: {type: object, description: D0, examples: [one]}}}, " +
			"{type: object, properties: {f: {type: object, description: D1, examples: [two]}}}]",
			"composed/M/allOf/1/properties/f model of (" +
				`M/allOf/0/properties/f model [description D0, example {"kind":"string","string":"one"}]; ` +
				`M/allOf/1/properties/f model [description D1, example {"kind":"string","string":"two"}])`,
			[]string{`M/allOf/0/properties/f model [description D0, example {"kind":"string","string":"one"}]`,
				`M/allOf/1/properties/f model [description D1, example {"kind":"string","string":"two"}]`}},
	}
	for _, tc := range models {
		for name, namer := range namers {
			for k, entry := range tc.entries {
				label := fmt.Sprintf("%s, entry %d named by %s", tc.name, k, name)
				ref := fmt.Sprintf("{$ref: '#/components/schemas/M/allOf/%d'}", k)
				user, model := "    User: {"+fmt.Sprintf(namer, ref)+"}\n", "    M: {"+tc.model+"}\n"
				for _, spec := range []string{user + model, model + user} {
					doc, _ := parseVerified(t, openapitest.ComponentSpec(spec))
					assert.Equal(t, tc.want, memberText(t, doc, componentID("M")), label)
					named, stands := doc.Types[ir.TypeID(fmt.Sprintf("type/anonymous/components/schemas/M/allOf/%d", k))]
					require.Equal(t, name != "an extension", stands, label)
					if stands {
						assert.Equal(t, entry, memberText(t, doc, named.Common().ID), "%s: its own member names the noted node", label)
					}
				}
			}
		}
	}
}

// TestAllOfFold_ADistributedModelsMemberDeclaredAgainKeepsItsNotes pins a model
// a union distributes, the union beside its allOf or on an entry of it: each
// variant's member, which the variants fold of the same two declarations,
// composes their noted nodes, and keeps none verbatim.
func TestAllOfFold_ADistributedModelsMemberDeclaredAgainKeepsItsNotes(t *testing.T) {
	t.Parallel()
	const branches = "oneOf: [{type: object, properties: {a: {type: string}}, required: [a]}, " +
		"{type: object, properties: {b: {type: string}}, required: [b]}]"
	want := "composed/M/allOf/1/properties/f model of (M/allOf/0/properties/f model [description D0]; M/allOf/1/properties/f model [description D1])"
	for at, model := range map[string]string{
		"M":         "type: object, " + foldedObjects + ", " + branches,
		"M/allOf/1": strings.TrimSuffix(foldedObjects, "}]") + ", " + branches + "}]",
	} {
		plain, spec := "    Plain: {type: object}\n", "    M: {"+model+"}\n"
		for _, src := range []string{spec + plain, plain + spec} {
			doc, diags := parseVerified(t, openapitest.ComponentSpec(src))
			requireErrorsAndWarnings(t, diags, nil)
			for k := range 2 {
				variant := ir.TypeID(fmt.Sprintf("type/composed/components/schemas/%s/oneOf/%d", at, k))
				assert.Equal(t, want, memberText(t, doc, variant), "%s: variant %d", at, k)
			}
		}
	}
}

// TestAllOfFold_AMemberDeclaredAgainReadsNoReplacementOffItsNode pins a policy
// mapping deprecation.replacedBy: each declaration's deprecation stays on its
// node beside its key, so the node holds the replacement, and the field, which
// holds none or that of a declaration building no node, reads no node's key,
// whether the fold keeps that node, builds one holding the key, or a reference
// names an entry (docs/compilers/openapi.md#extension-promotion).
func TestAllOfFold_AMemberDeclaredAgainReadsNoReplacementOffItsNode(t *testing.T) {
	t.Parallel()
	const replaced = "deprecated: true, x-replaced-by: New"
	for _, tc := range []struct {
		name, first, second, rest, want string
	}{
		{"the narrower enum's", "type: string, enum: [a, b, c]", "type: string, enum: [a, b], " + replaced, "",
			"M/allOf/1/properties/f enum a b [deprecated, replaced by type/openapi/components/schemas/New]"},
		{"one of two objects'", "type: object, properties: {p: {type: string}}", "type: object, properties: {q: {type: string}}, " + replaced, "",
			"composed/M/allOf/1/properties/f model of (M/allOf/0/properties/f model; " +
				"M/allOf/1/properties/f model [deprecated, replaced by type/openapi/components/schemas/New])"},
		{"a key beside no deprecation", "type: string, enum: [a, b], x-replaced-by: New", "type: string, deprecated: true", "",
			"[deprecated] M/allOf/0/properties/f enum a b"},
		{"a key a built node holds", "type: string, enum: [a, b], x-replaced-by: New", "type: string, enum: [b, c]", ", " +
			"{properties: {f: {type: string, deprecated: true}}}", "[deprecated] composed/M/allOf/1/properties/f enum b"},
	} {
		for _, user := range []string{"", "    User: {properties: {h: {$ref: '#/components/schemas/M/allOf/0'}}}\n"} {
			src := openapitest.ComponentSpec("    New: {type: string}\n    M: {allOf: [{properties: {f: {" + tc.first + "}}}, {properties: {f: {" +
				tc.second + "}}}" + tc.rest + "]}\n" + user)
			doc, _ := compileVerified(t, openapitest.SourceOf(src), openapi.Options{Promotions: openapi.ExtensionPromotions{
				Targets: map[openapi.ExtensionTarget][]openapi.ExtensionSource{openapi.TargetDeprecationReplacedBy: {{Key: "x-replaced-by"}}},
			}})
			assert.Equal(t, tc.want, memberText(t, doc, componentID("M")), "%s, an entry named %t", tc.name, user != "")
		}
	}
}

// TestAllOfFold_AMemberDeclaredAgainBesideOneBroughtLosesNoNote pins a member a
// model declares twice itself beside one a composed model declares: the model's
// own keep their notes on their nodes and the brought one's stays on its
// model's property. Two maps no type holds both of compose with the brought
// object, read as holding its note; two objects compose without it, as it says
// nothing more. The field, its own declarations lending it none, holds the
// brought note too, in every order of the entries and of the components
// (docs/compilers/openapi.md#allof-redeclared-property).
func TestAllOfFold_AMemberDeclaredAgainBesideOneBroughtLosesNoNote(t *testing.T) {
	t.Parallel()
	const base = "    Base: {type: object, properties: {f: {type: object, description: B}}}\n"
	for _, tc := range []struct {
		name, first, second, kind string
		baseComposes              bool
	}{
		{"maps apart", "additionalProperties: {type: string}", "additionalProperties: {type: object}", "map", true},
		{"objects with properties", "properties: {p: {type: string}}", "properties: {q: {type: string}}", "model", false},
	} {
		declarations := []string{
			"{properties: {f: {type: object, " + tc.first + ", description: D0}}}",
			"{$ref: '#/components/schemas/Base'}",
			"{properties: {f: {type: object, " + tc.second + ", description: D2}}}",
		}
		for _, order := range [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
			entries, parts, lastOwn := make([]string, 3), make([]string, 0, 3), 0
			for at, k := range order {
				entries[at] = declarations[k]
				if k != 1 {
					parts = append(parts, fmt.Sprintf("M/allOf/%d/properties/f %s [description D%d]", at, tc.kind, k))
					lastOwn = at
				} else if tc.baseComposes {
					parts = append(parts, "Base/properties/f model")
				}
			}
			model := "    M: {allOf: [" + strings.Join(entries, ", ") + "]}\n"
			for _, spec := range []string{base + model, model + base} {
				label := fmt.Sprintf("%s, %v", tc.name, order)
				doc, diags := parseVerified(t, openapitest.ComponentSpec(spec))
				assert.Empty(t, diags, label)
				want := fmt.Sprintf("[description B] composed/M/allOf/%d/properties/f model of (%s)", lastOwn, strings.Join(parts, "; "))
				assert.Equal(t, want, memberText(t, doc, componentID("M")), label)
				assert.Equal(t, "[description B] Base/properties/f model", memberText(t, doc, componentID("Base")), label)
			}
		}
	}

	// Two enums sharing no value: the one within the brought enum would win with
	// it, and the later wins where each is told apart by its note.
	doc, _ := parseVerified(t, openapitest.ComponentSpec("    Base: {properties: {f: {type: string, enum: [b, a], description: B}}}\n"+
		"    M: {properties: {f: {type: string, enum: [d], description: D0}}, allOf: [{properties: {f: {type: string, enum: [b], description: D1}}}, "+
		"{$ref: '#/components/schemas/Base'}]}\n"))
	assert.Equal(t, "[description B] M/properties/f enum d [description D0] | verbatim M/allOf/0/properties/f", memberText(t, doc, componentID("M")))
}

// TestAllOfFold_AnOwnDeclarationBesideTwoBroughtKeepsItsType pins a member a
// model declares once itself beside those of two models it composes, where the
// brought types as they stand would together drop the model's own: read holding
// its notes, each composes with the rest, in every order of the entries, or, at
// three maps no type holds together, the model's own, written last, is the
// field's. The field holds the model's own note, the others stay on their
// models, and nothing is kept verbatim.
func TestAllOfFold_AnOwnDeclarationBesideTwoBroughtKeepsItsType(t *testing.T) {
	t.Parallel()
	const objects = "type: object, additionalProperties: {type: object}"
	const strs = "type: object, additionalProperties: {type: string}"
	base := func(first, second string) string {
		return "    First: {properties: {f: {" + first + ", description: B1}}}\n    Second: {properties: {f: {" + second + ", description: B2}}}\n"
	}
	entries := []string{"{properties: {f: {" + objects + ", title: T0}}}", "{$ref: '#/components/schemas/First'}", "{$ref: '#/components/schemas/Second'}"}
	parts := []string{"M/allOf/%d/properties/f map", "First/properties/f model", "Second/properties/f map"}
	for _, order := range [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
		written, composed, ownAt := make([]string, 3), make([]string, 3), slices.Index(order[:], 0)
		for at, k := range order {
			written[at], composed[at] = entries[k], parts[k]
		}
		doc, diags := parseVerified(t, openapitest.ComponentSpec(base("type: object", strs)+"    M: {allOf: ["+strings.Join(written, ", ")+"]}\n"))
		requireErrorsAndWarnings(t, diags, nil)
		later := map[bool]string{true: "B2", false: "B1"}[slices.Index(order[:], 2) > slices.Index(order[:], 1)]
		want := fmt.Sprintf("[title T0, description "+later+"] composed/M/allOf/%d/properties/f model of ("+strings.Join(composed, "; ")+")", ownAt, ownAt)
		assert.Equal(t, want, memberText(t, doc, componentID("M")), "%v", order)
	}

	doc, diags := parseVerified(t, openapitest.ComponentSpec(base("type: object, additionalProperties: true, x-bar: 1", objects+", x-foo: 1")+
		"    M: {properties: {f: {"+strs+", title: T0}}, allOf: [{$ref: '#/components/schemas/Second'}, {$ref: '#/components/schemas/First'}]}\n"))
	requireErrorsAndWarnings(t, diags, nil)
	assert.Equal(t, "[title T0, description B1] M/properties/f map", memberText(t, doc, componentID("M")))
}

// TestAllOfFold_ANoteOfAUseIsNoNoteOfTheNodeItNames pins that a note written
// beside a $ref, which the property writing it holds, is none of the node the
// $ref names: a member a model declares again beside one brought, its type the
// enum its declarations meet in, adopts none of it, in either component order.
func TestAllOfFold_ANoteOfAUseIsNoNoteOfTheNodeItNames(t *testing.T) {
	t.Parallel()
	const enum = "    E: {type: string, enum: [a, b]}\n"
	const use = "    Use: {properties: {f: {$ref: '#/components/schemas/E', description: OF A USE}}}\n"
	const rest = "    Base: {properties: {f: {type: string, enum: [b, d]}}}\n" +
		"    M: {allOf: [{properties: {f: {$ref: '#/components/schemas/E'}}}, {$ref: '#/components/schemas/Base'}, {properties: {f: {type: string, enum: [b, c]}}}]}\n"
	for _, spec := range []string{enum + use + rest, rest + use + enum} {
		doc, diags := parseVerified(t, openapitest.ComponentSpec(spec))
		assert.Empty(t, diags)
		assert.Equal(t, "composed/M/allOf/2/properties/f enum b", memberText(t, doc, componentID("M")))
		assert.Equal(t, "[description OF A USE] openapi/E enum a b", memberText(t, doc, componentID("Use")))
	}
}

// TestAllOfFold_AnOwnDeclarationOfAnotherTypeLosesNoNoteToOneBrought pins a
// member a model declares once itself beside one of another type a composed
// model declares, with a note on either, both or neither, in both entry orders:
// the field takes the one type the two leave, holds the model's own note or
// else the brought one's, and the brought note stays on its model's property.
// Nothing is kept verbatim or warned of. Two deprecations by keys that differ
// keep two keys, which hold the types apart whatever notes they write.
func TestAllOfFold_AnOwnDeclarationOfAnotherTypeLosesNoNoteToOneBrought(t *testing.T) {
	t.Parallel()
	const wide, narrow = "type: object, additionalProperties: {type: string}", "type: object, additionalProperties: {type: string, enum: [a, b]}"
	const either = "anyOf: [{type: object}, {type: integer}]"
	const list, bounded = "type: array, items: {type: string}", "type: array, items: {type: string}, maxItems: 3"
	for _, tc := range []struct {
		name, own, base string
		baseWins        bool
	}{
		{"a map of strings beside a map of an enum", wide, narrow, true},
		{"a map of an enum beside a map of strings", narrow, wide, false},
		{"a union beside a map", either, wide, true},
		{"a map beside a union", wide, either, false},
		{"a list beside a bounded one", list, bounded, true},
		{"a bounded list beside one", bounded, list, false},
	} {
		for _, note := range againNotes[:3] {
			for noted := range 4 {
				write := func(v string, noted bool) string {
					if !noted {
						return ""
					}
					return ", " + fmt.Sprintf(note.write, v)
				}
				own := "{properties: {f: {" + tc.own + write("own", noted&1 != 0) + "}}}"
				base := "    Base: {properties: {f: {" + tc.base + write("base", noted&2 != 0) + "}}}\n"
				for ownAt, entries := range []string{own + ", {$ref: '#/components/schemas/Base'}", "{$ref: '#/components/schemas/Base'}, " + own} {
					doc, diags := parseVerified(t, openapitest.ComponentSpec(base+"    M: {allOf: ["+entries+"]}\n"))
					label := fmt.Sprintf("%s, %s, noted %b, own at %d", tc.name, note.name, noted, ownAt)

					requireErrorsAndWarnings(t, diags, nil)
					held := fmt.Sprintf(note.typed, "own")
					switch noted {
					case 0:
						held = ""
					case 2:
						held = fmt.Sprintf(note.typed, "base")
					}
					if held != "" {
						held = "[" + held + "] "
					}
					target := fmt.Sprintf("M/allOf/%d/properties/f", ownAt)
					if tc.baseWins {
						target = "Base/properties/f"
					}
					got := memberText(t, doc, componentID("M"))
					assert.True(t, strings.HasPrefix(got, held+target+" "), "%s: got %s", label, got)
					assert.NotContains(t, got, "verbatim", label)
					brought := memberText(t, doc, componentID("Base"))
					assert.Equal(t, noted&2 != 0, strings.HasPrefix(brought, "["+fmt.Sprintf(note.typed, "base")+"] "), "%s: Base's own: %s", label, brought)
				}
			}
		}
	}
}
