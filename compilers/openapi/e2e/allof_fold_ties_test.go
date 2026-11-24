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

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestAllOfFold_ATieOfBroughtNotesFollowsTheTypeNotTheOrder pins which of two
// notes other models' declarations bring the field holds, none of the model's
// own giving it: the one of a declaration building no node, in either order,
// and otherwise the one of the declaration whose node the field's type is
// built on, judged with each note on its node, the later where they tie there
// too. Where the type is of neither, the field holds neither. Each is reported.
func TestAllOfFold_ATieOfBroughtNotesFollowsTheTypeNotTheOrder(t *testing.T) {
	t.Parallel()
	const str, enum, wide = "type: string", "type: string, enum: [a, b]", "type: string, enum: [a, b, c]"
	const list, object = "type: array, items: {type: string}", "type: object, properties: {%s: {type: string}}"
	const a, b = "{$ref: '#/components/schemas/A'}", "{$ref: '#/components/schemas/B'}"
	own := func(schema string) string { return "{properties: {f: {" + schema + "}}}" }
	for _, note := range againNotes[:3] {
		for _, tc := range []struct {
			name, a, b, own, rest string
			want                  [2]string // of the entries in order, and reversed: %s a note's value
		}{
			{"one builds no node", str, enum, "", "", [2]string{"a B/properties/f", "a B/properties/f"}},
			{"one builds no node, untyped", "", list, "", "", [2]string{"a B/properties/f", "a B/properties/f"}},
			{"one builds no node, beside an own one", str, enum, own(str), "", [2]string{"a B/properties/f", "a B/properties/f"}},
			{"neither builds a node", str, str, "", "", [2]string{"b primitive/string", "a primitive/string"}},
			{"the types tie", wide, enum, "", "", [2]string{"b B/properties/f", "a B/properties/f"}},
			{"the types tie, a reference naming one", wide, enum, "", "    U: {properties: {h: {$ref: '#/components/schemas/A/properties/f'}}}\n",
				[2]string{"b composed/M/allOf/properties/f", "- composed/M/allOf/properties/f"}},
			{"one is of no type with the field's", list, "type: integer, enum: [1]", own(list + ", minItems: 1"), "",
				[2]string{"a M/allOf/0/properties/f", "a M/allOf/2/properties/f"}},
			{"they compose", fmt.Sprintf(object, "p"), fmt.Sprintf(object, "q"), "", "", [2]string{"none composed/M/allOf/properties/f", "none composed/M/allOf/properties/f"}},
			{"neither is of the field's type", list, list + ", minItems: 1", own("type: integer") + ", {$ref: '#/components/schemas/I'}", "    I: {properties: {f: {type: integer}}}\n",
				[2]string{"none primitive/integer", "none primitive/integer"}},
		} {
			noted := func(schema, v string) string { return strings.TrimPrefix(schema+", "+fmt.Sprintf(note.write, v), ", ") }
			for order, entries := range []string{tc.own + ", " + a + ", " + b, b + ", " + a + ", " + tc.own} {
				label := fmt.Sprintf("%s, %s, order %d", note.name, tc.name, order)
				doc, diags := parseVerified(t, openapitest.ComponentSpec("    A: {properties: {f: {"+noted(tc.a, "a")+"}}}\n    B: {properties: {f: {"+noted(tc.b, "b")+"}}}\n"+
					"    M: {allOf: ["+strings.Trim(entries, ", ")+"]}\n"+tc.rest))
				want, target, _ := strings.Cut(tc.want[order], " ")
				field := memberF(t, doc, componentID("M"))
				got := strings.ReplaceAll(strings.TrimPrefix(strings.TrimPrefix(string(field.Type.Target), "type/anonymous/"), "type/"), "components/schemas/", "")
				assert.Equal(t, target, got, label)
				held, reported := "", " differently; kept the one "
				switch want {
				case "none":
					reported = " differently, and the type it takes is of none of them alone; kept none, and the one the declaration at "
				case "-":
				default:
					held = fmt.Sprintf(note.typed, want)
				}
				assert.Equal(t, held, notesText(t, field.Docs, field.Deprecation, nil), label)
				reports := slices.DeleteFunc(foldReports(diags), func(report string) bool { return !strings.Contains(report, reported) })
				assert.Len(t, reports, map[bool]int{true: 2, false: 1}[want == "none"], "%s: %v", label, reports)
			}
		}
	}

	for order, entries := range []string{a + ", " + b, b + ", " + a} {
		doc, _ := parseVerified(t, openapitest.ComponentSpec("    A: {properties: {f: {type: string, deprecated: true, x-deprecated-reason: a}}}\n"+
			"    B: {properties: {f: {type: string, enum: [a, b], deprecated: true, x-deprecated-reason: b}}}\n    M: {allOf: ["+entries+"]}\n"))
		assert.Equal(t, "a", memberF(t, doc, componentID("M")).Deprecation.Message, "a deprecation, order %d", order)
	}
}

