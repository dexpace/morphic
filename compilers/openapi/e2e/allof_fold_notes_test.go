// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/v2"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// foldedNote is one note a declaration of a folded field writes: how a
// declaration writes it, given the value's name, and how typedNotes names it
// typed.
type foldedNote struct {
	name          string
	write, typed  func(value string) string
	objectExample bool
}

// foldedNotes are the notes the model's own declarations of a folded field
// write: a description, a title, an example and a deprecation's message, read
// from the key the default promotion maps to it.
var foldedNotes = []foldedNote{
	{name: "description", write: func(v string) string { return "description: " + v },
		typed: func(v string) string { return "description " + v }},
	{name: "title", write: func(v string) string { return "title: " + v },
		typed: func(v string) string { return "title " + v }},
	{name: "example", write: func(v string) string { return "example: {count: " + exampleCount(v) + "}" },
		typed: func(v string) string {
			return `example {"kind":"object","object":[{"name":"count","value":{"kind":"number","number":"` +
				exampleCount(v) + `"}}]}`
		}, objectExample: true},
	{name: "deprecation", write: func(v string) string { return "deprecated: true, x-deprecated-reason: " + v },
		typed: func(v string) string { return "deprecation " + v }},
}

// exampleCount is the count the example named v writes.
func exampleCount(v string) string {
	return map[string]string{"own": "1", "base": "2", "a": "1", "b": "2", "c": "3", "d": "4"}[v]
}

// notedFoldSpec is a document whose Coll writes entries as its allOf, beside
// Info and, where base is not empty, Base, which base declares. swap writes
// the entries the other way round and collFirst writes Coll before the
// schemas beside it, so the four layouts declare the same in every order.
func notedFoldSpec(entries [2]string, base string, swap, collFirst bool) string {
	if swap {
		entries[0], entries[1] = entries[1], entries[0]
	}
	coll := "    Coll: {allOf: [" + entries[0] + ", " + entries[1] + "]}\n"
	named := "    Info: {type: object, properties: {count: {type: integer}}}\n"
	if base != "" {
		named += "    Base: " + base + "\n"
	}
	if collFirst {
		return openapitest.ComponentSpec(coll + named)
	}
	return openapitest.ComponentSpec(named + coll)
}

// typedNotes lists every note doc types on a node a named type reaches, or on
// a property of one: its description, title, examples and deprecation's
// message, sorted. A note kept only verbatim under Unmodeled, or only on a
// node nothing names, is not among them.
func typedNotes(t *testing.T, doc *ir.Document) []string {
	t.Helper()
	var out []string
	add := func(docs ir.Docs, examples []ir.Example, deprecation *ir.Deprecation) {
		if docs.Description != "" {
			out = append(out, "description "+docs.Description)
		}
		if docs.Summary != "" {
			out = append(out, "title "+docs.Summary)
		}
		for _, example := range examples {
			raw, err := json.Marshal(example.Value, json.Deterministic(true))
			require.NoError(t, err)
			out = append(out, "example "+string(raw))
		}
		if deprecation != nil {
			out = append(out, "deprecation "+deprecation.Message)
		}
	}
	var pending []ir.TypeID
	for id, td := range doc.Types {
		if !td.Common().Anonymous {
			pending = append(pending, id)
		}
	}
	seen := map[ir.TypeID]bool{}
	for len(pending) > 0 {
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		td, ok := doc.Types[id]
		if seen[id] || !ok {
			continue
		}
		seen[id] = true
		common := td.Common()
		add(common.Docs, common.Examples, common.Deprecation)
		if model, isModel := td.(*ir.Model); isModel {
			for _, property := range model.Properties {
				add(property.Docs, property.Examples, property.Deprecation)
			}
		}
		pending = slices.AppendSeq(pending, ir.TypeEdges(td))
	}
	slices.Sort(out)
	return out
}

