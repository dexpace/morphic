// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reftable_test

import (
	"encoding/json/jsontext"
	"errors"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
)

const head = "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\n"

// self names the source "spec.yaml".
var self = reftable.Config{Self: "spec.yaml"}

// resolving names the source by an absolute path and places a document as
// RFC 3986 resolves a reference, so every spelling of the source names it, as
// with the load phase's Locate.
var resolving = reftable.Config{Self: "/spec.yaml", Locate: func(uri, base string) (string, bool) {
	b, errBase := url.Parse(base)
	r, errRef := url.Parse(uri)
	if errBase != nil || errRef != nil {
		return "", false
	}
	return b.ResolveReference(r).String(), true
}}

// bound binds src under cfg.
func bound(t *testing.T, src string, cfg reftable.Config) *reftable.ReferenceTargetTable {
	t.Helper()
	return reftable.Bind(t.Context(), openapitest.YAMLDocument(t, src), cfg)
}

// entryAt returns the entry the table holds at the source pointer site.
func entryAt(t *testing.T, table *reftable.ReferenceTargetTable, site jsontext.Pointer) reftable.ReferenceSite {
	t.Helper()
	e, ok := table.Entry(reftable.DocumentPointer{Pointer: site})
	require.True(t, ok, "an entry at %s", site)
	return e
}

// at is the source position pointer names.
func at(pointer jsontext.Pointer) reftable.DocumentPointer {
	return reftable.DocumentPointer{Pointer: pointer}
}

