// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reftable_test

import (
	"encoding/json/jsontext"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
)

// throughSpec writes a schema $ref whose pointer passes through the response
// reference at /components/responses/R, and one back into the source through
// a path item reference, as GitHub #945 writes it.
const throughSpec = `openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /a: {$ref: '#/components/pathItems/P'}
components:
  pathItems:
    P:
      get:
        responses:
          '200': {description: ok, content: {application/json: {schema: {$ref: '#/components/schemas/S'}}}}
  responses:
    R: {$ref: 'other.yaml#/R'}
    Inline: {description: ok, content: {application/json: {schema: {type: string}}}}
    Lost: {$ref: 'other.yaml#/Nothing'}
  schemas:
    S: {$ref: '#/paths/~1a/get/responses/200/content/application~1json/schema'}
    Body: {$ref: '#/components/responses/R/content/application~1json/schema'}
    Plain: {$ref: '#/components/responses/Inline/content/application~1json/schema'}
    Gone: {$ref: '#/components/responses/Lost/content/application~1json/schema'}
    Foreign: {$ref: '#/components/responses/R/content/application~1json/schema/properties/n/content/a~1b/schema'}
`

// throughConfig reads other.yaml's response R, which throughSpec's R names.
func throughConfig() reftable.Config {
	return withDocuments(map[string]string{"other.yaml": `R:
  description: ok
  content:
    application/json:
      schema:
        type: object
        properties:
          n: {$ref: '#/Elsewhere'}
Elsewhere: {type: string}
`})
}

// TestBind_ReadsAPointerThroughAReferenceObject pins a pointer passing a
// reference object: it reads on where that reference's chain ends, in whichever
// document, at the reference's own kind, so a response's schema there is a
// schema of the model though its YAML is another document's (GitHub #762). A
// schema chain closing that way is a loop like any other (GitHub #945). One
// passing a reference that resolves to nothing lands nowhere, and a schema is
// read for its own keywords, not through its $ref.
func TestBind_ReadsAPointerThroughAReferenceObject(t *testing.T) {
	t.Parallel()
	table := bound(t, throughSpec, throughConfig())

	body := entryAt(t, table, "/components/schemas/Body")
	require.Equal(t, reftable.Resolved, body.Hop.Status, body.Hop.Why())
	assert.Equal(t, "other.yaml", table.Document(body.Hop.Target.Doc).Name)
	assert.Equal(t, jsontext.Pointer("/R/content/application~1json/schema"), body.Hop.Target.Pointer)
	assert.Equal(t, reftable.Schema, body.Hop.Role)

	plain := entryAt(t, table, "/components/schemas/Plain")
	assert.Equal(t, at("/components/responses/Inline/content/application~1json/schema"), plain.Hop.Target,
		"an inline object is read where it is written")

	loops := table.Loops()
	require.Len(t, loops, 1)
	assert.Equal(t, []reftable.DocumentPointer{
		at("/components/pathItems/P/get/responses/200/content/application~1json/schema"),
		at("/components/schemas/S"),
	}, loops[0].Members)

	assert.Equal(t, reftable.Unpassable, entryAt(t, table, "/components/schemas/Gone").Hop.Reason)
	assert.Equal(t, reftable.NoTarget, entryAt(t, table, "/components/schemas/Foreign").Hop.Reason,
		"a schema is read for its own keywords, not through its $ref")
}

// TestBind_PassesAReferenceObjectOfAnotherDocument pins that a reference object
// another document writes is read through as the source's are (GitHub #751),
// in the document writing it. The pointer passes the source's /paths/~1x
// response into other.yaml's R, whose header H writes a $ref; the source writes
// one at the same pointer, so reading by the pointer alone would answer the
// source's header, not other.yaml's.
func TestBind_PassesAReferenceObjectOfAnotherDocument(t *testing.T) {
	t.Parallel()
	cfg := withDocuments(map[string]string{"other.yaml": "components:\n  responses:\n" +
		"    R: {description: o, headers: {H: {$ref: '#/components/headers/HO'}}}\n" +
		"  headers:\n    HO: {schema: {maxLength: 9}}\n"})
	table := bound(t, `openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /x: {get: {responses: {'200': {$ref: 'other.yaml#/components/responses/R'}}}}
components:
  responses:
    R: {description: s, headers: {H: {$ref: '#/components/headers/HS'}}}
  headers:
    HS: {schema: {maxLength: 1}}
  schemas:
    S: {$ref: '#/paths/~1x/get/responses/200/headers/H/schema'}
`, cfg)
	other, ok := table.Find("other.yaml")
	require.True(t, ok)
	s := entryAt(t, table, "/components/schemas/S")
	require.Equal(t, reftable.Resolved, s.Hop.Status, s.Hop.Why())
	assert.Equal(t, reftable.DocumentPointer{Doc: other, Pointer: "/components/headers/HO/schema"}, s.Hop.Target)
	h, ok := table.Reference(reftable.DocumentPointer{Doc: other, Pointer: "/components/responses/R/headers/H"}, reftable.RefHeader)
	require.True(t, ok, "other.yaml's header is read")
	assert.Equal(t, reftable.DocumentPointer{Doc: other, Pointer: "/components/headers/HO"}, h.Chain.End)
}

