package load

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// resolveSpec parses spec the way build does before resolving it — the
// pre-parse refusals and an overlay are out of scope here — and resolves it
// through resolveExternal with build's own kind of rebuild closure (re-unmarshal
// the same tree), at path.
func resolveSpec(t *testing.T, spec, path string) (*soa.OpenAPI, []ir.Diagnostic, error) {
	t.Helper()
	data := []byte(spec)
	root, _, err := decodeStream(data)
	require.NoError(t, err)
	releaseAnchors(root)
	doc, err := unmarshal(t.Context(), data, root)
	require.NoError(t, err)
	rebuild := func() (*soa.OpenAPI, error) {
		again, err := unmarshal(t.Context(), data, root)
		return again, err
	}
	resolved, _, _, pass, missed, err := resolveExternal(t.Context(), doc, Siblings{},
		newSourceDocument(path, data, root, judgment{}, newSourceTree(0, overlay.Origin{}, root), nil),
		Options{Reader: reader.Disk{}}, rebuild)
	return resolved, passDiags(pointerAt(0, overlay.Origin{}), pass, missed), err
}

// passDiags is what pass reports, and the documents missed it found
// unprepared, a finding in another document placed at the source's root, as no
// document is listed: nil for no pass, as a failed rebuild leaves.
func passDiags(at func(jsontext.Pointer) ir.Provenance, pass *resolution, missed []usedDocument) []ir.Diagnostic {
	if pass == nil {
		return nil
	}
	place := sitePlacer(at, Others{})
	return append(pass.diags(place), stillUnprepared(place, missed)...)
}

// rootReferencing is a minimal source document whose one path is a reference to
// /x in the document at url.
func rootReferencing(url string) string {
	return "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n" +
		"paths:\n  /x: {$ref: \"" + url + "#/paths/~1x\"}\n"
}

// anchoredExternalDoc anchors both of /x's operations, the way the "an
// operation" case of TestCompile_AnAnchoredEntryCompilesAsItsUnanchoredTwin
// does: the parser folds a path item's methods into a map
// and skips an entry whose value carries an anchor, so an external document
// read without this compiler's own anchor release drops whichever of these the
// resolver parses itself.
const anchoredExternalDoc = `openapi: 3.1.0
info: {title: O, version: "1"}
paths:
  /x:
    get: &g
      operationId: EXTGET
      responses: {"200": {description: ok}}
    put: &p
      operationId: EXTPUT
      responses: {"200": {description: ok}}
`

// assertRecovered requires that doc's /x carries both of anchoredExternalDoc's
// anchored operations — the outcome that fails silently (an empty or
// half-populated path item) when the resolver parsed the external document
// itself instead of this compiler's anchor-released copy.
func assertRecovered(t *testing.T, doc *soa.OpenAPI) {
	t.Helper()
	assertRecoveredAt(t, doc, "/x")
}

// assertRecoveredAt is assertRecovered for the path item at path.
func assertRecoveredAt(t *testing.T, doc *soa.OpenAPI, path string) {
	t.Helper()
	ref, ok := doc.Paths.Get(path)
	require.True(t, ok, "%s is in the resolved document", path)
	item := ref.GetObject()
	require.NotNil(t, item, "%s's reference resolved to a path item", path)
	get := item.Get()
	require.NotNil(t, get, "the anchored GET entry was folded, not skipped")
	assert.Equal(t, "EXTGET", get.GetOperationID())
	put := item.Put()
	require.NotNil(t, put, "the anchored PUT entry was folded, not skipped")
	assert.Equal(t, "EXTPUT", put.GetOperationID())
}

// countingServer starts an httptest.Server serving body to every request, and
// returns it with a counter of how many requests it received.
func countingServer(t *testing.T, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, err := w.Write([]byte(body))
		assert.NoError(t, err)
	}))
	t.Cleanup(srv.Close)
	return srv, &requests
}

