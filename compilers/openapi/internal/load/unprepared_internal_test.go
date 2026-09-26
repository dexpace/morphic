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
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/go-cmp/cmp"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
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
	doc, _, err := unmarshal(t.Context(), data, root)
	require.NoError(t, err)
	rebuild := func() (*soa.OpenAPI, error) {
		again, _, err := unmarshal(t.Context(), data, root)
		return again, err
	}
	return resolveExternal(t.Context(), pointerAt(0, overlay.Origin{}), doc, path, Options{AllowExternalRefs: true}, rebuild)
}

// resolveSpecWith is resolveSpec with a caller-supplied rebuild, for the tests
// that need to observe or fail whether it is called.
func resolveSpecWith(t *testing.T, spec, path string, rebuild func() (*soa.OpenAPI, error)) (*soa.OpenAPI, []ir.Diagnostic, error) {
	t.Helper()
	data := []byte(spec)
	root, _, err := decodeStream(data)
	require.NoError(t, err)
	releaseAnchors(root)
	doc, _, err := unmarshal(t.Context(), data, root)
	require.NoError(t, err)
	return resolveExternal(t.Context(), pointerAt(0, overlay.Origin{}), doc, path, Options{AllowExternalRefs: true}, rebuild)
}

// rootReferencing is a minimal source document whose one path is a reference to
// /x in the document at url.
func rootReferencing(url string) string {
	return "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n" +
		"paths:\n  /x: {$ref: \"" + url + "#/paths/~1x\"}\n"
}

// anchoredExternalDoc anchors both of /x's operations, the way anchors_test.go's
// "an operation" case does: the parser folds a path item's methods into a map
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
	ref, ok := doc.Paths.Get("/x")
	require.True(t, ok, "/x is in the resolved document")
	item := ref.GetObject()
	require.NotNil(t, item, "/x's reference resolved to a path item")
	get := item.Get()
	require.NotNil(t, get, "the anchored GET entry was folded, not skipped")
	assert.Equal(t, "EXTGET", get.GetOperationID())
	put := item.Put()
	require.NotNil(t, put, "the anchored PUT entry was folded, not skipped")
	assert.Equal(t, "EXTPUT", put.GetOperationID())
}

// respellings is the set of URL spellings net/url respells relative to how
// http.NewRequest's caller wrote them, reproducing GitHub #538: the resolver's
// own cache key is the reference's absolute URL as written, while this
// compiler's reader stores the document it prepared under req.URL.String().
//
// A literal space in the path (brief: "/do%20c.yaml served for /do c.yaml") is
// not in this table. Probed separately: url.Parse and http.NewRequest both
// normalize a literal space and its %20 escape to the identical
// "http://host/do%20c.yaml" request key, so the two spellings never produce
// different cache keys through this library's own resolution path, and there is
// nothing here for the recovery to be exercised against.
var respellings = []struct {
	name  string
	spell func(canonical string) string
}{
	{"upper-case scheme", func(c string) string {
		return "HTTP://" + strings.TrimPrefix(c, "http://")
	}},
	{"mixed-case scheme", func(c string) string {
		return "Http://" + strings.TrimPrefix(c, "http://")
	}},
	{"escaped userinfo", func(c string) string {
		return strings.Replace(c, "http://", "http://us%65r@", 1)
	}},
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

// TestResolveExternal_RecoversARespelledURL pins the fix for GitHub #538: for
// each URL spelling net/url respells, the anchored entries the resolver's own
// parse would have skipped are recovered, and the document is fetched exactly
// once — the second pass finds the first pass's prepared tree and bytes under
// the resolver's own key and reads nothing over the wire.
func TestResolveExternal_RecoversARespelledURL(t *testing.T) {
	t.Parallel()
	for _, tc := range respellings {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, requests := countingServer(t, anchoredExternalDoc)
			url := tc.spell(srv.URL + "/ext.yaml")

			doc, diags, err := resolveSpec(t, rootReferencing(url), "root.yaml")

			require.NoError(t, err)
			assert.Empty(t, diags, "recovery leaves no internal-invariant diagnostic")
			assertRecovered(t, doc)
			assert.Equal(t, int32(1), requests.Load(), "the document is fetched exactly once")
		})
	}
}

