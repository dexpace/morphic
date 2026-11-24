// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestAllOfFold_ANoteAMemberSettledDroppedWithItsTypeIsNoTypesNote pins a tie
// between a note one model brings and one a member another model settled took
// off a declaration whose type it dropped: the node of that member was never
// read holding it, so the note is the one of the node the field's type is built
// on, in either order of the entries, whatever the dropped type is.
func TestAllOfFold_ANoteAMemberSettledDroppedWithItsTypeIsNoTypesNote(t *testing.T) {
	t.Parallel()
	const list, dict = "type: array, items: {type: string}", "type: object, additionalProperties: {type: string}"
	const wide, narrow = "type: string, enum: [a, b]", "type: string, enum: [a]"
	for _, note := range againNotes {
		for _, tc := range []struct{ name, brought, dropped, kept string }{
			{"a union", wide, "oneOf: [{type: string}, {type: integer}]", narrow},
			{"a format not held", "type: string, enum: [a, b, c]", "type: string, format: email", wide},
			{"bytes", wide, "type: string, format: byte", narrow},
			{"items alone", list, "items: {}", list + ", minItems: 1"},
			{"a bound alone", list, "minItems: 0", list + ", minItems: 1"},
			{"additionalProperties alone", dict, "additionalProperties: {}", dict + ", minProperties: 1"},
			{"required alone", dict, "required: [k]", dict + ", minProperties: 1"},
		} {
			noted := func(schema, v string) string {
				return "{properties: {f: {" + schema + ", " + fmt.Sprintf(note.write, v) + "}}}"
			}
			for order, entries := range []string{"{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}", "{$ref: '#/components/schemas/B'}, {$ref: '#/components/schemas/A'}"} {
				doc, _ := parseVerified(t, openapitest.ComponentSpec("    A: "+noted(tc.brought, "a")+"\n    Base: "+noted(tc.dropped, "b")+"\n"+
					"    B: {allOf: [{$ref: '#/components/schemas/Base'}, {properties: {f: {"+tc.kept+"}}}]}\n    M: {allOf: ["+entries+"]}\n"))
				field := memberF(t, doc, componentID("M"))
				assert.Equal(t, fmt.Sprintf(note.typed, "a"), notesText(t, field.Docs, field.Deprecation, nil), "%s, %s, order %d", note.name, tc.name, order)
				assert.Nil(t, field.Unmodeled, "%s, %s, order %d", note.name, tc.name, order)
			}
		}
	}
}

// TestAllOfFold_AReferenceTakesNoNoteOffASettledMember pins a note of a middle
// model's own that its member leaves on the node of its type, as a reference
// names the position, over one another model's declaration builds no node for:
// it is the member's own there as it is without the reference, so a model
// composing it reads it, and so does one composing that, whatever node the
// field names.
func TestAllOfFold_AReferenceTakesNoNoteOffASettledMember(t *testing.T) {
	t.Parallel()
	const list = "type: array, items: {type: string}"
	const links, text, key = "externalDocs: {url: 'https://e.test/%s'}", "description: %s", "x-deprecated-reason: %s"
	for i, note := range againNotes {
		noted := func(v string) string { return fmt.Sprintf(note.write, v) }
		// What another declaration says that no node holds beside the note.
		apart := func(v string) string { return fmt.Sprintf([]string{links, links, text, key, key}[i], v) }
		var without string
		for _, rest := range []string{"", "    U: {properties: {h: {$ref: '#/components/schemas/A/allOf/1/properties/f'}}}\n"} {
			doc, _ := parseVerified(t, openapitest.ComponentSpec("    Base: {properties: {f: {"+noted("base")+"}}}\n"+
				"    A: {allOf: [{$ref: '#/components/schemas/Base'}, {properties: {f: {"+list+", "+noted("middle")+"}}}]}\n"+
				"    M: {allOf: [{$ref: '#/components/schemas/A'}, {properties: {f: {"+list+", maxItems: 1, "+apart("m")+"}}}, {properties: {f: {"+list+"}}}]}\n"+
				"    N: {allOf: [{$ref: '#/components/schemas/M'}, {properties: {f: {"+list+", minItems: 1, "+apart("n")+"}}}, {properties: {f: {"+list+"}}}]}\n"+rest))
			for _, name := range []string{"M", "N"} {
				field := memberF(t, doc, componentID(name))
				require.Contains(t, notesText(t, field.Docs, field.Deprecation, nil), fmt.Sprintf(note.typed, "middle"), "%s, %s: %s", note.name, name, rest)
			}
			inferred := string(memberF(t, doc, componentID("M")).Provenance.Inferred)
			if rest == "" {
				without = inferred
			}
			assert.Equal(t, without, inferred, "%s: the field names what promoted the note", note.name)
			assert.Equal(t, strings.HasPrefix(note.name, "deprecation"), inferred == "extension-promotion", note.name)
		}
	}
}

