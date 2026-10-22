package resolve_test

import (
	"encoding/json/jsontext"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
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

// besideSchemasSpec writes get beside /a's $ref to /b, which declares one too,
// and reaches the schema each declares by a schema $ref and by a mapping value.
// The resolver reads both as /b's. The schema /a's get declares holds a
// definition, a $ref to it and a discriminator mapping to it, all of them
// written beside the $ref.
const besideSchemasSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    get:
      requestBody:
        content:
          application/json:
            schema:
              type: object
              $defs: {k: {type: string}}
              properties: {viaDefs: {$ref: '#/$defs/k'}}
              discriminator:
                propertyName: kind
                mapping: {viaDefs: '#/$defs/k'}
      responses: {"200": {description: ok}}
  /b:
    get:
      requestBody: {content: {application/json: {schema: {type: string}}}}
      responses: {"200": {description: ok}}
components:
  schemas:
    Beside: {$ref: '#/paths/~1a/get/requestBody/content/application~1json/schema'}
    Elsewhere: {$ref: '#/paths/~1b/get/requestBody/content/application~1json/schema'}
    Pet:
      discriminator:
        propertyName: kind
        mapping:
          beside: '#/paths/~1a/get/requestBody/content/application~1json/schema'
          elsewhere: '#/paths/~1b/get/requestBody/content/application~1json/schema'
`

const (
	besideSchema    = "/paths/~1a/get/requestBody/content/application~1json/schema"
	elsewhereSchema = "/paths/~1b/get/requestBody/content/application~1json/schema"
)

// TestBindings_BesideHopsNameTheFieldWrittenBesideTheRef pins how the binder
// leaves a pointer through the fields beside a path item $ref unresolved: a $ref
// reaching the field is a BesideRef, a "#/$defs/..." one written in the field is
// a BesideDefs, each naming the path item and the key, and a mapping value is
// read as a $ref is. One landing elsewhere resolves. The reason is the binder's,
// so the report reads the same wherever the hop is reported.
func TestBindings_BesideHopsNameTheFieldWrittenBesideTheRef(t *testing.T) {
	t.Parallel()
	b := loadBound(t, "spec.yaml", besideSchemasSpec, load.Options{})
	pet := b.component(t, "Pet").GetSchema().GetDiscriminator()
	inside := b.at(t, besideSchema).GetSchema().GetDiscriminator()
	const viaDefs = "/paths/~1a/get/requestBody/content/application~1json/schema/properties/viaDefs"

	for name, tc := range map[string]struct {
		hop    bind.Hop
		status bind.Status
		reason bind.Reason
		why    string
	}{
		"$ref beside": {entryOf(t, b, b.component(t, "Beside")).Hop, bind.Unresolved, bind.BesideRef,
			"it reaches " + besideSchema + "; " + bind.BesideReason("/paths/~1a", "get")},
		"$defs beside": {entryOf(t, b, b.at(t, viaDefs)).Hop, bind.Unresolved, bind.BesideDefs,
			"it is written in the fields beside a path item $ref, as is any definition it can name; " + bind.BesideReason("/paths/~1a", "get")},
		"mapping beside": {mappingOf(t, b, pet, "#"+besideSchema).Hop, bind.Unresolved, bind.BesideRef,
			"it reaches " + besideSchema + "; " + bind.BesideReason("/paths/~1a", "get")},
		"mapping $defs beside": {mappingOf(t, b, inside, "#/$defs/k").Hop, bind.Unresolved, bind.BesideDefs,
			"it is written in the fields beside a path item $ref, as is any definition it can name; " + bind.BesideReason("/paths/~1a", "get")},
		"$ref elsewhere":    {entryOf(t, b, b.component(t, "Elsewhere")).Hop, bind.Resolved, bind.NoReason, ""},
		"mapping elsewhere": {mappingOf(t, b, pet, "#"+elsewhereSchema).Hop, bind.Resolved, bind.NoReason, ""},
	} {
		assert.Equal(t, tc.status, tc.hop.Status, name)
		assert.Equal(t, tc.reason, tc.hop.Reason, name)
		if tc.reason == bind.NoReason {
			continue
		}
		assert.Equal(t, jsontext.Pointer("/paths/~1a"), tc.hop.Use, name)
		assert.Equal(t, "get", tc.hop.Key, name)
		assert.Equal(t, tc.why, tc.hop.Why(), name)
	}
}

// entryOf returns the table's entry for the $ref js writes.
func entryOf(t *testing.T, b bound, js *schemaRef) bind.Entry {
	t.Helper()
	e, ok := b.doc.Bindings.Entry(js)
	require.True(t, ok, "the table read a $ref there")
	return e
}

// mappingOf returns the table's reading of value, written by the discriminator d.
func mappingOf(t *testing.T, b bound, d *oas3.Discriminator, value string) bind.Mapping {
	t.Helper()
	m, ok := b.doc.Bindings.Mapping(d, value)
	require.True(t, ok, "the table read the mapping value %q", value)
	return m
}

// TestScope_PointerReadersRefuseAPointerBesideAPathItemRef pins the two routes a
// schema's pointer takes, a $ref and a mapping value, each refused where it
// reaches, or is written in, the fields beside a path item $ref, and passing one
// elsewhere. The refusal is the binder's (see the test of its hops), and the
// scope reads it as it reads any $ref that lands nowhere, with no Beside of its
// own to consult.
func TestScope_PointerReadersRefuseAPointerBesideAPathItemRef(t *testing.T) {
	t.Parallel()
	b := loadBound(t, "spec.yaml", besideSchemasSpec, load.Options{})
	pet := b.component(t, "Pet").GetSchema().GetDiscriminator()
	inside := b.at(t, besideSchema).GetSchema().GetDiscriminator()

	got, ok := b.scope.Target(b.component(t, "Elsewhere"))
	assert.True(t, ok, "a schema's $ref elsewhere is not refused")
	assert.Equal(t, bind.Position{Pointer: elsewhereSchema}, got)
	_, ok = b.scope.Target(b.component(t, "Beside"))
	assert.False(t, ok, "a schema's $ref beside the $ref is refused")
	_, ok = b.scope.Target(b.at(t, besideSchema+"/properties/viaDefs"))
	assert.False(t, ok, "and a $defs one written there")

	got, _, ok = b.scope.MappingTarget(pet, "#"+elsewhereSchema)
	assert.True(t, ok, "a mapping value elsewhere is not refused")
	assert.Equal(t, bind.Position{Pointer: elsewhereSchema}, got)
	_, _, ok = b.scope.MappingTarget(pet, "#"+besideSchema)
	assert.False(t, ok, "a mapping value beside the $ref is refused")
	_, _, ok = b.scope.MappingTarget(inside, "#/$defs/k")
	assert.False(t, ok, "and a $defs one written there")
}
