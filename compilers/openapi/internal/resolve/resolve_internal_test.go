package resolve

import (
	"encoding/json/jsontext"
	"path/filepath"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonpointer"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
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

// TestScopeReached_PassesAReferenceTheWalkLeavesTheModelFrom pins a walk that
// leaves the model at a reference, for raw YAML its target holds: the
// reference is passed when the target holds the next token, as the resolver
// steps past it into that YAML, and not when it holds nothing there. Holds,
// asked of the target's mapping, answers in place of the library: as the
// library's read of that mapping does, and what it answers is what is passed.
func TestScopeReached_PassesAReferenceTheWalkLeavesTheModelFrom(t *testing.T) {
	t.Parallel()
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(`openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  responses:
    R: {$ref: '#/components/responses/S'}
    S: {description: s, x-foo: {a: 1}}
`))
	require.NoError(t, err)
	_, err = doc.ResolveAllReferences(t.Context(), soa.ResolveAllOptions{OpenAPILocation: "spec.yaml"})
	require.NoError(t, err)
	elsewhere := func(node any) (End, bool) {
		if r, ok := node.(*soa.ReferencedResponse); ok && r.IsReference() {
			return End{Document: "elsewhere", Path: "x.yaml"}, true
		}
		return End{}, false
	}
	library := func(raw *yaml.Node, token string) bool {
		_, err := jsonpointer.GetTarget(raw, jsonpointer.JSONPointer("/"+jsonpointer.EscapeString(token)))
		return err == nil
	}
	for _, holds := range []func(*yaml.Node, string) bool{nil, library} {
		sc := Scope{Doc: doc, Ends: elsewhere, Holds: holds}
		assert.True(t, atPointer(sc, "/components/responses/R/x-foo/a").Foreign, "S holds x-foo")
		assert.True(t, atPointer(sc, "/components/responses/R/x-foo").Foreign)
		assert.False(t, atPointer(sc, "/components/responses/R/x-nope").Foreign, "S holds no x-nope")
		assert.False(t, atPointer(sc, "/components/responses/S/x-foo").Foreign, "S is no reference")
	}

	never := Scope{Doc: doc, Ends: elsewhere, Holds: func(*yaml.Node, string) bool { return false }}
	assert.False(t, atPointer(never, "/components/responses/R/x-foo/a").Foreign, "Holds, not the library, answers")
	always := Scope{Doc: doc, Ends: elsewhere, Holds: func(*yaml.Node, string) bool { return true }}
	assert.True(t, atPointer(always, "/components/responses/R/x-nope").Foreign, "Holds, not the library, answers")
	assert.False(t, atPointer(always, "/components/responses/S/x-foo").Foreign, "only a reference asks it")
}

// TestScopeReached_StepsPastAnIndexTheLibraryRetries pins the walk past an
// index the library retries, reading it as a key with the rest below it and as
// an index once that fails: what it steps past is still noted a step at a
// time.
func TestScopeReached_StepsPastAnIndexTheLibraryRetries(t *testing.T) {
	t.Parallel()
	var built yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("{'0': {j: 5}}"), &built))
	js := openapitest.SchemaFromYAML(t.Context(), t, "type: string\n")
	doc := &indexedDoc{built: built.Content[0], schema: js}
	for _, ends := range []func(any) (End, bool){nil, elsewhere} {
		sc := Scope{Doc: doc, Ends: ends}
		assert.Same(t, js, wholeModelAt(doc, "/0/k"), "sanity: the index answers k, which the mapping's 0 does not hold")
		assert.Equal(t, ends != nil, atPointer(sc, "/0/k").Foreign, "the walk noted the document it stepped past")
	}
}

