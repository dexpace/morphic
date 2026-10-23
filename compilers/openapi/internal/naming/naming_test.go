package naming_test

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/naming"
	"github.com/dexpace/morphic/engine/reader"
)

// head is the part of every fixture before its paths.
const head = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n"

// table loads spec as root.yaml beside others, each written to disk under its
// name, and returns the table the load bound.
func table(t *testing.T, spec string, others map[string]string) *bind.Table {
	t.Helper()
	dir := t.TempDir()
	for name, body := range others {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: filepath.Join(dir, "root.yaml"),
		Data: []byte(spec)}, load.Options{Reader: reader.Disk{}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc.Bindings.Table()
}

// at is the source's position at pointer.
func at(pointer string) bind.Position {
	return bind.Position{Doc: bind.Source, Pointer: jsontext.Pointer(pointer)}
}

// docNamed returns the number of the document the table read as name.
func docNamed(t *testing.T, tb *bind.Table, name string) bind.DocID {
	t.Helper()
	for id := range tb.Documents() {
		if strings.HasSuffix(tb.Document(bind.DocID(id)).Name, "/"+name) {
			return bind.DocID(id)
		}
	}
	require.FailNow(t, "the load read no document named "+name)
	return 0
}

// TestAt_NamesASchemaByItsStructure pins the walk down a schema from its
// owner: each step composes the enclosing name by role (items,
// additionalProperties, contentSchema, a patternProperties entry, a
// prefixItems slot) or, for a keyed map, takes the key itself. A key is never
// read as a keyword (GitHub #518), and a branch holding no $ref is named
// variant_N. A namer with no table names these as one with the document does,
// since the owner is the component.
func TestAt_NamesASchemaByItsStructure(t *testing.T) {
	t.Parallel()
	tb := table(t, head+"paths: {}\ncomponents: {schemas: {Foo: {type: object}}}\n", nil)
	tests := []struct {
		name, pointer, want string
	}{
		{"a bare component", "/components/schemas/Foo", "Foo"},
		{"items under a component", "/components/schemas/Foo/items", compile.SubHint("Foo", "item")},
		{"nested roles", "/components/schemas/Foo/items/additionalProperties",
			compile.SubHint(compile.SubHint("Foo", "item"), "value")},
		{"a property literally named items is not the items keyword", "/components/schemas/Foo/properties/items", "items"},
		{"a property named items holding an array names its own items by role",
			"/components/schemas/Foo/properties/items/items", compile.SubHint("items", "item")},
		{"a property literally named properties holding items names its own items by role",
			"/components/schemas/Foo/properties/properties/items", compile.SubHint("properties", "item")},
		{"a property named additionalProperties whose own additionalProperties is a role",
			"/components/schemas/Foo/properties/additionalProperties/additionalProperties",
			compile.SubHint("additionalProperties", "value")},
		{"a patternProperties entry whose pattern holds ~1", "/components/schemas/Foo/patternProperties/a~1b",
			compile.SubHint("Foo", "pattern")},
		{"a patternProperties entry keyed items is not the items keyword",
			"/components/schemas/Foo/patternProperties/items", compile.SubHint("Foo", "pattern")},
		{"an items entry inside a patternProperties entry keyed items",
			"/components/schemas/Foo/patternProperties/items/items",
			compile.SubHint(compile.SubHint("Foo", "pattern"), "item")},
		{"a $defs entry keyed items", "/components/schemas/Foo/$defs/items", "items"},
		{"a dependentSchemas entry keyed items", "/components/schemas/Foo/dependentSchemas/items", "items"},
		{"a definitions entry keyed items", "/components/schemas/Foo/definitions/items", "items"},
		{"a dependencies entry keyed items", "/components/schemas/Foo/dependencies/items", "items"},
		{"a contentSchema", "/components/schemas/Foo/contentSchema", compile.SubHint("Foo", "content")},
		{"a prefixItems slot", "/components/schemas/Foo/prefixItems/2", compile.SubHint("Foo", "2")},
		{"a prefixItems member that is no slot is named after its own token",
			"/components/schemas/Foo/prefixItems/x", "x"},
		{"an enum member a literal is lowered to", "/components/schemas/Foo/enum/1", compile.SubHint("Foo", "1")},
		{"a variant of a multi-typed schema is named as the schema is", "/components/schemas/Foo/type/1", "Foo"},
		{"a component literally named items", "/components/schemas/items", "items"},
		{"a oneOf branch holding no $ref is named variant_N", "/components/schemas/Foo/oneOf/0", "variant_0"},
		{"an anyOf branch", "/components/schemas/Foo/anyOf/12", "variant_12"},
		{"an allOf branch", "/components/schemas/Foo/allOf/1", "variant_1"},
		{"items beneath a branch is named off the branch",
			"/components/schemas/Foo/oneOf/0/items", compile.SubHint("variant_0", "item")},
		{"a branch reached through a property named oneOf",
			"/components/schemas/Foo/properties/oneOf/anyOf/0", "variant_0"},
		{"a property named oneOf is not a branch", "/components/schemas/Foo/properties/oneOf", "oneOf"},
		{"a non-numeric child of a composition keyword is not a branch", "/components/schemas/Foo/oneOf/x", "x"},
		{"a negative index is not a branch", "/components/schemas/Foo/oneOf/-1", "-1"},
		{"an empty index is not a branch", "/components/schemas/Foo/oneOf/", ""},
		{"an unrecognised keyword is named after its own token", "/components/schemas/Foo/not/items",
			compile.SubHint("not", "item")},
		{"an extension keyword likewise", "/components/schemas/Foo/x-ext/items", compile.SubHint("x-ext", "item")},
		{"a keyed keyword with no key names itself", "/components/schemas/Foo/properties", "properties"},
		{`the component keyed ""`, "/components/schemas//items", compile.SubHint("", "item")},
		{"the schemas map holds no schema to own it", "/components/schemas", "schemas"},
		{"the empty pointer", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, naming.New(tb).At(at(tc.pointer)), "read")
			assert.Equal(t, tc.want, naming.New(nil).At(at(tc.pointer)), "by pointer alone")
		})
	}
}