// notedField compiles src and returns the document, Coll's field f, and what
// the run reports.
func notedField(t *testing.T, src string) (*ir.Document, ir.Property, []ir.Diagnostic) {
	t.Helper()
	doc, diags := parseVerified(t, src)
	require.NotNil(t, doc)
	model, ok := typeByName(doc, "Coll").(*ir.Model)
	require.True(t, ok, "Coll is a model")
	field, ok := propertyByWire(model, "f")
	require.True(t, ok, "Coll holds f")
	return doc, field, diags
}

// layoutName names a layout of notedFoldSpec.
func layoutName(swap, collFirst bool) string {
	return map[bool]string{false: "as written", true: "entries swapped"}[swap] +
		map[bool]string{false: ", Coll last", true: ", Coll first"}[collFirst]
}

// TestAllOfFold_ANoteTheModelWritesWinsOverOneItBrings pins that a note the
// composing model's own allOf entry writes for a field beside the one another
// model's declaration brings is the field's, in every order of the entries and
// of the components, while the brought one stays typed on that model's
// property: a description, a title, an example and a deprecation's message.
// Nothing is kept verbatim, and no layout types a note another does not
// (docs/compilers/openapi.md#allof-redeclared-property).
func TestAllOfFold_ANoteTheModelWritesWinsOverOneItBrings(t *testing.T) {
	t.Parallel()
	for _, note := range foldedNotes {
		own := "{properties: {f: {type: object, " + note.write("own") + "}}}"
		base := "{type: object, properties: {f: {type: object, properties: {count: {type: integer}}, " +
			note.write("base") + "}}}"
		entries := [2]string{own, "{$ref: '#/components/schemas/Base'}"}
		want := []string{note.typed("base"), note.typed("own")}
		slices.Sort(want)
		var compiled [2][2]*ir.Document
		for _, swap := range []bool{false, true} {
			for _, collFirst := range []bool{false, true} {
				label := note.name + ", " + layoutName(swap, collFirst)
				doc, field, diags := notedField(t, notedFoldSpec(entries, base, swap, collFirst))
				compiled[boolIndex(swap)][boolIndex(collFirst)] = doc

				assert.Equal(t, []string{note.typed("own")}, fieldNotes(t, field), "%s: the field holds the model's own", label)
				assert.Empty(t, field.Unmodeled, "%s: and keeps nothing verbatim", label)
				assert.Equal(t, want, typedNotes(t, doc), "%s: each note is typed once", label)
				requireErrorsAndWarnings(t, diags, nil)
			}
		}
		for _, swap := range []int{0, 1} {
			assert.Empty(t, cmp.Diff(compiled[swap][0].Types, compiled[swap][1].Types), "%s: the types do not depend on component order", note.name)
		}
	}
}