// referenceObjects declares a reference object of each kind a pointer into a
// schema can pass, and a response holding one below the pointer's start.
const referenceObjects = `components:
  headers:
    HO: {schema: {type: string}}
    HRef: {$ref: '#/components/headers/HO'}
  parameters:
    PO: {name: p, in: query, schema: {type: integer}}
    PRef: {$ref: '#/components/parameters/PO'}
  responses:
    RO: {description: o, content: {application/json: {schema: {type: boolean}}}}
    RRef: {$ref: '#/components/responses/RO'}
    R: {description: n, headers: {H: {$ref: '#/components/headers/HO'}}}
  requestBodies:
    BO: {content: {application/json: {schema: {type: number}}}}
    BRef: {$ref: '#/components/requestBodies/BO'}
  pathItems:
    IO: {get: {responses: {'200': {description: o, content: {application/json: {schema: {type: string}}}}}}}
    IRef: {$ref: '#/components/pathItems/IO'}
`

// TestBind_PassesAReferenceObjectWhereAPointerIntoAnotherDocumentStarts pins a
// pointer starting in another OpenAPI document: it passes the reference
// objects the grammar places there as one in the source does (GitHub #2016).
// A document with no openapi member has no grammar to place one, so a pointer
// into it passes none.
func TestBind_PassesAReferenceObjectWhereAPointerIntoAnotherDocumentStarts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pointer string
		target  jsontext.Pointer
	}{
		{"a header", "/components/headers/HRef/schema", "/components/headers/HO/schema"},
		{"a parameter", "/components/parameters/PRef/schema", "/components/parameters/PO/schema"},
		{"a response", "/components/responses/RRef/content/application~1json/schema",
			"/components/responses/RO/content/application~1json/schema"},
		{"a request body", "/components/requestBodies/BRef/content/application~1json/schema",
			"/components/requestBodies/BO/content/application~1json/schema"},
		{"a path item", "/components/pathItems/IRef/get/responses/200/content/application~1json/schema",
			"/components/pathItems/IO/get/responses/200/content/application~1json/schema"},
		{"one below the start", "/components/responses/R/headers/H/schema", "/components/headers/HO/schema"},
	}
	src := head + referenceObjects + "  schemas:\n"
	for i, tc := range tests {
		src += fmt.Sprintf("    Far%d: {$ref: 'other.yaml#%s'}\n    Near%d: {$ref: '#%s'}\n"+
			"    Bare%d: {$ref: 'bare.yaml#%s'}\n", i, tc.pointer, i, tc.pointer, i, tc.pointer)
	}
	table := bound(t, src, withDocuments(map[string]string{
		"other.yaml": head + referenceObjects, "bare.yaml": referenceObjects,
	}))
	other, ok := table.Find("other.yaml")
	require.True(t, ok)

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			far := entryAt(t, table, jsontext.Pointer(fmt.Sprintf("/components/schemas/Far%d", i))).Hop
			assert.Equal(t, reftable.Resolved, far.Status, far.Why())
			assert.Equal(t, reftable.DocumentPointer{Doc: other, Pointer: tc.target}, far.Target)
			near := entryAt(t, table, jsontext.Pointer(fmt.Sprintf("/components/schemas/Near%d", i))).Hop
			assert.Equal(t, at(tc.target), near.Target, "the same pointer into the source")
			bare := entryAt(t, table, jsontext.Pointer(fmt.Sprintf("/components/schemas/Bare%d", i))).Hop
			assert.Equal(t, reftable.NoTarget, bare.Reason, "a document with no openapi member")
		})
	}
}