// ownersSpec writes a schema under every owner the grammar names one for.
const ownersSpec = head + `paths:
  /a:
    parameters:
      - {name: shared, in: query, schema: {type: object}}
    post:
      operationId: postA
      parameters:
        - {name: limit, in: query, schema: {type: object}}
        - {name: filter, in: query, content: {application/json: {schema: {type: object}}}}
        - $ref: '#/components/parameters/P'
      requestBody:
        content:
          application/json: {schema: {type: object}}
          application/jsonl: {itemSchema: {type: object}}
      responses:
        "200":
          description: ok
          headers:
            X-Rate: {schema: {type: object}}
            X-Body: {content: {application/json: {schema: {type: object}}}}
          content:
            application/json: {schema: {type: object}}
      callbacks:
        onEvent:
          '{$request.body#/url}':
            post:
              operationId: notify
              requestBody: {content: {application/json: {schema: {type: object}}}}
              responses: {"200": {description: ok}}
    put:
      requestBody: {content: {application/json: {schema: {type: object}}}}
      responses: {"200": {description: ok}}
webhooks:
  hook:
    post:
      operationId: onHook
      requestBody: {content: {application/json: {schema: {type: object}}}}
      responses: {"200": {description: ok}}
components:
  parameters:
    P: {name: page, in: query, schema: {type: object}}
  requestBodies:
    Body: {content: {application/json: {schema: {type: object}}}}
  responses:
    Err: {description: e, content: {application/json: {schema: {type: object}}}}
  headers:
    Trace: {schema: {type: object}}
  pathItems:
    Item:
      get:
        operationId: getItem
        requestBody: {content: {application/json: {schema: {type: object}}}}
        responses: {"200": {description: ok}}
`