// TestScopeReached_PassesAReferenceUnderADefaultResponse pins that the walk goes on
// past an operation's responses, which the model holds by value: a default
// response that is a reference is passed, as one under a status code is.
func TestScopeReached_PassesAReferenceUnderADefaultResponse(t *testing.T) {
	t.Parallel()
	responses := func(node any) (End, bool) {
		if _, ok := node.(*soa.ReferencedResponse); ok {
			return End{Document: "elsewhere", Path: "x.yaml"}, true
		}
		return End{}, false
	}
	sc := Scope{Doc: unmarshalWalkDoc(t), Ends: responses}
	for _, code := range []string{"default", "200"} {
		at := atPointer(sc, jsontext.Pointer("/paths/~1a/get/responses/"+code+"/description"))
		assert.True(t, at.Foreign, "the %s response is passed", code)
		assert.Equal(t, "x.yaml", at.Holder, code)
	}
}

// TestScopeReached_StopsAtASchema pins where the walk stops reading references: at
// the first schema, since the resolver reads a schema's own $ref as a keyword,
// so nothing past it is asked whether it is one.
func TestScopeReached_StopsAtASchema(t *testing.T) {
	t.Parallel()
	asked := 0
	sc := Scope{Doc: unmarshalWalkDoc(t), Ends: func(any) (End, bool) { asked++; return End{}, false }}

	assert.False(t, atPointer(sc, "/components/schemas/S/properties/p/properties/q").Foreign)
	assert.Equal(t, 3, asked, "the document, its components and its schemas, then the schema stops it")
}

// TestScopeReached_StopsAtRawYAML pins the other place the walk stops: at a value
// the model holds as raw YAML, which holds no reference the resolver resolved,
// so a pointer into an extension costs one step rather than a key scan for
// each token past it.
func TestScopeReached_StopsAtRawYAML(t *testing.T) {
	t.Parallel()
	asked := 0
	sc := Scope{Doc: unmarshalWalkDoc(t), Ends: func(any) (End, bool) { asked++; return End{}, false }}

	assert.False(t, atPointer(sc, "/x-lib/a/b/c").Foreign)
	assert.Equal(t, 1, asked, "the document, then the extension's value stops it")

	rawAsReference := func(node any) (End, bool) {
		_, raw := node.(*yaml.Node)
		return End{Document: "elsewhere", Path: "x.yaml"}, raw
	}
	at := atPointer(Scope{Doc: unmarshalWalkDoc(t), Ends: rawAsReference}, "/extensions/x-lib/a/b")
	assert.False(t, at.Foreign, "raw YAML a field holds is passed by no walk, though Ends would read it as a reference")
}

// TestScopeReached_PassesOnlyWhatTheWalkStepsPast pins which nodes a walk passes:
// those it steps past to the next token. One where the walk finds nothing more
// is not passed, nor is the node the pointer ends at.
func TestScopeReached_PassesOnlyWhatTheWalkStepsPast(t *testing.T) {
	t.Parallel()
	sc := Scope{Doc: unmarshalWalkDoc(t), Ends: elsewhere}

	assert.False(t, atPointer(sc, "/nope/x").Foreign, "the walk finds nothing past the document")
	assert.False(t, atPointer(sc, "").Foreign, "the pointer ends at the document")
	past := atPointer(sc, "/paths/~1a")
	assert.True(t, past.Foreign, "the walk steps past the document and its paths")
	assert.Equal(t, "x.yaml", past.Holder)
}

// TestScopeReached_WithoutEndsPassesNothing pins a scope with no Ends: nothing can
// be read as a reference, so every pointer names the source's own content.
func TestScopeReached_WithoutEndsPassesNothing(t *testing.T) {
	t.Parallel()
	sc := Scope{Doc: unmarshalWalkDoc(t), Foreign: true, Holder: "x.yaml"}

	got := atPointer(sc, "/paths/~1a/get")
	assert.False(t, got.Foreign)
	assert.Empty(t, got.Holder)
}

// elsewhere is an Ends that reads every node it is asked about as a reference
// ending in another document, so any node a walk steps past shows.
func elsewhere(any) (End, bool) { return End{Document: "elsewhere", Path: "x.yaml"}, true }
