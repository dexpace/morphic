package resolve_test

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// The tests of an alias chain read documents the parser's resolver has run
// over, through the whole compiler or the loader, rather than hand-built values:
// a chain only exists once that resolver has built the resolution info each hop
// reads, which no hand-constructed value reproduces. Both import a package that
// imports this one, which only an external test package may.

// parseFull runs the whole public compiler pipeline over src. That reach back
// through openapi is also why openapitest cannot hold it — see that package's
// doc comment.
func parseFull(t *testing.T, src string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{openapitest.SourceOf(src)}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// TestObject_NilEntryIsNil pins the guard every caller relies on: a component
// slot the document leaves empty arrives as a typed nil pointer, and reading an
// object out of it must answer nil rather than dereference it. Each aliased
// type is listed because the constraint is generic and a compile error for one
// instantiation is not one for the others.
func TestObject_NilEntryIsNil(t *testing.T) {
	t.Parallel()
	var (
		rpi *soa.ReferencedPathItem
		rr  *soa.ReferencedResponse
		rh  *soa.ReferencedHeader
		rcb *soa.ReferencedCallback
		rp  *soa.ReferencedParameter
		rrb *soa.ReferencedRequestBody
		re  *soa.ReferencedExample
		rl  *soa.ReferencedLink
		rss *soa.ReferencedSecurityScheme
	)
	assert.Nil(t, resolve.Object[soa.PathItem](rpi))
	assert.Nil(t, resolve.Object[soa.Response](rr))
	assert.Nil(t, resolve.Object[soa.Header](rh))
	assert.Nil(t, resolve.Object[soa.Callback](rcb))
	assert.Nil(t, resolve.Object[soa.Parameter](rp))
	assert.Nil(t, resolve.Object[soa.RequestBody](rrb))
	assert.Nil(t, resolve.Object[soa.Example](re))
	assert.Nil(t, resolve.Object[soa.Link](rl))
	assert.Nil(t, resolve.Object[soa.SecurityScheme](rss))
}

// TestObjectAt_CrossDocumentInternsAtTheDeclarationThere is the
// cross-document counterpart to internal/operation's same-document sharing
// tests, such as TestParams_ComponentRefSharedAcrossOperationsInternsOnce. The
// fixture's operation $refs a parameter and a response into a sibling
// document, and both are lowered at their declaration there: their schemas
// intern under that document's label, with provenance naming it.
//
// Reaching the sibling file is what the case is about, so it opts in to the
// external resolution the compiler does not do on its own.
func TestObjectAt_CrossDocumentInternsAtTheDeclarationThere(t *testing.T) {
	t.Parallel()
	doc := compileFixture(t, "../../../../testdata/openapi/resolve_main_external_valid.yaml")
	assertCrossDocumentDeclarations(t, doc)
}

// TestObjectAt_AliasChainLeavingDocumentInternsAtTheDeclarationThere covers
// the hop the plain cross-document case does not: a chain that starts inside
// this document and only then leaves it (PageAlias -> ./target#/…/Page). The
// alias pointer walked past is a one-key $ref object with no schema child, so
// the walk must go on to the declaration in the sibling document, as a direct
// cross-document reference does, and no ID may spell the alias.
func TestObjectAt_AliasChainLeavingDocumentInternsAtTheDeclarationThere(t *testing.T) {
	t.Parallel()
	doc := compileFixture(t, "../../../../testdata/openapi/resolve_main_alias_external_valid.yaml")
	assertCrossDocumentDeclarations(t, doc)
	for id := range doc.Types {
		assert.NotContains(t, string(id), "Alias", "no ID derived from the alias pointer")
	}
}

// TestObjectAt_AliasedComponentChainInternsAtFinalDeclaration guards a
// component that is itself a bare $ref to another component (PageAlias ->
// Page, OKAlias -> OK): ObjectAt must walk the whole chain to Page's and
// OK's own declaration, not stop at the one-hop alias pointer, which is a
// one-key $ref object with no schema (or content) child of its own to hoist.
func TestObjectAt_AliasedComponentChainInternsAtFinalDeclaration(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, aliasedComponentChainSpec)
	openapitest.RequireNoErrorDiags(t, diags)
	getA := openapitest.FindOp(t, doc, "getA")
	getB := openapitest.FindOp(t, doc, "getB")

	require.Len(t, getA.Params, 1)
	require.Len(t, getB.Params, 1)
	wantParamID := ir.TypeID("t/anon/components/parameters/Page/schema")
	assert.Equal(t, wantParamID, getA.Params[0].Type.Target)
	assert.Equal(t, wantParamID, getB.Params[0].Type.Target,
		"both operations resolve the aliased parameter's final declaration")

	require.Len(t, getA.Responses, 1)
	require.Len(t, getB.Responses, 1)
	wantRespID := ir.TypeID("t/anon/components/responses/OK/content/application~1json/schema")
	assert.Equal(t, wantRespID, getA.Responses[0].Payload.Contents[0].Type.Target)
	assert.Equal(t, wantRespID, getB.Responses[0].Payload.Contents[0].Type.Target,
		"both operations resolve the aliased response's final declaration")

	for id := range doc.Types {
		assert.NotContains(t, string(id), "PageAlias", "no ID derived from the one-hop alias pointer")
		assert.NotContains(t, string(id), "OKAlias", "no ID derived from the one-hop alias pointer")
	}
}

