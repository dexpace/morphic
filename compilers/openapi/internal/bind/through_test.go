package bind_test

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
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

// throughConfig is what the load phase hands the binder for throughSpec: the
// source's path item P and other.yaml's response R where the parser resolved
// them, and nothing for Lost.
func throughConfig() bind.Config {
	cfg := withDocuments(map[string]string{"other.yaml": `R:
  description: ok
  content:
    application/json:
      schema:
        type: object
        properties:
          n: {$ref: '#/Elsewhere'}
Elsewhere: {type: string}
`})
	cfg.Through = func(p jsontext.Pointer) (string, jsontext.Pointer, bool) {
		switch p {
		case "/paths/~1a":
			return "spec.yaml", "/components/pathItems/P", true
		case "/components/responses/R":
			return "other.yaml", "/R", true
		default:
			return "", "", false
		}
	}
	return cfg
}

// TestBind_ReadsAPointerThroughAReferenceObject pins a pointer passing a
// reference object: it reads on where the parser resolved that reference, in
// whichever document, at the reference's own kind, so a response's schema there
// is a schema of the model though its YAML is another document's (GitHub
// #762). It names the source, spelled as written. A schema chain closing that
// way is a loop like any other (GitHub #945). One passing a reference that
// resolved to nothing, or one in another document's content, lands nowhere.
func TestBind_ReadsAPointerThroughAReferenceObject(t *testing.T) {
	t.Parallel()
	table := bound(t, throughSpec, throughConfig())

	body := entryAt(t, table, "/components/schemas/Body")
	require.Equal(t, bind.Resolved, body.Hop.Status, body.Hop.Why())
	assert.Equal(t, "other.yaml", table.Document(body.Hop.Target.Doc).Name)
	assert.Equal(t, jsontext.Pointer("/R/content/application~1json/schema"), body.Hop.Target.Pointer)
	assert.Equal(t, bind.Schema, body.Hop.Role)
	assert.True(t, body.Hop.Named)
	assert.Equal(t, jsontext.Pointer("/components/responses/R/content/application~1json/schema"), body.Hop.Spelled)

	plain := entryAt(t, table, "/components/schemas/Plain")
	assert.Equal(t, at("/components/responses/Inline/content/application~1json/schema"), plain.Hop.Target,
		"an inline object is read where it is written")

	loops := table.Loops()
	require.Len(t, loops, 1)
	assert.Equal(t, []bind.Position{
		at("/components/pathItems/P/get/responses/200/content/application~1json/schema"),
		at("/components/schemas/S"),
	}, loops[0].Members)

	assert.Equal(t, bind.Unpassable, entryAt(t, table, "/components/schemas/Gone").Hop.Reason)
	assert.Equal(t, bind.NoTarget, entryAt(t, table, "/components/schemas/Foreign").Hop.Reason,
		"a schema is read for its own keywords, not through its $ref")
}

// TestBind_ReadsWhatAReferenceObjectResolvedTo pins the sites the table reads
// in content a reference object resolved to: other.yaml's response, whose
// schema writes a $ref read against that document.
func TestBind_ReadsWhatAReferenceObjectResolvedTo(t *testing.T) {
	t.Parallel()
	table := bound(t, throughSpec, throughConfig())
	id, ok := table.Find("other.yaml")
	require.True(t, ok)
	e, ok := table.Entry(bind.Position{Doc: id, Pointer: "/R/content/application~1json/schema/properties/n"})
	require.True(t, ok, "the site in what R resolved to is read")
	assert.Equal(t, bind.Position{Doc: id, Pointer: "/Elsewhere"}, e.Hop.Target)
	assert.False(t, e.Hop.Named, "it names the document holding it")
}