// TestAt_NamesASchemaAfterItsOwner pins the owner a schema position is named
// for, as the grammar reads where it is written: a request body by its
// component's key or the operationId of the operation declaring it, else
// "request"; a response by its component's key, else "response"; a parameter
// by its name, its content included; a header by its key; an itemSchema as
// its media type's item. Each holds wherever the operation is written: a path
// item, a callback, a webhook or a component path item.
func TestAt_NamesASchemaAfterItsOwner(t *testing.T) {
	t.Parallel()
	tb := table(t, ownersSpec, nil)
	const op = "/paths/~1a/post"
	const json = "/content/application~1json/schema"
	tests := []struct {
		name, pointer, want string
	}{
		{"a request body, by its operation", op + "/requestBody" + json, "postA_request"},
		{"beneath it, off that name", op + "/requestBody" + json + "/properties/x/items", compile.SubHint("x", "item")},
		{"a pattern's schema beneath it, off that name", op + "/requestBody" + json + "/patternProperties/a",
			compile.SubHint("postA_request", "pattern")},
		{"an enum literal of it, off that name", op + "/requestBody" + json + "/enum/1",
			compile.SubHint("postA_request", "1")},
		{"a variant of it multi-typed, by that name", op + "/requestBody" + json + "/type/1", "postA_request"},
		{"an itemSchema, as its media type's item", op + "/requestBody/content/application~1jsonl/itemSchema",
			compile.SubHint("postA_request", "item")},
		{"a request body no operationId names", "/paths/~1a/put/requestBody" + json, "request"},
		{"a component request body, by its key", "/components/requestBodies/Body" + json, "Body"},
		{"a response", op + "/responses/200" + json, "response"},
		{"a tuple slot beneath it, off that name", op + "/responses/200" + json + "/prefixItems/1",
			compile.SubHint("response", "1")},
		{"a component response, by its key", "/components/responses/Err" + json, "Err"},
		{"a parameter, by its name", op + "/parameters/0/schema", "limit"},
		{"a parameter's content", op + "/parameters/1" + json, "filter"},
		{"a path item's parameter", "/paths/~1a/parameters/0/schema", "shared"},
		{"a component parameter, by its name", "/components/parameters/P/schema", "page"},
		{"a header, by its key", op + "/responses/200/headers/X-Rate/schema", "X-Rate"},
		{"a header's content", op + "/responses/200/headers/X-Body" + json, "X-Body"},
		{"a component header, by its key", "/components/headers/Trace/schema", "Trace"},
		{"a callback's request body", op + "/callbacks/onEvent/{$request.body#~1url}/post/requestBody" + json,
			"notify_request"},
		{"a webhook's request body", "/webhooks/hook/post/requestBody" + json, "onHook_request"},
		{"a component path item's request body", "/components/pathItems/Item/get/requestBody" + json,
			"getItem_request"},
		{"a position the grammar reads as no schema, from its pointer", op + "/responses/200/description", "description"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, naming.New(tb).At(at(tc.pointer)))
		})
	}
}

// TestAt_AnswersAsAFreshNamerWould pins that a name At remembers is the one a
// Namer that never asked gives.
func TestAt_AnswersAsAFreshNamerWould(t *testing.T) {
	t.Parallel()
	tb := table(t, ownersSpec, nil)
	p := at("/paths/~1a/post/requestBody/content/application~1json/schema")
	read := naming.New(tb)
	first := read.At(p)
	assert.Equal(t, "postA_request", first)
	assert.Equal(t, first, read.At(p), "asked again")
	assert.Equal(t, first, naming.New(tb).At(p), "asked of a fresh namer")
}

// TestAt_ReadsAnOwnerAsTheParserDoes pins that an owner's name is read from
// the document as the parser reads it: through an alias and a `<<` merge, a
// number as the text it spells, and not at all where the field is null, no
// scalar, or written in a mapping merged from past the depth the parser reads.
func TestAt_ReadsAnOwnerAsTheParserDoes(t *testing.T) {
	t.Parallel()
	spec := head + `x-names: {id: &id aliased}
paths:
  /a:
    post:
      operationId: *id
      requestBody: {content: {application/json: {schema: {type: object}}}}
      responses: {"200": {description: ok}}
      parameters:
        - {<<: {name: merged}, in: query, schema: {type: object}}
        - {name: ~, in: query, schema: {type: object}}
        - {name: [a], in: query, schema: {type: object}}
    put:
      operationId: 7
      requestBody: {content: {application/json: {schema: {type: object}}}}
      responses: {"200": {description: ok}}
      parameters:
        - {name: 42, in: query, schema: {type: object}}
`
	read := naming.New(table(t, spec, nil))
	assert.Equal(t, "aliased_request", read.At(at("/paths/~1a/post/requestBody/content/application~1json/schema")))
	assert.Equal(t, "merged", read.At(at("/paths/~1a/post/parameters/0/schema")))
	assert.Empty(t, read.At(at("/paths/~1a/post/parameters/1/schema")), "a null name names nothing")
	assert.Empty(t, read.At(at("/paths/~1a/post/parameters/2/schema")), "nor does one that is no scalar")
	assert.Equal(t, "7_request", read.At(at("/paths/~1a/put/requestBody/content/application~1json/schema")))
	assert.Equal(t, "42", read.At(at("/paths/~1a/put/parameters/0/schema")))
}