// TestAllOfFold_AFieldIsNoWiderThanASettledMembersNotesLeftIt pins a member
// judged beside one its own model settled, narrower than its notes gave it
// before: read as the type they gave, the settled member costs the field no
// bound another brought declaration holds, and costs the model's own no type.
func TestAllOfFold_AFieldIsNoWiderThanASettledMembersNotesLeftIt(t *testing.T) {
	t.Parallel()
	const dict, a, b = "type: object, additionalProperties: {type: string}", "{$ref: '#/components/schemas/A'}", "{$ref: '#/components/schemas/B'}"
	own := "{properties: {f: {" + dict + ", x-key: 2, externalDocs: {url: 'https://e.test/m'}}}}"
	entries := []string{a, b, own}
	for _, order := range entryOrders {
		doc, _ := parseVerified(t, openapitest.ComponentSpec("    Ax: {properties: {f: {"+dict+", minProperties: 1, description: x}}}\n"+
			"    A: {allOf: [{$ref: '#/components/schemas/Ax'}, {properties: {f: {"+dict+", title: T}}}]}\n"+
			"    B: {properties: {f: {"+dict+", minProperties: 1, x-key: 1}}}\n"+
			"    M: {allOf: ["+entries[order[0]]+", "+entries[order[1]]+", "+entries[order[2]]+"]}\n"))
		field := memberF(t, doc, componentID("M"))
		node, isMap := doc.Types[field.Type.Target].(*ir.MapT)
		require.True(t, isMap, "order %v", order)
		require.NotNil(t, node.Constraints, "order %v: the bound A and B hold", order)
		assert.Equal(t, new(int64(1)), node.Constraints.MinProperties, "order %v", order)
		assert.Equal(t, "title T, description x, link https://e.test/m", notesText(t, field.Docs, field.Deprecation, nil), "order %v", order)
	}

	ownEnum := "{properties: {f: {type: string, enum: [b]}}}"
	for order, entries := range []string{b + ", " + ownEnum, ownEnum + ", " + b} {
		doc, diags := parseVerified(t, openapitest.ComponentSpec("    A: {allOf: [{properties: {f: {}}}], properties: {f: {type: string, enum: [b], example: b}}}\n"+
			"    B: {allOf: ["+a+", {properties: {f: {type: string, enum: [b, c], externalDocs: {url: 'https://e.test/b'}}}}]}\n    M: {allOf: ["+entries+"]}\n"))
		assert.Equal(t, fmt.Sprintf("[link https://e.test/b] M/allOf/%d/properties/f enum b", 1-order), memberText(t, doc, componentID("M")), "order %d", order)
		assert.Empty(t, foldReports(diags), "order %d: nothing is dropped", order)
	}
}

// TestAllOfFold_ASettledMembersTypeDropsNoneInAnyOrder pins a model declaring a
// member again beside one its own model settled: where the types as they stand
// drop none, none is dropped for what a node is read holding, so the field
// holds each bound, and reads the one link, in every order of the entries.
func TestAllOfFold_ASettledMembersTypeDropsNoneInAnyOrder(t *testing.T) {
	t.Parallel()
	const list = "type: array, items: {type: string}"
	link := func(url string) string { return ", externalDocs: {url: 'https://e.test/" + url + "'}" }
	for _, tc := range []struct{ name, again, rest string }{
		{"beside one saying nothing", "", ""},
		{"beside one naming a $ref", "allOf: [{$ref: '#/components/schemas/Target'}], title: T", "    Target: {" + list + "}\n"},
	} {
		entries := []string{"{properties: {f: {" + tc.again + "}}}", "{$ref: '#/components/schemas/A'}", "{$ref: '#/components/schemas/B'}"}
		for _, order := range entryOrders {
			for _, version := range []string{"3.1.0", "3.0.3"} {
				doc, diags := parseVerified(t, openapitest.ComponentSpecVer(version, "    Ax: {properties: {f: {"+list+", minItems: 1"+link("a")+"}}}\n"+
					"    A: {allOf: [{$ref: '#/components/schemas/Ax'}, {properties: {f: {"+list+link("b")+"}}}]}\n"+
					"    B: {properties: {f: {"+list+", maxItems: 1"+link("c")+"}}}\n"+tc.rest+
					"    M: {allOf: ["+entries[order[0]]+", "+entries[order[1]]+", "+entries[order[2]]+"], properties: {f: {"+list+"}}}\n"))
				field := memberF(t, doc, componentID("M"))
				label := fmt.Sprintf("%s, order %v, %s", tc.name, order, version)
				node, isList := doc.Types[field.Type.Target].(*ir.List)
				require.True(t, isList, label)
				require.NotNil(t, node.Constraints, label)
				assert.Equal(t, [2]*int64{new(int64(1)), new(int64(1))}, [2]*int64{node.Constraints.MinItems, node.Constraints.MaxItems}, label)
				assert.Nil(t, field.Unmodeled, label)
				require.Len(t, field.Docs.ExternalDocs, 1, label)
				assert.Equal(t, "https://e.test/c", field.Docs.ExternalDocs[0].URL, label)
				for _, report := range foldReports(diags) {
					assert.NotContains(t, report, "M", label+": a type of M's is dropped")
				}
			}
		}
	}
}