// TestBind_ReadsEachHop pins one row per way a $ref's own hop lands: where,
// what the reading finds there, and why one that lands on no schema does not.
func TestBind_ReadsEachHop(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, body string
		reason     reftable.Reason
		target     jsontext.Pointer
		role       reftable.Role
	}{
		{"a component", "    A: {$ref: '#/components/schemas/B'}\n    B: {type: string}\n",
			reftable.NoReason, "/components/schemas/B", reftable.Schema},
		{"raw YAML under an extension", "    A: {$ref: '#/x-lib/S'}\nx-lib:\n  S: {type: string}\n",
			reftable.NoReason, "/x-lib/S", reftable.Raw},
		{"a link parameter, raw YAML in the model", "    A: {$ref: '#/components/links/L/parameters/p'}\n" +
			"  links:\n    L: {parameters: {p: {type: string}}}\n", reftable.NoReason, "/components/links/L/parameters/p", reftable.Raw},
		{"a literal read as a schema", "    A: {$ref: '#/components/schemas/B/example'}\n    B: {example: {type: string}}\n",
			reftable.NoReason, "/components/schemas/B/example", reftable.Raw},
		{"a boolean schema", "    A: {$ref: '#/components/schemas/B/properties/p'}\n    B: {properties: {p: true}}\n",
			reftable.NoReason, "/components/schemas/B/properties/p", reftable.Schema},
		{"a percent escape, and '+' as itself", "    A: {$ref: '#/components/schemas/a%20b+c'}\n    'a b+c': {}\n",
			reftable.NoReason, "/components/schemas/a b+c", reftable.Schema},
		{"the source by its own name", "    A: {$ref: 'spec.yaml#/components/schemas/B'}\n    B: {}\n",
			reftable.NoReason, "/components/schemas/B", reftable.Schema},
		{"an object no schema reference may name", "    A: {$ref: '#/info'}\n", reftable.NotSchema, "", reftable.NoRole},
		{"the document", "    A: {$ref: '#'}\n", reftable.NotSchema, "", reftable.NoRole},
		{"a lone slash, the root's member keyed \"\"", "    A: {$ref: '#/'}\n'': {type: string}\n",
			reftable.NoReason, "/", reftable.Raw},
		{"a lone slash where the root has no such member", "    A: {$ref: '#/'}\n", reftable.NoTarget, "", reftable.NoRole},
		{"a document part only", "    A: {$ref: ''}\n", reftable.NotSchema, "", reftable.NoRole},
		{"a list of schemas", "    A: {$ref: '#/components/schemas/B/allOf'}\n    B: {allOf: [{}]}\n",
			reftable.NotSchema, "", reftable.NoRole},
		{"the map of a link's parameters", "    A: {$ref: '#/components/links/L/parameters'}\n" +
			"  links:\n    L: {parameters: {p: {type: string}}}\n", reftable.NotSchema, "", reftable.NoRole},
		{"a key the model lacks, read as raw YAML", "    A: {$ref: '#/components/extras/M/schema'}\n" +
			"  extras:\n    M: {schema: {type: string}}\n", reftable.NoReason, "/components/extras/M/schema", reftable.Raw},
		{"a components/mediaTypes entry's schema, which the load phase models", "    A: {$ref: '#/components/mediaTypes/M/schema'}\n" +
			"  mediaTypes:\n    M: {schema: {type: string}}\n", reftable.NoReason, "/components/mediaTypes/M/schema", reftable.Schema},
		{"a header's schema beneath a nested encoding, which no model holds",
			"    A: {$ref: '#/components/requestBodies/B/content/m~1x/encoding/f/encoding/g/headers/H/schema'}\n" +
				"  requestBodies:\n    B: {content: {m/x: {encoding: {f: {encoding: {g: {headers: {H: {schema: {}}}}}}}}}}\n",
			reftable.NoReason, "/components/requestBodies/B/content/m~1x/encoding/f/encoding/g/headers/H/schema", reftable.Raw},
		{"a header's schema beneath an encoding, which the model holds",
			"    A: {$ref: '#/components/requestBodies/B/content/m~1x/encoding/f/headers/H/schema'}\n" +
				"  requestBodies:\n    B: {content: {m/x: {encoding: {f: {headers: {H: {schema: {}}}}}}}}\n",
			reftable.NoReason, "/components/requestBodies/B/content/m~1x/encoding/f/headers/H/schema", reftable.Schema},
		{"a nested encoding, an object no schema reference may name",
			"    A: {$ref: '#/components/requestBodies/B/content/m~1x/encoding/f/encoding/g'}\n" +
				"  requestBodies:\n    B: {content: {m/x: {encoding: {f: {encoding: {g: {}}}}}}}\n",
			reftable.NotSchema, "", reftable.NoRole},
		{"raw YAML that is no schema", "    A: {$ref: '#/x-lib/n'}\nx-lib: {n: 5}\n", reftable.NotSchema, "", reftable.NoRole},
		{"a component the YAML writes as no schema, which the parser builds one at", "    A: {$ref: '#/components/schemas/B'}\n    B: 5\n",
			reftable.NoReason, "/components/schemas/B", reftable.Schema},
		{"a property the YAML writes as no schema", "    A: {$ref: '#/components/schemas/B/properties/p'}\n    B: {properties: {p: just text}}\n",
			reftable.NoReason, "/components/schemas/B/properties/p", reftable.Schema},
		{"nothing there", "    A: {$ref: '#/components/schemas/Z'}\n", reftable.NoTarget, "", reftable.NoRole},
		{"a trailing slash, an empty key", "    A: {$ref: '#/components/schemas/B/'}\n    B: {}\n",
			reftable.NoTarget, "", reftable.NoRole},
		{"an index with a leading zero", "    A: {$ref: '#/components/schemas/B/allOf/00'}\n    B: {allOf: [{}]}\n",
			reftable.NoTarget, "", reftable.NoRole},
		{"an index with a sign", "    A: {$ref: '#/components/schemas/B/allOf/-0'}\n    B: {allOf: [{}]}\n",
			reftable.NoTarget, "", reftable.NoRole},
		{"an index with a plus", "    A: {$ref: '#/components/schemas/B/allOf/+0'}\n    B: {allOf: [{}]}\n",
			reftable.NoTarget, "", reftable.NoRole},
		{"an index", "    A: {$ref: '#/components/schemas/B/allOf/10'}\n    B: {allOf: [{}, {}, {}, {}, {}, {}, {}, {}, {}, {}, {}]}\n",
			reftable.NoReason, "/components/schemas/B/allOf/10", reftable.Schema},
		{"an anchor", "    A: {$ref: '#a'}\n    B: {$anchor: a}\n", reftable.NoReason, "/components/schemas/B", reftable.Schema},
		{"an anchor of every character one may hold", "    A: {$ref: '#_a-1.b'}\n    B: {$anchor: _a-1.b}\n",
			reftable.NoReason, "/components/schemas/B", reftable.Schema},
		{"an anchor no schema declares", "    A: {$ref: '#a'}\n", reftable.NoAnchor, "", reftable.NoRole},
		{"a fragment no anchor can be named", "    A: {$ref: '#a/b'}\n", reftable.BadPointer, "", reftable.NoRole},
		{"one opening with a digit", "    A: {$ref: '#1a'}\n", reftable.BadPointer, "", reftable.NoRole},
		{"one holding a character no anchor may", "    A: {$ref: '#a+b'}\n", reftable.BadPointer, "", reftable.NoRole},
		{"an invalid escape", "    A: {$ref: '#/components/schemas/A~B'}\n    'A~B': {}\n", reftable.BadPointer, "", reftable.NoRole},
		{"bytes that are not UTF-8", "    A: {$ref: '#/components/schemas/%FF'}\n", reftable.BadPointer, "", reftable.NoRole},
		{"a second '#'", "    A: {$ref: '#/components/schemas/B#x'}\n    B: {}\n", reftable.BadPointer, "", reftable.NoRole},
		{"a second '#' after an anchor", "    A: {$ref: '#a#x'}\n    B: {$anchor: a}\n", reftable.BadPointer, "", reftable.NoRole},
		{"another document, none read", "    A: {$ref: 'other.yaml#/X'}\n", reftable.ExternalOff, "", reftable.NoRole},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := entryAt(t, bound(t, head+"components:\n  schemas:\n"+tc.body, self), "/components/schemas/A")
			assert.Equal(t, tc.reason, e.Hop.Reason, e.Hop.Why())
			if tc.reason == reftable.NoReason {
				assert.Equal(t, reftable.Resolved, e.Hop.Status)
				assert.Equal(t, at(tc.target), e.Hop.Target)
			} else {
				assert.Equal(t, reftable.Unresolved, e.Hop.Status)
				assert.Equal(t, reftable.Unresolved, e.Chain.Status)
			}
			assert.Equal(t, tc.role, e.Hop.Role)
		})
	}
}

