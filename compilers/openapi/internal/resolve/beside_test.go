package resolve_test

import (
	"encoding/json/jsontext"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
)

// besideSpec writes get beside /a's $ref to /b, which declares one too, and
// reaches it from /c by a pointer through /a, by a schema $ref and by a mapping
// value. The resolver reads all three as /b's.
const besideSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    get:
      requestBody: {content: {application/json: {schema: {type: object}}}}
      responses: {"200": {description: ok}}
  /b:
    get:
      requestBody: {content: {application/json: {schema: {type: string}}}}
      responses: {"200": {description: ok}}
  /c:
    post:
      requestBody: {$ref: '#/paths/~1a/get/requestBody'}
      responses: {"200": {description: ok}}
`

// loadBeside loads besideSpec and returns it with the scope lowering reads it
// in, with and without the load phase's models.
func loadBeside(t *testing.T) (*load.Document, resolve.Scope, resolve.Scope) {
	t.Helper()
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(besideSpec)},
		load.Options{})
	require.NoError(t, err)
	bare := resolve.Scope{SelfPath: "spec.yaml", Doc: doc.Doc}
	carrying := bare
	carrying.Beside = doc.Siblings.Beside
	return doc, bare, carrying
}

// TestObjectAt_RefusesAHopBesideAPathItemRef pins the refusal on the route every
// reference but a schema's takes: /c's body, which the resolver found in /b,
// resolves to nothing once the scope knows /a writes get beside its $ref, so
// no lowering interns /b's body at the pointer /a's own is declared at.
func TestObjectAt_RefusesAHopBesideAPathItemRef(t *testing.T) {
	t.Parallel()
	doc, bare, carrying := loadBeside(t)
	c, ok := doc.Doc.GetPaths().Get("/c")
	require.True(t, ok)
	body := c.GetObject().Post().GetRequestBody()
	const use = jsontext.Pointer("/paths/~1c/post/requestBody")

	got, at := resolve.ObjectAt[soa.RequestBody](bare, body, use)
	require.NotNil(t, got, "without the models the resolver's answer stands")
	assert.Equal(t, jsontext.Pointer("/paths/~1a/get/requestBody"), at)

	got, at = resolve.ObjectAt[soa.RequestBody](carrying, body, use)
	assert.Nil(t, got, "with them the hop is refused")
	assert.Equal(t, use, at)
}

// TestObjectAt_RecordsNoHopOfARefusedChain pins what a refused chain leaves in
// Reached: nothing, so the component X it passes through, which no lowering
// reads, is still kept verbatim. Recording the hops before the refusal left X
// in no form at all. Accepted, the same chain records X.
func TestObjectAt_RecordsNoHopOfARefusedChain(t *testing.T) {
	t.Parallel()
	const spec = besideSpec + `  /d:
    post:
      requestBody: {$ref: '#/components/requestBodies/X'}
      responses: {"200": {description: ok}}
components:
  requestBodies:
    X: {$ref: '#/paths/~1a/get/requestBody'}