// TestBind_ReadsWhatAReferenceObjectResolvedTo pins the sites the table reads
// in content a reference object resolved to: other.yaml's response, whose
// schema writes a $ref read against that document.
func TestBind_ReadsWhatAReferenceObjectResolvedTo(t *testing.T) {
	t.Parallel()
	table := bound(t, throughSpec, throughConfig())
	id, ok := table.Find("other.yaml")
	require.True(t, ok)
	e, ok := table.Entry(reftable.DocumentPointer{Doc: id, Pointer: "/R/content/application~1json/schema/properties/n"})
	require.True(t, ok, "the site in what R resolved to is read")
	assert.Equal(t, reftable.DocumentPointer{Doc: id, Pointer: "/Elsewhere"}, e.Hop.Target, "read against the document holding it")
}

// TestBind_ReadsEverySchemaSiteOnce pins the sites a table holds: every schema
// position writing a $ref, by the grammar, an aliased one at every position it
// is placed at, none outside a schema position, a null $ref none either, and
// every schema position inside raw YAML a $ref lands on.
func TestBind_ReadsEverySchemaSiteOnce(t *testing.T) {
	t.Parallel()
	src := "openapi: 3.1.0\ninfo: {title: t, version: '1'}\n" +
		"paths:\n  x-p: {get: {parameters: [{schema: {$ref: '#/x'}}]}}\n  /a:\n    get:\n" +
		"      parameters: [{name: q, in: query, schema: {$ref: '#/components/schemas/A'}}]\n" +
		"      responses:\n        x-r: {content: {a/b: {schema: {$ref: '#/x'}}}}\n" +
		"        '200': {description: d, content: {a/b: {schema: {items: {$ref: '#/components/schemas/A'}}}}}\n" +
		"x-lib: {S: {$ref: '#/components/schemas/B'}, Unread: {$ref: '#/nothing'}}\n" +
		"x-raw: {T: {properties: {u: {$ref: '#/components/schemas/B'}}}}\n" +
		"components:\n  schemas:\n    A: &a {$ref: '#/components/schemas/B'}\n" +
		"    B: {properties: {x-p: *a, q: {$ref: null}}, allOf: [*a]}\n" +
		"    C: {$ref: '#/x-raw/T'}\n" +
		"  parameters:\n    P: {$ref: '#/components/parameters/Q'}\n" +
		"  responses:\n    R:\n      description: r\n      headers: {x-h: {schema: {$ref: '#/components/schemas/A'}}}\n"
	table := bound(t, src, self)
	sites := make([]jsontext.Pointer, 0, len(table.Entries()))
	for _, e := range table.Entries() {
		sites = append(sites, e.Site.Pointer)
	}
	assert.Equal(t, []jsontext.Pointer{"/components/responses/R/headers/x-h/schema", "/components/schemas/A",
		"/components/schemas/B/allOf/0", "/components/schemas/B/properties/x-p", "/components/schemas/C",
		"/paths/~1a/get/parameters/0/schema", "/paths/~1a/get/responses/200/content/a~1b/schema/items",
		"/x-raw/T/properties/u"}, sites)
	assert.Positive(t, table.Steps())
	assert.False(t, table.Exhausted())
	assert.Equal(t, 1, table.Documents())
}