// TestLoad_RecoversARespelledExternalReference drives the recovery through the
// public entry point rather than resolveExternal directly, so build's own
// rebuild closure — not a test-supplied stand-in — is what runs and succeeds.
func TestLoad_RecoversARespelledExternalReference(t *testing.T) {
	t.Parallel()
	srv, requests := countingServer(t, anchoredExternalDoc)
	url := "HTTP://" + strings.TrimPrefix(srv.URL, "http://") + "/ext.yaml"
	src := compilers.Source{Path: "root.yaml", Data: []byte(rootReferencing(url))}

	got, diags, err := Load(t.Context(), 0, src, Options{Reader: reader.Disk{}})

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Empty(t, diags)
	assertRecovered(t, got.Doc)
	assert.Equal(t, int32(1), requests.Load())
}

// TestLoad_RecoversARespelledURLBesideAPathItemRef pins the rebuild for a
// reference only the fields beside a path item's $ref write: the second pass
// models those fields again over the rebuilt document, so the anchored
// operations the resolver's own parse would skip are recovered there too. The
// first pass's models hold what that parse built, and keeping them loses both
// operations without a word.
func TestLoad_RecoversARespelledURLBesideAPathItemRef(t *testing.T) {
	t.Parallel()
	srv, requests := countingServer(t, anchoredExternalDoc)
	url := "HTTP://" + strings.TrimPrefix(srv.URL, "http://") + "/ext.yaml"
	src := compilers.Source{Path: "root.yaml", Data: []byte(`openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    $ref: '#/paths/~1b'
    put:
      callbacks:
        cb:
          '{$request.body#/u}': {$ref: '` + url + `#/paths/~1x'}
      responses: {"200": {description: ok}}
  /b:
    get: {responses: {"200": {description: ok}}}
`)}

	got, diags, err := Load(t.Context(), 0, src, Options{Reader: reader.Disk{}})

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Empty(t, diags)
	a, ok := got.Siblings.Of(pathEntry(t, got.Doc, "/a"))
	require.True(t, ok)
	cb, ok := a.Item.Put().GetCallbacks().Get("cb")
	require.True(t, ok)
	expr, ok := cb.GetObject().Get("{$request.body#/u}")
	require.True(t, ok)
	item := expr.GetObject()
	require.NotNil(t, item, "the callback's $ref resolved")
	require.NotNil(t, item.Get(), "the anchored GET entry was folded, not skipped")
	assert.Equal(t, "EXTGET", item.Get().GetOperationID())
	require.NotNil(t, item.Put(), "the anchored PUT entry was folded, not skipped")
	assert.Equal(t, "EXTPUT", item.Put().GetOperationID())
	assert.Equal(t, int32(1), requests.Load(), "the second pass fetched nothing again")
}

// TestLoad_RecoversARespelledURLOnlyANestedReferenceReads pins the rebuild for
// a document only a reference another document writes reads (GitHub #751):
// ext.yaml's callback names other.yaml by a respelled URL, and no walk of the
// source shows that reference, so the documents the pass resolved, not those
// a walk of the source shows, decide whether the anchored operations the
// resolver's own parse would skip are recovered.
func TestLoad_RecoversARespelledURLOnlyANestedReferenceReads(t *testing.T) {
	t.Parallel()
	other, requests := countingServer(t, anchoredExternalDoc)
	otherURL := "HTTP://" + strings.TrimPrefix(other.URL, "http://") + "/other.yaml"
	ext, _ := countingServer(t, `openapi: 3.1.0
info: {title: E, version: "1"}
paths:
  /x:
    get:
      callbacks:
        cb:
          '{$request.body#/u}': {$ref: '`+otherURL+`#/paths/~1x'}
      responses: {"200": {description: ok}}
`)
	src := compilers.Source{Path: "root.yaml", Data: []byte(rootReferencing(ext.URL + "/ext.yaml"))}

	got, diags, err := Load(t.Context(), 0, src, Options{Reader: reader.Disk{}})

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Empty(t, diags)
	x, ok := got.Doc.Paths.Get("/x")
	require.True(t, ok)
	require.NotNil(t, x.GetObject(), "the source's $ref resolved")
	cb, ok := x.GetObject().Get().GetCallbacks().Get("cb")
	require.True(t, ok)
	expr, ok := cb.GetObject().Get("{$request.body#/u}")
	require.True(t, ok)
	item := expr.GetObject()
	require.NotNil(t, item, "ext.yaml's $ref resolved")
	require.NotNil(t, item.Get(), "the anchored GET entry was folded, not skipped")
	assert.Equal(t, "EXTGET", item.Get().GetOperationID())
	require.NotNil(t, item.Put(), "the anchored PUT entry was folded, not skipped")
	assert.Equal(t, "EXTPUT", item.Put().GetOperationID())
	assert.Equal(t, int32(1), requests.Load(), "the second pass fetched nothing again")
}

