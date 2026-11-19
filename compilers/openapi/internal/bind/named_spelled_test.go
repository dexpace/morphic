// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// compileConfig returns what the load phase binds doc with where it names no
// dialect and no other document: its component keys, how its minor reads a
// requirement key, and its minor's own Schema Object rows as a dialect rule,
// written again here from the rule table.
func compileConfig(doc *yaml.Node) bind.Config {
	view, root := nodeview.New(), nodeview.DocumentRoot(doc)
	version := ""
	if v := nodeview.Deref(view.ChildByToken(root, "openapi")); v != nil {
		version = v.Value
	}
	keys := func(kind string) []string {
		components := nodeview.Deref(view.ChildByToken(root, "components"))
		pairs := view.MappingPairs(view.ChildByToken(components, kind))
		out := make([]string, 0, len(pairs))
		for _, p := range pairs {
			out = append(out, p.Key)
		}
		return out
	}
	rows, _ := oas.Table().SchemaRules(version, "")
	byURI := oas.Table().SecurityRules(version).NamesByURI()
	rule := bind.DialectRule{Keyword: rows.Identifier(), Definitions: rows.Takes("definitions", oas.Map),
		Dependencies: rows.Defines("dependencies"), Tuples: rows.Takes("items", oas.List),
		Recursive: rows.Defines("$recursiveRef"), IgnoresRefFields: rows.IgnoresRefFields(), NamesNone: true}
	return bind.Config{Self: "spec.yaml", Components: keys("schemas"), SecuritySchemes: keys("securitySchemes"),
		KeysByURI: func(string) bool { return byURI },
		Dialect:   func(string, string) bind.DialectRule { return rule }}
}

// spelled is what one document costs: the total NamedPointerBytes takes and
// whether it is whole, and the pointers of the frames a table's walks push,
// counted as the total counts one, a '/' and the bytes of each token: walked
// once a frame, and named once more for a mapping's member, whose key is a
// node at its value's pointer.
type spelled struct {
	weighed, walked, named int
	complete               bool
}

// spelledBy returns what the document text costs.
func spelledBy(t *testing.T, text string) spelled {
	t.Helper()
	doc := openapitest.YAMLDocument(t, text)
	var s spelled
	s.weighed, s.complete = bind.NamedPointerBytes(t.Context(), doc, namedSteps)
	table := bind.Bind(t.Context(), doc, compileConfig(doc).Spelling(func(at bind.DocumentPointer, member bool) {
		bytes := 0
		for token := range at.Pointer.Tokens() {
			bytes += len(token) + 1
		}
		s.walked += bytes
		s.named += bytes
		if member {
			s.named += bytes
		}
	}))
	require.False(t, table.Exhausted() || table.Truncated(), "the table is whole")
	require.Equal(t, 1, table.Passes(), "the table is read in one pass")
	return s
}

// landedOn returns a bringer whose n positions are raw values holding the
// anchored schema, each written by mount, which returns its line and the
// fragment naming the schema in it, and named by a $ref.
func landedOn(name string, mount func(id, i string) (line, fragment string)) bringer {
	return bringer{name: name, bring: func(id string, n int, inner string) brought {
		lines, refs := make([]string, 0, n), make([]string, 0, n)
		for i := range n {
			line, fragment := mount(id, strconv.Itoa(i))
			lines, refs = append(lines, line+"\n"), append(refs, "{$ref: '"+fragment+"'}")
		}
		return brought{defs: "a" + id + ": &a" + id + " " + inner, mounts: strings.Join(lines, ""),
			schema: "{allOf: [" + strings.Join(refs, ", ") + "]}"}
	}}
}

