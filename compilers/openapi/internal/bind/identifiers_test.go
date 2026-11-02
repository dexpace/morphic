// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind_test

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// recordingDocuments is withDocuments that notes each document read, in order.
func recordingDocuments(t *testing.T, docs map[string]string) (bind.Config, *[]string) {
	t.Helper()
	cfg := withDocuments(docs)
	var reads []string
	cfg.Open = func(name string) (*yaml.Node, error) {
		reads = append(reads, name)
		src, ok := docs[name]
		if !ok {
			return nil, errors.New("no such document")
		}
		return openapitest.YAMLDocument(t, src), nil
	}
	return cfg, &reads
}

// docNamed returns the ID the table gives the document named name.
func docNamed(t *testing.T, table *bind.Table, name string) bind.DocID {
	t.Helper()
	for id := range bind.DocID(table.Documents()) {
		if table.Document(id).Name == name {
			return id
		}
	}
	require.Failf(t, "no such document", "%s", name)
	return bind.Source
}

// TestBind_ReadsWhatAnotherDocumentDeclares pins each identifier of another
// document, read by what places its schemas: a document naming an OpenAPI version
// by its grammar, and any other as a document of schemas. A plain name answers
// in the resource it is declared in, so one under an $id is that $id's; a pointer
// and a name below an $id read against it.
func TestBind_ReadsWhatAnotherDocumentDeclares(t *testing.T) {
	t.Parallel()
	const pet = "Pet:\n  $id: 'https://x.test/pet'\n  $defs:\n    Tag: {$anchor: tag, type: string}\n" +
		"  properties:\n    a: {$ref: '#/$defs/Tag'}\n    b: {$ref: '#tag'}\n"
	tests := []struct {
		name, ref, other string
		want             jsontext.Pointer
		why              bind.Reason
	}{
		{"a plain name of a document of schemas", "o.yaml#t", "S: {$anchor: t, type: string}\n", "/S", 0},
		{"a plain name at a document's root", "o.yaml#t", "{$anchor: t, type: string}\n", "", 0},
		{"a name in $defs", "o.yaml#t", "$defs: {S: {$anchor: t}}\n", "/$defs/S", 0},
		{"a $dynamicAnchor", "o.yaml#t", "S: {$dynamicAnchor: t}\n", "/S", 0},
		{"a plain name of an OpenAPI document", "o.yaml#t",
			head + "components:\n  schemas:\n    S: {$anchor: t}\n", "/components/schemas/S", 0},
		{"none in an OpenAPI document's example", "o.yaml#t",
			head + "components:\n  schemas:\n    S: {example: {$anchor: t}}\n", "", bind.NoAnchor},
		{"none in an OpenAPI document's extension", "o.yaml#t", head + "x-e: {$anchor: t}\n", "", bind.NoAnchor},
		{"none in an OpenAPI document's object", "o.yaml#t", head + "tags: [{name: n, $anchor: t}]\n", "", bind.NoAnchor},
		{"none in a literal of a document of schemas", "o.yaml#t", "S: {const: {$anchor: t}}\n", "", bind.NoAnchor},
		{"none in an extension of a document of schemas", "o.yaml#t", "x-e: {$anchor: t}\n", "", bind.NoAnchor},
		{"none in a description", "o.yaml#t", "S: {description: {$anchor: t}}\n", "", bind.NoAnchor},
		{"none a property merely names", "o.yaml#t", "S: {properties: {$anchor: {type: string}}}\n", "", bind.NoAnchor},
		{"not two", "o.yaml#t", "A: {$anchor: t}\nB: {$anchor: t}\n", "", bind.DuplicateAnchor},
		{"none in another resource", "o.yaml#tag", pet, "", bind.NoAnchor},
		{"a URI another document declares", "https://x.test/pet", pet, "/Pet", 0},
		{"a name below that URI", "https://x.test/pet#tag", pet, "/Pet/$defs/Tag", 0},
		{"a pointer below that URI", "https://x.test/pet#/$defs/Tag", pet, "/Pet/$defs/Tag", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// The first $ref opens o.yaml, so a URI only it declares has a document
			// to be read from whichever way round the two are declared.
			for _, schemas := range []string{
				"    A: {$ref: '" + tc.ref + "'}\n    B: {$ref: 'o.yaml'}\n",
				"    A0: {$ref: 'o.yaml'}\n    A: {$ref: '" + tc.ref + "'}\n",
			} {
				cfg, _ := recordingDocuments(t, map[string]string{"o.yaml": tc.other})
				table := bound(t, head+"components:\n  schemas:\n"+schemas, cfg)
				e := entryAt(t, table, "/components/schemas/A")
				if tc.why != 0 {
					assert.Equal(t, tc.why, e.Hop.Reason, e.Hop.Why())
					continue
				}
				require.Equal(t, bind.Resolved, e.Hop.Status, e.Hop.Why())
				assert.Equal(t, bind.Position{Doc: docNamed(t, table, "o.yaml"), Pointer: tc.want}, e.Hop.Target)
			}
		})
	}
}

