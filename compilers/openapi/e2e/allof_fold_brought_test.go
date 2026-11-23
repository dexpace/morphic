// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// entryOrders is every order three allOf entries are written in, each naming
// which of them stands at each place.
var entryOrders = [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}

// besideTwoBrought is a document whose model M composes, in order, its own
// declaration of f, entry 0, and those of A and B, entries 1 and 2.
func besideTwoBrought(order [3]int, own, first, second string) string {
	entries := []string{"{properties: {f: {" + own + "}}}", "{$ref: '#/components/schemas/A'}", "{$ref: '#/components/schemas/B'}"}
	return openapitest.ComponentSpec("    A: {properties: {f: {" + first + "}}}\n    B: {properties: {f: {" + second + "}}}\n" +
		"    M: {allOf: [" + entries[order[0]] + ", " + entries[order[1]] + ", " + entries[order[2]] + "]}\n")
}

// memberF is the property f of the model id names.
func memberF(t *testing.T, doc *ir.Document, id ir.TypeID) ir.Property {
	t.Helper()
	model, isModel := doc.Types[id].(*ir.Model)
	require.True(t, isModel, "%s is a model", id)
	property, ok := propertyByWire(model, "f")
	require.True(t, ok, "%s holds f", id)
	return property
}

// foldReports returns the diagnostics a fold reports of f.
func foldReports(diags []ir.Diagnostic) []string {
	var out []string
	for _, diagnostic := range diags {
		if strings.Contains(diagnostic.Message, `declarations of field "f"`) {
			out = append(out, diagnostic.Message)
		}
	}
	return out
}

// TestAllOfFold_ATypeANoteHeldApartIsNotLostToTheNotesMove pins a member beside
// two brought declarations whose types as they stand would drop one that its
// note held apart while its node held it, in every order of the entries: a list
// of a union keeps its elements and its bound, a member an inline composition
// declares three times too, and a literal or a longer tuple is the field's
// where it is written last, as each was then, rather than in no order.
func TestAllOfFold_ATypeANoteHeldApartIsNotLostToTheNotesMove(t *testing.T) {
	t.Parallel()
	const union = "type: array, items: {anyOf: [{type: string}, {type: integer}]}"
	const bounded = "type: array, minItems: 1, description: at least one"
	elementOf := func(t *testing.T, doc *ir.Document, id ir.TypeID) (ir.TypeKind, int64) {
		t.Helper()
		list, isList := doc.Types[id].(*ir.List)
		require.True(t, isList, id)
		require.NotNil(t, list.Constraints, id)
		require.NotNil(t, list.Constraints.MinItems, id)
		return doc.Types[list.ElementType.Target].Kind(), *list.Constraints.MinItems
	}
	for _, order := range entryOrders {
		doc, _ := parseVerified(t, besideTwoBrought(order, union+", description: mine", union, bounded))
		field := memberF(t, doc, componentID("M"))
		element, least := elementOf(t, doc, field.Type.Target)
		assert.Equal(t, ir.KindUnion, element, "%v", order)
		assert.Equal(t, int64(1), least, "%v", order)
		assert.Equal(t, "description mine", notesText(t, field.Docs, field.Deprecation, field.Examples), "%v", order)

		declared := []string{union + ", description: mine", union, bounded}
		entries := make([]string, 3)
		for at, k := range order {
			entries[at] = "{properties: {f: {type: object, properties: {p: {" + declared[k] + "}}}}}"
		}
		doc, _ = parseVerified(t, openapitest.ComponentSpec("    M: {allOf: ["+strings.Join(entries, ", ")+"]}\n"))
		composed, isModel := doc.Types[memberF(t, doc, componentID("M")).Type.Target].(*ir.Model)
		require.True(t, isModel, "%v", order)
		nested, ok := propertyByWire(composed, "p")
		require.True(t, ok, "%v", order)
		element, least = elementOf(t, doc, nested.Type.Target)
		assert.Equal(t, ir.KindUnion, element, "nested, %v", order)
		assert.Equal(t, int64(1), least, "nested, %v", order)
	}

	for _, tc := range []struct{ name, wide, narrow string }{
		{"an enum and a literal within it", "type: string, enum: [cat, dog]", "const: cat"},
		{"a tuple and a longer one", "type: array, prefixItems: [{type: string}]", "type: array, prefixItems: [{type: string}, {type: integer}]"},
	} {
		for _, order := range entryOrders {
			doc, _ := parseVerified(t, besideTwoBrought(order, tc.wide, tc.wide+", description: of A", tc.narrow))
			field := memberF(t, doc, componentID("M"))
			later := []ir.TypeID{"type/anonymous/components/schemas/M/allOf/2/properties/f",
				"type/anonymous/components/schemas/A/properties/f", "type/anonymous/components/schemas/B/properties/f"}[order[2]]
			assert.Equal(t, later, field.Type.Target, "%s, %v", tc.name, order)
			assert.Equal(t, "description of A", notesText(t, field.Docs, field.Deprecation, field.Examples), "%s, %v", tc.name, order)
		}
	}
}