// landings lists more ways a $ref lands on a raw mount of a schema: under a
// `default`, in a list of lists, by a key its fragment escapes, in a mapping a
// `<<` merges in, and in the `$defs` of a raw value.
var landings = []bringer{
	landedOn("ref under a default", func(id, i string) (string, string) {
		return "x-d" + id + "-" + i + ": {default: *a" + id + "}", "#/x-d" + id + "-" + i + "/default"
	}),
	landedOn("ref into a list of lists", func(id, i string) (string, string) {
		return "x-l" + id + "-" + i + ": [[*a" + id + "]]", "#/x-l" + id + "-" + i + "/0/0"
	}),
	landedOn("ref by an escaped key", func(id, i string) (string, string) {
		return "'x/r~" + id + " " + i + "': *a" + id, "#/x~1r~0" + id + "%20" + i
	}),
	landedOn("ref to a merged-in mount", func(id, i string) (string, string) {
		return "x-h" + id + "-" + i + ": {<<: {m: *a" + id + "}}", "#/x-h" + id + "-" + i + "/m"
	}),
	landedOn("ref into the $defs of a raw value", func(id, i string) (string, string) {
		return "x-s" + id + "-" + i + ": {$defs: {d: *a" + id + "}}", "#/x-s" + id + "-" + i + "/$defs/d"
	}),
}

// spot is a place a document holds a value: the lines below its head, which
// write the value brought at each `@` and one member more at each `%` of the
// wider document. unread marks one whose `%` no walk reads, byURI one read
// only where a requirement key is a URI, and over one where the total names
// what no walk reads: what stands beside a Reference Object's $ref, and where
// a reference the binder refuses would land. full marks one held to every
// chain (spotChains).
type spot struct {
	name, body                string
	unread, byURI, over, full bool
}

// fully returns s held to every chain.
func fully(s spot) spot {
	s.full = true
	return s
}

// okResponses is the responses of an operation a spot writes.
const okResponses = "responses: {'200': {description: ok}}"

// schemaT is the components of a spot that writes no other.
const schemaT = "components: {schemas: {T: {type: string}}}\n"

// schemaSpot holds a schema component writing schema.
func schemaSpot(name, schema string) spot {
	return spot{name: name, body: "components: {schemas: {T: {type: string}, S: " + schema + "}}\n"}
}

// bodySpot holds a request body whose content writes media.
func bodySpot(name, media string) spot {
	return spot{name: name, body: "paths: {/p: {post: {requestBody: {content: {" + media + "}}, " + okResponses + "}}}\n" +
		schemaT}
}

// mountedSpot holds object at two raw mounts of one anchor, a $ref of each
// component map of maps landing on each.
func mountedSpot(name, object string, maps ...string) spot {
	schemas, refs := "T: {type: string}", ""
	for _, m := range maps {
		if m == "schemas" {
			schemas += ", A: {$ref: '#/x-o-0'}, B: {$ref: '#/x-o-1'}"
			continue
		}
		refs += ", " + m + ": {A: {$ref: '#/x-o-0'}, B: {$ref: '#/x-o-1'}}"
	}
	return spot{name: name + " mounted twice", body: "x-o: &o " + object + "\nx-o-0: *o\nx-o-1: *o\n" +
		"components: {schemas: {" + schemas + "}" + refs + "}\n"}
}