// TestLoad_ReportsAnUnpreparedReadWhereAnotherDocumentWritesItsReference pins
// that Load reports a document the second pass read unprepared, placed in the
// document writing the reference: the rebuild has /y reach mid.yaml, whose
// callback names other.yaml by a respelled URL the first pass never read.
func TestLoad_ReportsAnUnpreparedReadWhereAnotherDocumentWritesItsReference(t *testing.T) {
	t.Parallel()
	ext, _ := countingServer(t, anchoredExternalDoc)
	other, _ := countingServer(t, anchoredExternalDoc)
	extURL := "HTTP://" + strings.TrimPrefix(ext.URL, "http://") + "/ext.yaml"
	otherURL := "HTTP://" + strings.TrimPrefix(other.URL, "http://") + "/other.yaml"
	mid, _ := countingServer(t, "openapi: 3.1.0\ninfo: {title: M, version: \"1\"}\npaths:\n  /x:\n    get:\n"+
		"      callbacks: {cb: {'{$request.body#/u}': {$ref: '"+otherURL+"#/paths/~1x'}}}\n"+
		"      responses: {\"200\": {description: ok}}\n")
	_, rebuild := secondPassSpecs(t, extURL, mid.URL+"/mid.yaml")
	src := compilers.Source{Path: "root.yaml", Data: []byte(rootReferencing(extURL))}
	opts := Options{Reader: reader.Disk{}, rebuildDoc: func(context.Context, []byte, *yaml.Node) (*soa.OpenAPI, error) {
		return rebuild()
	}}

	got, diags, err := Load(t.Context(), 0, src, opts)

	require.NoError(t, err)
	require.NotNil(t, got)
	require.Len(t, diags, 1, "%+v", diags)
	assert.Equal(t, diag.InternalInvariant, diags[0].Code)
	assert.Contains(t, diags[0].Message, otherURL)
	i := slices.IndexFunc(got.Others.List(), func(o Other) bool { return strings.HasSuffix(o.Label, "/mid.yaml") })
	require.GreaterOrEqual(t, i, 0, "%+v", got.Others.List())
	assert.Equal(t, ir.Provenance{Source: got.Others.List()[i].Index, Pointer: "/paths/~1x/get/callbacks/cb/{$request.body#~1u}"},
		diags[0].Provenance, "reported at mid.yaml's $ref")
}

// TestLoad_ARebuildFailureIsWrappedAsRebuildSource drives build's wrap of a
// rebuild failure through the rebuildDoc seam, since the real rebuild repeats an
// unmarshal that already succeeded and cannot fail.
func TestLoad_ARebuildFailureIsWrappedAsRebuildSource(t *testing.T) {
	t.Parallel()
	srv, _ := countingServer(t, anchoredExternalDoc)
	url := "HTTP://" + strings.TrimPrefix(srv.URL, "http://") + "/ext.yaml"
	src := compilers.Source{Path: "root.yaml", Data: []byte(rootReferencing(url))}
	boom := errors.New("rebuild boom")
	opts := Options{
		Reader: reader.Disk{},
		rebuildDoc: func(context.Context, []byte, *yaml.Node) (*soa.OpenAPI, error) {
			return nil, boom
		},
	}

	got, diags, err := Load(t.Context(), 5, src, opts)

	assert.Nil(t, got)
	assert.Nil(t, diags)
	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
	assert.Contains(t, err.Error(), "rebuild source 5")
}