// TestBind_ReadsBelowAnIDOfAnotherDocument pins that an $id in another document
// gives its base to what is written below it, not only beside it: a $ref inside
// it reads its own resource.
func TestBind_ReadsBelowAnIDOfAnotherDocument(t *testing.T) {
	t.Parallel()
	const other = "Pet:\n  $id: 'https://x.test/pet'\n  $defs:\n    Tag: {$anchor: tag, type: string}\n" +
		"  properties:\n    a: {$ref: '#/$defs/Tag'}\n    b: {$ref: '#tag'}\n"
	table := bound(t, head+"components:\n  schemas:\n    Pet: {$ref: 'o.yaml#/Pet'}\n",
		withDocuments(map[string]string{"o.yaml": other}))
	doc := docNamed(t, table, "o.yaml")
	for _, site := range []jsontext.Pointer{"/Pet/properties/a", "/Pet/properties/b"} {
		e, ok := table.Entry(bind.Position{Doc: doc, Pointer: site})
		require.True(t, ok, site)
		require.Equal(t, bind.Resolved, e.Hop.Status, e.Hop.Why())
		assert.Equal(t, bind.Position{Doc: doc, Pointer: "/Pet/$defs/Tag"}, e.Hop.Target)
	}
}

// TestBind_AnIdentifierIsNoLocationToFetch pins that a URI a document declares
// answers as that identifier whichever reference comes first, and reaches no
// network: the read of a document on the network waits for the documents that
// could declare it. One no document declares is then read, once.
func TestBind_AnIdentifierIsNoLocationToFetch(t *testing.T) {
	t.Parallel()
	const declaring = "S: {$id: 'https://x.test/s', type: string}\n"
	for _, schemas := range []string{
		"    A: {$ref: 'https://x.test/s'}\n    Z: {$ref: 'o.yaml'}\n",
		"    A: {$ref: 'o.yaml'}\n    Z: {$ref: 'https://x.test/s'}\n",
	} {
		cfg, reads := recordingDocuments(t, map[string]string{"o.yaml": declaring})
		table := bound(t, head+"components:\n  schemas:\n"+schemas, cfg)
		assert.Equal(t, []string{"o.yaml"}, *reads, "an identifier is never read as a location")
		for _, e := range table.Entries() {
			assert.Equal(t, bind.Resolved, e.Hop.Status, e.Hop.Why())
		}
	}

	cfg, reads := recordingDocuments(t, map[string]string{"https://x.test/r": "S: {type: string}\n"})
	table := bound(t, head+"components:\n  schemas:\n    A: {$ref: 'https://x.test/r#/S'}\n", cfg)
	assert.Equal(t, []string{"https://x.test/r"}, *reads)
	assert.Equal(t, bind.Resolved, entryAt(t, table, "/components/schemas/A").Hop.Status)
}

// TestBind_ARefusedURIADocumentDeclaresIsAnIdentifier pins the same for a URI
// the refusal would answer first: it is judged only once no document declares it.
func TestBind_ARefusedURIADocumentDeclaresIsAnIdentifier(t *testing.T) {
	t.Parallel()
	cfg, _ := recordingDocuments(t, map[string]string{"o.yaml": "S: {$id: 'https://x.test/s', type: string}\n"})
	cfg.Refuse = func(uri string) error {
		if uri == "https://x.test/s" {
			return errors.New("refused")
		}
		return nil
	}
	table := bound(t, head+"components:\n  schemas:\n    A: {$ref: 'https://x.test/s'}\n    Z: {$ref: 'o.yaml'}\n", cfg)
	assert.Equal(t, bind.Resolved, entryAt(t, table, "/components/schemas/A").Hop.Status)
}