// TestAt_NamesABranchAfterItsTarget pins that a composition branch holding a
// $ref takes the name of the position the $ref lands on (GitHub #521, #729),
// beneath a component or not, in this document or another, whose root is
// named for the document; keeps its own where that names nothing; and, where
// the $ref lands on no position, takes the name its text suggests: the
// position a fragment spells, decoded at both layers (GitHub #505), else what
// follows its last '/', decoded when that is UTF-8 (GitHub #520).
func TestAt_NamesABranchAfterItsTarget(t *testing.T) {
	t.Parallel()
	const under = "/paths/~1x/get/responses/200/content/application~1json/schema"
	tests := []struct {
		name, at, to, want string
	}{
		{"a branch beneath a component", "/components/schemas/X/oneOf/0", "#/components/schemas/Y", "Y"},
		{"a branch elsewhere", under + "/oneOf/0", "#/components/schemas/Y", "Y"},
		{"a branch landing beneath a response", "/components/schemas/X/oneOf/0", "#" + under + "/items",
			compile.SubHint("response", "item")},
		{"a branch landing on another branch", "/components/schemas/X/oneOf/0", "#/components/schemas/Z/anyOf/0", "Y"},
		{"a branch whose target names nothing", "/components/schemas/X/oneOf/0", "#/components/schemas/", "variant_0"},
		{"a branch landing in another document", "/components/schemas/X/oneOf/0", "./other.yaml#/Pet", "Pet"},
		{"a branch landing on another document's root", "/components/schemas/X/oneOf/0", "./other.yaml", "other.yaml"},
		{"a branch landing on another document's root by an empty fragment", "/components/schemas/X/oneOf/0",
			"./other.yaml#", "other.yaml"},
		{"an unread document, by its fragment", "/components/schemas/X/oneOf/0",
			"./absent.yaml#/components/schemas/Cat~1Dog", "Cat/Dog"},
		{"an unread document, a percent escape decoded", "/components/schemas/X/oneOf/0",
			"./absent.yaml#/components/schemas/Fish%2DTank", "Fish-Tank"},
		{"an unread document, a structural position by its role", "/components/schemas/X/oneOf/0",
			"./absent.yaml#/components/schemas/A/items", compile.SubHint("A", "item")},
		{"an unread document, a branch by its position", "/components/schemas/X/oneOf/0",
			"./absent.yaml#/components/schemas/A/oneOf/3", "variant_3"},
		{"an unread document, no fragment", "/components/schemas/X/oneOf/0", "./schemas/Fish%2DTank.yaml", "Fish-Tank.yaml"},
		{"an unread document whose escape is invalid", "/components/schemas/X/oneOf/0", "./bad%ZZ.yaml", "bad%ZZ.yaml"},
		{"an unread document whose escape is not UTF-8", "/components/schemas/X/oneOf/0", "./x%FF.yaml", "x%FF.yaml"},
		{"an unread document, a fragment that is not UTF-8", "/components/schemas/X/oneOf/0",
			"./absent.yaml#/components/schemas/%FF", "%FF"},
		{"an unread document, a lone slash", "/components/schemas/X/oneOf/0", "./absent.yaml#/", "variant_0"},
		{"an unread document, a $anchor", "/components/schemas/X/oneOf/0", "./absent.yaml#anchor", "absent.yaml#anchor"},
		{"a position that is no branch keeps its own", "/components/schemas/X/items", "#/components/schemas/Y",
			compile.SubHint("X", "item")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			read := naming.New(table(t, branchSpec(tc.at, tc.to), map[string]string{
				"other.yaml": "Pet: {type: object}\n",
			}))
			assert.Equal(t, tc.want, read.At(at(tc.at)))
		})
	}
}

