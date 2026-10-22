package resolve

import (
	"encoding/json/jsontext"
	"path/filepath"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// walkDoc is a document whose positions a walk reaches past a path item and an
// operation's responses, which the model holds by value, into a schema,
// through a response keyed by the empty string, and into an extension's value,
// which the model holds as raw YAML.
const walkDoc = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {get: {responses: {"200": {description: ok}, default: {description: otherwise}}}}
components:
  schemas:
    S: {type: object, properties: {p: {type: object, properties: {q: {type: string}}}}}
  responses:
    "": {description: keyed by the empty string}
x-lib:
  a: {b: {c: deep}}
`

// unmarshalWalkDoc parses walkDoc.
func unmarshalWalkDoc(t *testing.T) *soa.OpenAPI {
	t.Helper()
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(walkDoc))
	require.NoError(t, err)
	return doc
}

// atPointer is the scope reached reads what pointer names in, a position of
// the source's own model: ScopeOf's answer for a chain whose last hop resolved
// there.
func atPointer(s Scope, pointer jsontext.Pointer) Scope {
	return s.reached(End{Document: s.Doc, Pointer: pointer})
}

// TestScopeReached_FollowsAtMostMaxRefChainRefs pins the bound on the $refs a walk
// follows, each to where its chain ends: a chain ending back at a position its
// own walk passes it again is followed maxRefChain times, then read as no
// document's. Read as the source's own, content past the bound that another
// document holds read its references against the source (GitHub #762).
func TestScopeReached_FollowsAtMostMaxRefChainRefs(t *testing.T) {
	t.Parallel()
	doc := unmarshalWalkDoc(t)
	turns := 0
	sc := Scope{Doc: doc, Ends: func(node any) (End, bool) {
		if _, ok := node.(*soa.ReferencedPathItem); !ok {
			return End{}, false
		}
		turns++
		return End{Document: doc, Pointer: "/paths/~1a/get"}, true
	}}

	past := atPointer(sc, "/paths/~1a/get")
	assert.True(t, past.Foreign, "past the bound, no document's")
	assert.Empty(t, past.Holder, "so no reference there names a document")
	assert.Equal(t, maxRefChain, turns, "each turn follows one $ref")

}

// TestScopeReached_AnEndInTheSourcesFileIsTheSources pins a hop that resolved
// against the source's file read again rather than its model, as the resolver
// reads it when the load phase does not hold the source: it is the source's
// own content, under any spelling of its path, and not another document's.
func TestScopeReached_AnEndInTheSourcesFileIsTheSources(t *testing.T) {
	t.Parallel()
	abs, err := filepath.Abs("dir/root.yaml")
	require.NoError(t, err)
	sc := Scope{SelfPath: "dir/root.yaml"}
	for _, path := range []string{"dir/root.yaml", "dir/./root.yaml", "dir/x/../root.yaml", abs} {
		got := sc.reached(End{Document: new(int), Path: path, Pointer: "/components/pathItems/Own"})
		assert.False(t, got.Foreign, path)
		assert.Empty(t, got.Holder, path)
	}
	for _, path := range []string{"dir/other.yaml", "other/root.yaml", "https://example.com/dir/root.yaml"} {
		got := sc.reached(End{Document: new(int), Path: path, Pointer: "/x"})
		assert.True(t, got.Foreign, path)
		assert.Equal(t, path, got.Holder, path)
	}
	none := Scope{}.reached(End{Document: new(int), Path: "dir/root.yaml", Pointer: "/x"})
	assert.True(t, none.Foreign, "a scope with no source path names no source file")
}