// TestBind_ReadsADefinitionByTheDefsRule pins the $defs rule: in an $id's
// resource, its root's definitions alone, as JSON Schema 2020-12 §8.2.1 reads
// a pointer; in the document's, its root, then the nearest object above holding
// "$defs" in the model, whatever was resolved before. An ancestor answers raw
// YAML at the path too (GitHub #997), and the root answers before the schema
// spelling the pointer (GitHub #995). A schema under an extension answers
// nothing from its own definitions unless its own $id makes it a resource
// (GitHub #570). Where none answers, the reason says which went unread.
func TestBind_ReadsADefinitionByTheDefsRule(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, body string
		site, want jsontext.Pointer
		why        reftable.Reason
	}{
		{"the nearest ancestor", "components:\n  schemas:\n    H: {$defs: {A: {}}, properties: {p: {$ref: '#/$defs/A'}}}\n",
			"/components/schemas/H/properties/p", "/components/schemas/H/$defs/A", 0},
		{"the root before an ancestor", "$defs:\n  A: {}\ncomponents:\n  schemas:\n    H: {$defs: {A: {}}, properties: {p: {$ref: '#/$defs/A'}}}\n",
			"/components/schemas/H/properties/p", "/$defs/A", 0},
		{"the root before the schema's own", "$defs:\n  A: {}\ncomponents:\n  schemas:\n    B: {$ref: '#/$defs/A', $defs: {A: {}}}\n",
			"/components/schemas/B", "/$defs/A", 0},
		{"a resource's own first", "$defs:\n  A: {}\ncomponents:\n  schemas:\n    B: {$id: 'https://x.test/b', $ref: '#/$defs/A', $defs: {A: {}}}\n",
			"/components/schemas/B", "/components/schemas/B/$defs/A", 0},
		{"a resource with none of its own, read from its root alone", "$defs:\n  A: {}\ncomponents:\n  schemas:\n" +
			"    B: {$id: 'https://x.test/b', $ref: '#/$defs/A'}\n", "/components/schemas/B", "", reftable.NoResourceTarget},
		{"a resource's own under an escaped name", "components:\n  schemas:\n    B: {$id: 'https://x.test/b', " +
			"allOf: [{$ref: '#/$defs/a~1b'}, {$ref: '#/$defs/x~0y'}], $defs: {a/b: {}, x~y: {}}}\n",
			"/components/schemas/B/allOf/0", "/components/schemas/B/$defs/a~1b", 0},
		{"no schema's own without an $id", "components:\n  schemas:\n    B: {$ref: '#/$defs/A', $defs: {A: {}}}\n",
			"/components/schemas/B", "", reftable.OwnDefinition},
		{"one sitting in a nested resource reads its root's", "components:\n  schemas:\n    B: {$id: 'https://x.test/b', " +
			"$defs: {A: {}}, properties: {p: {$id: 'p', $defs: {A: {}}, properties: {q: {$ref: '#/$defs/A', $defs: {A: {}}}}}}}\n",
			"/components/schemas/B/properties/p/properties/q", "/components/schemas/B/properties/p/$defs/A", 0},
		{"one sitting in a nested resource reads neither its own nor an outer one's", "components:\n  schemas:\n" +
			"    B: {$id: 'https://x.test/b', $defs: {A: {}}, properties: {p: {$id: 'p', " +
			"properties: {q: {$ref: '#/$defs/A', $defs: {A: {}}}}}}}\n",
			"/components/schemas/B/properties/p/properties/q", "", reftable.NoResourceTarget},
		{"one in the outermost resource, which is none of its own", "components:\n  schemas:\n    B: {$id: 'https://x.test/b', $defs: {A: {}}, " +
			"properties: {p: {$ref: '#/$defs/A', $defs: {A: {}}}}}\n",
			"/components/schemas/B/properties/p", "/components/schemas/B/$defs/A", 0},
		{"an $id with a fragment makes no resource", "components:\n  schemas:\n    B: {$id: '#f', $ref: '#/$defs/A', $defs: {A: {}}}\n",
			"/components/schemas/B", "", reftable.OwnDefinition},
		{"a nested $id of a fragment alone moves no base", "components:\n  schemas:\n    B: {$id: 'https://x.test/b', $defs: {A: {}}, properties: {p: {$id: '#f', " +
			"properties: {q: {$ref: '#/$defs/A', $defs: {A: {}}}}}}}\n",
			"/components/schemas/B/properties/p/properties/q", "/components/schemas/B/$defs/A", 0},
		{"an $id naming no document keeps the base", "components:\n  schemas:\n    B: {$defs: {A: {}}, properties: {p: {$id: '%zz', " +
			"properties: {q: {$ref: '#/$defs/A', $defs: {A: {}}}}}}}\n",
			"/components/schemas/B/properties/p/properties/q", "/components/schemas/B/$defs/A", 0},
		{"a path into a definition", "components:\n  schemas:\n    H: {$defs: {A: {properties: {x: {}}}}, properties: {p: {$ref: '#/$defs/A/properties/x'}}}\n",
			"/components/schemas/H/properties/p", "/components/schemas/H/$defs/A/properties/x", 0},
		{"raw YAML an ancestor holds", "components:\n  schemas:\n    H: {$defs: {A: {example: {type: string}}}, properties: {p: {$ref: '#/$defs/A/example'}}}\n",
			"/components/schemas/H/properties/p", "/components/schemas/H/$defs/A/example", 0},
		{"past an ancestor holding no schema there", "components:\n  schemas:\n    H: {$defs: {A: {type: [string]}}, properties: {p: {$ref: '#/$defs/A/type'}}}\n",
			"/components/schemas/H/properties/p", "", reftable.NoDefinition},
		{"a trailing slash names no single key", "components:\n  schemas:\n    B: {$ref: '#/$defs/A/', $defs: {A: {}}}\n",
			"/components/schemas/B", "", reftable.NoDefinition},
		{"a trailing slash in a resource names an empty key", "components:\n  schemas:\n" +
			"    B: {$id: 'https://x.test/b', $ref: '#/$defs/A/', $defs: {A: {}}}\n", "/components/schemas/B", "", reftable.NoResourceTarget},
		{"an ancestor that is a map holding $defs", "components:\n  schemas:\n    H: {properties: {$defs: {properties: {x: {}}}, p: {$ref: '#/$defs/properties/x'}}}\n",
			"/components/schemas/H/properties/p", "/components/schemas/H/properties/$defs/properties/x", 0},
		{"an object of the model whose $defs is no definitions", "components:\n  $defs: {K: {}}\n  schemas:\n    B: {$ref: '#/$defs/K'}\n",
			"/components/schemas/B", "", reftable.NoDefinition},
		{"a raw schema's own, where its $id makes it a resource", "x-lib:\n  S: {$id: 'https://x.test/s', $ref: '#/$defs/n', $defs: {n: {}}}\n" +
			"components:\n  schemas:\n    U: {$ref: '#/x-lib/S'}\n", "/x-lib/S", "/x-lib/S/$defs/n", 0},
		{"the schemas map holding $defs", "components:\n  schemas:\n    $defs: {K: {type: integer}}\n    B: {properties: {y: {$ref: '#/$defs/K'}}}\n",
			"/components/schemas/B/properties/y", "/components/schemas/$defs/K", 0},
		{"a null $id makes no resource", "components:\n  schemas:\n    B: {$id: null, $ref: '#/$defs/A', $defs: {A: {}}}\n",
			"/components/schemas/B", "", reftable.OwnDefinition},
		{"a relative $id repeated below its own base moves none", "$defs:\n  K: {}\ncomponents:\n  schemas:\n" +
			"    B: {$id: a.json, properties: {p: {$id: a.json, properties: {q: {$ref: '#/$defs/K', $defs: {K: {}}}}}}}\n",
			"/components/schemas/B/properties/p/properties/q", "", reftable.NoResourceTarget},
		{"a relative $id resolved against a relative base moves none", "$defs:\n  K: {}\ncomponents:\n  schemas:\n" +
			"    B: {$id: dir/c.json, properties: {p: {$id: c.json, properties: {q: {$ref: '#/$defs/K', $defs: {K: {}}}}}}}\n",
			"/components/schemas/B/properties/p/properties/q", "", reftable.NoResourceTarget},
		{"a nested $id of a dot segment names another base", "components:\n  schemas:\n    B: {$defs: {A: {}}, properties: {p: {$id: '.', " +
			"properties: {q: {$ref: '#/$defs/A', $defs: {A: {}}}}}}}\n",
			"/components/schemas/B/properties/p/properties/q", "", reftable.NoResourceTarget},
		{"a nested $id of the root path names another base", "components:\n  schemas:\n    B: {$defs: {A: {}}, properties: {p: {$id: '/', " +
			"properties: {q: {$ref: '#/$defs/A', $defs: {A: {}}}}}}}\n",
			"/components/schemas/B/properties/p/properties/q", "", reftable.NoResourceTarget},
		{"a nested $id with an empty fragment moves the base", "components:\n  schemas:\n    B: {$defs: {A: {}}, properties: {p: {$id: 'x.json#', " +
			"properties: {q: {$ref: '#/$defs/A', $defs: {A: {}}}}}}}\n",
			"/components/schemas/B/properties/p/properties/q", "", reftable.NoResourceTarget},
		{"a nested $id naming the document itself moves none", "components:\n  schemas:\n    B: {$defs: {A: {}}, properties: {p: {$id: './spec.yaml#', " +
			"properties: {q: {$ref: '#/$defs/A', $defs: {A: {}}}}}}}\n",
			"/components/schemas/B/properties/p/properties/q", "/components/schemas/B/$defs/A", 0},
		{"a relative $id naming another resource", "components:\n  schemas:\n" +
			"    B: {$id: a.json, properties: {p: {$id: b.json, properties: {q: {$ref: '#/$defs/K', $defs: {K: {}}}}}}}\n",
			"/components/schemas/B/properties/p/properties/q", "", reftable.NoResourceTarget},
		{"no ancestor in raw YAML", "x-lib:\n  $defs: {n: {}}\n  S: {$ref: '#/$defs/n'}\ncomponents:\n  schemas:\n    U: {$ref: '#/x-lib/S'}\n",
			"/x-lib/S", "", reftable.RawDefinition},
		{"no definition of a schema under an extension", "x-lib:\n  S: {properties: {p: {$ref: '#/$defs/n'}}, $defs: {n: {}}}\n" +
			"components:\n  schemas:\n    U: {$ref: '#/x-lib/S'}\n", "/x-lib/S/properties/p", "", reftable.RawDefinition},
		{"a definition of a schema beneath an Encoding Object", "components:\n  requestBodies:\n    B: {content: {m/x: {encoding: " +
			"{f: {headers: {H: {schema: {properties: {p: {$ref: '#/$defs/n'}}, $defs: {n: {}}}}}}}}}}\n",
			"/components/requestBodies/B/content/m~1x/encoding/f/headers/H/schema/properties/p",
			"/components/requestBodies/B/content/m~1x/encoding/f/headers/H/schema/$defs/n", 0},
		{"no definition of a schema beneath an Encoding Object another nests", "components:\n  requestBodies:\n" +
			"    B: {content: {m/x: {encoding: {f: {encoding: {g: {headers: {H: {schema: {properties: {p: {$ref: '#/$defs/n'}}, " +
			"$defs: {n: {}}}}}}}}}}}}\n",
			"/components/requestBodies/B/content/m~1x/encoding/f/encoding/g/headers/H/schema/properties/p", "", reftable.RawDefinition},
		{"a raw schema's own before its raw ancestor's", "x-lib:\n  $defs: {n: {}}\n  S: {$ref: '#/$defs/n', $defs: {n: {}}}\n" +
			"components:\n  schemas:\n    U: {$ref: '#/x-lib/S'}\n", "/x-lib/S", "", reftable.OwnDefinition},
		{"raw YAML at the root", "$defs:\n  A: {type: string}\ncomponents:\n  schemas:\n    H: {properties: {p: {$ref: '#/$defs/A'}}}\n",
			"/components/schemas/H/properties/p", "/$defs/A", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := entryAt(t, bound(t, head+tc.body, resolving), tc.site)
			if tc.want == "" {
				assert.Equal(t, tc.why, e.Hop.Reason, e.Hop.Why())
				return
			}
			require.Equal(t, reftable.Resolved, e.Hop.Status, e.Hop.Why())
			assert.Equal(t, at(tc.want), e.Hop.Target, "a definition is placed where it is written")
		})
	}
}

