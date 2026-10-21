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
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// These tests drive whole documents through the compiler rather than calling
// ObjectAt directly, and that is not a shortcut: a chain of component aliases
// only exists once speakeasy's resolver has built the resolution info each hop
// reads, which no hand-constructed value reproduces. Driving the compiler is
// what makes the fixture real. It costs an import of the package that imports
// this one, which only an external test package may have.

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

// TestObjectAt_CrossDocumentInternsAtTheDeclarationThere is the
// cross-document counterpart to the same-document sharing tests above. The
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

// TestScopeOf_AChainPastTheBoundIsReadAsNoDocuments pins the scope of an entry
// whose chain outruns maxRefChain: none, so no reference in what it reaches
// reads as internal. Taken to end at the last hop read, it was read in that
// hop's document, which need not hold the end (GitHub #762).
func TestScopeOf_AChainPastTheBoundIsReadAsNoDocuments(t *testing.T) {
	t.Parallel()
	for hops, foreign := range map[int]bool{resolve.MaxRefChain - 1: false, resolve.MaxRefChain + 4: true} {
		doc, _, err := load.Load(t.Context(), 0, openapitest.SourceOf(chainedAliasSpec(hops)), load.Options{})
		require.NoError(t, err)
		require.NotNil(t, doc)
		op, ok := doc.Doc.Paths.Get("/a")
		require.True(t, ok)
		params := op.GetObject().Get().GetParameters()
		require.Len(t, params, 1)

		scope := resolve.ScopeOf[soa.Parameter](resolve.Scope{Doc: doc.Doc, SelfPath: doc.Source.Path}, params[0])

		assert.Equal(t, foreign, scope.Foreign, "%d hops", hops)
		assert.Empty(t, scope.Holder, "%d hops", hops)
	}
}

// TestHops_ListsTheReferencesAChainPassesThrough pins Hops over an alias
// chain: each alias the chain passes after the entry's own $ref, nearest first
// and read as this document's, none for the declaration it ends at or for an
// inline entry, and no more than the bound reads.
func TestHops_ListsTheReferencesAChainPassesThrough(t *testing.T) {
	t.Parallel()
	for hops, want := range map[int]int{0: 0, 3: 3, resolve.MaxRefChain + 4: resolve.MaxRefChain} {
		doc, _, err := load.Load(t.Context(), 0, openapitest.SourceOf(chainedAliasSpec(hops)), load.Options{})
		require.NoError(t, err)
		require.NotNil(t, doc)
		scope := resolve.Scope{Doc: doc.Doc, SelfPath: doc.Source.Path}
		item, ok := doc.Doc.Paths.Get("/a")
		require.True(t, ok)
		assert.Empty(t, resolve.Hops[soa.PathItem](scope, item), "an inline entry has no hops")
		param := item.GetObject().Get().GetParameters()[0]

		got := resolve.Hops[soa.Parameter](scope, param)

		require.Len(t, got, want, "%d hops", hops)
		for i, hop := range got {
			assert.Equal(t, "#/components/parameters/P"+strconv.Itoa(i+1), string(hop.Ref.GetReference()),
				"hop %d is P%d, written as a $ref to the next", i, i)
			assert.False(t, hop.Foreign, "hop %d is written in this document", i)
		}
	}
}

// TestHops_AHopAnotherDocumentHoldsIsForeign pins where Hops reads a hop: past
// a $ref into another document, the hop written there is that document's, and
// a $ref from it back into the source reads the next hop as the source's.
func TestHops_AHopAnotherDocumentHoldsIsForeign(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.yaml")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(`openapi: 3.1.0
info: {title: O, version: "1"}
paths:
  /x: {$ref: 'main.yaml#/paths/~1b'}
`), 0o600))
	src := compilers.Source{Path: mainPath, Data: []byte(`openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: './other.yaml#/paths/~1x'}
  /b: {$ref: '#/paths/~1c'}
  /c: {get: {responses: {"200": {description: OK}}}}