// TestAllOfFold_ASettledMembersNotesAreJudgedOnItsNode pins a member a composed
// model settled, whose node held its note while a node held any: it is judged
// with the note there, whether the fold built the node or names another
// model's, so the composing model's own type and notes are the field's, as two
// lists two notes tell apart tie. A note of a type the settled member dropped
// is on no node.
func TestAllOfFold_ASettledMembersNotesAreJudgedOnItsNode(t *testing.T) {
	t.Parallel()
	const list, anyList = "type: array, items: {type: string}, maxItems: 9", "type: array, items: {}"
	entry := func(schema string) string { return "{properties: {f: {" + schema + "}}}" }
	const own = "M/properties/f list [title T, description D, link https://e.test/3, deprecated]"
	for _, tc := range []struct{ name, a, b, want string }{
		{"on a node the fold built", entry(list), "{allOf: [{$ref: '#/components/schemas/A'}, " + entry(anyList+", x-foo: 1, externalDocs: {url: 'https://e.test/2'}") + "]}", own},
		{"on another model's node", entry(list), "{allOf: [{$ref: '#/components/schemas/A'}, " + entry(anyList+", externalDocs: {url: 'https://e.test/2'}") + "]}", own},
		{"of a type it dropped", entry(list), "{allOf: [" + entry("type: string, enum: [a], externalDocs: {url: 'https://e.test/2'}") + ", {$ref: '#/components/schemas/A'}]}",
			"[title T, description D, deprecated] composed/M/properties/f list [link https://e.test/7]"},
	} {
		doc, _ := parseVerified(t, openapitest.ComponentSpec("    A: "+tc.a+"\n    B: "+tc.b+"\n"+
			"    M: {allOf: [{properties: {f: {"+anyList+", externalDocs: {url: 'https://e.test/7'}}}}, {$ref: '#/components/schemas/B'}], "+
			"properties: {f: {"+list+", title: T, description: D, deprecated: true, externalDocs: {url: 'https://e.test/3'}}}}\n"))
		got, _, _ := strings.Cut(memberText(t, doc, componentID("M")), " | verbatim")
		assert.Equal(t, tc.want, got, tc.name)
	}
}

