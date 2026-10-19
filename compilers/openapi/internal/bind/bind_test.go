package bind_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
)

const head = "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\n"

// tree decodes src.
func tree(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var n yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &n))
	return &n
}

// entryAt binds src and returns the entry at site.
func entryAt(t *testing.T, src string, cfg bind.Config, site jsontext.Pointer) bind.Entry {
	t.Helper()
	for _, e := range bind.Bind(bind.New(tree(t, src), cfg)).Entries() {
		if e.Site == site {
			return e
		}
	}
	require.Failf(t, "no entry", "at %s", site)
	return bind.Entry{}
}

// TestResolve_ReadsEachReferenceAsTheCompilerDoes pins one row per reading:
// where each chain ends, the role of what it ends at, the position its first
// $ref names, and how a chain that ends nowhere is reported.
func TestResolve_ReadsEachReferenceAsTheCompilerDoes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, body    string
		status        bind.Status
		first, target jsontext.Pointer
		role          bind.Role
		hops          int
	}{
		{"a component", "    A: {$ref: '#/components/schemas/B'}\n    B: {type: string}\n",
			bind.Resolved, "/components/schemas/B", "/components/schemas/B", bind.Schema, 1},
		{"a chain", "    A: {$ref: '#/components/schemas/B'}\n    B: {$ref: '#/components/schemas/C'}\n    C: {}\n",
			bind.Resolved, "/components/schemas/B", "/components/schemas/C", bind.Schema, 2},
		{"raw YAML under an extension", "    A: {$ref: '#/x-lib/S'}\nx-lib:\n  S: {type: string}\n",
			bind.Resolved, "/x-lib/S", "/x-lib/S", bind.Raw, 1},
		{"a literal", "    A: {$ref: '#/components/schemas/B/example'}\n    B: {example: {type: string}}\n",
			bind.Resolved, "/components/schemas/B/example", "/components/schemas/B/example", bind.Raw, 1},
		{"a boolean schema", "    A: {$ref: '#/components/schemas/B/properties/p'}\n    B: {properties: {p: true}}\n",
			bind.Resolved, "/components/schemas/B/properties/p", "/components/schemas/B/properties/p", bind.Schema, 1},
		{"an object no schema reference names", "    A: {$ref: '#/info'}\n", bind.Unresolved, "", "", bind.NoRole, 0},
		{"the document", "    A: {$ref: '#'}\n", bind.Unresolved, "", "", bind.NoRole, 0},
		{"a lone slash, read as the document", "    A: {$ref: '#/'}\n'': {type: string}\n", bind.Unresolved, "", "", bind.NoRole, 0},
		{"a document part only", "    A: {$ref: ''}\n", bind.Unresolved, "", "", bind.NoRole, 0},
		{"nothing there", "    A: {$ref: '#/components/schemas/Z'}\n", bind.Unresolved, "", "", bind.NoRole, 0},
		{"another document", "    A: {$ref: 'other.yaml#/X'}\n", bind.External, "", "", bind.NoRole, 0},
		{"an anchor", "    A: {$ref: '#a'}\n    B: {$anchor: a}\n", bind.Anchor, "", "", bind.NoRole, 0},
		{"a self reference", "    A: {$ref: '#/components/schemas/A'}\n", bind.Cyclic,
			"/components/schemas/A", "", bind.NoRole, 1},
		{"a loop it runs into", "    R: {$ref: '#/components/schemas/A'}\n    A: {$ref: '#/components/schemas/B'}\n" +
			"    B: {$ref: '#/components/schemas/A'}\n", bind.Cyclic, "/components/schemas/A", "", bind.NoRole, 3},
		{"a chain that ends nowhere", "    A: {$ref: '#/components/schemas/B'}\n    B: {$ref: '#/components/schemas/Z'}\n",
			bind.Unresolved, "/components/schemas/B", "", bind.NoRole, 1},
		{"a percent escape, and '+' as itself", "    A: {$ref: '#/components/schemas/a%20b+c'}\n    'a b+c': {}\n",
			bind.Resolved, "/components/schemas/a b+c", "/components/schemas/a b+c", bind.Schema, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := head + "components:\n  schemas:\n" + tc.body
			e := entryAt(t, src, bind.Config{}, "/components/schemas/"+jsontext.Pointer(firstKey(t, tc.body)))
			assert.Equal(t, tc.status, e.Status, e.Status.String())
			assert.Equal(t, tc.first, e.First)
			assert.Equal(t, tc.target, e.Target)
			assert.Equal(t, tc.role, e.Role)
			assert.Equal(t, tc.hops, e.Hops)
		})
	}
}