// TestObjectAt_AliasChainWithinBoundReachesDeclaration walks a chain
// several hops deeper than the two-hop regression above, to pin that the walk
// is genuinely iterative rather than a fixed one-or-two-hop peel.
func TestObjectAt_AliasChainWithinBoundReachesDeclaration(t *testing.T) {
	t.Parallel()
	const hops = 8
	doc, diags := parseFull(t, chainedAliasSpec(hops))
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FindOp(t, doc, "getA")
	require.Len(t, op.Params, 1)
	assert.Equal(t, ir.TypeID("t/anon/components/parameters/P8/schema"), op.Params[0].Type.Target,
		"the walk follows every hop to the final declaration")
}

// TestObjectAt_AliasChainBeyondBoundFallsBackToUseSite pins the behaviour
// of resolve.MaxRefChain itself. A chain longer than the bound has no declaration the
// walk can prove it reached, so it must fall back to the use-site pointer —
// terminating, deterministic, and never a mid-chain alias whose only key is
// $ref. Nothing in a real spec looks like this; the bound exists so a
// pathological one cannot make the walk unbounded.
func TestObjectAt_AliasChainBeyondBoundFallsBackToUseSite(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, chainedAliasSpec(resolve.MaxRefChain+4))
	openapitest.RequireNoErrorDiags(t, diags)
	op := openapitest.FindOp(t, doc, "getA")
	require.Len(t, op.Params, 1)
	assert.Equal(t, ir.TypeID("t/anon/paths/~1a/get/parameters/0/schema"), op.Params[0].Type.Target,
		"an over-long chain keeps the one pointer that is certainly addressable")
}

// chainedAliasSpec builds a spec whose operation $refs P0 at the head of an
// alias chain P0 -> P1 -> ... -> P<hops>, where only P<hops> is a real
// parameter declaration. The walk therefore takes hops+1 steps: one from the
// use site, then one per alias.
func chainedAliasSpec(hops int) string {
	var b strings.Builder
	b.WriteString(`openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: getA
      parameters:
        - {$ref: '#/components/parameters/P0'}
      responses: {"200": {description: ok}}
components:
  parameters:
`)
	for i := range hops {
		b.WriteString("    P" + strconv.Itoa(i) + ": {$ref: '#/components/parameters/P" + strconv.Itoa(i+1) + "'}\n")
	}
	b.WriteString("    P" + strconv.Itoa(hops) + ": {name: page, in: query, schema: {type: string, enum: [a, b]}}\n")
	return b.String()
}

const aliasedComponentChainSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: getA
      parameters:
        - {$ref: '#/components/parameters/PageAlias'}
      responses:
        "200": {$ref: '#/components/responses/OKAlias'}
  /b:
    get:
      operationId: getB
      parameters:
        - {$ref: '#/components/parameters/PageAlias'}
      responses:
        "200": {$ref: '#/components/responses/OKAlias'}
components:
  parameters:
    PageAlias: {$ref: '#/components/parameters/Page'}
    Page: {name: page, in: query, schema: {type: string, enum: [p, q]}}
  responses:
    OKAlias: {$ref: '#/components/responses/OK'}
    OK:
      description: ok
      content:
        application/json:
          schema: {type: object, properties: {n: {type: string}}}