// spots lists where a document holds what a chain of bringers brings: each
// schema keyword and each object that holds a schema, values the grammar keeps
// raw, objects holding no schema that a $ref or a requirement key lands on,
// one value landed on as every kind, and what stands beside a $ref.
var spots = []spot{
	fully(schemaSpot("a schema", "{allOf: [@]%}")),
	schemaSpot("$defs", "{$defs: {d: @%}}"),
	schemaSpot("definitions", "{definitions: {d: @}%}"),
	schemaSpot("dependent schemas", "{dependentSchemas: {d: @%}, dependencies: {d: @}}"),
	schemaSpot("a tuple", "{items: [@, @], additionalItems: @, prefixItems: [@]%}"),
	schemaSpot("a list of lists", "{allOf: [[@], [@]], anyOf: [[[@]]]%}"),
	fully(schemaSpot("raw members of a schema",
		"{type: object, example: @, default: @, const: @, enum: [@], x-e: @, examples: [@]%}")),
	schemaSpot("each keyword holding a schema", "{not: @, if: @, then: @, else: @, contains: @, propertyNames: @, "+
		"unevaluatedItems: @, unevaluatedProperties: @, additionalProperties: @, patternProperties: {p: @}, "+
		"contentSchema: @, items: @%}"),
	{name: "a parameter's content", body: "components: {schemas: {T: {type: string}}, " +
		"parameters: {P: {name: n, in: query%, content: {a/b: {schema: @}}}}}\n"},
	{name: "a webhook", body: "webhooks: {w: {post: {requestBody: {content: {a/b: {schema: @}}}, " + okResponses + "%}}}\n" +
		schemaT},
	{name: "a callback", body: "paths: {/p: {post: {callbacks: {c: {'{$url}': {post: {requestBody: " +
		"{content: {a/b: {schema: @}}}, " + okResponses + "%}}}}, " + okResponses + "}}}\n" + schemaT},
	{name: "a response's header and link", body: "paths: {/p: {get: {responses: {'200': {description: ok, " +
		"headers: {H: {schema: @%}}, links: {l: {operationId: o, parameters: {p: @}, requestBody: @%}}}}}}}\n" + schemaT},
	fully(bodySpot("an encoding nested twice", "multipart/form-data: {schema: {type: object}, "+
		"encoding: {f: {encoding: {g: {encoding: {h: {headers: {H: {schema: @%}}}}}}}}}")),
	bodySpot("a positional encoding", "multipart/mixed: {schema: {type: array}, "+
		"prefixEncoding: [{headers: {H: {schema: @%}}}], itemEncoding: {headers: {H: {schema: @}}}}"),
	bodySpot("an encoding nested in a positional one", "multipart/mixed: {schema: {type: array}, "+
		"prefixEncoding: [{encoding: {g: {headers: {H: {schema: @%}}}}}], itemEncoding: {encoding: {g: {headers: "+
		"{H: {schema: @}}}}, prefixEncoding: [{headers: {H: {schema: @}}}], itemEncoding: {headers: {H: {schema: @}}}}}"),
	bodySpot("an item schema", "a/b: {itemSchema: @%}"),
	{name: "additional operations and a query", body: "paths: {/p: {query: {requestBody: {content: {a/b: {schema: @}}}, " +
		okResponses + "%}, additionalOperations: {COPY: {requestBody: {content: {a/b: {schema: @}}}, " + okResponses +
		"}}}}\n" + schemaT},
	{name: "a path item's parameters", body: "paths: {/p: {parameters: [{name: n, in: query, schema: @%}]}}\n" + schemaT},
	{name: "raw members of objects", body: "paths: {/p: {x-p: @, get: {x-o: @, unknown: @, " +
		"parameters: [{name: n, in: query, example: @, examples: {e: {value: @%}}}], responses: {'200': {description: ok, " +
		"content: {a/b: {example: @, examples: {e: {value: @, x-e: @}}}}}}}}}\nx-root: @\nunknown: @\n" + schemaT},
	{name: "a component of each kind", body: "components: {schemas: {T: {type: string}}, mediaTypes: {M: {schema: @%}}, " +
		"examples: {E: {value: @}}, headers: {H: {schema: @}}, responses: {R: {description: d, content: {a/b: {schema: @}}}}, " +
		"requestBodies: {B: {content: {a/b: {schema: @}}}}, links: {L: {operationId: o, requestBody: @}}, " +
		"callbacks: {C: {'{$url}': {post: {" + okResponses + "}}}}, " +
		"pathItems: {I: {parameters: [{name: n, in: query, schema: @}]}}, " +
		"securitySchemes: {X: {type: http, scheme: basic, x-s: @}}}\n"},
	fully(mountedSpot("a security scheme", "{type: oauth2%, flows: {implicit: {authorizationUrl: u, scopes: {s: d}}}, x-s: @}",
		"securitySchemes")),
	mountedSpot("an example", "{summary: s%, value: @}", "examples"),
	mountedSpot("a link", "{operationId: o%, parameters: {p: @}, requestBody: @, "+
		"server: {url: u, variables: {v: {default: d, enum: [a, b]}}}}", "links"),
	mountedSpot("a header holding no schema", "{description: d%, content: {a/b: {example: @}}, example: @}", "headers"),
	mountedSpot("a response holding no schema", "{description: d%, headers: {H: {description: h, example: @}}, "+
		"links: {l: {operationId: o, parameters: {p: @}}}}", "responses"),
	mountedSpot("a request body holding no schema", "{description: d%, content: {a/b: {examples: {e: {value: @}}, "+
		"example: @}}}", "requestBodies"),
	mountedSpot("a path item holding no schema", "{summary: s%, get: {operationId: g, x-o: @, "+okResponses+"}, "+
		"servers: [{url: u}]}", "pathItems"),
	mountedSpot("a callback holding no schema", "{'{$url}': {summary: s%, post: {x-o: @, "+okResponses+"}}}", "callbacks"),
	mountedSpot("a parameter holding no schema", "{name: n, in: query%, example: @, examples: {e: {value: @}}}",
		"parameters"),
	fully(mountedSpot("a value landed on as each kind", "{type: object%, properties: {p: @}, content: {a/b: {schema: @}}, "+
		"schema: @, value: @, get: {parameters: [{name: n, in: query, schema: @}]}, flows: {implicit: {scopes: {s: d}}}, "+
		"headers: {H: {schema: @}}}", "schemas", "parameters", "headers", "responses", "requestBodies", "mediaTypes",
		"examples", "links", "pathItems", "callbacks", "securitySchemes")),
	{name: "requirement keys onto mounts", byURI: true, full: true, body: "x-s: &s {type: oauth2%, flows: {implicit: " +
		"{authorizationUrl: u, scopes: {s: d}}}, x-s: @}\nx-s-0: *s\nx-s-1: *s\nsecurity: [{'#/x-s-0': [], '#/x-s-1': []}]\n" +
		"paths: {/p: {get: {security: [{'#/x-s-1': [r]}], " + okResponses + "}}}\n" + schemaT},
	{name: "beside a reference object's $ref", unread: true, over: true, body: "x-o: &o {name: n, in: query, schema: {type: string}}\n" +
		"x-o-0: *o\ncomponents: {schemas: {T: {type: string}}, " +
		"parameters: {P: {$ref: '#/x-o-0', description: d, schema: @%}}}\n"},
	{name: "beside a path item's $ref", body: "x-o: &o {get: {" + okResponses + "}}\nx-o-0: *o\n" +
		"paths: {/p: {$ref: '#/x-o-0', parameters: [{name: n, in: query, schema: @%}]}}\n" + schemaT},
	{name: "the root named by a $ref", unread: true, over: true, body: "$defs: {d: @%}\nallOf: [@]\n" +
		"components: {schemas: {T: {type: string}, S: {$ref: '#'}}}\n"},
	{name: "a schema named as each object", over: true, body: "components: {schemas: {T: {type: string}, S: {type: object%, schema: @, " +
		"content: {a/b: {schema: @}}, headers: {H: {schema: @}}, get: {parameters: [{name: n, in: query, schema: @}]}}}, " +
		strings.Join(eachMap(func(m string) string { return m + ": {A: {$ref: '#/components/schemas/S'}}" },
			"parameters", "headers", "responses", "requestBodies", "mediaTypes", "pathItems", "callbacks"), ", ") + "}\n"},
	{name: "objects read as schemas", full: true, body: "components: {schemas: {T: {type: string}, S: {allOf: [" +
		"{$ref: '#/components/parameters/P'}, {$ref: '#/components'}, {$ref: '#/paths'}, " +
		"{$ref: '#/paths/~1p/get/responses'}]}}, parameters: {P: {name: n, in: query%, schema: @, properties: {p: @}, " +
		"items: @}}}\npaths: {/p: {get: {responses: {'200': {description: ok, properties: {p: @}}}, properties: {q: @}}, " +
		"allOf: [@]}}\n"},
}