// firstKey is the first schema name body declares.
func firstKey(t *testing.T, body string) string {
	t.Helper()
	var m yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(body), &m))
	return m.Content[0].Content[0].Value
}

// TestResolve_ReadsADefinitionByTheDefsRule pins the rule internal/defs
// documents: a resource's own definitions, then the document's root, then the
// nearest schema above holding a schema there, whatever was resolved before.
func TestResolve_ReadsADefinitionByTheDefsRule(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, body string
		site, want jsontext.Pointer
	}{
		{"the nearest ancestor", "components:\n  schemas:\n    H: {$defs: {A: {}}, properties: {p: {$ref: '#/$defs/A'}}}\n",
			"/components/schemas/H/properties/p", "/components/schemas/H/$defs/A"},
		{"the root before an ancestor", "$defs:\n  A: {}\ncomponents:\n  schemas:\n    H: {$defs: {A: {}}, properties: {p: {$ref: '#/$defs/A'}}}\n",
			"/components/schemas/H/properties/p", "/$defs/A"},
		{"the root before the schema's own", "$defs:\n  A: {}\ncomponents:\n  schemas:\n    B: {$ref: '#/$defs/A', $defs: {A: {}}}\n",
			"/components/schemas/B", "/$defs/A"},
		{"a resource's own first", "$defs:\n  A: {}\ncomponents:\n  schemas:\n    B: {$id: 'https://x.test/b', $ref: '#/$defs/A', $defs: {A: {}}}\n",
			"/components/schemas/B", "/components/schemas/B/$defs/A"},
		{"a resource's own, a trailing slash read as none", "components:\n  schemas:\n    B: {$id: 'https://x.test/b', $ref: '#/$defs/A/', $defs: {A: {}}}\n",
			"/components/schemas/B", "/components/schemas/B/$defs/A"},
		{"a resource with none of its own", "$defs:\n  A: {}\ncomponents:\n  schemas:\n    B: {$id: 'https://x.test/b', $ref: '#/$defs/A'}\n",
			"/components/schemas/B", "/$defs/A"},
		{"one sitting in a nested resource", "components:\n  schemas:\n    B: {$id: 'https://x.test/b', $defs: {A: {}}, properties: {p: {$id: 'p', " +
			"properties: {q: {$ref: '#/$defs/A', $defs: {A: {}}}, r: {$ref: '#/$defs/A', $defs: {A: {}}}}}}}\n",
			"/components/schemas/B/properties/p/properties/q", "/components/schemas/B/properties/p/properties/q/$defs/A"},
		{"one in the outermost resource, which is none of its own", "components:\n  schemas:\n    B: {$id: 'https://x.test/b', $defs: {A: {}}, " +
			"properties: {p: {$ref: '#/$defs/A', $defs: {A: {}}}}}\n",
			"/components/schemas/B/properties/p", "/components/schemas/B/$defs/A"},
		{"an $id of a fragment alone still makes a resource", "components:\n  schemas:\n    B: {$id: '#f', $ref: '#/$defs/A', $defs: {A: {}}}\n",
			"/components/schemas/B", "/components/schemas/B/$defs/A"},
		{"a nested $id of a fragment alone moves no base", "components:\n  schemas:\n    B: {$id: 'https://x.test/b', $defs: {A: {}}, properties: {p: {$id: '#f', " +
			"properties: {q: {$ref: '#/$defs/A', $defs: {A: {}}}}}}}\n",
			"/components/schemas/B/properties/p/properties/q", "/components/schemas/B/$defs/A"},
		{"an $id that is no URI reference keeps the base", "components:\n  schemas:\n    B: {$defs: {A: {}}, properties: {p: {$id: '%zz', " +
			"properties: {q: {$ref: '#/$defs/A', $defs: {A: {}}}}}}}\n",
			"/components/schemas/B/properties/p/properties/q", "/components/schemas/B/$defs/A"},
		{"a path into a definition", "components:\n  schemas:\n    H: {$defs: {A: {properties: {x: {}}}}, properties: {p: {$ref: '#/$defs/A/properties/x'}}}\n",
			"/components/schemas/H/properties/p", "/components/schemas/H/$defs/A/properties/x"},
		{"past an ancestor holding no schema there", "components:\n  schemas:\n    H: {$defs: {A: {type: [string]}}, properties: {p: {$ref: '#/$defs/A/type'}}}\n",
			"/components/schemas/H/properties/p", ""},
		{"past an ancestor holding raw YAML there", "components:\n  schemas:\n    H: {$defs: {A: {example: {}}}, properties: {p: {$ref: '#/$defs/A/example'}}}\n",
			"/components/schemas/H/properties/p", ""},
		{"an ancestor that is a map holding $defs", "components:\n  schemas:\n    H: {properties: {$defs: {properties: {x: {}}}, p: {$ref: '#/$defs/properties/x'}}}\n",
			"/components/schemas/H/properties/p", "/components/schemas/H/properties/$defs/properties/x"},
		{"no ancestor in raw YAML", "x-lib:\n  $defs: {n: {}}\n  S: {$ref: '#/$defs/n'}\ncomponents:\n  schemas:\n    U: {$ref: '#/x-lib/S'}\n",
			"/components/schemas/U", ""},
		{"raw YAML at the root", "$defs:\n  A: {type: string}\ncomponents:\n  schemas:\n    H: {properties: {p: {$ref: '#/$defs/A'}}}\n",
			"/components/schemas/H/properties/p", "/$defs/A"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := entryAt(t, head+tc.body, bind.Config{}, tc.site)
			assert.Equal(t, tc.want, e.Target, e.Status.String())
		})
	}
}