// branchSpec writes a $ref to to at, a composition branch or an items
// position, beneath the component X or a response, beside the schema Y, a
// branch of Z holding a $ref to Y, and the component keyed "".
func branchSpec(at, to string) string {
	holder := "{oneOf: [{$ref: '" + to + "'}]}"
	if strings.HasSuffix(at, "/items") {
		holder = "{type: array, items: {$ref: '" + to + "'}}"
	}
	paths := "  /x: {get: {responses: {'200': {description: ok, content: {application/json: {schema: " +
		"{type: array, items: {type: string}}}}}}}}\n"
	x := ""
	if strings.HasPrefix(at, "/components/schemas/X") {
		x = "    X: " + holder + "\n"
	} else {
		paths = "  /x: {get: {responses: {'200': {description: ok, content: {application/json: {schema: " +
			holder + "}}}}}}\n"
	}
	return head + "paths:\n" + paths + "components:\n  schemas:\n" + x +
		"    Y: {type: object}\n    Z: {anyOf: [{$ref: '#/components/schemas/Y'}]}\n    \"\": {type: object}\n"
}

// TestAt_StopsAtTheHopCap pins MaxHops at its boundary. Each branch holds a
// $ref to the next and the last to T: the first is named after T while T is
// within MaxHops hops, and keeps its own name once it is not. The second
// branch starts a hop further on, so it still reaches T, whichever of the two
// is asked first: a name a capped chain reaches on the way is not remembered.
func TestAt_StopsAtTheHopCap(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		branches int
		first    string
	}{
		{"T within reach", naming.MaxHops, "T"},
		{"T one hop past the cap", naming.MaxHops + 1, "variant_0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var spec strings.Builder
			spec.WriteString(head + "paths: {}\ncomponents:\n  schemas:\n")
			for i := range tc.branches {
				next := "#/components/schemas/T"
				if i+1 < tc.branches {
					next = "#/components/schemas/S" + strconv.Itoa(i+1) + "/oneOf/0"
				}
				spec.WriteString("    S" + strconv.Itoa(i) + ": {oneOf: [{$ref: '" + next + "'}]}\n")
			}
			spec.WriteString("    T: {type: object}\n")
			tb := table(t, spec.String(), nil)
			first, second := at("/components/schemas/S0/oneOf/0"), at("/components/schemas/S1/oneOf/0")

			read := naming.New(tb)
			assert.Equal(t, tc.first, read.At(first))
			assert.Equal(t, "T", read.At(second), "asked after the first")
			reversed := naming.New(tb)
			assert.Equal(t, "T", reversed.At(second))
			assert.Equal(t, tc.first, reversed.At(first), "asked after the second")
		})
	}
}

// TestThrough_IsTheNameARefSuggests pins Through apart from the branch rule it
// serves: the name a $ref suggests wherever it is written, and "" where none
// is written or the one written suggests none.
func TestThrough_IsTheNameARefSuggests(t *testing.T) {
	t.Parallel()
	read := naming.New(table(t, head+`paths: {}
components:
  schemas:
    A: {allOf: [{$ref: '#/components/schemas/B'}, {$ref: '#/components/schemas/'}, {type: object}]}
    B: {type: object}
    "": {type: object}
    C: {$ref: '#/components/schemas/B'}
`, nil))
	assert.Equal(t, "B", read.Through(at("/components/schemas/A/allOf/0")))
	assert.Empty(t, read.Through(at("/components/schemas/A/allOf/1")), `a component keyed "" names nothing`)
	assert.Empty(t, read.Through(at("/components/schemas/A/allOf/2")), "no $ref is written there")
	assert.Equal(t, "B", read.Through(at("/components/schemas/C")), "a $ref that is no branch suggests a name too")
	assert.Empty(t, naming.New(nil).Through(at("/components/schemas/A/allOf/0")), "a namer with no table reads none")
}