`
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, load.Options{})
	require.NoError(t, err)
	d, ok := doc.Doc.GetPaths().Get("/d")
	require.True(t, ok)
	body := d.GetObject().Post().GetRequestBody()
	const x = jsontext.Pointer("/components/requestBodies/X")

	refused := resolve.Scope{SelfPath: "spec.yaml", Doc: doc.Doc, Beside: doc.Siblings.Beside, Reached: resolve.NewReached()}
	got, _ := resolve.ObjectAt[soa.RequestBody](refused, body, "/paths/~1d/post/requestBody")
	require.Nil(t, got)
	assert.False(t, refused.Reached.Has(x), "a refused chain records no hop")

	accepted := resolve.Scope{SelfPath: "spec.yaml", Doc: doc.Doc, Reached: resolve.NewReached()}
	got, _ = resolve.ObjectAt[soa.RequestBody](accepted, body, "/paths/~1d/post/requestBody")
	require.NotNil(t, got)
	assert.True(t, accepted.Reached.Has(x), "an accepted one records each")
}

// TestFound_HandsOnNoTypedNil pins the answer ObjectAt hands Scope.refuses for
// a hop: a nil interface for a nil object, since a typed nil panics in the read
// of the mapping a reference is written as. It is compared by Equal against nil,
// which compares the interface itself: a typed nil fails it, and assert.Nil
// would let one through.
func TestFound_HandsOnNoTypedNil(t *testing.T) {
	t.Parallel()
	assert.Equal(t, nil, resolve.Found[soa.ReferencedRequestBody](nil))
	body := &soa.ReferencedRequestBody{}
	assert.Equal(t, any(body), resolve.Found(body))
}

// TestScope_PointerReadersRefuseAPointerBesideAPathItemRef pins the two other
// routes, a schema's $ref and a mapping value, each refusing the same pointer
// and passing one nothing writes beside the $ref.
func TestScope_PointerReadersRefuseAPointerBesideAPathItemRef(t *testing.T) {
	t.Parallel()
	_, bare, carrying := loadBeside(t)
	const beside = "#/paths/~1a/get/requestBody/content/application~1json/schema"
	const elsewhere = "#/paths/~1b/get/requestBody/content/application~1json/schema"
	js := oas3.NewJSONSchemaFromReference(beside)
	d := &oas3.Discriminator{}

	for _, ref := range []string{beside, elsewhere} {
		_, ok := bare.TargetPointer(js, ref)
		assert.True(t, ok, "%s without the models", ref)
		_, ok = bare.MappingPointer(d, ref)
		assert.True(t, ok, "%s without the models", ref)
	}
	_, ok := carrying.TargetPointer(js, beside)
	assert.False(t, ok, "a schema's $ref beside the $ref is refused")
	_, ok = carrying.MappingPointer(d, beside)
	assert.False(t, ok, "and a mapping value")
	_, ok = carrying.TargetPointer(js, elsewhere)
	assert.True(t, ok, "one elsewhere is not")
	_, ok = carrying.MappingPointer(d, elsewhere)
	assert.True(t, ok)
}

// TestScope_BesideWhySaysWhyAReferenceIsRefused pins the reason a lowering's own
// unresolved report gives: a pointer landing beside the $ref names where; a
// "#/$defs/..." pointer is refused by where it is written; and anything else,
// or a scope without the models, has no reason to give.
func TestScope_BesideWhySaysWhyAReferenceIsRefused(t *testing.T) {
	t.Parallel()
	_, bare, carrying := loadBeside(t)
	const inA = jsontext.Pointer("/paths/~1a/get/requestBody/content/application~1json/schema")
	const inB = jsontext.Pointer("/paths/~1b/get/requestBody/content/application~1json/schema")

	assert.Equal(t, ": it reaches "+string(inA)+"; "+resolve.BesideReason("/paths/~1a", "get"),
		carrying.BesideWhy("/paths/~1c", inA))
	assert.Equal(t, ": "+resolve.DefsBesideReason("/paths/~1a", "get"), carrying.BesideWhy(inA, "/$defs/X"))
	assert.Contains(t, resolve.DefsBesideReason("/paths/~1a", "get"), resolve.BesideReason("/paths/~1a", "get"))
	assert.Contains(t, resolve.BesideReason("/paths/~1a", "get"), `"get"`)
	for _, why := range []string{
		carrying.BesideWhy("/paths/~1c", inB),
		carrying.BesideWhy(inB, "/$defs/X"),
		bare.BesideWhy("/paths/~1c", inA),
	} {
		assert.Empty(t, why)
	}
}

// TestChained_SaysWhetherHopsHasOneToList pins Chained against Hops: a chain
// passing a reference after its entry has a hop, and one whose entry names an
// item, or that is inline, has none.
func TestChained_SaysWhetherHopsHasOneToList(t *testing.T) {
	t.Parallel()
	const spec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x: {$ref: '#/paths/~1a'}
  /a: {$ref: '#/paths/~1b'}
  /b:
    get: {responses: {"200": {description: ok}}}
`
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, load.Options{})
	require.NoError(t, err)
	scope := resolve.Scope{SelfPath: "spec.yaml", Doc: doc.Doc}
	for path, want := range map[string]bool{"/x": true, "/a": false, "/b": false} {
		rp, ok := doc.Doc.GetPaths().Get(path)
		require.True(t, ok)
		assert.Equal(t, want, resolve.Chained[soa.PathItem](rp), path)
		assert.Equal(t, want, len(resolve.Hops[soa.PathItem](scope, rp)) > 0, "%s: as Hops lists", path)
	}
}

// TestHops_ReadsNoScopeForAChainOfOneHop pins Hops's cost on the common chain,
// a use site naming an item that is no reference: there is no hop to read a
// scope for, so it reads none. Reading one before asking cost every $ref'd path
// item a pointer walk whose answer was thrown away.
func TestHops_ReadsNoScopeForAChainOfOneHop(t *testing.T) {
	doc, bare, _ := loadBeside(t)
	c, ok := doc.Doc.GetPaths().Get("/a")
	require.True(t, ok)
	require.True(t, c.IsReference())

	assert.Empty(t, resolve.Hops[soa.PathItem](bare, c))
	assert.Zero(t, testing.AllocsPerRun(10, func() { resolve.Hops[soa.PathItem](bare, c) }))
}