// TestResolve_ReadsADefinitionInRawYAMLByTheSameRule pins a chain through raw
// YAML: a "#/$defs/..." pointer written there is read by the rule from where
// it is written, a raw ancestor answering for nothing and the schema above it
// for its own definitions.
func TestResolve_ReadsADefinitionInRawYAMLByTheSameRule(t *testing.T) {
	t.Parallel()
	src := head + "x-lib:\n  S: {$ref: '#/$defs/n', $defs: {n: {}}}\n" +
		"components:\n  schemas:\n    U: {$ref: '#/x-lib/S'}\n    H: {$defs: {n: {}}, example: {$ref: '#/$defs/n'}}\n" +
		"    V: {$ref: '#/components/schemas/H/example'}\n"
	assert.Equal(t, bind.Unresolved, entryAt(t, src, bind.Config{}, "/components/schemas/U").Status, "raw YAML holds no definitions")
	v := entryAt(t, src, bind.Config{}, "/components/schemas/V")
	assert.Equal(t, jsontext.Pointer("/components/schemas/H/$defs/n"), v.Target, v.Status.String())
}

// TestResolve_NamesTheDocumentAsItIsTold pins Config.Names: a document part it
// accepts is read as this document, any other names another.
func TestResolve_NamesTheDocumentAsItIsTold(t *testing.T) {
	t.Parallel()
	src := head + "components:\n  schemas:\n    A: {$ref: 'spec.yaml#/components/schemas/B'}\n    B: {}\n"
	named := bind.Config{Names: func(uri string) bool { return uri == "spec.yaml" }}
	assert.Equal(t, jsontext.Pointer("/components/schemas/B"), entryAt(t, src, named, "/components/schemas/A").Target)
	assert.Equal(t, bind.External, entryAt(t, src, bind.Config{}, "/components/schemas/A").Status)
}

// TestResolve_ASiteWithNoReferenceResolvesNothing pins Resolve's guard: a
// position writing no $ref, or none of the document's, reads no hop.
func TestResolve_ASiteWithNoReferenceResolvesNothing(t *testing.T) {
	t.Parallel()
	b := bind.New(tree(t, head+"components:\n  schemas:\n    A: {type: string}\n    B: [1]\n"), bind.Config{})
	for _, site := range []jsontext.Pointer{"/components/schemas/A", "/components/schemas/B", "/components/schemas/Z"} {
		e := b.Resolve(site)
		assert.Equal(t, bind.Unresolved, e.Status, site)
		assert.Zero(t, e.Hops, site)
		assert.Empty(t, e.Ref, site)
	}
}