// TestBind_ALocationADocumentDeclaresIsThatIdentifier pins the pass that does not
// stand: a reference answered as a location, read before a document declaring
// that URI, is answered again as the identifier, and the document the first
// answer read holds nothing the table does, so it is marked unreached.
func TestBind_ALocationADocumentDeclaresIsThatIdentifier(t *testing.T) {
	t.Parallel()
	docs := map[string]string{
		"/a.yaml": "S: {type: integer}\n",
		"/z.yaml": "S: {$id: 'a.yaml', $defs: {T: {type: string}}}\n",
	}
	src := head + "components:\n  schemas:\n    A: {$ref: 'a.yaml#/$defs/T'}\n    Z: {$ref: 'z.yaml#/S'}\n"
	cfg := resolving
	cfg.Open = func(name string) (*yaml.Node, error) { return openapitest.YAMLDocument(t, docs[name]), nil }
	table := bound(t, src, cfg)

	e := entryAt(t, table, "/components/schemas/A")
	require.Equal(t, bind.Resolved, e.Hop.Status, e.Hop.Why())
	assert.Equal(t, bind.Position{Doc: docNamed(t, table, "/z.yaml"), Pointer: "/S/$defs/T"}, e.Hop.Target)
	assert.True(t, table.Document(docNamed(t, table, "/a.yaml")).Unreached)
	assert.False(t, table.Document(docNamed(t, table, "/z.yaml")).Unreached)
}

// TestBind_ADocumentNoPassReachesDeclaresNothing pins the document a pass read
// as a location before another declared that URI. Once no pass reaches it, what
// it declares answers for nothing, so a URI it and a reached document declare
// names the reached one's, and its merge past the bound is no verdict of the
// table, in either order.
func TestBind_ADocumentNoPassReachesDeclaresNothing(t *testing.T) {
	t.Parallel()
	var deep strings.Builder
	deep.WriteString("x-c0: &c0 {description: d}\n")
	for i := 1; i <= 70; i++ {
		fmt.Fprintf(&deep, "x-c%d: &c%d {<<: *c%d}\n", i, i, i-1)
	}
	docs := map[string]string{
		"/a.yaml": deep.String() + "T: {$id: 'https://x.test/u', type: integer}\nW: {<<: *c70}\n",
		"/z.yaml": "S: {$id: 'a.yaml', T: {type: boolean}, $defs: {V: {$id: 'https://x.test/u', type: string}}}\n",
	}
	refs := []string{"    A: {$ref: 'a.yaml#/T'}\n", "    Z: {$ref: 'z.yaml#/S'}\n", "    U: {$ref: 'https://x.test/u'}\n"}
	for _, order := range [][]int{{0, 1, 2}, {2, 1, 0}} {
		src := head + "components:\n  schemas:\n"
		for _, i := range order {
			src += refs[i]
		}
		cfg := resolving
		cfg.Open = func(name string) (*yaml.Node, error) { return openapitest.YAMLDocument(t, docs[name]), nil }
		table := bound(t, src, cfg)

		e := entryAt(t, table, "/components/schemas/U")
		require.Equal(t, bind.Resolved, e.Hop.Status, e.Hop.Why())
		assert.Equal(t, bind.Position{Doc: docNamed(t, table, "/z.yaml"), Pointer: "/S/$defs/V"}, e.Hop.Target)
		assert.True(t, table.Document(docNamed(t, table, "/a.yaml")).Unreached)
		assert.False(t, table.Truncated(), "a merge past the bound where nothing is reached decides nothing")
	}
}