// TestStillUnprepared is a unit test of the report itself. No document makes
// the second pass read one the first did not, as both passes walk alike, so it
// is driven with fabricated usedDocuments. The tests after it reach it through
// resolveExternal with a rebuild that differs from the first model.
func TestStillUnprepared(t *testing.T) {
	t.Parallel()
	place := func(s refSite) ir.Provenance {
		if s.inSource() {
			return ir.Provenance{Source: 3, Pointer: s.pointer}
		}
		return ir.Provenance{Source: 4, Pointer: s.pointer}
	}
	missed := []usedDocument{
		{key: "http://a/1.yaml", site: refSite{pointer: "/paths/~1x"}},
		{key: "http://a/1.yaml", site: refSite{pointer: "/paths/~1y"}}, // same key: reported once
		{key: "http://a/2.yaml", site: refSite{doc: "b.yaml", pointer: "/paths/~1z"}},
	}

	diags := stillUnprepared(place, missed)

	require.Len(t, diags, 2, "one report per distinct key")
	for _, d := range diags {
		assert.Equal(t, ir.SeverityError, d.Severity)
		assert.Equal(t, diag.InternalInvariant, d.Code)
	}
	assert.Contains(t, diags[0].Message, "http://a/1.yaml")
	assert.Equal(t, ir.Provenance{Source: 3, Pointer: "/paths/~1x"}, diags[0].Provenance,
		"reported at the first reference that read it, not at the second one to the same key")
	assert.Contains(t, diags[1].Message, "http://a/2.yaml")
	assert.Equal(t, ir.Provenance{Source: 4, Pointer: "/paths/~1z"}, diags[1].Provenance,
		"a reference another document writes is placed in that document")
}

// modelOf builds spec's model the way build does before resolving it.
func modelOf(t *testing.T, spec string) *soa.OpenAPI {
	t.Helper()
	data := []byte(spec)
	root, _, err := decodeStream(data)
	require.NoError(t, err)
	releaseAnchors(root)
	doc, err := unmarshal(t.Context(), data, root)
	require.NoError(t, err)
	return doc
}

// secondPassSpecs returns the source resolveExternal is given, whose one path
// reaches extURL, and a rebuild of it that also has /y reach otherURL: a
// document the first pass never reads.
func secondPassSpecs(t *testing.T, extURL, otherURL string) (string, func() (*soa.OpenAPI, error)) {
	t.Helper()
	second := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n" +
		"  /x: {$ref: \"" + extURL + "#/paths/~1x\"}\n" +
		"  /y: {$ref: \"" + otherURL + "#/paths/~1x\"}\n"
	return rootReferencing(extURL), func() (*soa.OpenAPI, error) { return modelOf(t, second), nil }
}

// aliasKindsFixture carries one internal $ref of every Referenced* alias
// resolutionTrail switches on, resolved against the document itself (no I/O),
// so TestResolutionTrail can drive the dispatch alone without the recovery
// machinery.
const aliasKindsFixture = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    $ref: "#/components/pathItems/PI"
components:
  pathItems:
    PI:
      get:
        operationId: opX
        parameters:
          - $ref: "#/components/parameters/P"
        requestBody:
          $ref: "#/components/requestBodies/RB"
        responses:
          "200":
            $ref: "#/components/responses/R"
        callbacks:
          cb:
            $ref: "#/components/callbacks/CB"
  parameters:
    P: {name: q, in: query, schema: {type: string}}
  requestBodies:
    RB:
      content:
        application/json:
          schema: {type: object}
          examples:
            ex1:
              $ref: "#/components/examples/EX"
  examples:
    EX: {value: hi}
  responses:
    R:
      description: ok
      headers:
        H:
          $ref: "#/components/headers/H2"
      links:
        L:
          $ref: "#/components/links/L2"
  headers:
    H2: {schema: {type: string}}
  links:
    L2: {operationId: opX}
  callbacks:
    CB:
      "{$request.body#/url}":
        post: {operationId: cbPost, responses: {"200": {description: ok}}}
  securitySchemes:
    S:
      $ref: "#/components/securitySchemes/Actual"
    Actual: {type: apiKey, in: header, name: X-Key}
