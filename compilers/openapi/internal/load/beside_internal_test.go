package load

import (
	"encoding/json/jsontext"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// besideChainSpec reaches /a's get request body, written beside its $ref,
// through a component, and its schema through a schema component that another
// names.
const besideChainSpec = `openapi: 3.1.0
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
      requestBody: {$ref: '#/components/requestBodies/X'}
      responses: {"200": {description: ok}}
components:
  requestBodies:
    X: {$ref: '#/paths/~1a/get/requestBody'}
  schemas:
    S: {$ref: '#/paths/~1a/get/requestBody/content/application~1json/schema'}
    T: {$ref: '#/components/schemas/S'}
`

// TestLoad_ReportsAChainIntoTheFieldsBesideARefAsTheLoweringReadsIt pins which
// references the refusal reports: each whose chain reaches the fields beside a
// $ref hop by hop, as resolve.ObjectAt reads it, so /c is reported beside X;
// and a schema's $ref at its first hop only, as a schema reference is lowered,
// so S is and T, which names S, is not. Each is reported once.
func TestLoad_ReportsAChainIntoTheFieldsBesideARefAsTheLoweringReadsIt(t *testing.T) {
	t.Parallel()
	_, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(besideChainSpec)}, Options{})
	require.NoError(t, err)

	refused := map[jsontext.Pointer]int{}
	for _, d := range diags {
		if d.Code == diag.UnresolvedRef && d.Severity == ir.SeverityError {
			assert.Contains(t, d.Message, "GitHub #824")
			refused[d.Provenance.Pointer]++
		}
	}
	assert.Equal(t, map[jsontext.Pointer]int{
		"/paths/~1c/post/requestBody": 1,
		"/components/requestBodies/X": 1,
		"/components/schemas/S":       1,
	}, refused)
}

// TestLoad_TakesAHopThatFindsTheFieldWrittenThere pins the one answer the
// refusal takes. With external references allowed, this phase holds the
// source's objects for the resolver, the fields beside a $ref among them, so
// /c's chain through X finds the body /a's get writes, and neither is
// reported. S still is: a schema's $ref is refused by its pointer, which the
// lowering reads through the $ref, so Writes never names a schema.
func TestLoad_TakesAHopThatFindsTheFieldWrittenThere(t *testing.T) {
	t.Parallel()
	got, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(besideChainSpec)},
		Options{Reader: reader.Disk{}})
	require.NoError(t, err)

	refused := map[jsontext.Pointer]int{}
	for _, d := range diags {
		if d.Code == diag.UnresolvedRef && d.Severity == ir.SeverityError {
			refused[d.Provenance.Pointer]++
		}
	}
	assert.Equal(t, map[jsontext.Pointer]int{"/components/schemas/S": 1}, refused)

	a, ok := got.Siblings.Of(pathEntry(t, got.Doc, "/a"))
	require.True(t, ok)
	near := a.Item.Get().GetRequestBody()
	far := pathEntry(t, got.Doc, "/b").GetObject().Get().GetRequestBody()
	media, ok := near.GetObject().GetContent().Get("application/json")
	require.True(t, ok)
	const body = jsontext.Pointer("/paths/~1a/get/requestBody")
	assert.True(t, got.Siblings.Writes(body, near), "the body written beside the $ref")
	assert.False(t, got.Siblings.Writes(body, far), "not the one the $ref names")
	assert.False(t, got.Siblings.Writes("/paths/~1b/get/requestBody", far), "nor one at a position no model holds")
	assert.False(t, got.Siblings.Writes(body+"/content/application~1json/schema", media.GetSchema()), "nor a schema")
	assert.NotContains(t, got.Siblings.writtenAs, jsontext.Pointer("/paths/~1c/post/requestBody"),
		"the document's own objects are never asked about, so they are not recorded")
}

// TestLoad_HoldsADefsPointerBesideARefUnresolved pins a "#/$defs/..." pointer
// written in the fields beside a $ref: its definition is in those fields too,
// at a pointer the resolver reads through the $ref, so it is held unresolved
// and reported once, saying so, rather than as a definition not found.
func TestLoad_HoldsADefsPointerBesideARefUnresolved(t *testing.T) {
	t.Parallel()
	const spec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    get:
      requestBody:
        content:
          application/json:
            schema:
              $defs: {X: {type: string}}
              properties: {x: {$ref: '#/$defs/X'}}
      responses: {"200": {description: ok}}
  /b:
    get: {responses: {"200": {description: ok}}}
`
	got, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, Options{})
	require.NoError(t, err)

	const site = "/paths/~1a/get/requestBody/content/application~1json/schema/properties/x"
	var messages []string
	for _, d := range diags {
		if d.Code == diag.UnresolvedRef {
			assert.Equal(t, jsontext.Pointer(site), d.Provenance.Pointer)
			messages = append(messages, d.Message)
		}
	}
	require.Len(t, messages, 1)
	assert.Contains(t, messages[0], "as is any definition it can name")
	a, ok := got.Siblings.Of(pathEntry(t, got.Doc, "/a"))
	require.True(t, ok)
	x, ok := a.Item.Get().GetRequestBody().GetObject().GetContent().Get("application/json")
	require.True(t, ok)
	prop, ok := x.GetSchema().GetSchema().GetProperties().Get("x")
	require.True(t, ok)
	assert.False(t, prop.IsResolved(), "the reference is left unresolved")
	assert.Equal(t, "#/$defs/X", prop.GetRef().String(), "and put back as written")
}

// TestBesideRefs_AWalkThatPanicsIsReported pins the barrier around the walk, as
// for every pass over it: the panic is an error at the model it reached, not a
// crashed compile.
func TestBesideRefs_AWalkThatPanicsIsReported(t *testing.T) {
	t.Parallel()
	sibs, _ := modelSiblings(t.Context(), modelOf(t, siblingSpec), scan.InSource(0))
	require.NotEmpty(t, sibs.list, "a registry holding a model is walked")
	panics := func(func(soa.WalkItem) bool) { panic("walk fault") }
	at := func(p jsontext.Pointer) ir.Provenance { return ir.Provenance{Pointer: p} }

	diags := besideRefs(at, sibs, panics, nil)

	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, diag.Validation, diags[0].Code)
	assert.Contains(t, diags[0].Message, "path item $ref check panicked: walk fault")
}

// bareReference is a $ref model with no core to read, and coredReference one
// whose core holds no mapping: shapes no library model takes, which ownRoot
// must still answer for rather than fault on.
type (
	bareReference  struct{}
	coredReference struct{ core any }
)

func (bareReference) GetRootNode() *yaml.Node  { return nil }
func (bareReference) IsReference() bool        { return true }
func (coredReference) GetRootNode() *yaml.Node { return nil }
func (coredReference) IsReference() bool       { return true }
func (r coredReference) GetCoreAny() any       { return r.core }
