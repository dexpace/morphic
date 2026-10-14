package load

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/ir"
)

// TestSiblings_BesideNamesTheFieldAPointerPasses pins which pointers pass
// through the fields beside a path item's $ref: one naming a key written there,
// at any position the walk met the use site at, an alias's included, and at a
// key a `<<` merge key supplies there. The first use site on the pointer is the
// one named. A key nothing writes beside the $ref, the use site itself, and a
// $ref only a merge key supplies, which the library reads as an inline item,
// are not, and an empty registry passes none.
func TestSiblings_BesideNamesTheFieldAPointerPasses(t *testing.T) {
	t.Parallel()
	doc := modelOf(t, siblingSpec+`  /e:
    $ref: '#/paths/~1b'
    <<: {patch: {responses: {"200": {description: ok}}}}
`)
	sibs, _, faults := modelSiblings(t.Context(), doc, scan.InSource(0))
	require.Empty(t, faults)

	cases := []struct {
		pointer  jsontext.Pointer
		use, key string
	}{
		{"/paths/~1a/put/responses", "/paths/~1a", "put"},
		{"/paths/~1a/summary", "/paths/~1a", "summary"},
		{"/paths/~1d/put", "/paths/~1d", "put"},
		{"/paths/~1e/patch", "/paths/~1e", "patch"},
		{"/paths/~1a/put/callbacks/cb/{$request.body#~1url}/delete", "/paths/~1a", "put"},
		{"/paths/~1a/get", "", ""},
		{"/paths/~1a", "", ""},
		{"/paths/~1c/put", "", ""},
		{"/paths/~1m/post", "", ""},
		{"", "", ""},
	}
	for _, tc := range cases {
		use, key, ok := sibs.Beside(tc.pointer)
		assert.Equal(t, tc.use != "", ok, "%s", tc.pointer)
		assert.Equal(t, jsontext.Pointer(tc.use), use, "%s", tc.pointer)
		assert.Equal(t, tc.key, key, "%s", tc.pointer)
	}
	_, _, ok := Siblings{}.Beside("/paths/~1a/put")
	assert.False(t, ok, "an empty registry holds no use site")
}

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
		Options{AllowExternalRefs: true})
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

// TestSiblings_WritesReadsTheNodeAnAnswerIsWrittenAs pins Writes on the answers
// a copy gives: one the resolver builds from the node the fields write, as for
// a pointer through an alias's position, is that field, though it is not the
// object their model holds. An answer with no node of its own is none.
func TestSiblings_WritesReadsTheNodeAnAnswerIsWrittenAs(t *testing.T) {
	t.Parallel()
	spec := strings.Replace(besideChainSpec, "  /a:\n", "  /a: &a\n", 1)
	spec = strings.Replace(spec, "components:\n", `  /z: *a
  /d:
    post:
      requestBody: {$ref: 'spec.yaml#/paths/~1z/get/requestBody'}
      responses: {"200": {description: ok}}
components:
`, 1)
	got, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)},
		Options{AllowExternalRefs: true})
	require.NoError(t, err)
	for _, d := range diags {
		assert.NotEqual(t, jsontext.Pointer("/paths/~1d/post/requestBody"), d.Provenance.Pointer, "%s", d.Message)
	}

	d := pathEntry(t, got.Doc, "/d").GetObject().Post().GetRequestBody()
	copied := d.GetReferenceResolutionInfo().Object
	a, ok := got.Siblings.Of(pathEntry(t, got.Doc, "/a"))
	require.True(t, ok)
	require.NotSame(t, a.Item.Get().GetRequestBody(), copied, "the resolver built a copy")
	assert.True(t, got.Siblings.Writes("/paths/~1z/get/requestBody", copied))

	got.Siblings.write("/paths/~1a/get/requestBody", bareReference{})
	assert.False(t, got.Siblings.Writes("/paths/~1a/get/requestBody", bareReference{}))
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
	sibs, _, _ := modelSiblings(t.Context(), modelOf(t, siblingSpec), scan.InSource(0))
	require.NotEmpty(t, sibs.list, "a registry holding a model is walked")
	panics := func(func(soa.WalkItem) bool) { panic("walk fault") }
	at := func(p jsontext.Pointer) ir.Provenance { return ir.Provenance{Pointer: p} }

	diags := besideRefs(at, sibs, panics, nil)

	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, diag.Validation, diags[0].Code)
	assert.Contains(t, diags[0].Message, "path item $ref check panicked: walk fault")
}

// TestSiblings_BesideReadsAPointerInBoundedWork pins Beside's cost, which every
// hop of every reference pays in this phase and in the lowering: it reads a
// pointer only as deep as some use site's position goes, so a long pointer costs
// what a short one does. Building each prefix of the pointer instead made it
// quadratic in the pointer's length. Allocations count the work, since they do
// not depend on the machine.
func TestSiblings_BesideReadsAPointerInBoundedWork(t *testing.T) {
	sibs, _, _ := modelSiblings(t.Context(), modelOf(t, siblingSpec), scan.InSource(0))
	long := func(head string, tokens int) jsontext.Pointer {
		return jsontext.Pointer(head + strings.Repeat("/x", tokens))
	}
	for _, head := range []string{"/paths/~1a/put", "/paths/~1a/get", "/paths/~1c", "/components"} {
		short := testing.AllocsPerRun(20, func() { sibs.Beside(long(head, 1_000)) })
		longer := testing.AllocsPerRun(20, func() { sibs.Beside(long(head, 100_000)) })
		assert.Equal(t, short, longer, "%s: a hundred times the tokens, the same work", head)
	}
}