// TestBind_ResolvesEachMappingValue pins a discriminator's mapping values: each
// read as a $ref written at the discriminator, a component's name read by the
// lowering instead, defaultMapping alike, wherever the discriminator is
// written, raw YAML a $ref lands on included. A "#/$defs/..." value reads the
// definitions of the schema holding the discriminator, and a chain through a
// "#/$defs/..." $ref reads it where that $ref is written (GitHub #996).
func TestBind_ResolvesEachMappingValue(t *testing.T) {
	t.Parallel()
	table := bound(t, head+`components:
  schemas:
    P:
      $defs: {D: {type: object}}
      discriminator:
        propertyName: k
        mapping: {a: '#/x-lib/a', d: '#/$defs/D', h: '#/x-lib/h', n: Cat, z: '#/x-lib/missing'}
        defaultMapping: '#/x-lib/b'
    Holder: {$defs: {X: {type: string}}, properties: {p: {$ref: '#/$defs/X'}}}
    Box: {$ref: '#/x-lib/Box'}
    Q: {discriminator: {propertyName: k, mapping: {b: '#/x-lib/b'}}}
    Cat: {type: object}
x-lib:
  a: {$ref: '#/x-lib/b'}
  b: {type: object}
  h: {$ref: '#/components/schemas/Holder/properties/p'}
  Box: {discriminator: {propertyName: k, mapping: {i: '#/x-lib/b'}}}
`, reftable.Config{Self: self.Self, Components: []string{"P", "Holder", "Box", "Q", "Cat"}})
	disc := at("/components/schemas/P/discriminator")
	for value, want := range map[string]reftable.Chain{
		"#/x-lib/a":       {Status: reftable.Resolved, End: at("/x-lib/b"), Hops: 2},
		"#/$defs/D":       {Status: reftable.Resolved, End: at("/components/schemas/P/$defs/D"), Hops: 1},
		"#/x-lib/h":       {Status: reftable.Resolved, End: at("/components/schemas/Holder/$defs/X"), Hops: 3},
		"#/x-lib/b":       {Status: reftable.Resolved, End: at("/x-lib/b"), Hops: 1},
		"#/x-lib/missing": {Status: reftable.Unresolved},
	} {
		m, ok := table.Mapping(disc, value)
		require.True(t, ok, value)
		assert.Equal(t, want, m.Chain, value)
		assert.Equal(t, disc, m.At, value)
	}
	_, ok := table.Mapping(disc, "Cat")
	assert.False(t, ok, "a component's name is the lowering's to read")
	_, ok = table.Mapping(at("/x-lib/Box/discriminator"), "#/x-lib/b")
	assert.True(t, ok, "a discriminator in raw YAML a $ref lands on is read")
	_, ok = table.Mapping(at("/components/schemas/Q/discriminator"), "#/x-lib/b")
	assert.True(t, ok, "and every other discriminator")
}

// TestBind_PlacesEachLandingAtTheLeastPositionReachingIt pins the landings a
// table reports, positions a schema has to be built for: each raw YAML or
// other document's position some hop lands on, placed at the least $ref or
// mapping value whose chain reaches it, the source's before another
// document's, whichever document writes it.
func TestBind_PlacesEachLandingAtTheLeastPositionReachingIt(t *testing.T) {
	t.Parallel()
	const otherSrc = "A: {$ref: '#/B'}\nB: {type: string}\nC: {$ref: '#/D'}\nD: {}\nE: {}\nF: {$ref: '#/E'}\n" +
		"R: {description: ok, content: {a/b: {schema: {$ref: '#/C'}}}}\n"
	cfg := withDocuments(map[string]string{"other.yaml": otherSrc})
	table := bound(t, head+`components:
  responses:
    R: {$ref: 'other.yaml#/R'}
  schemas:
    Z: {$ref: '#/x-lib/S'}
    M: {$ref: 'other.yaml#/A'}
    Y: {$ref: 'other.yaml#/F'}
    P: {discriminator: {propertyName: k, mapping: {s: '#/x-lib/S'}}}
x-lib:
  S: {type: string}
`, cfg)
	other, ok := table.Find("other.yaml")
	require.True(t, ok)
	got := map[reftable.DocumentPointer]reftable.Landing{}
	for _, l := range table.Landings() {
		got[l.At] = l
	}
	in := func(pointer jsontext.Pointer) reftable.DocumentPointer {
		return reftable.DocumentPointer{Doc: other, Pointer: pointer}
	}
	assert.Equal(t, reftable.Landing{At: at("/x-lib/S"), Name: "spec.yaml", Site: at("/components/schemas/P/discriminator"),
		Reached: true}, got[at("/x-lib/S")], "the least of a mapping value and a $ref reaching it")
	assert.Equal(t, reftable.Landing{At: in("/B"), Name: "other.yaml", Site: at("/components/schemas/M"), Reached: true},
		got[in("/B")], "reached through a chain")
	assert.Equal(t, reftable.Landing{At: in("/D"), Name: "other.yaml", Site: in("/C"), Reached: true},
		got[in("/D")], "a $ref another document writes places it, though it also leads there through R")
	assert.Equal(t, reftable.Landing{At: in("/C"), Name: "other.yaml", Site: in("/R/content/a~1b/schema"), Reached: true},
		got[in("/C")], "the content R resolved to is read")
	assert.Equal(t, reftable.Landing{At: in("/E"), Name: "other.yaml", Site: at("/components/schemas/Y"), Reached: true},
		got[in("/E")], "a source position orders before the other document's own $ref to it")
}