// TestResolveExternal_RecoversAChainedHop probes a chain: the root's reference
// reaches a.yaml canonically, and a.yaml's own path item is itself a reference
// to a respelled b.yaml. Both documents are fetched exactly once in total, so
// the second pass re-fetched neither — it found both under the keys the first
// pass's hop-by-hop walk (hopDocuments) recorded.
func TestResolveExternal_RecoversAChainedHop(t *testing.T) {
	t.Parallel()
	var reqA, reqB atomic.Int32
	mux := http.NewServeMux()
	var srvURL string // set once the server exists; the handler closes over it
	mux.HandleFunc("/a.yaml", func(w http.ResponseWriter, _ *http.Request) {
		reqA.Add(1)
		bURL := "HTTP://" + strings.TrimPrefix(srvURL, "http://") + "/b.yaml"
		_, err := w.Write([]byte("openapi: 3.1.0\ninfo: {title: A, version: \"1\"}\n" +
			"paths:\n  /x: {$ref: \"" + bURL + "#/paths/~1x\"}\n"))
		assert.NoError(t, err)
	})
	mux.HandleFunc("/b.yaml", func(w http.ResponseWriter, _ *http.Request) {
		reqB.Add(1)
		_, err := w.Write([]byte(anchoredExternalDoc))
		assert.NoError(t, err)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	srvURL = srv.URL

	doc, diags, err := resolveSpec(t, rootReferencing(srv.URL+"/a.yaml"), "root.yaml")

	require.NoError(t, err)
	assert.Empty(t, diags)
	assertRecovered(t, doc)
	assert.Equal(t, int32(1), reqA.Load(), "a.yaml is fetched exactly once")
	assert.Equal(t, int32(1), reqB.Load(), "b.yaml is fetched exactly once; the second pass fetches nothing new")
}

// TestResolveExternal_AnAnchorlessRespellingIsNotRebuilt pins the anyAnchored
// gate: a respelled URL whose document carries no anchor at all has nothing the
// resolver's own parse could have skipped, so the second pass — and the rebuild
// it would cost — never runs.
func TestResolveExternal_AnAnchorlessRespellingIsNotRebuilt(t *testing.T) {
	t.Parallel()
	const plain = "openapi: 3.1.0\ninfo: {title: O, version: \"1\"}\n" +
		"paths:\n  /x:\n    get: {operationId: getX, responses: {\"200\": {description: ok}}}\n"
	srv, requests := countingServer(t, plain)
	url := "HTTP://" + strings.TrimPrefix(srv.URL, "http://") + "/ext.yaml"
	rebuild := func() (*soa.OpenAPI, error) {
		t.Fatal("rebuild must not be called: the respelled document carries no anchor to recover")
		return nil, nil // unreachable: t.Fatal stops this goroutine first
	}

	doc, diags, err := resolveSpecWith(t, rootReferencing(url), "root.yaml", rebuild)

	require.NoError(t, err)
	assert.Empty(t, diags)
	ref, ok := doc.Paths.Get("/x")
	require.True(t, ok)
	get := ref.GetObject().Get()
	require.NotNil(t, get)
	assert.Equal(t, "getX", get.GetOperationID())
	assert.Equal(t, int32(1), requests.Load())
}

// TestResolveExternal_ARebuildErrorIsReturned pins that resolveExternal returns
// a rebuild failure to its caller rather than swallowing it.
//
// It stops at resolveExternal's own boundary and does not chase the error into
// Load's "rebuild source" wrap (compilers/openapi/internal/load/load.go, build):
// build's rebuild closure re-unmarshals the exact same (ctx, src.Data, root)
// its own first, already-successful unmarshal call used, and unmarshal is a
// pure function of those three arguments (probed directly: the same input
// unmarshaled twice always succeeds twice, and an already-canceled context
// changes nothing — unmarshal never inspects ctx). So build's particular
// rebuild closure cannot fail once its own first unmarshal has already
// succeeded, which it must have for execution to reach resolve/rebuild at all;
// only a hard error injected here, as resolveExternal's rebuild parameter
// allows, can reach this branch. build's wrap itself —
// fmt.Errorf("openapi: rebuild source %d: %w", srcIndex, err) — is read off
// load.go rather than exercised, since there is no legitimate input that
// reaches it.
func TestResolveExternal_ARebuildErrorIsReturned(t *testing.T) {
	t.Parallel()
	srv, requests := countingServer(t, anchoredExternalDoc)
	url := "HTTP://" + strings.TrimPrefix(srv.URL, "http://") + "/ext.yaml"
	sentinel := errors.New("rebuild boom")
	rebuild := func() (*soa.OpenAPI, error) { return nil, sentinel }

	doc, diags, err := resolveSpecWith(t, rootReferencing(url), "root.yaml", rebuild)

	assert.Nil(t, doc)
	assert.Nil(t, diags)
	assert.ErrorIs(t, err, sentinel)
	assert.Equal(t, int32(1), requests.Load(), "the first pass still reads the document once before rebuild fails")
}

// otherDocWithAnchoredGetAndFinding is a document reached only through an
// external $ref: an anchored get operation (so the resolver's own parse would
// fold right over it, GitHub #501) whose 200 response carries a finding
// (GitHub #537), and a second, separately anchored response on the same
// operation — proving recovery for the responses map's fold too, not only the
// path item's, which anchoredExternalDoc already covers.
const otherDocWithAnchoredGetAndFinding = `openapi: 3.1.0
info: {title: O, version: "1"}
paths:
  /x:
    get: &g
      operationId: EXTGET
      responses:
        "200":
          description: ok
          headers:
            X:
              schema: {type: string}
              required: notabool
        "404": &n
          description: missing
`

// combinedRootSpec is root.yaml from the reconciliation with m4: two ordinary
// ghost references alongside the $ref to other.yaml at url, so the diagnostic
// list the test below compares is more than the one entry the recovery pass
// touches.
func combinedRootSpec(url string) string {
	return "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n" +
		"  /a:\n    parameters:\n      - {$ref: '#/components/parameters/GhostParam'}\n" +
		"    get: {operationId: getA, responses: {\"200\": {description: ok}}}\n" +
		"  /x: {$ref: \"" + url + "#/paths/~1x\"}\n" +
		"components:\n  schemas:\n    S: {$ref: '#/components/schemas/GhostSchema'}\n"
}

// TestResolveExternal_ASecondPassReportsWhatOnePassWould pins the reconciliation
// with m4's two-pass resolveExternal (see its own doc comment): the second pass
// REPLACES the first pass's diagnostics rather than adding to them, and
// replacing must neither double nor lose a report. The two ordinary ghost
// references in combinedRootSpec are what makes that a claim about the whole
// diagnostic list a compile reports, not only about the one entry the recovery
// touches.
func TestResolveExternal_ASecondPassReportsWhatOnePassWould(t *testing.T) {
	t.Parallel()
	srv, requests := countingServer(t, otherDocWithAnchoredGetAndFinding)

	run := func(url string) (*Document, []ir.Diagnostic) {
		requests.Store(0)
		src := compilers.Source{Path: "root.yaml", Data: []byte(combinedRootSpec(url))}
		got, diags, err := Load(t.Context(), 0, src, Options{AllowExternalRefs: true})
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, int32(1), requests.Load(), "one request per compile")
		return got, diags
	}

	recovered, twoPassDiags := run("HTTP://" + strings.TrimPrefix(srv.URL, "http://") + "/other.yaml")
	_, onePassDiags := run(srv.URL + "/other.yaml")

	if d := cmp.Diff(onePassDiags, twoPassDiags); d != "" {
		t.Errorf("a second pass must report exactly what one pass would (-onePass +twoPass):\n%s", d)
	}

	ref, ok := recovered.Doc.Paths.Get("/x")
	require.True(t, ok)
	item := ref.GetObject()
	require.NotNil(t, item)
	get := item.Get()
	require.NotNil(t, get, "the anchored get was folded, not skipped")
	assert.Equal(t, "EXTGET", get.GetOperationID())
	_, ok = get.GetResponses().Get("404")
	assert.True(t, ok, "the separately anchored 404 was folded, not skipped")
}

// TestLoad_RecoversARespelledExternalReference drives the recovery through the
// public entry point rather than resolveExternal directly, so build's own
// rebuild closure — not a test-supplied stand-in — is what runs and succeeds.
func TestLoad_RecoversARespelledExternalReference(t *testing.T) {
	t.Parallel()
	srv, requests := countingServer(t, anchoredExternalDoc)
	url := "HTTP://" + strings.TrimPrefix(srv.URL, "http://") + "/ext.yaml"
	src := compilers.Source{Path: "root.yaml", Data: []byte(rootReferencing(url))}

	got, diags, err := Load(t.Context(), 0, src, Options{AllowExternalRefs: true})

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Empty(t, diags)
	assertRecovered(t, got.Doc)
	assert.Equal(t, int32(1), requests.Load())
}

// TestLoad_ARebuildFailureIsWrappedAsRebuildSource drives build's own wrap of a
// rebuild failure — fmt.Errorf("openapi: rebuild source %d: %w", ...) — using
// the rebuildDoc test seam, since unmarshal itself never fails on the second of
// two identical calls (see TestResolveExternal_ARebuildErrorIsReturned's own
// comment for why, and the seam's doc comment on Options.rebuildDoc for what
// replaces it here).
func TestLoad_ARebuildFailureIsWrappedAsRebuildSource(t *testing.T) {
	t.Parallel()
	srv, _ := countingServer(t, anchoredExternalDoc)
	url := "HTTP://" + strings.TrimPrefix(srv.URL, "http://") + "/ext.yaml"
	src := compilers.Source{Path: "root.yaml", Data: []byte(rootReferencing(url))}
	boom := errors.New("rebuild boom")
	opts := Options{
		AllowExternalRefs: true,
		rebuildDoc: func(context.Context, []byte, *yaml.Node) (*soa.OpenAPI, []error, error) {
			return nil, nil, boom
		},
	}

	got, diags, err := Load(t.Context(), 5, src, opts)

	assert.Nil(t, got)
	assert.Nil(t, diags)
	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
	assert.Contains(t, err.Error(), "rebuild source 5")
}

// TestStillUnprepared is a unit test: the state it reports — a document still
// unprepared after the recovery pass — is not reachable by feeding a document
// through resolveExternal, since the second pass pre-stores every document the
// first pass used (see resolveExternal's own doc comment for why that is
// exhaustive). It is driven directly with fabricated usedDocuments instead.
func TestStillUnprepared(t *testing.T) {
	t.Parallel()
	at := pointerAt(3, overlay.Origin{})
	missed := []usedDocument{
		{key: "http://a/1.yaml", site: jsontext.Pointer("/paths/~1x")},
		{key: "http://a/1.yaml", site: jsontext.Pointer("/paths/~1y")}, // same key: reported once
		{key: "http://a/2.yaml", site: jsontext.Pointer("/paths/~1z")},
	}

	diags := stillUnprepared(at, missed)

	require.Len(t, diags, 2, "one report per distinct key")
	for _, d := range diags {
		assert.Equal(t, ir.SeverityError, d.Severity)
		assert.Equal(t, diag.InternalInvariant, d.Code)
		assert.Equal(t, 3, d.Provenance.Source)
	}
	assert.Contains(t, diags[0].Message, "http://a/1.yaml")
	assert.Equal(t, "/paths/~1x", diags[0].Provenance.Pointer,
		"reported at the first reference that read it, not at the second one to the same key")
	assert.Contains(t, diags[1].Message, "http://a/2.yaml")
	assert.Equal(t, "/paths/~1z", diags[1].Provenance.Pointer)
}

// aliasKindsFixture carries one internal $ref of every Referenced* alias
// hopDocuments switches on, resolved against the document itself (no I/O), so
// TestHopDocuments can drive the dispatch alone without the recovery machinery.
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
// each alias kind hopDocuments switches on that is itself a $ref.
//
// Walk visits some kinds — a path item, a header, a link, a security scheme —
// twice: once as the $ref this fixture wrote, and again as an inline wrapper
// Walk synthesizes around the resolved content on its way to walking that
// content's own fields (probed directly: IsReference is true on the first
// sighting of each and false, with a nil GetReferenceResolutionInfo, on the
// second). Keeping only the sighting that IsReference is what a fixture with
// exactly one reference of each kind needs.
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

// TestHopDocuments covers hopDocuments' dispatch: each Referenced* alias arm,
// the schema arm (a direct external ref and a chain through two documents), and
// the default arm for a model none of the cases name.
func TestHopDocuments(t *testing.T) {
	t.Parallel()

	t.Run("each alias kind", func(t *testing.T) {
		t.Parallel()
		doc, diags, err := resolveSpec(t, aliasKindsFixture, "root.yaml")
		require.NoError(t, err)
		assert.Empty(t, diags)
		models := resolvedModels(t, doc)

		for _, kind := range []string{
			"pathItem", "parameter", "requestBody", "response",
			"example", "header", "link", "callback", "securityScheme",
		} {
			t.Run(kind, func(t *testing.T) {
				model, ok := models[kind]
				require.True(t, ok, "fixture produced a resolved %s", kind)
				got := hopDocuments(model)
				assert.Equal(t, []string{"root.yaml"}, got,
					"an internal reference's one hop is the source document itself")
			})
		}
	})

	t.Run("a schema", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "a.yaml", "openapi: 3.1.0\ninfo: {title: A, version: \"1\"}\npaths: {}\n"+
			"components:\n  schemas:\n    A: {$ref: \"./b.yaml#/components/schemas/B\"}\n")
		writeFile(t, dir, "b.yaml", "openapi: 3.1.0\ninfo: {title: B, version: \"1\"}\npaths: {}\n"+
			"components:\n  schemas:\n    B: {type: string}\n")
		bPath := filepath.Join(dir, "b.yaml")
		aPath := filepath.Join(dir, "a.yaml")
		rootPath := filepath.Join(dir, "root.yaml")
		root := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n" +
			"components:\n  schemas:\n" +
			"    Direct: {$ref: \"" + bPath + "#/components/schemas/B\"}\n" +
			"    Chained: {$ref: \"" + aPath + "#/components/schemas/A\"}\n"

		doc, diags, err := resolveSpec(t, root, rootPath)
		require.NoError(t, err)
		assert.Empty(t, diags)

		direct, ok := doc.Components.Schemas.Get("Direct")
		require.True(t, ok)
		assert.Equal(t, []string{bPath}, schemaHops(direct), "a direct reference is one hop, not its document twice")

		chained, ok := doc.Components.Schemas.Get("Chained")
		require.True(t, ok)
		assert.Equal(t, []string{aPath, bPath}, schemaHops(chained), "a chain visits each document once, in order")
	})

	t.Run("the default arm", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, hopDocuments(42))
		assert.Nil(t, hopDocuments(&soa.Info{}))
		assert.Nil(t, hopDocuments(nil))
	})
}