// TestBind_AReadingNoPassSettlesCountsEveryDocument pins the input no pass
// settles: a document reached only through a URI it declares a second time.
// Counted, it is not reached, and uncounted, it is, so once a pass would return
// to what one read before, every document read counts and the passes end, the
// URI naming neither schema, in either order.
func TestBind_AReadingNoPassSettlesCountsEveryDocument(t *testing.T) {
	t.Parallel()
	docs := map[string]string{
		"/e.yaml":        "A: {type: string}\nU: {$id: 'https://x.test/u', properties: {p: {$ref: 'file:///d.yaml#/D'}}}\n",
		"file:///d.yaml": "D: {$id: 'https://x.test/u', type: integer}\n",
	}
	refs := []string{"    X: {$ref: 'https://x.test/u'}\n", "    Y: {$ref: 'e.yaml#/A'}\n"}
	for _, order := range [][]int{{0, 1}, {1, 0}} {
		src := head + "components:\n  schemas:\n"
		for _, i := range order {
			src += refs[i]
		}
		cfg := resolving
		cfg.Open = func(name string) (*yaml.Node, error) { return openapitest.YAMLDocument(t, docs[name]), nil }
		cfg.Limit = budget.AtMost(1 << 16)
		table := bound(t, src, cfg)

		require.False(t, table.Exhausted(), "the passes end within the budget")
		assert.Equal(t, bind.DuplicateID, entryAt(t, table, "/components/schemas/X").Hop.Reason)
	}
}

// TestBind_AURIASecondDocumentDeclaresNamesNeither pins that a URI answers from
// every document read, not from the nearest: however deep the second declaration
// lies, in a later pass than the first, the URI names neither, as two in one
// document do, rather than whichever was read first.
func TestBind_AURIASecondDocumentDeclaresNamesNeither(t *testing.T) {
	t.Parallel()
	const uri = "https://x.test/same"
	tests := []struct {
		name, schemas string
		docs          map[string]string
	}{
		{"the source and a document", "    Own: {$id: '" + uri + "'}\n    R: {$ref: '" + uri + "'}\n    O: {$ref: 'o.yaml#/S'}\n",
			map[string]string{"o.yaml": "S: {$id: '" + uri + "'}\n"}},
		{"two documents a pass apart", "    R: {$ref: '" + uri + "'}\n    O: {$ref: 'o.yaml#/S'}\n",
			map[string]string{"o.yaml": "S: {$id: '" + uri + "'}\nN: {$ref: 'p.yaml#/S'}\n", "p.yaml": "S: {$id: '" + uri + "'}\n"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, _ := recordingDocuments(t, tc.docs)
			table := bound(t, head+"components:\n  schemas:\n"+tc.schemas+"    Z: {$ref: 'o.yaml#/N'}\n", cfg)
			assert.Equal(t, bind.DuplicateID, entryAt(t, table, "/components/schemas/R").Hop.Reason)
		})
	}
}

// TestBind_ADocumentOnlyAnIdentifierReachesIsReached pins the other side: a
// document no reference names by location any more, that a URI it declares still
// names, holds what the table does, so it stays reached.
func TestBind_ADocumentOnlyAnIdentifierReachesIsReached(t *testing.T) {
	t.Parallel()
	docs := map[string]string{
		"/a.yaml": "S: {$id: 'https://x.test/u', type: integer}\n",
		"/z.yaml": "S: {$id: 'a.yaml', $defs: {T: {type: string}}}\n",
	}
	src := head + "components:\n  schemas:\n    A: {$ref: 'a.yaml#/$defs/T'}\n    U: {$ref: 'https://x.test/u'}\n" +
		"    Z: {$ref: 'z.yaml#/S'}\n"
	cfg := resolving
	cfg.Open = func(name string) (*yaml.Node, error) { return openapitest.YAMLDocument(t, docs[name]), nil }
	table := bound(t, src, cfg)

	doc := docNamed(t, table, "/a.yaml")
	e := entryAt(t, table, "/components/schemas/U")
	require.Equal(t, bind.Resolved, e.Hop.Status, e.Hop.Why())
	assert.Equal(t, bind.Position{Doc: doc, Pointer: "/S"}, e.Hop.Target)
	assert.False(t, table.Document(doc).Unreached)
}

