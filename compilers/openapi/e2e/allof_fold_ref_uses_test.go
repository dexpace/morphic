// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// declarationForm is how an allOf entry declares a field beside a note: a use
// of Info by $ref, the same written as an allOf of that $ref, or an inline
// object of Info's shape.
type declarationForm struct {
	name  string
	write func(note string) string
	ref   bool
}

var (
	refUse    = declarationForm{"a $ref", func(note string) string { return "{$ref: '#/components/schemas/Info', " + note + "}" }, true}
	allOfUse  = declarationForm{"an allOf of a $ref", func(note string) string { return "{allOf: [{$ref: '#/components/schemas/Info'}], " + note + "}" }, true}
	inlineUse = declarationForm{"an inline object", func(note string) string {
		return "{type: object, properties: {count: {type: integer}}, " + note + "}"
	}, false}
)

// collFieldPointer is where the allOf entry at index i of Coll declares f.
func collFieldPointer(i int) string {
	return "/components/schemas/Coll/allOf/" + strconv.Itoa(i) + "/properties/f"
}

// keptRedeclarations lists every declaration doc keeps verbatim because a
// fold did not take it, on any type or property.
func keptRedeclarations(doc *ir.Document) []string {
	var out []string
	collect := func(unmodeled ir.Unmodeled) {
		for key := range unmodeled {
			if strings.HasPrefix(key, "openapi:conflicting-redeclaration") {
				out = append(out, key)
			}
		}
	}
	for _, td := range doc.Types {
		collect(td.Common().Unmodeled)
		if model, ok := td.(*ir.Model); ok {
			for _, property := range model.Properties {
				collect(property.Unmodeled)
			}
		}
	}
	return out
}

// TestAllOfFold_ANoteBesideAReferenceTheModelWritesIsTheFields pins that a
// note Coll's own entry writes beside a $ref, or beside an allOf of one, for a
// field Base declares with another note beside the same $ref, is the field's,
// and Base's stays on Base's property: a description, a title and an example,
// in every order of the entries and of the components. The field names Info,
// nothing is kept verbatim, and the four layouts type the same notes in the
// same places (docs/compilers/openapi.md#allof-redeclared-property).
func TestAllOfFold_ANoteBesideAReferenceTheModelWritesIsTheFields(t *testing.T) {
	t.Parallel()
	for _, form := range []declarationForm{refUse, allOfUse} {
		for _, note := range foldedNotes {
			if note.name == "deprecation" {
				// A promoted key keeps the node, as TwoOwnDeclarationsBesideAReferenceKeepEachNote pins.
				continue
			}
			want := []string{"Base.result_info: " + note.typed("base"), "Coll.result_info: " + note.typed("own")}
			slices.Sort(want)
			var compiled [2][2]*ir.Document
			for _, inlineFirst := range []bool{false, true} {
				for _, collFirst := range []bool{false, true} {
					label := form.name + ", " + note.name + ", " + layoutName(inlineFirst, collFirst)
					field, diags, doc := collField(t, foldedExampleSpec(form.write(note.write("base")), form.write(note.write("own")), inlineFirst, collFirst))
					compiled[boolIndex(inlineFirst)][boolIndex(collFirst)] = doc

					assert.Equal(t, want, placedNotes(t, doc), "%s: where each note is typed", label)
					assert.Equal(t, componentID("Info"), field.Type.Target, "%s: the field names Info", label)
					assert.Empty(t, keptRedeclarations(doc), "%s: nothing is kept verbatim", label)
					requireErrorsAndWarnings(t, diags, nil)
				}
			}
			for _, inlineFirst := range []int{0, 1} {
				assert.Empty(t, cmp.Diff(compiled[inlineFirst][0].Types, compiled[inlineFirst][1].Types),
					"%s, %s: the types do not depend on component order", form.name, note.name)
			}
		}
	}
}