// TestHopsUsed_StopsOnMatchError pins hopsUsed's own early return: a WalkItem
// whose Match reports an error ends the walk with what was collected so far,
// the same contract matchSchemas (load.go) is held to over the same library,
// and for the same reason — Any's own callback in hopsUsed always returns nil,
// so nothing the real soa.Walk produces can reach this branch; only a
// fabricated WalkItem, as matchSchemas' own test uses, can.
func TestHopsUsed_StopsOnMatchError(t *testing.T) {
	t.Parallel()
	visited := 0
	failing := soa.WalkItem{Match: func(soa.Matcher) error { return errors.New("walk stopped") }}
	after := soa.WalkItem{Match: func(soa.Matcher) error {
		visited++
		return nil
	}}

	got := hopsUsed(func(yield func(soa.WalkItem) bool) {
		if !yield(failing) {
			return
		}
		yield(after)
	})

	assert.Nil(t, got, "a failed match ends the walk with what it collected so far — nothing, here")
	assert.Zero(t, visited, "the item after the failure is never reached")
}

// TestSchemaHops_BoundedAtMaxResolutionHops proves the bounded-everything cap:
// a chain longer than maxResolutionHops is truncated to it rather than grown
// without limit.
func TestSchemaHops_BoundedAtMaxResolutionHops(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const chainLen = maxResolutionHops + 8
	for i := range chainLen {
		body := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n" +
			"components:\n  schemas:\n    S: {type: string}\n"
		if i < chainLen-1 {
			next := filepath.Join(dir, fmt.Sprintf("f%d.yaml", i+1))
			body = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n" +
				"components:\n  schemas:\n    S: {$ref: \"" + next + "#/components/schemas/S\"}\n"
		}
		writeFile(t, dir, fmt.Sprintf("f%d.yaml", i), body)
	}
	rootPath := filepath.Join(dir, "root.yaml")
	root := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n" +
		"components:\n  schemas:\n    S: {$ref: \"" + filepath.Join(dir, "f0.yaml") + "#/components/schemas/S\"}\n"

	doc, diags, err := resolveSpec(t, root, rootPath)
	require.NoError(t, err)
	assert.Empty(t, diags)

	sch, ok := doc.Components.Schemas.Get("S")
	require.True(t, ok)
	assert.Len(t, schemaHops(sch), maxResolutionHops, "a %d-document chain is capped at %d hops", chainLen, maxResolutionHops)
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
		r := newExternalReads()
		tree := &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}
		r.record("/tmp/doc.yaml", []byte("data"), tree)

		data, got, ok := r.preparedFor("/tmp/doc.yaml")

		require.True(t, ok)
		assert.Equal(t, []byte("data"), data)
		assert.Same(t, tree, got)
	})

	t.Run("a URL key found by its request spelling", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads()
		tree := &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}
		// The spelling external.Do actually stores under: the request's own URL.
		req, err := http.NewRequest(http.MethodGet, "HTTP://host/doc.yaml", nil)
		require.NoError(t, err)
		r.record(req.URL.String(), []byte("data"), tree)

		// The resolver's own, unnormalized key.
		data, got, ok := r.preparedFor("HTTP://host/doc.yaml")

		require.True(t, ok)
		assert.Equal(t, []byte("data"), data)
		assert.Same(t, tree, got)
	})

	t.Run("a key http.NewRequest rejects", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads()

		_, _, ok := r.preparedFor("http://host/\x00bad")

		assert.False(t, ok)
	})

	t.Run("a missing key", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads()

		_, _, ok := r.preparedFor("/no/such/file.yaml")

		assert.False(t, ok)
	})
}