// TestBind_DeclarationOrderReachesNoAnswer holds the rounds to their claim on one
// tangle: each document names the next by a URI the one after it declares.
func TestBind_DeclarationOrderReachesNoAnswer(t *testing.T) {
	t.Parallel()
	docs := map[string]string{
		"/a.yaml": "S: {$id: 'https://x.test/b', $defs: {D: {type: string}}}\n",
		"/b.yaml": "S: {$ref: 'https://x.test/b#/$defs/D'}\n",
		"/c.yaml": "S: {$id: 'https://x.test/a', $defs: {D: {type: string}}}\n",
	}
	refs := []string{"    A: {$ref: 'b.yaml#/S'}\n", "    B: {$ref: 'a.yaml#/S'}\n", "    C: {$ref: 'c.yaml#/S'}\n",
		"    D: {$ref: 'https://x.test/a'}\n"}
	answers := make([][]bind.Status, 0, 3)
	for _, order := range [][]int{{0, 1, 2, 3}, {3, 2, 1, 0}, {1, 3, 0, 2}} {
		src := head + "components:\n  schemas:\n"
		for _, i := range order {
			src += refs[i]
		}
		cfg := resolving
		cfg.Open = func(name string) (*yaml.Node, error) { return openapitest.YAMLDocument(t, docs[name]), nil }
		table := bound(t, src, cfg)
		entries := table.Entries()
		got := make([]bind.Status, 0, len(entries))
		for _, e := range entries {
			got = append(got, e.Hop.Status)
		}
		answers = append(answers, got)
	}
	assert.True(t, slices.Equal(answers[0], answers[1]) && slices.Equal(answers[1], answers[2]), "%v", answers)
}

// TestBind_LandsOnTheAnchorAnotherDocumentsDynamicRefNames pins that an expansion
// reads as a reference does: a schema another document holds is built only where
// something lands on it, so one nothing but a $dynamicRef reaches is landed on,
// and what it holds is read. Two declarations name no target, so neither is, and
// a spelling that names no anchor, or a $dynamicRef that is no string, lands
// nowhere.
func TestBind_LandsOnTheAnchorAnotherDocumentsDynamicRefNames(t *testing.T) {
	t.Parallel()
	const entry = "    E: {$ref: 'o.yaml#/E'}\n"
	landed := func(t *testing.T, other string) (*bind.Table, bind.DocID, []jsontext.Pointer) {
		t.Helper()
		table := bound(t, head+"components:\n  schemas:\n"+entry, withDocuments(map[string]string{"o.yaml": other}))
		doc := docNamed(t, table, "o.yaml")
		var at []jsontext.Pointer
		for _, l := range table.Landings() {
			if l.At.Doc == doc && l.At.Pointer != "/E" {
				at = append(at, l.At.Pointer)
			}
		}
		return table, doc, at
	}

	table, doc, at := landed(t, "E: {$dynamicRef: '#n'}\nT: {$dynamicAnchor: n, properties: {p: {$ref: '#/U'}}}\nU: {type: string}\n")
	assert.Contains(t, at, jsontext.Pointer("/T"))
	for _, l := range table.Landings() {
		if l.At.Pointer == "/T" {
			assert.True(t, l.Reached, "the expansion reaches it, so what reads a reached schema reads it")
			assert.Equal(t, bind.Position{Doc: doc, Pointer: "/E"}, l.Site)
		}
	}
	e, ok := table.Entry(bind.Position{Doc: doc, Pointer: "/T/properties/p"})
	require.True(t, ok, "what the anchor's schema holds is read")
	assert.Equal(t, bind.Resolved, e.Hop.Status, e.Hop.Why())

	source := bound(t, head+"components:\n  schemas:\n    D: {$dynamicRef: '#n'}\n    T: {$dynamicAnchor: n}\n", self)
	assert.Empty(t, source.Landings(), "the source's schemas are the model's own")

	for name, other := range map[string]string{
		"declared twice":   "E: {$dynamicRef: '#n'}\nT: {$dynamicAnchor: n}\nU: {$dynamicAnchor: n}\n",
		"declared nowhere": "E: {$dynamicRef: '#n'}\n",
		"a pointer":        "E: {$dynamicRef: '#/T'}\nT: {$dynamicAnchor: n}\n",
		"no string":        "E: {$dynamicRef: [n]}\nT: {$dynamicAnchor: n}\n",
	} {
		_, _, at := landed(t, other)
		assert.Empty(t, at, name)
	}
}