// TestAt_NamesWhatAReferenceReadsAsASchema pins positions the grammar reads as
// raw YAML, or in another document, named as the reference reaching them
// reads them (bind.Table.Kinds): a schema an extension holds is named from
// its pointer, as no owner names it, a parameter another document holds by
// its name, and a document a reference reads as a schema after the document.
func TestAt_NamesWhatAReferenceReadsAsASchema(t *testing.T) {
	t.Parallel()
	tb := table(t, head+`x-lib: {Thing: {type: object, properties: {p: {type: object}}}}
paths:
  /a:
    get:
      parameters: [{$ref: './params.yaml#/Size'}]
      responses: {"200": {description: ok}}
components:
  schemas:
    A: {$ref: '#/x-lib/Thing'}
    B: {$ref: './pet.yaml'}
    C: {$ref: './list.yaml'}
`, map[string]string{
		"list.yaml":   "type: array\nitems: {type: object}\n",
		"params.yaml": "Size: {name: size, in: query, schema: {type: object, properties: {q: {type: object}}}}\n",
		"pet.yaml":    "type: object\nproperties: {tag: {type: object, items: {type: string}}}\n",
	})
	read := naming.New(tb)
	params, pet, list := docNamed(t, tb, "params.yaml"), docNamed(t, tb, "pet.yaml"), docNamed(t, tb, "list.yaml")

	assert.Equal(t, "Thing", read.At(at("/x-lib/Thing")), "an extension's schema is named from its pointer")
	assert.Equal(t, "p", read.At(at("/x-lib/Thing/properties/p")))
	assert.Equal(t, "size", read.At(bind.Position{Doc: params, Pointer: "/Size/schema"}))
	assert.Equal(t, compile.SubHint("q", "item"),
		read.At(bind.Position{Doc: params, Pointer: "/Size/schema/properties/q/items"}))
	assert.Equal(t, "pet.yaml", read.At(bind.Position{Doc: pet}), "a document's root schema is named for the document")
	assert.Equal(t, compile.SubHint("tag", "item"), read.At(bind.Position{Doc: pet, Pointer: "/properties/tag/items"}))
	assert.Equal(t, compile.SubHint("list.yaml", "item"), read.At(bind.Position{Doc: list, Pointer: "/items"}),
		"the root's items are named by role, not as a key")
}

// TestAt_NamesAStructureAReferenceReadsInsideRawYAML pins that a reference
// reading raw YAML as an owner starts a structure of its own: a parameter, a
// request body or a response kept under a schema's x- key is named as one
// written in its own place is, not after the schema holding the raw YAML, and
// so is a parameter in a document another reference reads whole as a schema.
func TestAt_NamesAStructureAReferenceReadsInsideRawYAML(t *testing.T) {
	t.Parallel()
	tb := table(t, head+`paths:
  /a:
    post:
      parameters:
        - $ref: '#/components/schemas/S/x-param'
        - $ref: './lib.yaml#/x-params/P'
      requestBody: {$ref: '#/components/schemas/S/x-body'}
      responses:
        "200": {$ref: '#/components/schemas/S/x-resp'}
components:
  schemas:
    S:
      type: object
      x-param: {name: foo, in: query, schema: {type: object}}
      x-body: {content: {application/json: {schema: {type: object}}}}
      x-resp: {description: r, content: {application/json: {schema: {type: array, items: {type: object}}}}}
    L: {$ref: './lib.yaml'}
`, map[string]string{
		"lib.yaml": "type: object\nx-params:\n  P: {name: page, in: query, schema: {type: object}}\n",
	})
	read := naming.New(tb)
	lib := docNamed(t, tb, "lib.yaml")
	const json = "/content/application~1json/schema"

	assert.Equal(t, "foo", read.At(at("/components/schemas/S/x-param/schema")))
	assert.Equal(t, "request", read.At(at("/components/schemas/S/x-body"+json)))
	assert.Equal(t, compile.SubHint("response", "item"), read.At(at("/components/schemas/S/x-resp"+json+"/items")))
	assert.Equal(t, "page", read.At(bind.Position{Doc: lib, Pointer: "/x-params/P/schema"}),
		"a parameter in a document read whole as a schema")
	assert.Equal(t, "lib.yaml", read.At(bind.Position{Doc: lib}), "the document read whole")
}