// TestHasAnchor covers both arms: a tree with no anchor anywhere, and one whose
// only anchor is several levels deep.
func TestHasAnchor(t *testing.T) {
	t.Parallel()

	t.Run("no anchor anywhere", func(t *testing.T) {
		t.Parallel()
		root, parsed := parseTree([]byte("a: {b: [1, 2]}\n"))
		require.True(t, parsed)

		assert.False(t, hasAnchor(root))
	})

	t.Run("an anchor deep in the tree", func(t *testing.T) {
		t.Parallel()
		root, parsed := parseTree([]byte("a: {b: [1, &x 2]}\n"))
		require.True(t, parsed)

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
		read := newExternalReads() // nothing of ours is cached under "k"

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
		read := newExternalReads()
		read.record("k", []byte("data"), tree) // the same pointer: ours

		got := unprepared(doc, read, []usedDocument{{key: "k"}})

		assert.Empty(t, got)
	})

	t.Run("a cached value that is not a yaml.Node", func(t *testing.T) {
		t.Parallel()
		doc := &soa.OpenAPI{}
		doc.InitCache()
		doc.StoreExternalDocumentInCache("k", "not a tree")
		read := newExternalReads()

		got := unprepared(doc, read, []usedDocument{{key: "k"}})

		assert.Empty(t, got, "a non-*yaml.Node cached value is not something the resolver parsed as a tree")
	})

	t.Run("a key with nothing cached", func(t *testing.T) {
		t.Parallel()
		doc := &soa.OpenAPI{}
		doc.InitCache()
		read := newExternalReads()

		got := unprepared(doc, read, []usedDocument{{key: "missing"}})

		assert.Empty(t, got)
	})
}