`

// resolvedModels resolves doc and walks it, keeping the model Walk visits under
// each alias kind resolutionTrail switches on that is itself a $ref.
//
// Walk visits a path item, header, link or security scheme twice: as the $ref
// this fixture wrote, and as an inline wrapper around the resolved content,
// where IsReference is false and GetReferenceResolutionInfo nil. Keeping the
// sighting that IsReference suits a fixture with one reference of each kind.
func resolvedModels(t *testing.T, doc *soa.OpenAPI) map[string]any {
	t.Helper()
	out := map[string]any{}
	keep := func(key string, v any, isRef bool) {
		if isRef {
			out[key] = v
		}
	}
	for item := range soa.Walk(t.Context(), doc) {
		err := item.Match(soa.Matcher{
			ReferencedPathItem: func(v *soa.ReferencedPathItem) error {
				keep("pathItem", v, v.IsReference())
				return nil
			},
			ReferencedParameter: func(v *soa.ReferencedParameter) error {
				keep("parameter", v, v.IsReference())
				return nil
			},
			ReferencedHeader: func(v *soa.ReferencedHeader) error {
				keep("header", v, v.IsReference())
				return nil
			},
			ReferencedRequestBody: func(v *soa.ReferencedRequestBody) error {
				keep("requestBody", v, v.IsReference())
				return nil
			},
			ReferencedResponse: func(v *soa.ReferencedResponse) error {
				keep("response", v, v.IsReference())
				return nil
			},
			ReferencedExample: func(v *soa.ReferencedExample) error {
				keep("example", v, v.IsReference())
				return nil
			},
			ReferencedLink: func(v *soa.ReferencedLink) error {
				keep("link", v, v.IsReference())
				return nil
			},
			ReferencedCallback: func(v *soa.ReferencedCallback) error {
				keep("callback", v, v.IsReference())
				return nil
			},
			ReferencedSecurityScheme: func(v *soa.ReferencedSecurityScheme) error {
				keep("securityScheme", v, v.IsReference())
				return nil
			},
		})
		require.NoError(t, err)
	}
	return out
}

// TestResolutionTrail covers resolutionTrail's dispatch: each Referenced* alias
// arm, a chain of them (a direct reference, a chain through three documents,
// and a chain whose target another reference shares), and the default arm for a
// model none of the cases name, a schema among them: the compiler binds its
// $ref itself. It also covers where a trail stops: at nothing for a resolution
// that ended on an object, at the reference that failed for one that did not.
func TestResolutionTrail(t *testing.T) {
	t.Parallel()

	t.Run("each alias kind", func(t *testing.T) {
		t.Parallel()
		doc, diags, err := resolveSpec(t, aliasKindsFixture, "root.yaml")
		require.NoError(t, err)
		assert.Empty(t, diags)
		models := resolvedModels(t, doc)

		for _, kind := range []string{"pathItem", "parameter", "requestBody", "response", "example", "header",
			"link", "callback", "securityScheme"} {
			t.Run(kind, func(t *testing.T) {
				model, ok := models[kind]
				require.True(t, ok, "fixture produced a resolved %s", kind)
				got := resolutionTrail(model)
				assert.Equal(t, trail{docs: []string{"root.yaml"}, endsInSource: true}, got,
					"an internal reference's one hop is the source document itself")
			})
		}
	})

	t.Run("a chain of reference objects", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		const hdr = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  responses:\n"
		writeFile(t, dir, "c.yaml", hdr+"    C: {$ref: \"./a.yaml#/components/responses/A\"}\n")
		writeFile(t, dir, "a.yaml", hdr+
			"    A: {$ref: \"#/components/responses/X\"}\n"+
			"    A2: {$ref: \"#/components/responses/X\"}\n"+
			"    X: {$ref: \"./b.yaml#/components/responses/B\"}\n")
		writeFile(t, dir, "b.yaml", hdr+"    B: {description: ok}\n")
		aPath, bPath, cPath := filepath.Join(dir, "a.yaml"), filepath.Join(dir, "b.yaml"), filepath.Join(dir, "c.yaml")
		// Shared reaches X after Chained does, which leaves X's parent on Shared's
		// route: a chain read off the shared target would be Shared's.
		root := hdr +
			"    Direct: {$ref: \"" + bPath + "#/components/responses/B\"}\n" +
			"    Chained: {$ref: \"" + cPath + "#/components/responses/C\"}\n" +
			"    Shared: {$ref: \"" + aPath + "#/components/responses/A2\"}\n" +
			"    Local: {$ref: \"#/components/responses/Direct\"}\n"

		doc, diags, err := resolveSpec(t, root, filepath.Join(dir, "root.yaml"))
		require.NoError(t, err)
		assert.Empty(t, diags)

		for _, tc := range []struct {
			name string
			want []string
			why  string
		}{
			{"Direct", []string{bPath}, "a direct reference is one hop, not its document twice"},
			{"Chained", []string{cPath, aPath, aPath, bPath}, "every hop, its own first, though a later reference shares its target"},
			{"Shared", []string{aPath, aPath, bPath}, "an internal hop names the document it was read in"},
			{"Local", []string{filepath.Join(dir, "root.yaml"), bPath}, "a trail out of the source ends outside it"},
		} {
			response, ok := doc.Components.Responses.Get(tc.name)
			require.True(t, ok)
			assert.Equal(t, trail{docs: tc.want}, resolutionTrail(response), "%s: %s", tc.name, tc.why)
		}
	})

	t.Run("a resolution that failed", func(t *testing.T) {
		t.Parallel()
		doc, _, err := resolveSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  responses:
    Direct: {$ref: '#/components/responses/Ghost'}
    Chained: {$ref: '#/components/responses/Direct'}
`, "root.yaml")
		require.NoError(t, err)

		for name, want := range map[string]trail{
			"Direct":  {stopped: "#/components/responses/Ghost"},
			"Chained": {docs: []string{"root.yaml"}, stopped: "#/components/responses/Ghost", endsInSource: true},
		} {
			ref, ok := doc.Components.Responses.Get(name)
			require.True(t, ok)
			assert.Equal(t, want, resolutionTrail(ref), "response %s stops at the reference that failed", name)
		}
	})

	t.Run("the default arm", func(t *testing.T) {
		t.Parallel()
		assert.Zero(t, resolutionTrail(42))
		assert.Zero(t, resolutionTrail(&soa.Info{}))
		assert.Zero(t, resolutionTrail(nil))
		assert.Zero(t, resolutionTrail(oas3.NewJSONSchemaFromReference("#/components/schemas/S")), "a schema's $ref is bound, not resolved")
	})
}