// TestBind_ReadsEachPositionAsTheJSONItSpells pins what an alias or a merge
// key means: the content is written at each position it is placed at, so a
// $ref inside B, an alias of A, reads B's own definitions, and one Child
// merges from Base reads Child's.
func TestBind_ReadsEachPositionAsTheJSONItSpells(t *testing.T) {
	t.Parallel()
	table := bound(t, head+`components:
  schemas:
    A: &a {$defs: {n: {}}, properties: {p: {$ref: '#/$defs/n'}}}
    B: *a
    Base: &b {$defs: {m: {}}, properties: {r: {$ref: '#/$defs/m'}}}
    Child: {<<: *b, type: object}
`, self)
	for site, want := range map[jsontext.Pointer]jsontext.Pointer{
		"/components/schemas/A/properties/p":     "/components/schemas/A/$defs/n",
		"/components/schemas/B/properties/p":     "/components/schemas/B/$defs/n",
		"/components/schemas/Base/properties/r":  "/components/schemas/Base/$defs/m",
		"/components/schemas/Child/properties/r": "/components/schemas/Child/$defs/m",
	} {
		assert.Equal(t, at(want), entryAt(t, table, site).Hop.Target, site)
	}
}

// TestBind_ReadsEveryWebhookAsAPathItem pins that the webhooks map holds a path
// item under any name, so a schema $ref in one is read: one named like an
// extension or like a keyword holding raw YAML elsewhere too, since a map of
// the model takes no extensions (GitHub #949).
func TestBind_ReadsEveryWebhookAsAPathItem(t *testing.T) {
	t.Parallel()
	const op = "{post: {requestBody: {content: {a/b: {schema: {$ref: '#/components/schemas/T'}}}}}}\n"
	table := bound(t, head+"webhooks:\n  x-h: "+op+"  example: "+op+"  plain: "+op+
		"components:\n  schemas:\n    T: {type: string}\n", self)
	for _, name := range []string{"x-h", "example", "plain"} {
		e := entryAt(t, table, jsontext.Pointer("/webhooks/"+name+"/post/requestBody/content/a~1b/schema"))
		assert.Equal(t, at("/components/schemas/T"), e.Hop.Target, name)
	}
}