// twoOwnPlaces is where the notes a and b, written by Coll's entries 0 and 1
// as first and second declare f, are typed: a title or a description is the
// field's from the later, the earlier's on the node its schema builds; an
// example of each is the field's; a deprecation a promoted key gives stays on
// the node of a use of a $ref and is the field's from an inline object.
func twoOwnPlaces(note foldedNote, first, second declarationForm, a, b string) []string {
	want := make([]string, 0, 2)
	switch note.name {
	case "example":
		want = append(want, "Coll.f: "+note.typed(a), "Coll.f: "+note.typed(b))
	case "deprecation":
		for i, declared := range []struct {
			form declarationForm
			v    string
		}{{first, a}, {second, b}} {
			place := "Coll.f"
			if declared.form.ref {
				place = "node " + collFieldPointer(i)
			}
			want = append(want, place+": "+note.typed(declared.v))
		}
	default:
		want = append(want, "Coll.f: "+note.typed(b), "node "+collFieldPointer(0)+": "+note.typed(a))
	}
	slices.Sort(want)
	return want
}

// TestAllOfFold_TwoOwnDeclarationsBesideAReferenceKeepEachNote pins that where
// two of Coll's own entries declare f, at least one a use of Info writing a
// note beside its $ref or beside an allOf of it, and they give the note
// otherwise, both stay typed in every order of the entries and of the
// components: a use giving its note apart keeps the node its schema builds, so
// the fold has somewhere to leave the note the field does not keep, and
// nothing is kept verbatim (docs/compilers/openapi.md#ref-use-site).
func TestAllOfFold_TwoOwnDeclarationsBesideAReferenceKeepEachNote(t *testing.T) {
	t.Parallel()
	for _, pair := range [][2]declarationForm{{refUse, refUse}, {allOfUse, allOfUse}, {refUse, allOfUse}, {refUse, inlineUse}, {allOfUse, inlineUse}} {
		for _, note := range foldedNotes {
			entries := [2]string{
				"{properties: {f: " + pair[0].write(note.write("a")) + "}}",
				"{properties: {f: " + pair[1].write(note.write("b")) + "}}",
			}
			var compiled [2][2]*ir.Document
			for _, swap := range []bool{false, true} {
				for _, collFirst := range []bool{false, true} {
					label := pair[0].name + " and " + pair[1].name + ", " + note.name + ", " + layoutName(swap, collFirst)
					doc, _, diags := notedField(t, notedFoldSpec(entries, "", swap, collFirst))
					compiled[boolIndex(swap)][boolIndex(collFirst)] = doc

					want := twoOwnPlaces(note, pair[0], pair[1], "a", "b")
					if swap {
						want = twoOwnPlaces(note, pair[1], pair[0], "b", "a")
					}
					assert.Equal(t, want, placedNotes(t, doc), "%s: where each note is typed", label)
					assert.Empty(t, keptRedeclarations(doc), "%s: nothing is kept verbatim", label)
					requireErrorsAndWarnings(t, diags, nil)
				}
			}
			for _, swap := range []int{0, 1} {
				assert.Empty(t, cmp.Diff(compiled[swap][0].Types, compiled[swap][1].Types),
					"%s and %s, %s: the types do not depend on component order", pair[0].name, pair[1].name, note.name)
			}
		}
	}
}