// TestDynamicAnchors_CountsWhatADocumentDeclares pins the count an expansion
// rests on. An alias and a `<<` merge key each hand a position the anchor another
// wrote, and the parser reads both as declaring it, so a count of what is spelled
// would report one where the document has two; a key written twice declares its
// last. A $dynamicAnchor in an example or an extension declares nothing, and one
// under an $id counts, since dynamic scope can reach it.
func TestDynamicAnchors_CountsWhatADocumentDeclares(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, schemas string
		want          []jsontext.Pointer
	}{
		{"an alias re-declares at its own position", "    A: &a {$dynamicAnchor: t}\n    B: *a\n",
			[]jsontext.Pointer{"/components/schemas/A", "/components/schemas/B"}},
		{"a merge key does too", "    A: &a {$dynamicAnchor: t}\n    B: {<<: *a, description: d}\n",
			[]jsontext.Pointer{"/components/schemas/A", "/components/schemas/B"}},
		{"an explicit key beats the merged one", "    A: &a {$dynamicAnchor: t}\n    B: {<<: *a, $dynamicAnchor: o}\n",
			[]jsontext.Pointer{"/components/schemas/A"}},
		{"a key written twice declares its last", "    A: {$dynamicAnchor: o, $dynamicAnchor: t}\n",
			[]jsontext.Pointer{"/components/schemas/A"}},
		{"an example declares nothing", "    A: {example: {$dynamicAnchor: t}}\n", nil},
		{"one under an $id counts", "    A: {$id: 'https://x.test/a', $dynamicAnchor: t}\n    B: {$dynamicAnchor: t}\n",
			[]jsontext.Pointer{"/components/schemas/A", "/components/schemas/B"}},
		{"a value that is no name declares nothing", "    A: {$dynamicAnchor: [t]}\n", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sites, whole := bound(t, head+"components:\n  schemas:\n"+tc.schemas, self).DynamicAnchors(bind.Source, "t")
			assert.True(t, whole)
			var got []jsontext.Pointer
			for _, p := range sites {
				got = append(got, p.Pointer)
			}
			slices.Sort(got)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestDynamicAnchors_ANullAnchorNamesNothing pins that a null $dynamicAnchor, in
// each of its spellings, is the keyword left out, where its text would be a name
// a $dynamicRef could spell. A quoted one is that name.
func TestDynamicAnchors_ANullAnchorNamesNothing(t *testing.T) {
	t.Parallel()
	table := bound(t, head+"components:\n  schemas:\n    A: {$dynamicAnchor: null}\n    B: {$dynamicAnchor: ~}\n"+
		"    C: {$dynamicAnchor: Null}\n    D: {$dynamicAnchor: }\n    E: {$dynamicAnchor: \"null\"}\n", self)
	want := map[string][]bind.Position{"null": {{Doc: bind.Source, Pointer: "/components/schemas/E"}}, "Null": nil}
	for name, at := range want {
		sites, whole := table.DynamicAnchors(bind.Source, name)
		assert.True(t, whole)
		assert.Equal(t, at, sites, "$dynamicAnchor %q", name)
	}
}

// anchoredPastTheBound writes a document with one $dynamicAnchor and a schema
// merging through a chain deeper than the view expands.
func anchoredPastTheBound() string {
	var b strings.Builder
	b.WriteString(head + "x-c0: &c0 {description: d}\n")
	for i := 1; i <= 70; i++ {
		fmt.Fprintf(&b, "x-c%d: &c%d {<<: *c%d}\n", i, i, i-1)
	}
	b.WriteString("components:\n  schemas:\n    A: {$dynamicAnchor: t}\n    B: {<<: *c70}\n")
	return b.String()
}

// TestDynamicAnchors_SaysWhenTheCountProvesNothing pins that a read a merge past
// the bound stopped, or the budget, does not pass for whole, since an undercount
// reads as the one declaration an expansion needs. A merge past the bound outside
// a schema hides none, and a nil table knows of none.
func TestDynamicAnchors_SaysWhenTheCountProvesNothing(t *testing.T) {
	t.Parallel()
	const anchored = "components:\n  schemas:\n    A: {$dynamicAnchor: t}\n"
	shallow := head + "x-m: &m {description: d}\n" + anchored + "    B: {<<: *m}\n"
	_, whole := bound(t, shallow, self).DynamicAnchors(bind.Source, "t")
	assert.True(t, whole, "a merge within the bound hides nothing")

	sites, whole := bound(t, anchoredPastTheBound(), self).DynamicAnchors(bind.Source, "t")
	assert.Len(t, sites, 1)
	assert.False(t, whole, "a schema past the bound may declare a second")

	outside := bound(t, strings.Replace(anchoredPastTheBound(), "    B: {<<: *c70}\n", "  responses:\n    R: {<<: *c70}\n", 1),
		self)
	require.True(t, outside.Truncated(), "the response merges past the bound")
	_, whole = outside.DynamicAnchors(bind.Source, "t")
	assert.True(t, whole, "a response past the bound declares no anchor")

	_, whole = bound(t, head+anchored, budgetConfig(budget.AtMost(4))).DynamicAnchors(bind.Source, "t")
	assert.False(t, whole, "nor does a read the budget stopped")

	e := entryAt(t, bound(t, anchoredPastTheBound()+"    C: {$ref: '#ghost'}\n", self), "/components/schemas/C")
	assert.Equal(t, bind.MergeBound, e.Hop.Reason, "a name none declares may be past the bound")

	var none *bind.Table
	sites, whole = none.DynamicAnchors(bind.Source, "t")
	assert.Empty(t, sites)
	assert.True(t, whole)
}

// TestInOwnResource_ReadsTheResourceAPositionIsIn pins what stands in for a walk
// of every key on the way down: only a schema's $id starts a resource, so a
// property named $id does not, raw YAML does not, and an $id restating its
// document's does not, in the source and in a document of schemas alike.
func TestInOwnResource_ReadsTheResourceAPositionIsIn(t *testing.T) {
	t.Parallel()
	table := bound(t, head+"x-e: {$id: 'https://x.test/e', k: {type: string}}\ncomponents:\n  schemas:\n"+
		"    P: {properties: {$id: {type: string}, q: {type: string}}}\n"+
		"    I: {$id: 'https://x.test/i', properties: {q: {type: string}}}\n"+
		"    Same: {$id: 'spec.yaml', properties: {q: {type: string}}}\n"+
		"    O: {$ref: 'o.yaml#/S'}\n",
		withDocuments(map[string]string{"o.yaml": "S: {$id: 'https://x.test/o', a: {b: {type: string}}}\nT: {type: string}\n"}))
	doc := docNamed(t, table, "o.yaml")
	for p, want := range map[bind.Position]bool{
		{Pointer: "/components/schemas/P/properties/q"}:    false,
		{Pointer: "/components/schemas/I"}:                 true,
		{Pointer: "/components/schemas/I/properties/q"}:    true,
		{Pointer: "/components/schemas/Same/properties/q"}: false,
		{Pointer: "/x-e/k"}:                                false,
		{Doc: doc, Pointer: "/S/a/b"}:                      true,
		{Doc: doc, Pointer: "/T"}:                          false,
	} {
		own, known := table.InOwnResource(p)
		assert.True(t, known, p.Pointer)
		assert.Equal(t, want, own, p.Pointer)
	}
	own, known := (*bind.Table)(nil).InOwnResource(bind.Position{})
	assert.False(t, own)
	assert.True(t, known)
}

// TestDynamicFragment_ReadsAPlainFragment pins what a $dynamicRef can spell.
func TestDynamicFragment_ReadsAPlainFragment(t *testing.T) {
	t.Parallel()
	for ref, want := range map[string]string{"#a": "a", "#my%2Danchor": "my-anchor", "#a+b": "a+b"} {
		name, _, ok := bind.DynamicFragment(ref)
		assert.True(t, ok, ref)
		assert.Equal(t, want, name, ref)
	}
	for _, ref := range []string{"a", "#", "#/a", "o.yaml#a", "#%zz"} {
		_, why, ok := bind.DynamicFragment(ref)
		assert.False(t, ok, ref)
		assert.NotEmpty(t, why, ref)
	}
}