// TestAllOfFold_ANoteTwoOwnDeclarationsGiveDifferentlyIsTypedInEveryOrder
// pins that where two allOf entries of the composing model write a note of a
// field differently, both stay typed in every order of the entries and of the
// components, wherever the field composes or narrows the two: of bare objects
// or objects with properties, the note the field does not keep stays on the
// node the other's schema builds, which the field's type reaches, and both
// examples are the field's, of two lists too. Two lists differing in a note
// alone are judged apart, so the field keeps the later and the earlier
// declaration verbatim (docs/compilers/openapi.md#allof-redeclared-property).
func TestAllOfFold_ANoteTwoOwnDeclarationsGiveDifferentlyIsTypedInEveryOrder(t *testing.T) {
	t.Parallel()
	shapes := []struct {
		name, a, b string
		composes   bool
	}{
		{"bare objects", "type: object", "type: object", true},
		{"objects with properties", "type: object, properties: {count: {type: integer}}",
			"type: object, properties: {page: {type: integer}}", true},
		{"lists", "type: array, items: {type: object}", "type: array, items: {type: object}", false},
	}
	for _, shape := range shapes {
		for _, note := range foldedNotes {
			if note.objectExample && !shape.composes {
				note = listExample
			}
			entries := [2]string{
				"{properties: {f: {" + shape.a + ", " + note.write("a") + "}}}",
				"{properties: {f: {" + shape.b + ", " + note.write("b") + "}}}",
			}
			var compiled [2][2]*ir.Document
			for _, swap := range []bool{false, true} {
				for _, collFirst := range []bool{false, true} {
					label := shape.name + ", " + note.name + ", " + layoutName(swap, collFirst)
					doc, field, diags := notedField(t, notedFoldSpec(entries, "", swap, collFirst))
					compiled[boolIndex(swap)][boolIndex(collFirst)] = doc

					want, verbatim := []string{note.typed("a"), note.typed("b")}, 0
					if !shape.composes && note.name != "example" {
						want, verbatim = []string{note.typed(map[bool]string{false: "b", true: "a"}[swap])}, 1
					}
					slices.Sort(want)
					assert.Equal(t, want, typedNotes(t, doc), "%s: the notes typed", label)
					assert.Len(t, field.Unmodeled, verbatim, "%s: the declarations kept verbatim", label)
					requireErrorsAndWarnings(t, diags, nil)
					// Lists keeping deprecation keys that differ are two types apart whatever
					// their notes, so that overlap is reported beside the tie.
					reports := map[string]int{"example": 0, "deprecation": 1 + boolIndex(!shape.composes)}[note.name]
					if note.name == "description" || note.name == "title" {
						reports = 1
					}
					assert.Equal(t, reports, codeCounts(diags)[diag.DegradedConstruct],
						"%s: the tie is reported, the types judged as with no note left", label)
				}
			}
			for _, swap := range []int{0, 1} {
				assert.Empty(t, cmp.Diff(compiled[swap][0].Types, compiled[swap][1].Types),
					"%s, %s: the types do not depend on component order", shape.name, note.name)
			}
		}
	}
}

// listExample is the example note of a list of objects.
var listExample = foldedNote{name: "example", write: func(v string) string { return "example: [{count: " + exampleCount(v) + "}]" },
	typed: func(v string) string {
		return `example {"kind":"list","list":[{"kind":"object","object":[{"name":"count","value":{"kind":"number","number":"` +
			exampleCount(v) + `"}}]}]}`
	}}

// fieldNotes lists the notes field holds itself, as typedNotes names them.
func fieldNotes(t *testing.T, field ir.Property) []string {
	t.Helper()
	doc := &ir.Document{Types: ir.TypeRegistry{"type/holder": &ir.Model{ID: "type/holder",
		Properties: []ir.Property{{Docs: field.Docs, Examples: field.Examples, Deprecation: field.Deprecation}}}}}
	return typedNotes(t, doc)
}

// boolIndex is 1 for true and 0 for false.
func boolIndex(b bool) int {
	if b {
		return 1
	}
	return 0
}