// TestBind_ReadsARepeatedKeyAsItsLast pins a key written twice in a mapping,
// which YAML forbids and the parser reads anyway: it is read as its last
// occurrence in raw YAML as in the model, so a pointer names one node
// wherever it leads (docs/parser-library.md#duplicate-key). Each first copy
// is a $ref that would carry the chain on.
func TestBind_ReadsARepeatedKeyAsItsLast(t *testing.T) {
	t.Parallel()
	table := bound(t, head+`components:
  schemas:
    A: {$ref: '#/x-lib/S'}
    B: {$ref: '#/components/schemas/M'}
    M: {$ref: '#/components/schemas/Missing'}
    M: {type: string}
x-lib:
  S: {$ref: '#/components/schemas/Missing'}
  S: {type: string}
`, self)
	assert.Equal(t, reftable.Chain{Status: reftable.Resolved, End: at("/x-lib/S"), Hops: 1},
		entryAt(t, table, "/components/schemas/A").Chain, "in raw YAML")
	assert.Equal(t, reftable.Chain{Status: reftable.Resolved, End: at("/components/schemas/M"), Hops: 1}, entryAt(t, table, "/components/schemas/B").Chain, "in the model")
}

// TestBind_FollowsEachChainToItsEnd pins where a chain ends: at the first
// position holding no $ref, counting the hops it read, or unresolved, where a
// hop past the first fails, which its own $ref reports.
func TestBind_FollowsEachChainToItsEnd(t *testing.T) {
	t.Parallel()
	table := bound(t, head+`components:
  schemas:
    A: {$ref: '#/components/schemas/B'}
    B: {$ref: '#/x-lib/C'}
    D: {$ref: '#/components/schemas/E'}
    E: {$ref: '#/components/schemas/Missing'}
x-lib:
  C: {type: string}
`, self)
	a := entryAt(t, table, "/components/schemas/A")
	assert.Equal(t, reftable.Chain{Status: reftable.Resolved, End: at("/x-lib/C"), Hops: 2},
		a.Chain)
	d := entryAt(t, table, "/components/schemas/D")
	assert.Equal(t, reftable.Resolved, d.Hop.Status, "D's own hop lands")
	assert.Equal(t, reftable.Chain{Status: reftable.Unresolved}, d.Chain)
	assert.Equal(t, reftable.NoTarget, entryAt(t, table, "/components/schemas/E").Hop.Reason)
}