// eachMap writes item for each of maps.
func eachMap(item func(m string) string, maps ...string) []string {
	out := make([]string, len(maps))
	for i, m := range maps {
		out[i] = item(m)
	}
	return out
}

// wideMember is the member a wider document writes at each `%` of a spot.
const wideMember = ", x-wwwwwwwwwwwwwwwwwwwwwwww: {a: b}"

// spotDocument writes the document of a minor holding, at each `@` of s, the
// leaf chain brings to broughtCopies positions by each bringer in turn.
func spotDocument(s spot, version string, chain []bringer, wide bool) string {
	var defs, mounts []string
	schema, more := broughtLeaf(wide), ""
	for depth, by := range slices.Backward(chain) {
		b := by.bring(strconv.Itoa(depth), broughtCopies, schema)
		defs, mounts, schema = append(defs, b.defs), append(mounts, b.mounts), b.schema
	}
	if wide {
		more = wideMember
	}
	return "openapi: " + version + "\ninfo: {title: t, version: '1'}\nx-defs: {" + strings.Join(defs, ", ") + "}\n" +
		strings.Join(mounts, "") + strings.NewReplacer("@", schema, "%", more).Replace(s.body)
}

// spotChains lists the chains a spot is held to: each bringer and landing
// alone and each pair of the core bringers, and where full is set, every chain
// broughtChains lists and each landing around and inside each core bringer.
func spotChains(full bool) [][]bringer {
	var chains [][]bringer
	for _, a := range slices.Concat(bringers, landings) {
		chains = append(chains, []bringer{a})
	}
	if !full {
		for _, a := range bringers[:coreBringers] {
			for _, b := range bringers[:coreBringers] {
				chains = append(chains, []bringer{a, b})
			}
		}
		return chains
	}
	for _, c := range broughtChains() {
		if len(c) > 1 {
			chains = append(chains, c)
		}
	}
	for _, a := range landings {
		for _, b := range bringers[:coreBringers] {
			chains = append(chains, []bringer{a, b}, []bringer{b, a})
		}
	}
	return chains
}