// TestAllOfFold_AnEntryOrderDecidesNoTypeItsNotesSettle pins that where one
// order of the entries keeps every declaration's type, so does every other, and
// reports nothing: a map keeping an extension, its copy and a noted wider one
// are one type, the node that says all three, and so are two lists a note alone
// tells apart from a narrower one, beside a dropped integer.
func TestAllOfFold_AnEntryOrderDecidesNoTypeItsNotesSettle(t *testing.T) {
	t.Parallel()
	const narrowMap = "type: object, additionalProperties: {type: string, enum: [a, b]}"
	for _, tc := range []struct {
		name, own, first, second, want string
		dropped                        bool
	}{
		{"a map keeping an extension", narrowMap, narrowMap + ", x-foo: 1", "type: object, additionalProperties: {type: string}, description: text",
			"[description text] A/properties/f map", false},
		{"an enum within a noted one", "type: integer", "type: string, enum: [a, b]", "type: string, enum: [a, b, c], description: text",
			"[description text] A/properties/f enum a b", true},
		{"a list within a noted one", "type: integer", "type: array, items: {type: string, enum: [a, b]}", "type: array, items: {type: string}, description: text",
			"[description text] A/properties/f list", true},
	} {
		for _, order := range entryOrders {
			doc, diags := parseVerified(t, besideTwoBrought(order, tc.own, tc.first, tc.second))
			got, _, _ := strings.Cut(memberText(t, doc, componentID("M")), " | verbatim")
			assert.Equal(t, tc.want, got, "%s, %v", tc.name, order)
			assert.Equal(t, tc.dropped, len(foldReports(diags)) > 0, "%s, %v: %v", tc.name, order, foldReports(diags))
		}
	}
}