// TestBind_ReportsALoopOfDefinitionsAtItsLeastMember pins a loop among one
// schema's definitions, each closed by the $defs rule: it is reported at its
// least member whichever definition is written first (GitHub #912).
func TestBind_ReportsALoopOfDefinitionsAtItsLeastMember(t *testing.T) {
	t.Parallel()
	for _, defs := range []string{"{B: {$ref: '#/$defs/A'}, A: {$ref: '#/$defs/B'}}",
		"{A: {$ref: '#/$defs/B'}, B: {$ref: '#/$defs/A'}}"} {
		table := bound(t, head+"components:\n  schemas:\n    H: {$defs: "+defs+"}\n", self)
		require.Len(t, table.Loops(), 1, defs)
		assert.Equal(t, at("/components/schemas/H/$defs/A"), table.Loops()[0].Site, defs)
	}
}

// TestBind_ReportsEachLoopOnceAtItsLeastMember pins the loops a table finds:
// one per cycle, its members in order, reported at the least of them in the
// source whatever the declaration order (GitHub #912), each member marked as on
// it and a position leading in as not. A schema reaching itself through what
// it declares is recursion, not a loop.
func TestBind_ReportsEachLoopOnceAtItsLeastMember(t *testing.T) {
	t.Parallel()
	loop := []string{"    B: {$ref: '#/components/schemas/C'}\n", "    C: {$ref: '#/components/schemas/B'}\n",
		"    A: {$ref: '#/components/schemas/C'}\n", "    P: {$ref: '#/components/schemas/P/properties/q'}\n" +
			"    R: {properties: {self: {$ref: '#/components/schemas/R'}}}\n"}
	for _, order := range [][]int{{0, 1, 2, 3}, {3, 2, 1, 0}} {
		var b strings.Builder
		for _, i := range order {
			b.WriteString(loop[i])
		}
		table := bound(t, head+"components:\n  schemas:\n"+b.String(), self)
		loops := table.Loops()
		require.Len(t, loops, 1, "%v", order)
		assert.Equal(t, reftable.Loop{Members: []reftable.DocumentPointer{at("/components/schemas/B"), at("/components/schemas/C")},
			Site: at("/components/schemas/B")}, loops[0], "%v", order)
		for site, member := range map[jsontext.Pointer]bool{"/components/schemas/B": true,
			"/components/schemas/C": true, "/components/schemas/A": false} {
			e := entryAt(t, table, site)
			assert.Equal(t, reftable.Cyclic, e.Chain.Status, site)
			assert.Equal(t, member, e.Chain.PositionLiesOnReferenceCycle, site)
		}
		assert.Equal(t, reftable.NoTarget, entryAt(t, table, "/components/schemas/P").Hop.Reason,
			"a pointer into what P only refers to names nothing")
		assert.Equal(t, reftable.Resolved, entryAt(t, table, "/components/schemas/R/properties/self").Chain.Status)
	}
}