// TestTable_ThroughReadsAnEndPastAnotherReferenceObject pins Table.Through:
// where each of the source's reference objects resolves to. The parser places
// the end of /b's response in the source, past /a's $ref into other.yaml
// (GitHub #762), so it is read on through that one, and a schema there is read
// in other.yaml. A loop of such ends reads on no further than the pass bound,
// and resolves nowhere, as an object writing no $ref, or one the parser left
// without an end, does.
func TestTable_ThroughReadsAnEndPastAnotherReferenceObject(t *testing.T) {
	t.Parallel()
	cfg := withDocuments(map[string]string{"other.yaml": "paths:\n  /a:\n    get:\n      responses:\n        '200':\n" +
		"          description: ok\n          content: {application/json: {schema: {$ref: '#/S'}}}\nS: {type: string}\n"})
	cfg.Through = func(p jsontext.Pointer) (string, jsontext.Pointer, bool) {
		switch p {
		case "/paths/~1a":
			return "other.yaml", "/paths/~1a", true
		case "/paths/~1b/get/responses/200":
			return "spec.yaml", "/paths/~1a/get/responses/200", true
		case "/paths/~1x":
			return "spec.yaml", "/paths/~1y/get", true
		case "/paths/~1y":
			return "spec.yaml", "/paths/~1x/get", true
		default:
			return "", "", false
		}
	}
	table := bound(t, strings.TrimSuffix(head, "paths: {}\n")+`paths:
  /a: {$ref: 'other.yaml#/paths/~1a'}
  /b: {get: {responses: {'200': {$ref: '#/paths/~1a/get/responses/200'}, '201': {description: ok}}}}
  /x: {$ref: '#/paths/~1y/get'}
  /y: {$ref: '#/paths/~1x/get'}
components:
  schemas:
    B: {$ref: '#/paths/~1b/get/responses/200/content/application~1json/schema'}
    X: {$ref: '#/paths/~1x/get/responses'}
`, cfg)
	other, ok := table.Find("other.yaml")
	require.True(t, ok)
	for pointer, want := range map[jsontext.Pointer]jsontext.Pointer{
		"/paths/~1a":                   "/paths/~1a",
		"/paths/~1b/get/responses/200": "/paths/~1a/get/responses/200",
	} {
		end, ok := table.Through(pointer)
		require.True(t, ok, pointer)
		assert.Equal(t, bind.Position{Doc: other, Pointer: want}, end, pointer)
	}
	for _, pointer := range []jsontext.Pointer{"/paths/~1x", "/paths/~1y", "/paths/~1b/get/responses/201", "/paths"} {
		_, ok := table.Through(pointer)
		assert.False(t, ok, pointer)
	}
	b := entryAt(t, table, "/components/schemas/B")
	assert.Equal(t, bind.Position{Doc: other, Pointer: "/paths/~1a/get/responses/200/content/application~1json/schema"},
		b.Hop.Target)
	assert.Equal(t, bind.Position{Doc: other, Pointer: "/S"}, b.Chain.End)
	assert.Equal(t, bind.Unpassable, entryAt(t, table, "/components/schemas/X").Hop.Reason)
}

// TestBind_RefusesAPointerThroughTheFieldsBesideAPathItemRef pins the refusal
// of a pointer through the fields written beside a path item's $ref, and of a
// "#/$defs/..." pointer written there, which Config.Beside names: the parser
// reads such a pointer through the $ref instead (GitHub #824).
func TestBind_RefusesAPointerThroughTheFieldsBesideAPathItemRef(t *testing.T) {
	t.Parallel()
	cfg := self
	cfg.Beside = func(p jsontext.Pointer) (jsontext.Pointer, string, bool) {
		if p == "/paths/~1a/get/responses/200/content/application~1json/schema" ||
			p == "/paths/~1a/get/responses/200/content/application~1json/schema/properties/d" {
			return "/paths/~1a", "get", true
		}
		return "", "", false
	}
	table := bound(t, `openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /a:
    $ref: '#/components/pathItems/P'
    get:
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema: {$defs: {X: {}}, properties: {d: {$ref: '#/$defs/X'}}}
components:
  pathItems: {P: {}}
  schemas:
    S: {$ref: '#/paths/~1a/get/responses/200/content/application~1json/schema'}
`, cfg)
	s := entryAt(t, table, "/components/schemas/S")
	assert.Equal(t, bind.BesideRef, s.Hop.Reason)
	assert.Equal(t, jsontext.Pointer("/paths/~1a"), s.Hop.Use)
	assert.Equal(t, "get", s.Hop.Key)
	assert.Contains(t, s.Hop.Why(), "it reaches /paths/~1a/get/responses/200/content/application~1json/schema; ")
	d := entryAt(t, table, "/paths/~1a/get/responses/200/content/application~1json/schema/properties/d")
	assert.Equal(t, bind.BesideDefs, d.Hop.Reason)
	assert.Contains(t, d.Hop.Why(), bind.BesideReason("/paths/~1a", "get"))
}