// TestAllOfFold_ABuiltNodeHoldsNoNoteItsFieldHolds pins where a note stands
// that a brought declaration's property carries, where its type meets the
// model's own in a node the fold builds: on the field alone, whether the types
// meet as they stand or, one being dropped, read with their notes, and whether
// the model declares the member once or more. A note an own node holds is the
// built node's, so the field holds no brought one, and what is reported is the
// field's tie, not one of a type holding a note it does not.
func TestAllOfFold_ABuiltNodeHoldsNoNoteItsFieldHolds(t *testing.T) {
	t.Parallel()
	const base = "type: string, enum: [a, b, c], title: Tb, description: Db, externalDocs: {url: 'https://e.test/b'}, deprecated: true, x-deprecated-reason: Rb"
	const held = "[title Tb, description Db, link https://e.test/b, deprecated Rb] "
	for _, tc := range []struct {
		name, want string
		own        []string
		rest       string
		reports    []string
	}{
		{"declared once, met as they stand", held + "composed/M/allOf/0/properties/f enum a b", []string{"type: string, enum: [a, b, d]"}, "", nil},
		{"declared once, met beside a dropped type", held + "composed/M/allOf/0/properties/f enum a b", []string{"type: string, enum: [a, b, d]"},
			", {$ref: '#/components/schemas/Other'}", []string{"incompatible types"}},
		{"declared twice", held + "composed/M/allOf/1/properties/f enum a b",
			[]string{"type: string, enum: [a, b, d]", "type: string, enum: [a, b, d, e]"}, "", nil},
		{"declared twice, beside a dropped type", held + "composed/M/allOf/1/properties/f enum a b",
			[]string{"type: string, enum: [a, b, d]", "type: string, enum: [a, b, d, e]"}, ", {$ref: '#/components/schemas/Other'}", []string{"incompatible types"}},
		{"declared twice, one's node described",
			"[title Tb, link https://e.test/b, deprecated Rb] composed/M/allOf/1/properties/f enum a b [description D0]",
			[]string{"type: string, enum: [a, b, d], description: D0", "type: string, enum: [a, b, d, e]"}, "",
			[]string{"give its description differently; kept the one the declaration at /components/schemas/M/allOf/0/properties/f gives over the one at " +
				"/components/schemas/Base/properties/f, which stays declared on its model"}},
	} {
		entries := make([]string, len(tc.own))
		for i, own := range tc.own {
			entries[i] = "{properties: {f: {" + own + "}}}"
		}
		doc, diags := parseVerified(t, openapitest.ComponentSpec("    Base: {properties: {f: {"+base+"}}}\n    Other: {properties: {f: {type: integer}}}\n"+
			"    M: {allOf: ["+strings.Join(entries, ", ")+", {$ref: '#/components/schemas/Base'}"+tc.rest+"]}\n"))
		assert.Equal(t, tc.want, memberText(t, doc, componentID("M")), tc.name)
		assert.Equal(t, held+"Base/properties/f enum a b c", memberText(t, doc, componentID("Base")), tc.name)
		reports := foldReports(diags)
		require.Len(t, reports, len(tc.reports), "%s: %v", tc.name, reports)
		for i, report := range tc.reports {
			assert.Contains(t, reports[i], report, tc.name)
		}
	}
}

// TestAllOfFold_ADeprecationIsEachMembersOwn pins that a deprecation a fold
// takes is the folded member's own copy: a message naming a member beside it is
// read into each model's by that model's members, whichever kind the member's
// type is and whichever model sorts first, so a base two models compose holds
// its own and each of theirs names its own.
func TestAllOfFold_ADeprecationIsEachMembersOwn(t *testing.T) {
	t.Parallel()
	const reason = ", deprecated: true, x-deprecated-reason: 'Use `g` instead.'"
	for _, kind := range []string{"type: array, items: {type: string}", "type: object, additionalProperties: {type: string}", "type: string, enum: [a, b]",
		"type: object, properties: {p: {type: string}}", "anyOf: [{type: string}, {type: integer}]", "type: string"} {
		for _, names := range [][2]string{{"Cat", "Dog"}, {"Dog", "Cat"}, {"Aa", "Zz"}} {
			src := "    Base: {properties: {f: {" + kind + reason + "}, g: {type: array, items: {type: string}}}}\n"
			for i, name := range names {
				src += "    " + name + ": {allOf: [{$ref: '#/components/schemas/Base'}, {properties: {f: {" + kind +
					fmt.Sprintf("}, g: {type: array, items: {type: string}, maxItems: %d}}}]}\n", 5+i)
			}
			doc, _ := parseVerified(t, openapitest.ComponentSpec(src))
			for _, name := range []string{"Base", names[0], names[1]} {
				model, isModel := doc.Types[componentID(name)].(*ir.Model)
				require.True(t, isModel, name)
				sibling, ok := propertyByWire(model, "g")
				require.True(t, ok, name)
				deprecation := memberF(t, doc, componentID(name)).Deprecation
				require.NotNil(t, deprecation, "%s: %s", kind, name)
				assert.Equal(t, []ir.EntityRef{{Property: sibling.ID}}, deprecation.ReplacedBy, "%s, %v: %s", kind, names, name)
			}
		}
	}

	// An object's members an inline composition declares twice: the first
	// object's names its own sibling, not the composed model's.
	doc, _ := parseVerified(t, openapitest.ComponentSpec("    M: {allOf: ["+
		"{properties: {f: {type: object, properties: {f: {type: array, items: {type: string}"+reason+"}, g: {type: array, items: {type: string}}}}}}, "+
		"{properties: {f: {type: object, properties: {f: {type: array, items: {type: string}}, g: {type: array, items: {type: string}, maxItems: 5}}}}}]}\n"))
	const first = "components/schemas/M/allOf/0/properties/f"
	deprecation := memberF(t, doc, "type/anonymous/"+first).Deprecation
	require.NotNil(t, deprecation)
	assert.Equal(t, []ir.EntityRef{{Property: "property/openapi/" + first + "/properties/g"}}, deprecation.ReplacedBy)
}