// TestBind_ReadsEverySchemaSiteOnce pins the sites a table holds: every schema
// position writing a $ref, each pointer once and in order, an aliased one at
// every position it is placed at, and none outside a schema position, a null
// $ref none either.
func TestBind_ReadsEverySchemaSiteOnce(t *testing.T) {
	t.Parallel()
	src := "openapi: 3.1.0\ninfo: {title: t, version: '1'}\n" +
		"paths:\n  x-p: {get: {parameters: [{schema: {$ref: '#/x'}}]}}\n  /a:\n    get:\n" +
		"      parameters: [{name: q, in: query, schema: {$ref: '#/components/schemas/A'}}]\n" +
		"      responses:\n        x-r: {content: {a/b: {schema: {$ref: '#/x'}}}}\n" +
		"        '200': {description: d, content: {a/b: {schema: {items: {$ref: '#/components/schemas/A'}}}}}\n" +
		"x-lib: {S: {$ref: '#/components/schemas/B'}}\n" +
		"components:\n  schemas:\n    A: &a {$ref: '#/components/schemas/B'}\n    B: {properties: {x-p: *a, q: {$ref: null}}, allOf: [*a]}\n" +
		"  parameters:\n    P: {$ref: '#/components/parameters/Q'}\n" +
		"  responses:\n    R:\n      description: r\n      headers: {x-h: {schema: {$ref: '#/components/schemas/A'}}}\n"
	table := bind.Bind(bind.New(tree(t, src), bind.Config{}))
	sites := make([]jsontext.Pointer, 0, len(table.Entries()))
	for _, e := range table.Entries() {
		sites = append(sites, e.Site)
	}
	assert.Equal(t, []jsontext.Pointer{"/components/responses/R/headers/x-h/schema", "/components/schemas/A",
		"/components/schemas/B/allOf/0", "/components/schemas/B/properties/x-p",
		"/paths/~1a/get/parameters/0/schema", "/paths/~1a/get/responses/200/content/a~1b/schema/items"}, sites)
	assert.Positive(t, table.Steps())
	assert.False(t, table.Incomplete())
}

// TestBind_StopsAtItsBudget pins the budget: a table the limit cuts says so,
// and a chain it stops is reported over budget rather than resolved.
func TestBind_StopsAtItsBudget(t *testing.T) {
	t.Parallel()
	src := head + "components:\n  schemas:\n    A: {$ref: '#/components/schemas/B'}\n    B: {$ref: '#/components/schemas/C'}\n" +
		"    C: {$defs: {D: {}}, properties: {p: {$ref: '#/$defs/D'}}}\n"
	full := bind.Bind(bind.New(tree(t, src), bind.Config{}))
	require.False(t, full.Incomplete())

	want := map[jsontext.Pointer]bind.Entry{}
	for _, e := range full.Entries() {
		want[e.Site] = e
	}
	var stopped int
	for limit := 1; limit < full.Steps(); limit++ {
		cut := bind.Bind(bind.New(tree(t, src), bind.Config{Limit: limit}))
		require.True(t, cut.Incomplete(), limit)
		for _, e := range cut.Entries() {
			if e.Status == bind.OverBudget {
				stopped++
				assert.Empty(t, e.Target, "limit %d: %s", limit, e.Site)
				continue
			}
			assert.Equal(t, want[e.Site], e, "limit %d: an answer read within the budget is the full one", limit)
		}
	}
	assert.Positive(t, stopped, "some limit stops a chain partway")
}

// TestStatus_String spells every status, and an invalid one as such.
func TestStatus_String(t *testing.T) {
	t.Parallel()
	for s, want := range map[bind.Status]string{0: "invalid", bind.Resolved: "resolved", bind.Unresolved: "unresolved",
		bind.External: "external", bind.Anchor: "anchor", bind.Cyclic: "cyclic", bind.OverBudget: "over-budget", 7: "invalid"} {
		assert.Equal(t, want, s.String())
	}
	for r, want := range map[bind.Role]string{bind.NoRole: "none", bind.Schema: "schema", bind.Raw: "raw", bind.Object: "object", 9: "none"} {
		assert.Equal(t, want, r.String())
	}
}