// spelledMinors lists a version of each minor the rule table tells apart.
var spelledMinors = []string{"3.0.3", "3.1.1", "3.2.0"}

// TestNamedPointerBytes_TotalsWhatATablesWalksSpell holds the invariant the
// pointer bound's second weighing rests on: the total is no less than the
// pointers of the frames a table's walks push (Config.Spelling), over every
// spot at every minor and each chain spotChains lists, in full at the last
// minor for the spots marked so unless the run is short. A member more adds no
// less to the total than to the walks, which shows a place read and not
// weighed, and the total is within four times the walks' but where a spot or
// a bringer says why not. It counts the binder's walks alone.
func TestNamedPointerBytes_TotalsWhatATablesWalksSpell(t *testing.T) {
	t.Parallel()
	for _, version := range spelledMinors {
		last := version == spelledMinors[len(spelledMinors)-1]
		byURI := oas.Table().SecurityRules(version).NamesByURI()
		for _, s := range spots {
			t.Run(s.name+" at "+version, func(t *testing.T) {
				t.Parallel()
				for _, chain := range spotChains(s.full && last && !testing.Short()) {
					name := chainName(chain)
					narrow := spelledBy(t, spotDocument(s, version, chain, false))
					wide := spelledBy(t, spotDocument(s, version, chain, true))
					require.True(t, narrow.complete && wide.complete, "%s: the total is taken", name)
					if !s.unread && (byURI || !s.byURI) {
						require.Greater(t, wide.named, narrow.named, "%s: a walk reads the member more", name)
					}

					assert.GreaterOrEqual(t, narrow.weighed, narrow.named, "%s: the total names what the walks read", name)
					assert.GreaterOrEqual(t, wide.weighed-narrow.weighed, wide.named-narrow.named,
						"%s: the total names the member more wherever a walk reads it", name)
					if !s.over && !slices.ContainsFunc(chain, func(b bringer) bool { return b.repeats }) {
						assert.LessOrEqual(t, narrow.weighed, 4*narrow.named, "%s: the total names little no walk reads", name)
					}
				}
			})
		}
	}
}