// TestAllOfFold_ABuiltNodesDeprecationIsItsOwn pins the node a fold builds of a
// deprecation one declaration's node holds and a replacement key another's
// does: the built node resolves the key it keeps, and the node it took the
// deprecation from its own.
func TestAllOfFold_ABuiltNodesDeprecationIsItsOwn(t *testing.T) {
	t.Parallel()
	src := openapitest.ComponentSpec("    New: {type: string}\n    Newer: {type: string}\n    M: {allOf: [" +
		"{properties: {f: {type: string, enum: [a, b], deprecated: true, x-replaced-by: New}}}, " +
		"{properties: {f: {type: string, enum: [a, c], x-replaced-by: Newer}}}, " +
		"{properties: {f: {type: string, enum: [a, d]}}}]}\n")
	doc, _ := compileVerified(t, openapitest.SourceOf(src), openapi.Options{Promotions: openapi.ExtensionPromotions{
		Targets: map[openapi.ExtensionTarget][]openapi.ExtensionSource{openapi.TargetDeprecationReplacedBy: {{Key: "x-replaced-by"}}},
	}})

	built := doc.Types[memberF(t, doc, componentID("M")).Type.Target].Common()
	require.NotNil(t, built.Deprecation)
	assert.Equal(t, []ir.EntityRef{{Type: componentID("Newer")}}, built.Deprecation.ReplacedBy)
	written := doc.Types["type/anonymous/components/schemas/M/allOf/0/properties/f"].Common()
	require.NotNil(t, written.Deprecation)
	assert.Equal(t, []ir.EntityRef{{Type: componentID("New")}}, written.Deprecation.ReplacedBy)
}