// TestResolutionTrail_StopsAtMaxResolutionHops proves the bounded-everything
// cap on both kinds of chain resolutionTrail follows: one of a document past
// maxResolutionHops is cut at it rather than followed to its end, and says so,
// so that findingPlace names no document of the trail as a finding's.
func TestResolutionTrail_StopsAtMaxResolutionHops(t *testing.T) {
	t.Parallel()
	const hdr = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n"
	for _, tc := range []struct {
		name      string
		end, link string // the last document's body, and every other's: %s is the next file
		first     func(doc *soa.OpenAPI) any
	}{
		{
			name: "a path item",
			end:  hdr + "paths:\n  /x:\n    get: {operationId: end, responses: {\"200\": {description: ok}}}\n",
			link: hdr + "paths:\n  /x: {$ref: \"%s#/paths/~1x\"}\n",
			first: func(doc *soa.OpenAPI) any {
				ref, _ := doc.Paths.Get("/x")
				return ref
			},
		},
		{
			name: "a response",
			end:  hdr + "paths: {}\ncomponents:\n  responses:\n    S: {description: ok}\n",
			link: hdr + "paths: {}\ncomponents:\n  responses:\n    S: {$ref: \"%s#/components/responses/S\"}\n",
			first: func(doc *soa.OpenAPI) any {
				response, _ := doc.Components.Responses.Get("S")
				return response
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			const chainLen = maxResolutionHops + 8
			for i := range chainLen {
				body := tc.end
				if i < chainLen-1 {
					body = fmt.Sprintf(tc.link, filepath.Join(dir, fmt.Sprintf("f%d.yaml", i+1)))
				}
				writeFile(t, dir, fmt.Sprintf("f%d.yaml", i), body)
			}
			root := fmt.Sprintf(tc.link, filepath.Join(dir, "f0.yaml"))

			doc, diags, err := resolveSpec(t, root, filepath.Join(dir, "root.yaml"))
			require.NoError(t, err)
			assert.Empty(t, diags)

			first := tc.first(doc)
			require.NotNil(t, first)
			got := resolutionTrail(first)
			assert.Len(t, got.docs, maxResolutionHops,
				"a %d-document chain is capped at %d hops", chainLen, maxResolutionHops)
			assert.True(t, got.cut, "and the trail records that it was cut")
		})
	}
}