// TestAllOfFold_ANoteAMemberSettledKeepsItsHome pins what a composing model
// reads of a note a composed model's member settled: the member's own where
// some declaration of it gives one off no node, whichever it kept, so beside
// another such note the later entry's is the field's; a node's otherwise, so
// the other is, in either order.
func TestAllOfFold_ANoteAMemberSettledKeepsItsHome(t *testing.T) {
	t.Parallel()
	const enum = "{properties: {f: {type: string, enum: [a, b], description: of C}}}"
	const a, b = "{$ref: '#/components/schemas/A'}", "{$ref: '#/components/schemas/B'}"
	for _, tc := range []struct {
		name, base string
		want       [2]string
	}{
		{"one of its declarations builds no node", "type: string, description: of Base", [2]string{"of B", "of C"}},
		{"none but its enum's", "type: string", [2]string{"of B", "of B"}},
	} {
		for order, entries := range []string{a + ", " + b, b + ", " + a} {
			doc, _ := parseVerified(t, openapitest.ComponentSpec("    Base: {properties: {f: {"+tc.base+"}}}\n"+
				"    A: {allOf: [{$ref: '#/components/schemas/Base'}, "+enum+"]}\n    B: {properties: {f: {type: string, description: of B}}}\n"+
				"    M: {allOf: ["+entries+"]}\n"))
			assert.Equal(t, "of C", memberF(t, doc, componentID("A")).Docs.Description, tc.name)
			field := memberF(t, doc, componentID("M"))
			assert.Equal(t, tc.want[order], field.Docs.Description, "%s, order %d", tc.name, order)
			assert.Equal(t, ir.TypeID("type/anonymous/components/schemas/A/allOf/1/properties/f"), field.Type.Target, "%s, order %d", tc.name, order)
		}
	}
}

// TestAllOfFold_TwoSettledMembersOfOneNodeAreReadInOneWay pins two members two
// composed models settled on one node of a third, each with a note of its own:
// the node is read holding that of the one written first, so which of the
// composing model's own declarations it drops, each with a note of its own,
// does not follow the order of the entries.
func TestAllOfFold_TwoSettledMembersOfOneNodeAreReadInOneWay(t *testing.T) {
	t.Parallel()
	const list = "type: array, items: {type: string}, maxItems: 9"
	settled := func(url string) string {
		return "{allOf: [{$ref: '#/components/schemas/A'}, {properties: {f: {type: array, items: {}, externalDocs: {url: 'https://e.test/" + url + "'}}}}]}"
	}
	for _, entries := range []string{"{$ref: '#/components/schemas/B'}, {$ref: '#/components/schemas/C'}", "{$ref: '#/components/schemas/C'}, {$ref: '#/components/schemas/B'}"} {
		doc, _ := parseVerified(t, openapitest.ComponentSpec("    A: {properties: {f: {"+list+"}}}\n    B: "+settled("b")+"\n    C: "+settled("c")+"\n"+
			"    M: {allOf: ["+entries+", {properties: {f: {type: array, items: {}, externalDocs: {url: 'https://e.test/b'}}}}], properties: {f: {"+list+
			", title: T, externalDocs: {url: 'https://e.test/3'}}}}\n"))
		assert.Equal(t, "[title T, link https://e.test/3] A/properties/f list | verbatim M/properties/f", memberText(t, doc, componentID("M")), entries)
	}
}

// TestAllOfFold_AFieldNamesNoHeuristicItsNodesKindWasChosenBy pins the
// heuristics a field names whose deprecation is one a node of the model's own
// holds: the promotion's, where it read one there, and none where the keyword
// alone deprecates it, though a policy chose the node's kind.
func TestAllOfFold_AFieldNamesNoHeuristicItsNodesKindWasChosenBy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, note string
		want       ir.Inferred
	}{
		{"by keyword", "deprecated: true", ""},
		{"by keyword, a key giving its message", "deprecated: true, x-deprecated-reason: own", "extension-promotion"},
	} {
		doc, _ := parseVerified(t, openapitest.ComponentSpec("    A: {properties: {f: {type: string, enum: [a, b], deprecated: true, x-deprecated-reason: brought}}}\n"+
			"    M: {allOf: [{properties: {f: {properties: {p: {type: string}}, "+tc.note+"}}}, {properties: {f: {properties: {q: {type: string}}}}}, {$ref: '#/components/schemas/A'}]}\n"))
		node := doc.Types["type/anonymous/components/schemas/M/allOf/0/properties/f"].Common()
		assert.Contains(t, string(node.Provenance.Inferred), "kind-inference", tc.name)
		field := memberF(t, doc, componentID("M"))
		require.NotNil(t, field.Deprecation, tc.name)
		assert.Equal(t, node.Deprecation, field.Deprecation, tc.name)
		assert.Equal(t, tc.want, field.Provenance.Inferred, tc.name)
	}
}