// TestBind_ReportsALoopOutsideTheSourceAtItsLeastMember pins a loop with no
// member in the source: reported at its least member, in the document holding
// it, whether the source leads into it or only another document's content
// does, as W's property leads into Z.
func TestBind_ReportsALoopOutsideTheSourceAtItsLeastMember(t *testing.T) {
	t.Parallel()
	docs := map[string]string{"other.yaml": "X: {$ref: '#/Y'}\nY: {$ref: '#/X'}\nZ: {$ref: '#/Z'}\n" +
		"W: {properties: {p: {$ref: '#/Z'}}}\n"}
	table := bound(t, head+`components:
  schemas:
    B: {$ref: 'other.yaml#/X'}
    A: {$ref: 'other.yaml#/Y'}
    C: {$ref: 'other.yaml#/W'}
`, withDocuments(docs))
	require.Len(t, table.Loops(), 2)
	byFirst := map[jsontext.Pointer]reftable.Loop{}
	for _, l := range table.Loops() {
		byFirst[l.Members[0].Pointer] = l
	}
	xy := byFirst["/X"]
	assert.Equal(t, reftable.DocumentPointer{Doc: 1, Pointer: "/X"}, xy.Site)
	assert.Equal(t, reftable.DocID(1), xy.Members[0].Doc)
	assert.Equal(t, []reftable.DocumentPointer{{Doc: 1, Pointer: "/Z"}}, byFirst["/Z"].Members)
	assert.Equal(t, reftable.DocumentPointer{Doc: 1, Pointer: "/Z"}, byFirst["/Z"].Site, "only W's content leads into Z")
}

// withDocuments is self reading other documents from docs, by name.
func withDocuments(docs map[string]string) reftable.Config {
	cfg := self
	cfg.Open = func(name, _ string) (*yaml.Node, error) {
		src, ok := docs[name]
		if !ok {
			return nil, errors.New("no such document")
		}
		var n yaml.Node
		if err := yaml.Unmarshal([]byte(src), &n); err != nil {
			return nil, err
		}
		return &n, nil
	}
	return cfg
}

// TestBind_RefuseJudgesEachReferenceByItsSpelling pins Config.Refuse: a $ref
// whose document part it refuses fails unread with its reason, though another
// $ref names the same document and it is read. It is handed the document part
// as written, spaces trimmed, and never an $id, nor a $ref one answers: Locate
// places both, but neither reads anything.
func TestBind_RefuseJudgesEachReferenceByItsSpelling(t *testing.T) {
	t.Parallel()
	cfg := withDocuments(map[string]string{"/other.yaml": "X: {type: string}\n"})
	cfg.Self = resolving.Self
	opened := map[string]int{}
	open := cfg.Open
	cfg.Open = func(name, from string) (*yaml.Node, error) { opened[name]++; return open(name, from) }
	var located, refused []string
	cfg.Locate = func(uri, base string) (string, bool) {
		located = append(located, uri)
		return resolving.Locate(uri, base)
	}
	cfg.Refuse = func(uri string) error {
		refused = append(refused, uri)
		if strings.HasPrefix(uri, "/") {
			return errors.New("named by its spelling")
		}
		return nil
	}

	table := bound(t, `components:
  schemas:
    Rel: {$ref: 'other.yaml#/X'}
    Abs: {$ref: ' /other.yaml #/X'}
    Res: {$id: '/elsewhere/b', $ref: '#/$defs/A', $defs: {A: {}}}
    Id: {$ref: '/elsewhere/b'}
`, cfg)

	rel := entryAt(t, table, "/components/schemas/Rel")
	assert.Equal(t, reftable.Resolved, rel.Hop.Status, rel.Hop.Why())
	abs := entryAt(t, table, "/components/schemas/Abs")
	assert.Equal(t, reftable.Unreadable, abs.Hop.Reason)
	assert.Equal(t, "the document it names could not be read: named by its spelling", abs.Hop.Why())
	assert.Equal(t, 1, opened["/other.yaml"], "read for the relative $ref alone")
	slices.Sort(refused)
	assert.Equal(t, []string{"/other.yaml", "other.yaml"}, slices.Compact(refused),
		"each spelling is judged, once for each time its hop is read")
	assert.Contains(t, located, "/elsewhere/b", "the $id is placed")
	id := entryAt(t, table, "/components/schemas/Id")
	assert.Equal(t, reftable.DocumentPointer{Pointer: "/components/schemas/Res"}, id.Hop.Target, id.Hop.Why())
}

// TestIsEmpty_HoldsNoNode pins what an empty document is: one parsed to no
// node, white space and comments aside, or no tree at all, while a document
// holding null, or an empty mapping, holds a node.
func TestIsEmpty_HoldsNoNode(t *testing.T) {
	t.Parallel()
	for src, want := range map[string]bool{"": true, "\n": true, "# a comment\n": true, "---\n": false, "~\n": false, "{}\n": false} {
		assert.Equal(t, want, reftable.IsEmpty(openapitest.YAMLDocument(t, src)), "%q", src)
	}
	assert.True(t, reftable.IsEmpty(nil), "no tree")
}