`)}
	doc, _, err := load.Load(t.Context(), 0, src, load.Options{Reader: reader.Disk{}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	item, ok := doc.Doc.Paths.Get("/a")
	require.True(t, ok)

	got := resolve.Hops[soa.PathItem](resolve.Scope{Doc: doc.Doc, SelfPath: mainPath}, item)

	require.Len(t, got, 2, "other.yaml's /x, then the source's /b")
	assert.Equal(t, "main.yaml#/paths/~1b", string(got[0].Ref.GetReference()))
	assert.True(t, got[0].Foreign, "/x is written in other.yaml")
	assert.Equal(t, "#/paths/~1c", string(got[1].Ref.GetReference()))
	assert.False(t, got[1].Foreign, "/b is written in the source")
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
// cases below call this, and without it the loader refuses to leave the
// document, so both would assert on a $ref that was never followed.
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
// declines to depend on it two lines further down for the resolved-object
// fallback ("rather than coupling this compiler to that undocumented
// nil-tolerance"); the guard is what makes that stance hold at the top too.
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

// TestUseDocs_UseSiteSiblingsOverrideTheDeclaration pins the fold at one hop: a
// summary or description written beside a $ref wins field by field over the
// declaration's, and one the entry omits leaves the declaration's standing. A
// hand-built Reference resolves nothing, so it is a chain of one; the parser's
// longer chains are pinned where a document builds them.
func TestUseDocs_UseSiteSiblingsOverrideTheDeclaration(t *testing.T) {
	t.Parallel()
	summary, description := "use-site summary", "use-site description"
	declared := ir.Docs{Summary: "declared summary", Description: "declared description"}

	for _, tc := range []struct {
		name string
		ref  *soa.ReferencedParameter
		want ir.Docs
	}{
		{
			name: "both siblings win",
			ref:  &soa.ReferencedParameter{Summary: &summary, Description: &description},
			want: ir.Docs{Summary: "use-site summary", Description: "use-site description"},
		},
		{
			// The parser fills these two only for an entry that really is a $ref
			// (Reference.Populate), so an inline entry arrives with both nil.
			name: "an inline entry writes neither",
			ref:  &soa.ReferencedParameter{},
			want: declared,
		},
		{
			name: "a description alone leaves the declaration's summary standing",
			ref:  &soa.ReferencedParameter{Description: &description},
			want: ir.Docs{Summary: "declared summary", Description: "use-site description"},
		},
		{
			name: "a nil wrapper reports nothing",
			ref:  nil,
			want: declared,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, cmp.Diff(tc.want, resolve.UseDocs[soa.Parameter](tc.ref, declared)))
		})
	}
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

// TestScopeOf pins the scope each entry's object is read in, given a Foreign
// scope over another document, which tells every answer apart. One whose chain
// ends in ext.yaml is read there. One whose chain ends in the source, however
// it got there, is read as the source's. One written inline, and one that
// resolved nothing, keep the scope given.
func TestScopeOf(t *testing.T) {
	t.Parallel()
	doc, ext := loadElsewhere(t)
	given := resolve.Scope{Doc: doc.Doc, Foreign: true, Holder: "other.yaml"}

	for path, want := range map[string]string{
		"/inline": "other.yaml", "/internal": "", "/ext": ext, "/back": "", "/hop": "", "/missing": "other.yaml",
	} {
		rp, ok := doc.Doc.Paths.Get(path)
		require.True(t, ok, path)
		scope := resolve.ScopeOf[soa.PathItem](given, rp)
		assert.Equal(t, want, scope.Holder, path)
		assert.Equal(t, want != "", scope.Foreign, path)
		assert.Same(t, doc.Doc, scope.Doc, path)
	}
}

// pathItemEnds is the Ends of a scope whose walks meet path items alone, as
// those through elsewhereRoot do.
func pathItemEnds(node any) (resolve.End, bool) {
	rp, ok := node.(*soa.ReferencedPathItem)
	if !ok {
		return resolve.End{}, false
	}
	return resolve.EndOf[soa.PathItem](rp)
}

// TestScopeOf_PastARefReadsWhereItsChainEnds pins the scope of an entry whose
// pointer passes a $ref, which the resolver's walk follows to what it resolved
// to, though it reports the source as what the pointer resolved against. Past
// /ext's, the response is ext.yaml's, read as /ext's own entry reads it, and
// ObjectAt names it by the pointer /ext's own lowering reaches it at, so both
// lowerings read one position alike (GitHub #762). Past /back's or
// /internal's, whose chains end in the source, it is the source's.
func TestScopeOf_PastARefReadsWhereItsChainEnds(t *testing.T) {
	t.Parallel()
	doc, ext := loadElsewhere(t)
	given := resolve.Scope{SelfPath: doc.Source.Path, Doc: doc.Doc, Foreign: true, Holder: "other.yaml",
		Ends: pathItemEnds}
	through, ok := doc.Doc.Paths.Get("/through")
	require.True(t, ok)
	responses := through.GetObject().Get().GetResponses()
	rp, ok := doc.Doc.Paths.Get("/ext")
	require.True(t, ok)
	own := resolve.ScopeOf[soa.PathItem](given, rp)
	require.Equal(t, ext, own.Holder, "/ext's own entry is read where ext.yaml sits")

	for code, want := range map[string]string{"200": ext, "201": "", "202": ""} {
		rr, ok := responses.Get(code)
		require.True(t, ok, code)
		require.NotNil(t, resolve.Object[soa.Response](rr), "%s: the resolver walked through the $ref", code)
		scope := resolve.ScopeOf[soa.Response](given, rr)
		assert.Equal(t, want, scope.Holder, code)
		assert.Equal(t, want != "", scope.Foreign, code)
	}
	rr, ok := responses.Get("200")
	require.True(t, ok)
	_, pointer := resolve.ObjectAt[soa.Response](given.InSource(), rr, "/paths/~1through/get/responses/200")
	assert.Equal(t, jsontext.Pointer("/paths/~1ext/get/responses/200"), pointer,
		"named where /ext's own lowering reaches it, in the scope that lowering reads it in")
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

// TestReferenceEnd_NamesWhereEachKindOfReferenceEnds pins ReferenceEnd for a
// resolved reference of each kind the library resolves, reached as a walk meets
// one, as a value of any type: each ends at the component it names, in the
// source, and nothing else is a reference. A schema's $ref is no such kind any
// more: the compiler resolves it (internal/bind) and the library never does, so
// the one the document writes ends nowhere the library read. Driving the
// compiler is what makes the resolution info real (see parseFull).
func TestReferenceEnd_NamesWhereEachKindOfReferenceEnds(t *testing.T) {
	t.Parallel()
	got, diags, err := load.Load(t.Context(), 0, openapitest.SourceOf(openapitest.EveryKindOfReference), load.Options{})
	require.NoError(t, err)
	require.NotNil(t, got, "%+v", diags)

	ends := map[jsontext.Pointer]bool{}
	schemaRefs := 0
	for item := range soa.Walk(t.Context(), got.Doc) {
		_ = item.Match(soa.Matcher{Any: func(model any) error {
			if js, ok := model.(*oas3.JSONSchema[oas3.Referenceable]); ok && js.IsReference() {
				schemaRefs++
				_, ended := resolve.ReferenceEnd(js)
				assert.False(t, ended, "the library resolved no schema $ref")
				return nil
			}
			if end, ok := resolve.ReferenceEnd(model); ok {
				assert.Same(t, got.Doc, end.Document, "%T ends in the source's model", model)
				ends[end.Pointer] = true
			}
			return nil
		}})
	}

	assert.Equal(t, map[jsontext.Pointer]bool{
		"/components/pathItems/P": true, "/components/parameters/Q": true, "/components/requestBodies/B": true,
		"/components/callbacks/C": true, "/components/headers/H": true, "/components/links/L": true,
		"/components/examples/E": true, "/components/responses/R": true, "/components/securitySchemes/K": true,
	}, ends)
	assert.Positive(t, schemaRefs, "the document writes a schema $ref")
	_, ok := resolve.ReferenceEnd(got.Doc)
	assert.False(t, ok, "a document is no reference")
}

// TestOperationIDAt_ReadsTheOperationWrittenAtAPointer pins the read a mounted
// request body is named by: the operationId of the operation written at a
// pointer, directly or under a path item component, in a document's model and
// in its raw YAML alike, any scalar read as its text, and nothing at a
// position holding another object, one holding nothing, an operation naming
// none or null, or a pointer no token spells.
func TestOperationIDAt_ReadsTheOperationWrittenAtAPointer(t *testing.T) {
	t.Parallel()
	const spec = "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths:\n" +
		"  /a: {post: {operationId: postA}, get: {summary: s}, put: {operationId: 7}, patch: {operationId: ~}}\n" +
		"components:\n  pathItems:\n    P: {put: {operationId: putP}}\n"
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, load.Options{})
	require.NoError(t, err)
	var raw yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(spec), &raw))
	for name, root := range map[string]any{"model": doc.Doc, "raw YAML": raw.Content[0]} {
		assert.Equal(t, "postA", resolve.OperationIDAt(root, "/paths/~1a/post"), name)
		assert.Equal(t, "putP", resolve.OperationIDAt(root, "/components/pathItems/P/put"), name)
		assert.Equal(t, "7", resolve.OperationIDAt(root, "/paths/~1a/put"), name)
		for _, pointer := range []jsontext.Pointer{"/paths/~1a", "/paths/~1a/get", "/paths/~1a/patch", "/paths/~1b/post", "/paths/~1a/post/~2"} {
			assert.Empty(t, resolve.OperationIDAt(root, pointer), "%s: %s", name, pointer)
		}
	}
	assert.Empty(t, resolve.OperationIDAt(raw.Content[0], "/paths/~1a/post/operationId"), "a scalar is no operation")
}