// TestAllOfFold_AFieldHoldsNoBroughtNoteOverItsModelsOwn pins a member whose
// model's own declaration keeps its note on the node it builds, as one the
// model declares again does, one a reference names and one naming a $ref,
// beside one another model brings: a reader of the member, its own note first
// and then its type's, reads the model's own, which the type holds, or the
// field where the type is the brought one or a node that holds neither. The
// brought note stays on its model and each tie is reported.
func TestAllOfFold_AFieldHoldsNoBroughtNoteOverItsModelsOwn(t *testing.T) {
	t.Parallel()
	const list, narrow = "type: array, items: {type: string}", "type: array, items: {type: string, enum: [a, b]}"
	const entry = "{$ref: '#/components/schemas/A'}"
	for _, note := range againNotes {
		own := func(v string) string { return "{properties: {f: {" + list + ", " + fmt.Sprintf(note.write, v) + "}}}" }
		typed := func(v string) string { return "[" + fmt.Sprintf(note.typed, v) + "]" }
		plain := "{properties: {f: {" + list + "}}}"
		noted := note.name + " "
		// Two deprecations keyed apart meet in no node, so the brought one is the type.
		adopted := "composed/M/allOf/1/properties/f list " + typed("o0")
		if strings.HasPrefix(note.name, "deprecation") {
			adopted = typed("o0") + " A/properties/f list"
		}
		for _, tc := range []struct {
			name, brought, entries, rest string
			want                         string
			reports                      int // of the note
		}{
			{"the later own node is the type", list, own("o0") + ", " + entry + ", " + own("o1"), "", "M/allOf/2/properties/f list " + typed("o1"), 1},
			{"the brought node is the type", list, own("o0") + ", " + own("o1") + ", " + entry, "", typed("o1") + " A/properties/f list", 2},
			{"the own ones agree", list, own("o0") + ", " + own("o0") + ", " + entry, "", "M/allOf/0/properties/f list " + typed("o0"), 1},
			{"a node built of the brought one adopts it", narrow, plain + ", " + own("o0") + ", " + entry, "", adopted, 1},
			{"it says the same", narrow, plain + ", " + own("b") + ", " + entry, "", "composed/M/allOf/1/properties/f list " + typed("b"), 0},
			{"the brought node is the type, beside a dropped one, and it says the same", narrow,
				own("b") + ", {properties: {f: {type: integer}}}, " + entry, "", typed("b") + " A/properties/f list", 0},
			{"a reference names the own one", list, entry + ", " + own("o0"),
				"    U: {properties: {h: {$ref: '#/components/schemas/M/allOf/1/properties/f'}}}\n", "M/allOf/1/properties/f list " + typed("o0"), 1},
			{"a reference names the own one, dropped", list, own("o0") + ", " + entry,
				"    U: {properties: {h: {$ref: '#/components/schemas/M/allOf/0/properties/f'}}}\n", typed("o0") + " A/properties/f list", 1},
		} {
			src := openapitest.ComponentSpec("    A: {properties: {f: {" + tc.brought + ", " + fmt.Sprintf(note.write, "b") + "}}}\n" +
				"    M: {allOf: [" + tc.entries + "]}\n" + tc.rest)
			doc, diags := parseVerified(t, src)
			got, _, _ := strings.Cut(memberText(t, doc, componentID("M")), " | verbatim")
			assert.Equal(t, tc.want, got, noted+tc.name)
			assert.Contains(t, memberText(t, doc, componentID("A")), typed("b"), noted+tc.name)
			ties := slices.DeleteFunc(foldReports(diags), func(report string) bool { return !strings.Contains(report, " differently; ") })
			assert.Len(t, ties, tc.reports, "%s: %v", noted+tc.name, ties)
		}
	}
}

// TestAllOfFold_ANoteOnABroughtNodeIsNotItsModelsOwn pins that a brought
// declaration's node keeping its note, as a reference names it, is none of the
// model's own: the field holds the note another brought one carries, in either
// order, or, where the model's own node keeps one its type is not read holding,
// that one.
func TestAllOfFold_ANoteOnABroughtNodeIsNotItsModelsOwn(t *testing.T) {
	t.Parallel()
	const list = "type: array, items: {type: string}"
	for _, entries := range []string{"{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}", "{$ref: '#/components/schemas/B'}, {$ref: '#/components/schemas/A'}"} {
		doc, _ := parseVerified(t, openapitest.ComponentSpec("    A: {properties: {f: {"+list+", description: of A}}}\n    B: {properties: {f: {"+list+", description: of B}}}\n"+
			"    M: {allOf: ["+entries+"], properties: {f: {"+list+"}}}\n    U: {properties: {h: {$ref: '#/components/schemas/A/properties/f'}}}\n"))
		field := memberF(t, doc, componentID("M"))
		assert.Equal(t, "of B", field.Docs.Description, entries)
		assert.Equal(t, "of A", doc.Types["type/anonymous/components/schemas/A/properties/f"].Common().Docs.Description, entries)
	}

	doc, _ := parseVerified(t, openapitest.ComponentSpec("    A: {properties: {f: {type: array, items: {type: string, enum: [a, b]}, description: of A}}}\n"+
		"    B: {properties: {f: {"+list+", description: of B}}}\n"+
		"    M: {allOf: [{properties: {f: {"+list+", description: own}}}, {$ref: '#/components/schemas/B'}, {$ref: '#/components/schemas/A'}]}\n"+
		"    U: {properties: {h: {$ref: '#/components/schemas/A/properties/f'}, i: {$ref: '#/components/schemas/M/allOf/0/properties/f'}}}\n"))
	field := memberF(t, doc, componentID("M"))
	assert.Equal(t, ir.TypeID("type/anonymous/components/schemas/A/properties/f"), field.Type.Target)
	assert.Equal(t, "own", field.Docs.Description)
}