// TestAllOfFold_ANoteOfAnEntryAReferenceNamesIsTypedOnce pins that where a
// reference names an allOf entry of the composing model, which then lowers as
// a model of its own whose member carries that entry's note, the fold leaves
// the note on no node and adds no example of it to the field: the field keeps
// the other entry's, the named entry's declaration is kept verbatim, and each
// note is typed once, in every order of the entries and of the components,
// whether a property's $ref or another model's allOf names the entry
// (docs/compilers/openapi.md#allof-redeclared-property).
func TestAllOfFold_ANoteOfAnEntryAReferenceNamesIsTypedOnce(t *testing.T) {
	t.Parallel()
	shapes := []struct{ name, schema string }{
		{"bare objects", "type: object"},
		{"objects with properties", "type: object, properties: {count: {type: integer}}"},
		{"lists", "type: array, items: {type: object}"},
	}
	for _, shape := range shapes {
		for _, note := range foldedNotes {
			if note.objectExample && strings.HasPrefix(shape.schema, "type: array") {
				note = listExample
			}
			entry := func(v string) string { return "{properties: {f: {" + shape.schema + ", " + note.write(v) + "}}}" }
			namers := []struct{ name, schema, writes string }{
				{"a property's $ref", "    Other: {type: object, properties: {g: {$ref: '#/components/schemas/Coll/allOf/0'}}}\n", ""},
				{"another model's allOf", "    Other: {allOf: [{$ref: '#/components/schemas/Coll/allOf/0'}, " + entry("c") + "]}\n", "c"},
			}
			for _, namer := range namers {
				want := []string{note.typed("a"), note.typed("b")}
				if namer.writes != "" {
					want = append(want, note.typed(namer.writes))
				}
				slices.Sort(want)
				var compiled [2][2]*ir.Document
				for _, swap := range []bool{false, true} {
					for _, collFirst := range []bool{false, true} {
						label := shape.name + ", " + note.name + ", " + namer.name + ", " + layoutName(swap, collFirst)
						entries := [2]string{entry("a"), entry("b")}
						if swap {
							entries[0], entries[1] = entries[1], entries[0]
						}
						coll := "    Coll: {allOf: [" + entries[0] + ", " + entries[1] + "]}\n"
						src := openapitest.ComponentSpec(namer.schema + coll)
						if collFirst {
							src = openapitest.ComponentSpec(coll + namer.schema)
						}
						doc, field, diags := notedField(t, src)
						compiled[boolIndex(swap)][boolIndex(collFirst)] = doc

						assert.Equal(t, want, typedNotes(t, doc), "%s: each note is typed once", label)
						assert.Len(t, field.Unmodeled, 1, "%s: the named entry's declaration is kept verbatim", label)
						requireErrorsAndWarnings(t, diags, nil)
						requireTieReportsTrue(t, doc, field, diags, func(int, string) string { return "" })
					}
				}
				for _, swap := range []int{0, 1} {
					assert.Empty(t, cmp.Diff(compiled[swap][0].Types, compiled[swap][1].Types),
						"%s, %s, %s: the types do not depend on component order", shape.name, note.name, namer.name)
				}
			}
		}
	}
}

// TestAllOfFold_EachTieReportNamesWhereTheNoteIsKept pins that every report of
// a note three or four allOf entries of the composing model give differently
// says truly where the note it did not keep is: on the field's type, which
// then holds it, or verbatim under Unmodeled, which then holds that
// declaration. Enums sharing some members meet in a node the fold builds,
// which adopts one note alone; lists, maps and bare objects are judged as
// their kinds are (docs/compilers/openapi.md#allof-redeclared-property).
func TestAllOfFold_EachTieReportNamesWhereTheNoteIsKept(t *testing.T) {
	t.Parallel()
	shapes := []struct{ name, schema string }{
		{"enums", "type: string, enum: [b, c, %s]"},
		{"lists of enums", "type: array, items: {type: string, enum: [b, c, %s]}"},
		{"maps", "type: object, additionalProperties: {type: string}"},
		{"bare objects", "type: object"},
	}
	values := []string{"a", "b", "c", "d"}
	for _, shape := range shapes {
		for _, note := range foldedNotes {
			if note.objectExample {
				continue
			}
			for _, count := range []int{3, 4} {
				entries := make([]string, count)
				for k := range count {
					schema := shape.schema
					if strings.Contains(schema, "%s") {
						schema = strings.ReplaceAll(schema, "%s", "x"+values[k])
					}
					entries[k] = "{type: object, properties: {f: {" + schema + ", " + note.write(values[k]) + "}}}"
				}
				label := fmt.Sprintf("%s, %s, %d entries", shape.name, note.name, count)
				doc, field, diags := notedField(t, openapitest.ComponentSpec("    Coll: {allOf: ["+strings.Join(entries, ", ")+"]}\n"))

				requireErrorsAndWarnings(t, diags, nil)
				reports := requireTieReportsTrue(t, doc, field, diags, func(k int, _ string) string { return note.typed(values[k]) })
				assert.Equal(t, count-1, reports, "%s: each note the field does not keep is reported", label)
			}
		}
	}

	// One entry gives a title alone and two a title and a deprecation, in every
	// order, so the node the enums meet in can adopt one entry's deprecation
	// and another's title.
	title, deprecation := foldedNotes[1], foldedNotes[3]
	for _, shape := range shapes[:2] {
		for _, order := range [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
			entries := make([]string, 3)
			for k, writer := range order {
				schema := strings.ReplaceAll(shape.schema, "%s", "x"+values[writer])
				written := title.write(values[writer])
				if writer > 0 {
					written += ", " + deprecation.write(values[writer])
				}
				entries[k] = "{type: object, properties: {f: {" + schema + ", " + written + "}}}"
			}
			label := fmt.Sprintf("%s, a title beside a deprecation, written in the order %v", shape.name, order)
			doc, field, diags := notedField(t, openapitest.ComponentSpec("    Coll: {allOf: ["+strings.Join(entries, ", ")+"]}\n"))

			requireErrorsAndWarnings(t, diags, nil)
			reports := requireTieReportsTrue(t, doc, field, diags, func(k int, detail string) string {
				return map[string]foldedNote{"title": title, "deprecation": deprecation}[detail].typed(values[order[k]])
			})
			assert.Equal(t, 3, reports, "%s: two titles and a deprecation are reported", label)
		}
	}
}