`

// compileFixture loads and lowers one on-disk spec with external references
// allowed, which — unlike a spec written inline under the compiler's own
// defaults — is what lets a $ref reach a sibling document. It drives the public
// entry point because that is the only one visible from here, and because a
// cross-document reference is resolved by the loader either way.
//
// The opt-in is the point rather than boilerplate: only the two cross-document
// cases call this, and without it the loader refuses to leave the document, so
// both would assert on a $ref that was never followed.
func compileFixture(t *testing.T, path string) *ir.Document {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: path, Data: data}},
		compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	openapitest.RequireNoErrorDiags(t, diags)
	return doc
}

// assertCrossDocumentDeclarations pins the shared outcome of both
// cross-document fixtures. The registry holds the sibling document's two
// schemas, in the namespace its label qualifies, declared where it declares
// them (Document.Sources index 1), and the string they share; nothing interns
// at a use site or under the entry document's components. The parameter keeps
// the ID its mount gives it, its provenance names the declaration, and the
// operation stays the entry document's.
func assertCrossDocumentDeclarations(t *testing.T, doc *ir.Document) {
	t.Helper()
	const label = "resolve_target_valid.yaml"
	require.Len(t, doc.Sources, 2, "the entry document and the sibling it reads")
	assert.True(t, strings.HasSuffix(doc.Sources[1].Path, label), "Sources[1] is %s", doc.Sources[1].Path)

	sibling := ids.In(label)
	const paramPtr = "/components/parameters/Page"
	const respPtr = "/components/responses/OK/content/application~1json/schema"
	wantParamID := sibling.AnonType(paramPtr + "/schema")
	wantRespID := sibling.AnonType(respPtr)

	got := make([]string, 0, len(doc.Types))
	for id := range doc.Types {
		got = append(got, string(id))
	}
	slices.Sort(got)
	want := []string{string(wantParamID), string(wantRespID), string(ir.PrimTypeID(ir.PrimString))}
	slices.Sort(want)
	assert.Empty(t, cmp.Diff(want, got), "the types in the registry")

	for id, pointer := range map[ir.TypeID]jsontext.Pointer{wantParamID: paramPtr + "/schema", wantRespID: respPtr} {
		td, ok := doc.Types[id]
		require.True(t, ok, "%s is in the registry", id)
		assert.Equal(t, ir.Provenance{Source: 1, Pointer: pointer}, td.Common().Provenance, "where %s is declared", id)
	}

	op := openapitest.FindOp(t, doc, "a")
	assert.Equal(t, ir.Provenance{Source: 0, Pointer: "/paths/~1a/get"}, op.Provenance, "the operation is the entry document's")
	require.Len(t, op.Params, 1)
	assert.Equal(t, ir.ParamID("param/openapi/paths/~1a/get/parameters/page/query"), op.Params[0].ID, "by its mount")
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: paramPtr}, op.Params[0].Provenance, "declared in the sibling")
	assert.Equal(t, wantParamID, op.Params[0].Type.Target)
	require.Len(t, op.Responses, 1)
	assert.Equal(t, wantRespID, op.Responses[0].Payload.Contents[0].Type.Target)
}

// TestObject_UnresolvedReferenceHasNoObject pins the fallback between the two
// getters. An entry that is a $ref has no inline object, so the answer can only
// come from what the reference resolved to — and when it resolved to nothing,
// the answer is nothing rather than a panic or a zero value the caller would
// lower as though the document had written it.
func TestObject_UnresolvedReferenceHasNoObject(t *testing.T) {
	t.Parallel()
	ref := &soa.ReferencedSecurityScheme{}
	valErrs, err := marshaller.Unmarshal(t.Context(),
		strings.NewReader("$ref: '#/components/securitySchemes/Missing'"), ref)
	require.NoError(t, err)
	require.Empty(t, valErrs, "the fixture parses cleanly")
	require.Nil(t, ref.GetObject(), "a $ref entry carries no inline object, which is the state under test")

	assert.Nil(t, resolve.Object[soa.SecurityScheme](ref))
}

// brittleEntry is a reference-or-inline entry whose getters dereference their
// receiver. Every speakeasy alias happens to tolerate a nil one, which is why
// TestObject_NilEntryIsNil passes with or without the guard Object opens with —
// the entries it lists cannot tell the two apart.
//
// This one can. Nothing about the constraint promises nil-tolerance, and Object
// declines to depend on it further down, for the resolved-object fallback
// ("rather than coupling this compiler to that undocumented nil-tolerance");
// the guard is what makes that stance hold at the top too.
type brittleEntry struct{ obj int }

func (b *brittleEntry) GetObject() *int         { return &b.obj }
func (b *brittleEntry) GetObjectAny() any       { return &b.obj }
func (b *brittleEntry) GetResolvedObject() *int { return &b.obj }
func (b *brittleEntry) GetReference() references.Reference {
	return references.Reference("#/components/schemas/" + strconv.Itoa(b.obj))
}

func (b *brittleEntry) GetReferenceResolutionInfo() *references.ResolveResult[brittleEntry] {
	return nil
}

// TestObject_NilEntryIsNotDereferenced holds the guard itself, which
// TestObject_NilEntryIsNil cannot: an entry that does not tolerate a nil
// receiver must still answer nil rather than panic.
func TestObject_NilEntryIsNotDereferenced(t *testing.T) {
	t.Parallel()
	var ref *brittleEntry
	require.Panics(t, func() { _ = ref.GetObject() },
		"the fixture must be brittle, or this asserts nothing")

	assert.Nil(t, resolve.Object[int, brittleEntry](ref))
}

// elsewhereRoot is a source whose path items are reached each way an entry can
// be: written inline, by an internal $ref, by a $ref into ext.yaml, by one into
// ext.yaml that comes back into the source, once directly and once through a
// further alias there, and by one naming nothing. The responses of /through are
// reached by pointers passing the $ref of /ext, /back and /internal on the way.
const elsewhereRoot = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /inline: {get: {responses: {"200": {description: ok}}}}
  /internal: {$ref: '#/components/pathItems/P'}
  /ext: {$ref: './ext.yaml#/paths/~1x'}
  /back: {$ref: './ext.yaml#/paths/~1back'}
  /hop: {$ref: './ext.yaml#/paths/~1hop'}
  /missing: {$ref: '#/components/pathItems/Nope'}
  /through:
    get:
      responses:
        "200": {$ref: '#/paths/~1ext/get/responses/200'}
        "201": {$ref: '#/paths/~1back/get/responses/200'}
        "202": {$ref: '#/paths/~1internal/get/responses/200'}
components:
  pathItems:
    P: {get: {responses: {"200": {description: ok}}}}
    Q: {$ref: '#/components/pathItems/P'}
`

