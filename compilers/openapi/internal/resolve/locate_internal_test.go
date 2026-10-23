package resolve

import (
	"encoding/json/jsontext"
	"errors"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonpointer"
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/navigation"
)

// rawMappingDoc is a document whose pointers leave the model for raw YAML:
// the root's under an extension, and a schema's.
const rawMappingDoc = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S: {type: object, x-ext: {k: {type: object}}}
x-lib:
  a: {b: {type: object}}
`

// wholeModelAt reads the whole pointer in one library call, as the library's
// resolver reads it: the schema found there, or nil.
func wholeModelAt(doc any, pointer jsontext.Pointer) *oas3.JSONSchema[oas3.Referenceable] {
	target, err := jsonpointer.GetTarget(doc, jsonpointer.JSONPointer(pointer), jsonpointer.WithStructTags("key"))
	if js, ok := target.(*oas3.JSONSchema[oas3.Referenceable]); ok && err == nil && js != nil {
		return js
	}
	return nil
}

// TestScope_ReadsNoRawMappingAPointerPasses pins GitHub #778 for the walk
// reached takes: it scans no mapping where a pointer leaves the model, the
// object's own, nor any raw mapping below. Each such mapping is given a first
// key that is no node, which the library's read of it faults on, as the whole
// read shows.
func TestScope_ReadsNoRawMappingAPointerPasses(t *testing.T) {
	t.Parallel()
	for _, at := range []struct{ mapping, pointer jsontext.Pointer }{
		{"", "/x-lib/a/b"},
		{"/x-lib", "/x-lib/a/b"},
		{"/components/schemas/S", "/components/schemas/S/x-ext/k"},
		{"/components/schemas/S/x-ext", "/components/schemas/S/x-ext/k"},
	} {
		doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(rawMappingDoc))
		require.NoError(t, err)
		mapping := builtAt(t, doc, at.mapping)
		mapping.Content = append([]*yaml.Node{nil, {Kind: yaml.ScalarNode}}, mapping.Content...)

		assert.Panics(t, func() { wholeModelAt(doc, at.pointer) }, "the whole read scans %q", at.mapping)
		sc := Scope{Doc: doc, Ends: ReferenceEnd}
		assert.NotPanics(t, func() {
			assert.False(t, atPointer(sc, at.pointer).Foreign)
		}, "%q", at.pointer)
	}
}

// builtAt returns the mapping the model holds at pointer: raw YAML itself, or
// the one the object there was built from.
func builtAt(t *testing.T, doc *soa.OpenAPI, pointer jsontext.Pointer) *yaml.Node {
	t.Helper()
	switch v := walkTo(doc, pointer).(type) {
	case *yaml.Node:
		return v
	case *oas3.JSONSchema[oas3.Referenceable]:
		return v.GetSchema().GetRootNode()
	case *soa.OpenAPI:
		return v.GetRootNode()
	default:
		require.Failf(t, "no mapping", "%q holds %T", pointer, v)
		return nil
	}
}

// indexedDoc is a document that is no model and that the library reads by
// index as well as by the mapping it was built from: that mapping's 0 holds j,
// and its index 0 holds the schema under k.
type indexedDoc struct {
	built  *yaml.Node
	schema *oas3.JSONSchema[oas3.Referenceable]
}

// GetRootNode returns the mapping the document was built from.
func (d *indexedDoc) GetRootNode() *yaml.Node { return d.built }

// NavigateWithIndex answers index 0 with a map holding the schema under k.
func (d *indexedDoc) NavigateWithIndex(i int) (any, error) {
	if i != 0 {
		return nil, errors.New("no such index")
	}
	return map[string]any{"k": d.schema}, nil
}

// walkTo returns what the model holds at pointer, a step at a time.
func walkTo(doc *soa.OpenAPI, pointer jsontext.Pointer) any {
	var node any = doc
	for token := range pointer.Tokens() {
		node, _ = navigation.Step(node, token)
	}
	return node
}