// tieReport reads a report of a note two declarations give differently.
var tieReport = regexp.MustCompile(`give its ([a-z ]+) differently; kept the one the declaration at (\S+) gives over the one at (\S+), which (.+)$`)

// requireTieReportsTrue holds each report diags make of a note Coll's
// declarations give differently to the document: a declaration whose note
// "the field's type holds" gives one the field's type or a model it composes
// holds, as typed(k, detail) names the note of detail Coll's k-th allOf entry
// gives, empty where that is not to be checked; and one "kept verbatim" is under field's
// Unmodeled. One staying on its model, as another model's fold reports, is
// read and not checked. It returns how many reports it read.
func requireTieReportsTrue(t *testing.T, doc *ir.Document, field ir.Property, diags []ir.Diagnostic, typed func(k int, detail string) string) int {
	t.Helper()
	read := 0
	for _, d := range diags {
		match := tieReport.FindStringSubmatch(d.Message)
		if match == nil {
			continue
		}
		read++
		lost := match[3]
		switch match[4] {
		case "the field's type holds":
			var k int
			_, err := fmt.Sscanf(lost, "/components/schemas/Coll/allOf/%d/properties/f", &k)
			require.NoError(t, err, d.Message)
			want := typed(k, match[1])
			if want == "" {
				continue
			}
			holders := append([]ir.TypeID{field.Type.Target}, mixinsOf(doc, field.Type.Target)...)
			held := make([]string, 0, len(holders))
			for _, id := range holders {
				td, ok := doc.Types[id]
				require.True(t, ok, id)
				common := td.Common()
				held = append(held, fieldNotes(t, ir.Property{Docs: common.Docs, Examples: common.Examples, Deprecation: common.Deprecation})...)
			}
			assert.Contains(t, held, want, "%s: the field's type holds it", d.Message)
		case "is kept verbatim under Unmodeled":
			assert.Contains(t, field.Unmodeled, "openapi:conflicting-redeclaration"+lost, "%s: it is kept verbatim", d.Message)
		case "stays declared on its model":
		default:
			assert.Fail(t, "a report saying neither", d.Message)
		}
	}
	return read
}

// mixinsOf lists the types the model id names composes, none where it is no
// model.
func mixinsOf(doc *ir.Document, id ir.TypeID) []ir.TypeID {
	model, isModel := doc.Types[id].(*ir.Model)
	if !isModel {
		return nil
	}
	out := make([]ir.TypeID, 0, len(model.Mixins))
	for _, mixin := range model.Mixins {
		out = append(out, mixin.Target)
	}
	return out
}
