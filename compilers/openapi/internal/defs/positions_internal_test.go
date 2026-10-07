package defs

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"
)

// positionsDocs are documents whose containers are met more than once or under
// keys that need escaping, the places an index could disagree with the search
// it replaces: an alias reuses a container, a merge key splices one in, a
// sequence numbers its children, a key holds a '/' or a '~', and an empty key
// is skipped whole, which leaves a container reachable only through an alias.
var positionsDocs = map[string]string{
	"plain": `
openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      type: object
      properties:
        p: {type: string}
        q: {type: array, items: {type: integer}}
`,
	"an alias reuses a container": `
openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A: &a
      type: object
      $defs: {n: {type: string}}
      properties: {p: {$ref: "#/$defs/n"}}
    B: *a
    C:
      properties: {x: *a, y: *a}
`,
	"a merge key splices a container in": `
openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Base: &b
      properties: {z: {type: string}}
    Top:
      <<: *b
      type: object
      $defs: {n: {type: string}}
    Other:
      allOf: [*b, {<<: *b}]
`,
	"sequences number their children": `
openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      oneOf:
        - {type: string}
        - allOf: [{type: integer}, {type: number, $defs: {n: {type: string}}}]
        - &shared {type: boolean}
        - *shared
      prefixItems: [{type: string}, *shared]
`,
	"keys that need escaping": `
openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  "/a~b/{x y}/c":
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                $defs: {"a/b": {type: string}, "c~d": {type: integer}}
                properties: {"e/f~g": {$ref: "#/$defs/a~1b"}}
components:
  schemas:
    "x/y~z": {type: string}
`,
	"an anchor met only through its alias": `
openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      properties:
        "": &hidden {type: object, properties: {inner: {type: string}}}
        shown: *hidden
`,
	"an empty key is skipped whole": `
openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      properties:
        "": {type: string}
        after: {type: integer}
`,
}

// locatable is what a parsed object offers: the node it was built from, and the
// library's own search for where that node sits.
type locatable interface {
	GetRootNode() *yaml.Node
	GetJSONPointer(root *yaml.Node) string
}

// TestPositions_AgreeWithTheLibrariesSearch pins what the index replaces: for
// every schema and discriminator of each document, the pointer it answers is the
// one the library's CoreModel.GetJSONPointer finds by searching the tree, which
// is what the resolver itself reads a $defs pointer from.
func TestPositions_AgreeWithTheLibrariesSearch(t *testing.T) {
	t.Parallel()
	for name, spec := range positionsDocs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(spec))
			require.NoError(t, err)
			r := NewReader(doc)

			var checked, moved int
			check := func(obj locatable, where jsontext.Pointer) {
				want := jsontext.Pointer(obj.GetJSONPointer(doc.GetRootNode()))
				assert.Equal(t, want, r.positionOf(obj), "where the library places the object the walk reached at %q", where)
				checked++
				if want != where {
					moved++
				}
			}
			for item := range soa.Walk(t.Context(), doc) {
				at := jsontext.Pointer(item.Location.ToJSONPointer())
				require.NoError(t, item.Match(soa.Matcher{
					Schema:        func(js *oas3.JSONSchema[oas3.Referenceable]) error { check(js.GetCore(), at); return nil },
					Discriminator: func(d *oas3.Discriminator) error { check(d.GetCore(), at); return nil },
				}))
			}

			assert.Positive(t, checked, "the document holds schemas")
			if strings.Contains(name, "alias") || strings.Contains(name, "merge key") {
				assert.Positive(t, moved, "the library places an object met twice where it was first met, not where the walk reached it")
			}
		})
	}
}

// TestPositions_AreIndexedOnceHoweverOftenAsked pins the cost: the walk
// of the tree happens on the first question and never again, so asking about
// every object costs the tree once, not once per object.
func TestPositions_AreIndexedOnceHoweverOftenAsked(t *testing.T) {
	t.Parallel()
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(positionsDocs["sequences number their children"]))
	require.NoError(t, err)
	r := NewReader(doc)
	require.Nil(t, r.positions, "nothing is indexed before a question is asked")

	var objects []locatable
	for item := range soa.Walk(t.Context(), doc) {
		require.NoError(t, item.Match(soa.Matcher{Schema: func(js *oas3.JSONSchema[oas3.Referenceable]) error {
			objects = append(objects, js.GetCore())
			return nil
		}}))
	}
	require.NotEmpty(t, objects)

	r.positionOf(objects[0])
	indexed, visits := r.positions, r.positions.visits
	require.Positive(t, visits)
	for _, obj := range objects {
		r.positionOf(obj)
	}
	assert.Same(t, indexed, r.positions, "the index is built once")
	assert.Equal(t, visits, r.positions.visits, "and the tree is not walked again")
}

// TestPositions_Boundaries pins what an index over nothing, or a node that is
// not in it, answers: no position.
func TestPositions_Boundaries(t *testing.T) {
	t.Parallel()
	root := &yaml.Node{Kind: yaml.MappingNode}
	leaf := &yaml.Node{Kind: yaml.MappingNode}
	root.Content = []*yaml.Node{{Kind: yaml.ScalarNode, Value: "a"}, leaf}

	p := indexPositions(root)
	assert.Equal(t, jsontext.Pointer("/"), p.pointerOf(root), "the root")
	assert.Equal(t, jsontext.Pointer("/a"), p.pointerOf(leaf))
	assert.Equal(t, jsontext.Pointer(""), p.pointerOf(&yaml.Node{Kind: yaml.MappingNode}), "a node the tree does not hold")
	assert.Equal(t, jsontext.Pointer(""), p.pointerOf(nil), "no node")

	// A key that is no scalar skips its whole entry, as the library's search does,
	// and a key that is an alias names what the alias stands for.
	hidden := &yaml.Node{Kind: yaml.MappingNode}
	scalar := &yaml.Node{Kind: yaml.ScalarNode, Value: "k"}
	viaAlias := &yaml.Node{Kind: yaml.MappingNode}
	skipped := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.MappingNode}, hidden,
		{Kind: yaml.AliasNode, Alias: scalar}, viaAlias,
	}}
	assert.Equal(t, jsontext.Pointer(""), indexPositions(skipped).pointerOf(hidden), "the entry under a non-scalar key is skipped")
	assert.Equal(t, jsontext.Pointer("/k"), indexPositions(skipped).pointerOf(viaAlias), "an aliased key is the scalar it stands for")

	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
	assert.Equal(t, jsontext.Pointer("/a"), indexPositions(doc).pointerOf(leaf), "a document node is its content")
	assert.Equal(t, jsontext.Pointer(""), indexPositions(&yaml.Node{Kind: yaml.DocumentNode}).pointerOf(leaf), "an empty document holds nothing")
	assert.Equal(t, jsontext.Pointer(""), indexPositions(nil).pointerOf(leaf), "no tree holds nothing")
}