// TestAllOfFold_OwnUsesAgreeingOnANoteNameTheReferent pins that two of Coll's
// own uses of Info that write no note otherwise, the same description or
// example, or a description and a title, keep this lowering: the field names Info and holds
// both notes, and no node stands at either declaration.
func TestAllOfFold_OwnUsesAgreeingOnANoteNameTheReferent(t *testing.T) {
	t.Parallel()
	for name, notes := range map[string][2]string{
		"one description":           {"description: a", "description: a"},
		"a description and a title": {"description: a", "title: b"},
		"one example":               {"example: {count: 1}", "example: {count: 1}"},
	} {
		entries := [2]string{"{properties: {f: " + refUse.write(notes[0]) + "}}", "{properties: {f: " + allOfUse.write(notes[1]) + "}}"}
		doc, field, diags := notedField(t, notedFoldSpec(entries, "", false, false))

		assert.Equal(t, componentID("Info"), field.Type.Target, "%s: the field names Info", name)
		for _, places := range placedNotes(t, doc) {
			assert.True(t, strings.HasPrefix(places, "Coll.f: "), "%s: %s is the field's", name, places)
		}
		assert.Empty(t, keptRedeclarations(doc), name)
		requireErrorsAndWarnings(t, diags, nil)
	}
}

// TestAllOfFold_TwoOwnUsesAndABroughtOneKeepEachNote pins that where Coll's
// two own entries give f's note otherwise beside a $ref, or beside an allOf of
// one, and Base, which it composes, gives a third, every note stays typed in
// every order of the three entries and of the components: Base's on Base's
// property, the later own one on the field and the earlier on the node its
// schema builds, and each example on the field but Base's.
func TestAllOfFold_TwoOwnUsesAndABroughtOneKeepEachNote(t *testing.T) {
	t.Parallel()
	for _, form := range []declarationForm{refUse, allOfUse} {
		for _, note := range foldedNotes {
			if note.name == "deprecation" {
				// A promoted key keeps the node, as TwoOwnDeclarationsBesideAReferenceKeepEachNote pins.
				continue
			}
			written := []string{"{$ref: '#/components/schemas/Base'}",
				"{properties: {f: " + form.write(note.write("a")) + "}}", "{properties: {f: " + form.write(note.write("b")) + "}}"}
			base := "{type: object, properties: {f: " + form.write(note.write("c")) + "}}"
			for _, order := range [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
				var compiled [2]*ir.Document
				for _, collFirst := range []bool{false, true} {
					label := fmt.Sprintf("%s, %s, entries %v, Coll first %v", form.name, note.name, order, collFirst)
					coll := "    Coll: {allOf: [" + written[order[0]] + ", " + written[order[1]] + ", " + written[order[2]] + "]}\n"
					named := "    Info: {type: object, properties: {count: {type: integer}}}\n    Base: " + base + "\n"
					src := openapitest.ComponentSpec(named + coll)
					if collFirst {
						src = openapitest.ComponentSpec(coll + named)
					}
					doc, _, diags := notedField(t, src)
					compiled[boolIndex(collFirst)] = doc

					assert.Equal(t, threeDeclarationPlaces(note, order), placedNotes(t, doc), "%s: where each note is typed", label)
					assert.Empty(t, keptRedeclarations(doc), "%s: nothing is kept verbatim", label)
					requireErrorsAndWarnings(t, diags, nil)
				}
				assert.Empty(t, cmp.Diff(compiled[0].Types, compiled[1].Types), "%s, %s, %v: the types do not depend on component order", form.name, note.name, order)
			}
		}
	}
}

// threeDeclarationPlaces is where TwoOwnUsesAndABroughtOne types each note,
// Coll's entries writing, by index, the $ref to Base, a and b in order, and
// Base the note c.
func threeDeclarationPlaces(note foldedNote, order [3]int) []string {
	base := "Base.f: " + note.typed("c")
	if note.name == "example" {
		want := []string{base, "Coll.f: " + note.typed("a"), "Coll.f: " + note.typed("b")}
		slices.Sort(want)
		return want
	}
	at := map[int]int{}
	for i, written := range order {
		at[written] = i
	}
	earlier, later := "a", "b"
	if at[2] < at[1] {
		earlier, later = "b", "a"
	}
	earlierAt := min(at[1], at[2])
	want := []string{base, "Coll.f: " + note.typed(later), "node " + collFieldPointer(earlierAt) + ": " + note.typed(earlier)}
	slices.Sort(want)
	return want
}