// writeFile writes content to name under dir, failing the test on error.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

// TestPreparedFor covers externalReads.preparedFor's four outcomes: a file key
// found directly, a URL key found by normalizing used the way the resolver's
// own request would have been built, a key http.NewRequest itself refuses, and
// a key nothing was ever recorded under.
func TestPreparedFor(t *testing.T) {
	t.Parallel()

	t.Run("a file key", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads(sourceDocument{})
		tree := &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}
		r.recordTree("/tmp/doc.yaml", tree, nil)

		got, ok := r.preparedFor("/tmp/doc.yaml")

		require.True(t, ok)
		assert.Same(t, tree, got)
	})

	t.Run("a file key http.NewRequest would respell", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads(sourceDocument{})
		tree := &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}
		r.recordTree("dir/with space/doc.yaml", tree, nil)

		got, ok := r.preparedFor("dir/with space/doc.yaml")

		require.True(t, ok, "the path is its own key; the request spelling %20 is not")
		assert.Same(t, tree, got)
	})

	t.Run("a URL key found by its request spelling", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads(sourceDocument{})
		tree := &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}
		// The spelling external.Do actually stores under: the request's own URL.
		req, err := http.NewRequest(http.MethodGet, "HTTP://host/doc.yaml", nil)
		require.NoError(t, err)
		r.recordTree(req.URL.String(), tree, nil)

		// The resolver's own, unnormalized key.
		got, ok := r.preparedFor("HTTP://host/doc.yaml")

		require.True(t, ok)
		assert.Same(t, tree, got)
	})

	t.Run("a URL key spelled with an empty port", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads(sourceDocument{})
		tree := &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}
		// http.NewRequest drops the empty port, so external.Do stores it without one.
		r.recordTree("http://host/doc.yaml", tree, nil)

		got, ok := r.preparedFor("http://host:/doc.yaml")

		require.True(t, ok)
		assert.Same(t, tree, got)
	})

	t.Run("a key http.NewRequest rejects", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads(sourceDocument{})

		_, ok := r.preparedFor("http://host/\x00bad")

		assert.False(t, ok)
	})

	t.Run("a missing key", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads(sourceDocument{})

		_, ok := r.preparedFor("/no/such/file.yaml")

		assert.False(t, ok)
	})
}

// TestHasAnchor covers both arms: a tree with no anchor anywhere, and one whose
// only anchor is several levels deep.
func TestHasAnchor(t *testing.T) {
	t.Parallel()

	t.Run("no anchor anywhere", func(t *testing.T) {
		t.Parallel()
		root, err := parseTree([]byte("a: {b: [1, 2]}\n"))
		require.NoError(t, err)

		assert.False(t, hasAnchor(root))
	})

	t.Run("an anchor deep in the tree", func(t *testing.T) {
		t.Parallel()
		root, err := parseTree([]byte("a: {b: [1, &x 2]}\n"))
		require.NoError(t, err)

		assert.True(t, hasAnchor(root))
	})
}