// loadElsewhere loads elsewhereRoot beside the ext.yaml its entries name, and
// returns it with that file's path.
func loadElsewhere(t *testing.T) (*load.Document, string) {
	t.Helper()
	dir := t.TempDir()
	ext := filepath.Join(dir, "ext.yaml")
	require.NoError(t, os.WriteFile(ext, []byte(`paths:
  /x: {get: {responses: {"200": {description: ok}}}}
  /back: {$ref: 'root.yaml#/components/pathItems/P'}
  /hop: {$ref: 'root.yaml#/components/pathItems/Q'}
`), 0o600))
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: filepath.Join(dir, "root.yaml"),
		Data: []byte(elsewhereRoot)}, load.Options{Reader: reader.Disk{}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, ext
}

// TestObjectAt_AHopIntoTheSourceReadsOnAsTheSources pins the scope ObjectAt
// reads each hop in. The entry is ext.yaml's, read where ext.yaml sits: its
// $ref names the source's Q, a $ref written in the source, so the next hop
// reads `#/...` as the source's and reaches P. Read as ext.yaml's, it named a
// position there, and the walk fell back to the use site.
func TestObjectAt_AHopIntoTheSourceReadsOnAsTheSources(t *testing.T) {
	t.Parallel()
	doc, ext := loadElsewhere(t)
	rp, ok := doc.Doc.Paths.Get("/hop")
	require.True(t, ok)
	info := rp.GetReferenceResolutionInfo()
	require.NotNil(t, info)
	require.NotNil(t, info.Object, "the source's entry resolved to ext.yaml's")

	scope := resolve.Scope{SelfPath: doc.Source.Path, Doc: doc.Doc, Foreign: true, Holder: ext}
	obj, pointer := resolve.ObjectAt[soa.PathItem](scope, info.Object, "/use")
	require.NotNil(t, obj)
	assert.Equal(t, jsontext.Pointer("/components/pathItems/P"), pointer)
}