// TestAllOfFold_ACompositionHoldingItsModelsOwnNoteLeavesTheBroughtOne pins the
// same where the types compose: the own declaration's node is a part, or is
// one a $ref it names reaches through, so its note is read there and the field
// holds no brought one.
func TestAllOfFold_ACompositionHoldingItsModelsOwnNoteLeavesTheBroughtOne(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, own, want string }{
		{"declared again", "{properties: {f: {type: object, properties: {p: {type: string}}, description: o0}}}, {properties: {f: {type: object}}}",
			"composed/M/allOf/1/properties/f model of (M/allOf/0/properties/f model [description o0]; A/properties/f model)"},
		{"naming a $ref", "{properties: {f: {allOf: [{$ref: '#/components/schemas/P'}], description: o0}}}",
			"composed/M/allOf/0/properties/f model of (M/allOf/0/properties/f model [description o0]; A/properties/f model)"},
	} {
		doc, diags := parseVerified(t, openapitest.ComponentSpec("    P: {type: object, properties: {p: {type: string}}}\n"+
			"    A: {properties: {f: {type: object, properties: {q: {type: string}}, title: Ta, description: b}}}\n"+
			"    M: {allOf: ["+tc.own+", {$ref: '#/components/schemas/A'}]}\n"))
		assert.Equal(t, "[title Ta] "+tc.want, memberText(t, doc, componentID("M")), tc.name)
		reports := foldReports(diags)
		require.Len(t, reports, 1, "%s: %v", tc.name, reports)
		assert.Contains(t, reports[0], "give its description differently; kept the one the declaration at /components/schemas/M/allOf/0/properties/f gives", tc.name)
	}
}

// TestAllOfFold_AFieldHoldsNoBroughtExampleOverItsTypesOwn pins the examples of
// such a member: the field holds no brought example where its type is read
// holding the model's own, and holds one the type admits where the type is the
// brought one's, whose node the model's own examples, kept verbatim, never
// stood on.
func TestAllOfFold_AFieldHoldsNoBroughtExampleOverItsTypesOwn(t *testing.T) {
	t.Parallel()
	const held = `[example {"kind":"string","string":"%s"}]`
	const narrow, wide = "type: string, enum: [a, b]", "type: string, enum: [a, b, c]"
	own := func(schema, example string) string {
		return "{properties: {f: {" + schema + ", examples: [" + example + "]}}}"
	}
	for _, tc := range []struct{ name, entries, want, report string }{
		{"the own node is the type", own(narrow, "a") + ", {properties: {f: {" + wide + "}}}", "M/allOf/0/properties/f enum a b " + fmt.Sprintf(held, "a"),
			"give its examples differently; kept the one the declaration at /components/schemas/M/allOf/0/properties/f gives"},
		{"the brought node is the type", own(wide, "a") + ", " + own(wide, "c"), fmt.Sprintf(held, "b") + " A/properties/f enum a b c",
			"whose values overlap but that the merge does not narrow to one; kept the type the declaration at /components/schemas/A/properties/f gives"},
	} {
		doc, diags := parseVerified(t, openapitest.ComponentSpec("    A: {properties: {f: {"+wide+", examples: [b]}}}\n"+
			"    M: {allOf: ["+tc.entries+", {$ref: '#/components/schemas/A'}]}\n"))
		got, _, _ := strings.Cut(memberText(t, doc, componentID("M")), " | verbatim")
		assert.Equal(t, tc.want, got, tc.name)
		reports := foldReports(diags)
		require.NotEmpty(t, reports, tc.name)
		assert.Contains(t, reports[0], tc.report, tc.name)
	}
}
