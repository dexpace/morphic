package resolve_test

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// These tests drive whole documents through the compiler rather than calling
// ObjectAt directly, and that is not a shortcut: a chain of component aliases
// only exists once speakeasy's resolver has built the resolution info each hop
// reads, which no hand-constructed value reproduces. Driving the compiler is
// what makes the fixture real. It costs an import of the package that imports
// this one, which only an external test package may have.

// parseFull runs the whole public compiler pipeline over src.
func parseFull(t *testing.T, src string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(context.Background(),
		[]compilers.Source{{Path: "spec.yaml", Data: []byte(src)}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// requireNoErrorDiags fails the test if any diagnostic has error severity.
func requireNoErrorDiags(t *testing.T, diags []ir.Diagnostic) {
	t.Helper()
	d, ok := ir.FirstError(diags)
	require.False(t, ok, "unexpected error diagnostic: %+v", d)
}

// findOp returns the operation whose source name matches.
func findOp(t *testing.T, doc *ir.Document, source string) ir.Operation {
	t.Helper()
	for _, g := range doc.Services[0].Groups {
		for _, op := range g.Operations {
			if op.Name.Source == source {
				return op
			}
		}
	}
	t.Fatalf("operation %q not found", source)
	return ir.Operation{}
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
		rss *soa.ReferencedSecurityScheme
	)
	assert.Nil(t, resolve.Object[soa.PathItem](rpi))
	assert.Nil(t, resolve.Object[soa.Response](rr))
	assert.Nil(t, resolve.Object[soa.Header](rh))
	assert.Nil(t, resolve.Object[soa.Callback](rcb))
	assert.Nil(t, resolve.Object[soa.Parameter](rp))
	assert.Nil(t, resolve.Object[soa.RequestBody](rrb))
	assert.Nil(t, resolve.Object[soa.Example](re))
	assert.Nil(t, resolve.Object[soa.SecurityScheme](rss))
}

// TestObjectAt_CrossDocumentKeepsUseSitePointer is the cross-document
// counterpart to the same-document sharing tests above (issue #107). The
// fixture's operation $refs a parameter and a response into a sibling
// document; both resolve to real objects (resolveAll follows external refs),
// but ObjectAt's internal-pointer check rejects the target because it lives
// in another document, so the use-site pointer is kept rather than a pointer
// into a document this IR has no node for.
func TestObjectAt_CrossDocumentKeepsUseSitePointer(t *testing.T) {
	t.Parallel()
	doc := compileFixture(t, "../../../../testdata/openapi/resolve_main_external_valid.yaml")
	assertCrossDocumentUseSitePointers(t, doc)
}

// TestObjectAt_AliasChainLeavingDocumentKeepsUseSitePointer covers the hop
// the plain cross-document case does not: a chain that starts inside this
// document and only then leaves it (PageAlias -> ./target#/…/Page). The alias
// pointer walked past is itself a one-key $ref object with no schema child, so
// adopting it would fabricate a pointer just as surely as the use site did
// before issue #107 — the walk must fall all the way back to the use site,
// matching what a direct cross-document reference already does.
func TestObjectAt_AliasChainLeavingDocumentKeepsUseSitePointer(t *testing.T) {
	t.Parallel()
	doc := compileFixture(t, "../../../../testdata/openapi/resolve_main_alias_external_valid.yaml")
	assertCrossDocumentUseSitePointers(t, doc)
}

// TestObjectAt_AliasedComponentChainInternsAtFinalDeclaration guards a
// component that is itself a bare $ref to another component (PageAlias ->
// Page, OKAlias -> OK): ObjectAt must walk the whole chain to Page's and
// OK's own declaration, not stop at the one-hop alias pointer, which is a
// one-key $ref object with no schema (or content) child of its own to hoist.
func TestObjectAt_AliasedComponentChainInternsAtFinalDeclaration(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, aliasedComponentChainSpec)
	requireNoErrorDiags(t, diags)
	getA := findOp(t, doc, "getA")
	getB := findOp(t, doc, "getB")

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
	requireNoErrorDiags(t, diags)
	op := findOp(t, doc, "getA")
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
	requireNoErrorDiags(t, diags)
	op := findOp(t, doc, "getA")
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

// compileFixture loads and lowers one on-disk spec, which — unlike a spec
// written inline — is what lets a $ref reach a sibling document. It drives the
// public entry point because that is the only one visible from here, and
// because a cross-document reference is resolved by the loader either way.
func compileFixture(t *testing.T, path string) *ir.Document {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: path, Data: data}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	requireNoErrorDiags(t, diags)
	return doc
}

// assertCrossDocumentUseSitePointers pins the shared outcome of both
// cross-document fixtures: the schemas intern at the operation's own pointers,
// and nothing interns under a /components/ pointer — neither the sibling
// document's (this IR has no node there) nor a local alias's.
func assertCrossDocumentUseSitePointers(t *testing.T, doc *ir.Document) {
	t.Helper()
	wantParamID := ir.TypeID("t/anon/paths/~1a/get/parameters/0/schema")
	wantRespID := ir.TypeID("t/anon/paths/~1a/get/responses/200/content/application~1json/schema")
	_, hasParam := doc.Types[wantParamID]
	_, hasResp := doc.Types[wantRespID]
	assert.True(t, hasParam, "the cross-document parameter schema interns at the use-site pointer")
	assert.True(t, hasResp, "the cross-document response schema interns at the use-site pointer")

	for id := range doc.Types {
		assert.False(t, strings.HasPrefix(string(id), "t/anon/components/"),
			"no type interned under an unaddressable declaration pointer: %s", id)
	}
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
// declines to depend on it two lines further down for the resolved-object
// fallback ("rather than coupling this compiler to that undocumented
// nil-tolerance"); the guard is what makes that stance hold at the top too.
type brittleEntry struct{ obj int }

func (b *brittleEntry) GetObject() *int         { return &b.obj }
func (b *brittleEntry) GetResolvedObject() *int { return &b.obj }
func (b *brittleEntry) GetReference() references.Reference {
	return references.Reference("#/components/schemas/" + strconv.Itoa(b.obj))
}

func (b *brittleEntry) GetReferenceResolutionInfo() *references.ResolveResult[brittleEntry] {
	return nil
}

// TestObject_NilEntryIsNotDereferenced holds the guard itself, which the
// aliased-type test above cannot: an entry that does not tolerate a nil
// receiver must still answer nil rather than panic.
func TestObject_NilEntryIsNotDereferenced(t *testing.T) {
	t.Parallel()
	var ref *brittleEntry
	require.Panics(t, func() { _ = ref.GetObject() },
		"the fixture must be brittle, or this asserts nothing")

	assert.Nil(t, resolve.Object[int, brittleEntry](ref))
}