// TestBind_ReportsADocumentThatDoesNotParse pins Unparsable: a document Open
// read and could not parse fails its $ref with the parser's reason, which the
// error carries whole and Hop.Why repeats after its own phrasing.
func TestBind_ReportsADocumentThatDoesNotParse(t *testing.T) {
	t.Parallel()
	parser := errors.New("yaml: line 1: did not find expected node content")
	cfg := self
	cfg.Open = func(string, string) (*yaml.Node, error) { return nil, &reftable.UnparsableError{Err: parser} }

	table := bound(t, "components:\n  schemas:\n    A: {$ref: 'bad.yaml#/x'}\n", cfg)

	a := entryAt(t, table, "/components/schemas/A")
	assert.Equal(t, reftable.Unparsable, a.Hop.Reason)
	assert.Equal(t, "the document it names does not parse: yaml: line 1: did not find expected node content", a.Hop.Why())
	err := &reftable.UnparsableError{Err: parser}
	require.EqualError(t, err, "it does not parse: yaml: line 1: did not find expected node content")
	assert.ErrorIs(t, err, parser)
}

// TestBind_ReadsOtherDocuments pins a reference to another document: read once
// however many name it, as one that failed is, as raw YAML, the whole of it
// named by a reference with no fragment; a chain followed across documents and
// back; a document that cannot be read, or holds nothing, reported as such;
// and a reference naming no document at all.
func TestBind_ReadsOtherDocuments(t *testing.T) {
	t.Parallel()
	docs := map[string]string{
		"pet.yaml": "type: object\n",
		"other.yaml": "S: {$ref: 'spec.yaml#/components/schemas/Leaf'}\nT: {type: string, $defs: {X: {}}, " +
			"properties: {d: {$ref: '#/$defs/X'}}}\n",
		"empty.yaml": "",
	}
	cfg := withDocuments(docs)
	opened := map[string]int{}
	open := cfg.Open
	cfg.Open = func(name, from string) (*yaml.Node, error) { opened[name]++; return open(name, from) }
	cfg.Locate = func(uri, base string) (string, bool) { return uri, uri != "nowhere" }
	table := bound(t, head+`components:
  schemas:
    Whole: {$ref: 'pet.yaml'}
    Back: {$ref: 'other.yaml#/S'}
    Again: {$ref: 'other.yaml#/T'}
    Leaf: {type: integer}
    Missing: {$ref: 'gone.yaml#/X'}
    MissingToo: {$ref: 'gone.yaml#/Y'}
    Empty: {$ref: 'empty.yaml#/X'}
    Nowhere: {$ref: 'nowhere#/X'}
    Pick:
      discriminator:
        propertyName: k
        mapping: {s: 'other.yaml#/S', t: 'other.yaml#/T', l: '#/components/schemas/Leaf'}
`, cfg)

	whole := entryAt(t, table, "/components/schemas/Whole")
	require.Equal(t, reftable.Resolved, whole.Hop.Status, whole.Hop.Why())
	assert.Equal(t, "pet.yaml", table.Document(whole.Hop.Target.Doc).Name)
	assert.Equal(t, reftable.Raw, whole.Hop.Role)
	back := entryAt(t, table, "/components/schemas/Back")
	assert.Equal(t, reftable.Chain{Status: reftable.Resolved, End: at("/components/schemas/Leaf"), Hops: 2},
		back.Chain, "a chain back into the source ends there, past a $ref naming another document")
	there, ok := table.Entry(reftable.DocumentPointer{Doc: back.Hop.Target.Doc, Pointer: "/S"})
	require.True(t, ok)
	assert.Equal(t, reftable.Chain{Status: reftable.Resolved, End: at("/components/schemas/Leaf"), Hops: 1}, there.Chain, "another document's $ref naming the source")
	for _, value := range []string{"other.yaml#/S", "other.yaml#/T", "#/components/schemas/Leaf"} {
		m, ok := table.Mapping(at("/components/schemas/Pick/discriminator"), value)
		require.True(t, ok, value)
		require.Equal(t, reftable.Resolved, m.Chain.Status, value)
	}
	assert.Equal(t, 1, opened["other.yaml"], "read once however many name it")
	assert.Equal(t, 1, opened["gone.yaml"], "a document that could not be read is not asked for again")
	assert.Equal(t, reftable.Unreadable, entryAt(t, table, "/components/schemas/Missing").Hop.Reason)
	assert.Equal(t, "the document it names could not be read: no such document",
		entryAt(t, table, "/components/schemas/Missing").Hop.Why())
	assert.Equal(t, reftable.Empty, entryAt(t, table, "/components/schemas/Empty").Hop.Reason)
	assert.Equal(t, "the document it names is empty", entryAt(t, table, "/components/schemas/Empty").Hop.Why())
	assert.Equal(t, reftable.Unreadable, entryAt(t, table, "/components/schemas/Nowhere").Hop.Reason)
	id, ok := table.Find("other.yaml")
	require.True(t, ok)
	assert.Equal(t, "other.yaml", table.Document(id).Name)
	defs, ok := table.Entry(reftable.DocumentPointer{Doc: id, Pointer: "/T/properties/d"})
	require.True(t, ok, "a site in raw YAML another document holds")
	assert.Equal(t, reftable.NoDefinition, defs.Hop.Reason, "raw YAML holds no ancestor's definitions")
	_, ok = table.Find("gone.yaml")
	assert.False(t, ok, "a document that could not be read is none the table read")
}