// TestBind_LeavesAReferenceObjectWithoutAnEnd pins a pointer through a
// reference object the load phase reports no end for, or one with no
// Config.Through at all: it lands nowhere.
func TestBind_LeavesAReferenceObjectWithoutAnEnd(t *testing.T) {
	t.Parallel()
	table := bound(t, throughSpec, self)
	assert.Equal(t, bind.Unpassable, entryAt(t, table, "/components/schemas/Body").Hop.Reason)
	cfg := throughConfig()
	cfg.Through = func(jsontext.Pointer) (string, jsontext.Pointer, bool) { return "gone.yaml", "/R", true }
	assert.Equal(t, bind.Unpassable, entryAt(t, bound(t, throughSpec, cfg), "/components/schemas/Body").Hop.Reason,
		"an end in a document that cannot be read")
	cfg.Through = func(jsontext.Pointer) (string, jsontext.Pointer, bool) { return "other.yaml", "/Missing", true }
	assert.Equal(t, bind.Unpassable, entryAt(t, bound(t, throughSpec, cfg), "/components/schemas/Body").Hop.Reason,
		"an end the document holds nothing at")
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
// holding a '#' read as a $ref written at the discriminator, a bare name read by
// the lowering instead, defaultMapping alike, wherever the discriminator is
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
x-lib:
  a: {$ref: '#/x-lib/b'}
  b: {type: object}
  h: {$ref: '#/components/schemas/Holder/properties/p'}
  Box: {discriminator: {propertyName: k, mapping: {i: '#/x-lib/b'}}}
`, self)
	disc := at("/components/schemas/P/discriminator")
	for value, want := range map[string]bind.Chain{
		"#/x-lib/a":       {Status: bind.Resolved, End: at("/x-lib/b"), Role: bind.Raw, Hops: 2},
		"#/$defs/D":       {Status: bind.Resolved, End: at("/components/schemas/P/$defs/D"), Role: bind.Schema, Hops: 1},
		"#/x-lib/h":       {Status: bind.Resolved, End: at("/components/schemas/Holder/$defs/X"), Role: bind.Schema, Hops: 3},
		"#/x-lib/b":       {Status: bind.Resolved, End: at("/x-lib/b"), Role: bind.Raw, Hops: 1},
		"#/x-lib/missing": {Status: bind.Unresolved, Failed: disc},
	} {
		m, ok := table.Mapping(disc, value)
		require.True(t, ok, value)
		assert.Equal(t, want, m.Chain, value)
		assert.Equal(t, value, m.Value)
	}
	_, ok := table.Mapping(disc, "Cat")
	assert.False(t, ok, "a bare name is the lowering's to read")
	_, ok = table.Mapping(at("/x-lib/Box/discriminator"), "#/x-lib/b")
	assert.True(t, ok, "a discriminator in raw YAML a $ref lands on is read")
	_, ok = table.Mapping(at("/components/schemas/Q/discriminator"), "#/x-lib/b")
	assert.True(t, ok, "and every other discriminator")
}

// TestBind_PlacesEachLandingAtTheLeastSourcePositionReachingIt pins the
// landings a table reports, positions a schema has to be built for: each raw
// YAML or other document's position some hop lands on, placed at the least
// source $ref or mapping value whose chain reaches it, and marked unreached
// when only another document's content leads there.
func TestBind_PlacesEachLandingAtTheLeastSourcePositionReachingIt(t *testing.T) {
	t.Parallel()
	cfg := withDocuments(map[string]string{"other.yaml": "A: {$ref: '#/B'}\nB: {type: string}\nC: {$ref: '#/D'}\nD: {}\n"})
	cfg.Through = func(p jsontext.Pointer) (string, jsontext.Pointer, bool) {
		return "other.yaml", "/R", p == "/components/responses/R"
	}
	cfg.Open = withRaw(cfg.Open, "other.yaml", "A: {$ref: '#/B'}\nB: {type: string}\nC: {$ref: '#/D'}\nD: {}\n"+
		"R: {description: ok, content: {a/b: {schema: {$ref: '#/C'}}}}\n")
	table := bound(t, head+`components:
  responses:
    R: {$ref: 'other.yaml#/R'}
  schemas:
    Z: {$ref: '#/x-lib/S'}
    M: {$ref: 'other.yaml#/A'}
    P: {discriminator: {propertyName: k, mapping: {s: '#/x-lib/S'}}}
x-lib:
  S: {type: string}
`, cfg)
	other, ok := table.Find("other.yaml")
	require.True(t, ok)
	got := map[bind.Position]bind.Landing{}
	for _, l := range table.Landings() {
		got[l.At] = l
	}
	assert.Equal(t, bind.Landing{At: at("/x-lib/S"), Name: "spec.yaml", Site: at("/components/schemas/P/discriminator"),
		Reached: true}, got[at("/x-lib/S")], "the least of a mapping value and a $ref reaching it")
	assert.Equal(t, at("/components/schemas/M"), got[bind.Position{Doc: other, Pointer: "/B"}].Site,
		"reached through a chain")
	assert.False(t, got[bind.Position{Doc: other, Pointer: "/D"}].Reached,
		"only what R resolved to leads there")
}

// withRaw is open answering name with src instead.
func withRaw(open func(string) (*yaml.Node, error), name, src string) func(string) (*yaml.Node, error) {
	return func(n string) (*yaml.Node, error) {
		if n != name {
			return open(n)
		}
		var node yaml.Node
		if err := yaml.Unmarshal([]byte(src), &node); err != nil {
			return nil, err
		}
		return &node, nil
	}
}