// TestUnprepared covers unprepared's edge arms directly: a document this
// compile prepared is excluded by pointer identity, a document the resolver
// parsed itself is reported, a cached value that is not a *yaml.Node is not
// mistaken for one, and a key with nothing cached at all is skipped.
func TestUnprepared(t *testing.T) {
	t.Parallel()

	t.Run("a document the resolver parsed itself is reported", func(t *testing.T) {
		t.Parallel()
		doc := &soa.OpenAPI{}
		doc.InitCache()
		tree := &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}
		doc.StoreExternalDocumentInCache("k", tree)
		read := newExternalReads(sourceDocument{}) // nothing of ours is cached under "k"

		got := unprepared(doc, read, []usedDocument{{key: "k"}})

		require.Len(t, got, 1)
		assert.Equal(t, "k", got[0].key)
	})

	t.Run("a document this compile prepared is not reported", func(t *testing.T) {
		t.Parallel()
		doc := &soa.OpenAPI{}
		doc.InitCache()
		tree := &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}
		doc.StoreExternalDocumentInCache("k", tree)
		read := newExternalReads(sourceDocument{})
		read.recordTree("k", tree, nil) // the same pointer: ours

		got := unprepared(doc, read, []usedDocument{{key: "k"}})

		assert.Empty(t, got)
	})

	t.Run("a cached value that is not a yaml.Node", func(t *testing.T) {
		t.Parallel()
		doc := &soa.OpenAPI{}
		doc.InitCache()
		doc.StoreExternalDocumentInCache("k", "not a tree")
		read := newExternalReads(sourceDocument{})

		got := unprepared(doc, read, []usedDocument{{key: "k"}})

		assert.Empty(t, got, "a non-*yaml.Node cached value is not something the resolver parsed as a tree")
	})

	t.Run("a key with nothing cached", func(t *testing.T) {
		t.Parallel()
		doc := &soa.OpenAPI{}
		doc.InitCache()
		read := newExternalReads(sourceDocument{})

		got := unprepared(doc, read, []usedDocument{{key: "missing"}})

		assert.Empty(t, got)
	})
}

// TestChain_EndsOnNothing pins when a resolution's chain is said to end on a
// position holding no object (GitHub #1044): it reached the position its last
// hop names and built nothing there. A chain that holds no record, was cut, or
// stopped at a reference reached no position, whatever its records say, and
// only the last record's node is read.
func TestChain_EndsOnNothing(t *testing.T) {
	t.Parallel()
	built := &yaml.Node{Kind: yaml.MappingNode}
	for _, tc := range []struct {
		name string
		c    chain
		want bool
	}{
		{"no records", chain{}, false},
		{"a cut chain", chain{cut: true, records: []record{{}}}, false},
		{"a chain that stopped at a reference", chain{stopped: "#/components/parameters/P", records: []record{{}}}, false},
		{"a last record that built no node", chain{records: []record{{node: built}, {}}}, true},
		{"a single record that built no node", chain{records: []record{{}}}, true},
		{"a last record that built a node", chain{records: []record{{}, {node: built}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.c.endsOnNothing())
		})
	}
}

// TestStandIn pins the stand-in held for each kind of reference object: an
// empty object of the kind, which mend knows to stand for the object. A
// resolvable of no known kind has none.
func TestStandIn(t *testing.T) {
	t.Parallel()
	read := newExternalReads(sourceDocument{})
	for _, obj := range []resolvable{
		&soa.ReferencedPathItem{}, &soa.ReferencedParameter{}, &soa.ReferencedHeader{},
		&soa.ReferencedRequestBody{}, &soa.ReferencedResponse{}, &soa.ReferencedExample{},
		&soa.ReferencedLink{}, &soa.ReferencedCallback{}, &soa.ReferencedSecurityScheme{},
	} {
		stand, ok := read.standIn(obj)
		require.True(t, ok, "%T", obj)
		assert.IsType(t, obj, stand)
		assert.NotSame(t, obj, stand)
		stood, ok := read.stoodFor(stand)
		require.True(t, ok, "%T", obj)
		assert.Same(t, obj, stood)
	}
	_, ok := read.standIn(fakeResolvable{})
	assert.False(t, ok, "a kind of no known stand-in is held as none")
	_, ok = read.stoodFor(&soa.ReferencedResponse{})
	assert.False(t, ok, "an object made elsewhere stands for nothing")
}
