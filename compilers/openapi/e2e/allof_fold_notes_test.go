// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/v2"
	"slices"
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
	return map[string]string{"own": "1", "base": "2", "a": "1", "b": "2"}[v]
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
