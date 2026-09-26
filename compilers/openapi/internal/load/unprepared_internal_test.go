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

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
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
	return resolveExternal(t.Context(), scan.InSource(0), doc, path, Options{AllowExternalRefs: true}, rebuild)
}

// resolveSpecWith is resolveSpec at root.yaml with a caller-supplied rebuild, for
// the tests that need to observe or fail whether it is called.
func resolveSpecWith(t *testing.T, spec string, rebuild func() (*soa.OpenAPI, error)) (*soa.OpenAPI, []ir.Diagnostic, error) {
	t.Helper()
	return resolveExternal(t.Context(), scan.InSource(0), modelOf(t, spec), "root.yaml",
		Options{AllowExternalRefs: true}, rebuild)
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

// respellings is the set of URL spellings net/url respells relative to how
// http.NewRequest's caller wrote them (GitHub #538): the resolver keys a
// document by the reference's absolute URL as written, while the reader stores
// it under req.URL.String().
//
// A literal space in the path is absent: url.Parse and http.NewRequest
// normalize it and its %20 escape to one request key, so the two spellings
// never produce different keys. An empty port is covered by TestPreparedFor,
// since a test server cannot listen on the default port.
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

	doc, diags, err := resolveSpecWith(t, rootReferencing(url), rebuild)

	require.NoError(t, err)
	assert.Empty(t, diags)
	ref, ok := doc.Paths.Get("/x")
	require.True(t, ok)
	get := ref.GetObject().Get()
	require.NotNil(t, get)
	assert.Equal(t, "getX", get.GetOperationID())
	assert.Equal(t, int32(1), requests.Load())
}

// respelled returns url with its scheme spelled scheme, which net/url respells
// back to "http" in the request built from it.
func respelled(url, scheme string) string {
	return scheme + "://" + strings.TrimPrefix(url, "http://")
}

// TestResolveExternal_TheSecondPassSendsNoRequestTheFirstMade pins that the
// second pass is answered as the first pass's requests were, for the references
// that failed in it: a failed status, a transport error, a refusal, and a
// document without the fragment named each reach the server once and are
// reported once. flaky.yaml would succeed if asked again, under a spelling the
// resolver keys apart, so a second request would read it unprepared where the
// first pass built nothing from it.
func TestResolveExternal_TheSecondPassSendsNoRequestTheFirstMade(t *testing.T) {
	t.Parallel()
	var flaky, refused, unfound atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/ext.yaml", func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(anchoredExternalDoc))
		assert.NoError(t, err)
	})
	mux.HandleFunc("/flaky.yaml", func(w http.ResponseWriter, _ *http.Request) {
		if flaky.Add(1) == 1 {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		_, err := w.Write([]byte(anchoredExternalDoc))
		assert.NoError(t, err)
	})
	mux.HandleFunc("/refused.yaml", func(w http.ResponseWriter, _ *http.Request) {
		refused.Add(1)
		_, err := w.Write([]byte("p: &a [*a]\n"))
		assert.NoError(t, err)
	})
	mux.HandleFunc("/unfound.yaml", func(w http.ResponseWriter, _ *http.Request) {
		unfound.Add(1)
		_, err := w.Write([]byte(anchoredExternalDoc))
		assert.NoError(t, err)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	// A server of its own: the transport retries a request that fails on a
	// reused connection, and the first one to a server never is.
	var cut atomic.Int32
	cutSrv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		cut.Add(1)
		hj, ok := w.(http.Hijacker)
		if !assert.True(t, ok) {
			return
		}
		conn, _, err := hj.Hijack()
		if !assert.NoError(t, err) {
			return
		}
		assert.NoError(t, conn.Close())
	})
	spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n" +
		"  /x: {$ref: \"" + respelled(srv.URL, "HTTP") + "/ext.yaml#/paths/~1x\"}\n" +
		"  /flaky: {$ref: \"" + respelled(srv.URL, "HTTP") + "/flaky.yaml#/paths/~1x\"}\n" +
		"  /refused: {$ref: \"" + srv.URL + "/refused.yaml#/paths/~1x\"}\n" +
		"  /unfound: {$ref: \"" + srv.URL + "/unfound.yaml#/paths/~1nowhere\"}\n" +
		"  /cut: {$ref: \"" + cutSrv.URL + "/cut.yaml#/paths/~1x\"}\n"

	doc, diags, err := resolveSpec(t, spec, "root.yaml")

	require.NoError(t, err)
	assertRecovered(t, doc)
	codes := make([]string, len(diags))
	for i, d := range diags {
		codes[i] = d.Code
	}
	assert.Equal(t, []string{diag.UnresolvedRef, diag.UnresolvedRef, diag.UnresolvedRef, diag.UnresolvedRef}, codes,
		"each failure once, and no internal fault for flaky.yaml")
	assert.Equal(t, int32(1), flaky.Load(), "a failed status is replayed")
	assert.Equal(t, int32(1), refused.Load(), "a refusal is replayed")
	assert.Equal(t, int32(1), unfound.Load(), "a document's bytes are replayed")
	assert.Equal(t, int32(1), cut.Load(), "a transport error is replayed")
}

// TestResolveExternal_TheSecondPassGetsTheFirstPassAnswersInOrder pins the
// replay's order on one URL that fails one request. Spelled three ways, /a and
// /c resolve under keys of their own and /b fails, as in the first pass;
// spelled once, /a fails and /b resolves, as in the first pass. Answering a
// failed request with a success would have it read the document unprepared, or
// resolve what the first pass did not; answering every request with the
// failure would fail a reference the first pass resolved.
func TestResolveExternal_TheSecondPassGetsTheFirstPassAnswersInOrder(t *testing.T) {
	t.Parallel()

	t.Run("three spellings", func(t *testing.T) {
		t.Parallel()
		var requests atomic.Int32
		srv := newTestServer(t, failingHandler(t, 2, &requests))
		spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n" +
			"  /a: {$ref: \"" + respelled(srv.URL, "HTTP") + "/ext.yaml#/paths/~1x\"}\n" +
			"  /b: {$ref: \"" + respelled(srv.URL, "Http") + "/ext.yaml#/paths/~1x\"}\n" +
			"  /c: {$ref: \"" + respelled(srv.URL, "hTTP") + "/ext.yaml#/paths/~1x\"}\n"

		doc, diags, err := resolveSpec(t, spec, "root.yaml")

		require.NoError(t, err)
		assertRecoveredAt(t, doc, "/a")
		assertRecoveredAt(t, doc, "/c")
		require.Len(t, diags, 1)
		assert.Equal(t, diag.UnresolvedRef, diags[0].Code)
		assert.Contains(t, diags[0].Message, "503")
		assert.Equal(t, int32(3), requests.Load(), "the second pass sends nothing")
	})

	t.Run("one spelling", func(t *testing.T) {
		t.Parallel()
		var requests atomic.Int32
		srv := newTestServer(t, failingHandler(t, 1, &requests))
		trigger, _ := countingServer(t, anchoredExternalDoc)
		spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n" +
			"  /x: {$ref: \"" + respelled(trigger.URL, "HTTP") + "/ext.yaml#/paths/~1x\"}\n" +
			"  /a: {$ref: \"" + srv.URL + "/ext.yaml#/paths/~1x\"}\n" +
			"  /b: {$ref: \"" + srv.URL + "/ext.yaml#/paths/~1x\"}\n"

		doc, diags, err := resolveSpec(t, spec, "root.yaml")

		require.NoError(t, err)
		assertRecoveredAt(t, doc, "/x")
		assertRecoveredAt(t, doc, "/b")
		require.Len(t, diags, 1, "/a fails as it did in the first pass")
		assert.Contains(t, diags[0].Message, "503")
		assert.Equal(t, int32(2), requests.Load(), "the second pass sends nothing")
	})
}

// failingHandler serves anchoredExternalDoc to every request but the n-th,
// which fails with 503, counting them in requests.
func failingHandler(t *testing.T, n int32, requests *atomic.Int32) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == n {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		_, err := w.Write([]byte(anchoredExternalDoc))
		assert.NoError(t, err)
	}
}

// TestResolveExternal_TheFirstPassSendsEveryRequest pins that only the second
// pass replays: in the first, a reference whose document failed to arrive does
// not fail the next reference to it, which asks again and resolves.
func TestResolveExternal_TheFirstPassSendsEveryRequest(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	srv := newTestServer(t, failingHandler(t, 1, &requests))
	spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n" +
		"  /a: {$ref: \"" + srv.URL + "/ext.yaml#/paths/~1x\"}\n" +
		"  /b: {$ref: \"" + srv.URL + "/ext.yaml#/paths/~1x\"}\n"

	doc, diags, err := resolveSpec(t, spec, "root.yaml")

	require.NoError(t, err)
	assertRecoveredAt(t, doc, "/b")
	require.Len(t, diags, 1)
	assert.Contains(t, diags[0].Message, "503")
	assert.Equal(t, int32(2), requests.Load())
}

// TestResolveExternal_TheSecondPassResolvesASelfReferenceAsTheFirstDid pins
// that the hand-over leaves the resolver's own caching alone. ./self.yaml is
// read as another document (GitHub #576), so /b mounts a copy of what /a does.
// Had the second pass been handed the source's bytes, the resolver would have
// returned /a's object for /b instead: whether some other $ref is respelled
// would decide how /b resolves.
func TestResolveExternal_TheSecondPassResolvesASelfReferenceAsTheFirstDid(t *testing.T) {
	t.Parallel()
	trigger, _ := countingServer(t, anchoredExternalDoc)
	for _, tc := range []struct {
		name  string
		other string
	}{
		{"alone", ""},
		{"beside a respelled $ref", "  /x: {$ref: \"" + respelled(trigger.URL, "HTTP") + "/ext.yaml#/paths/~1x\"}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n" + tc.other +
				"  /a: {$ref: '#/components/pathItems/Shared'}\n" +
				"  /b: {$ref: './self.yaml#/components/pathItems/Shared'}\n" +
				"components:\n  pathItems:\n    Shared:\n" +
				"      get: {operationId: dup, responses: {\"200\": {description: ok}}}\n"
			dir := t.TempDir()
			writeFile(t, dir, "self.yaml", spec)

			doc, diags, err := resolveSpec(t, spec, filepath.Join(dir, "self.yaml"))

			require.NoError(t, err)
			assert.Empty(t, diags)
			a, ok := doc.Paths.Get("/a")
			require.True(t, ok)
			b, ok := doc.Paths.Get("/b")
			require.True(t, ok)
			require.NotNil(t, a.GetObject())
			require.NotNil(t, b.GetObject())
			assert.NotSame(t, a.GetObject().GetRootNode(), b.GetObject().GetRootNode(),
				"/b mounts the copy read from self.yaml, not /a's own declaration")
		})
	}
}

// TestResolveExternal_ARebuildErrorIsReturned pins that resolveExternal returns
// a rebuild failure to its caller rather than swallowing it.
//
// Load's own rebuild cannot fail once its first unmarshal has succeeded:
// unmarshal is a pure function of (ctx, data, root) and ignores ctx, so only an
// injected error reaches this branch. Load's wrap of it is driven through the
// rebuildDoc seam by TestLoad_ARebuildFailureIsWrappedAsRebuildSource.
func TestResolveExternal_ARebuildErrorIsReturned(t *testing.T) {
	t.Parallel()
	srv, requests := countingServer(t, anchoredExternalDoc)
	url := "HTTP://" + strings.TrimPrefix(srv.URL, "http://") + "/ext.yaml"
	sentinel := errors.New("rebuild boom")
	rebuild := func() (*soa.OpenAPI, error) { return nil, sentinel }

	doc, diags, err := resolveSpecWith(t, rootReferencing(url), rebuild)

	assert.Nil(t, doc)
	assert.Nil(t, diags)
	assert.ErrorIs(t, err, sentinel)
	assert.Equal(t, int32(1), requests.Load(), "the first pass still reads the document once before rebuild fails")
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

// TestStillUnprepared is a unit test of the report itself. No document makes
// the second pass read one the first did not, since the resolver never follows
// a reference inside a resolved object, so it is driven with fabricated
// usedDocuments. The tests after it reach it through resolveExternal with a
// rebuild that differs from the first model.
func TestStillUnprepared(t *testing.T) {
	t.Parallel()
	locate := scan.InSource(3)
	missed := []usedDocument{
		{key: "http://a/1.yaml", site: jsontext.Pointer("/paths/~1x")},
		{key: "http://a/1.yaml", site: jsontext.Pointer("/paths/~1y")}, // same key: reported once
		{key: "http://a/2.yaml", site: jsontext.Pointer("/paths/~1z")},
	}

	diags := stillUnprepared(locate, missed)

	require.Len(t, diags, 2, "one report per distinct key")
	for _, d := range diags {
		assert.Equal(t, ir.SeverityError, d.Severity)
		assert.Equal(t, diag.InternalInvariant, d.Code)
		assert.Equal(t, 3, d.Provenance.Source)
	}
	assert.Contains(t, diags[0].Message, "http://a/1.yaml")
	assert.Contains(t, diags[0].Message, "/paths/~1x", "names the site of the first reference that read it")
	assert.NotContains(t, diags[0].Message, "/paths/~1y", "the second reference to the same key is not a second report")
	assert.Contains(t, diags[1].Message, "http://a/2.yaml")
	assert.Contains(t, diags[1].Message, "/paths/~1z")
}

// modelOf builds spec's model the way build does before resolving it.
func modelOf(t *testing.T, spec string) *soa.OpenAPI {
	t.Helper()
	data := []byte(spec)
	root, _, err := decodeStream(data)
	require.NoError(t, err)
	releaseAnchors(root)
	doc, _, err := unmarshal(t.Context(), data, root)
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

// TestResolveExternal_ADocumentOnlyTheSecondPassReadsIsPrepared holds the second
// pass's reader to the model it resolves: other.yaml is read for the first time
// there, and its prepared tree must be stored where the rebuilt model looks, not
// in the first model, or its anchored entries are skipped.
func TestResolveExternal_ADocumentOnlyTheSecondPassReadsIsPrepared(t *testing.T) {
	t.Parallel()
	ext, extRequests := countingServer(t, anchoredExternalDoc)
	other, otherRequests := countingServer(t, anchoredExternalDoc)
	extURL := "HTTP://" + strings.TrimPrefix(ext.URL, "http://") + "/ext.yaml"
	first, rebuild := secondPassSpecs(t, extURL, other.URL+"/other.yaml")

	doc, diags, err := resolveSpecWith(t, first, rebuild)

	require.NoError(t, err)
	assert.Empty(t, diags)
	assertRecoveredAt(t, doc, "/x")
	assertRecoveredAt(t, doc, "/y")
	assert.Equal(t, int32(1), extRequests.Load(), "ext.yaml is fetched once, in the first pass")
	assert.Equal(t, int32(1), otherRequests.Load(), "other.yaml is fetched once, in the second")
}

// TestResolveExternal_ARespelledDocumentOnlyTheSecondPassReadsIsReported pins
// that resolveExternal reports a document the second pass read unprepared,
// rather than dropping its anchored entries in silence: other.yaml is respelled,
// so the resolver misses the tree stored under its request's URL.
func TestResolveExternal_ARespelledDocumentOnlyTheSecondPassReadsIsReported(t *testing.T) {
	t.Parallel()
	ext, _ := countingServer(t, anchoredExternalDoc)
	other, _ := countingServer(t, anchoredExternalDoc)
	extURL := "HTTP://" + strings.TrimPrefix(ext.URL, "http://") + "/ext.yaml"
	otherURL := "HTTP://" + strings.TrimPrefix(other.URL, "http://") + "/other.yaml"
	first, rebuild := secondPassSpecs(t, extURL, otherURL)

	_, diags, err := resolveSpecWith(t, first, rebuild)

	require.NoError(t, err)
	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, diag.InternalInvariant, diags[0].Code)
	assert.Contains(t, diags[0].Message, otherURL)
	assert.Contains(t, diags[0].Message, "/paths/~1y", "names the reference that read it")
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

// TestHopDocuments covers hopDocuments' dispatch: each Referenced* alias arm,
// the schema arm (a direct reference, a chain through three documents, and a
// chain whose target another reference shares), and the default arm for a
// model none of the cases name.
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
		const hdr = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n"
		writeFile(t, dir, "c.yaml", hdr+"    C: {$ref: \"./a.yaml#/components/schemas/A\"}\n")
		writeFile(t, dir, "a.yaml", hdr+
			"    A: {$ref: \"#/components/schemas/X\"}\n"+
			"    A2: {$ref: \"#/components/schemas/X\"}\n"+
			"    X: {$ref: \"./b.yaml#/components/schemas/B\"}\n")
		writeFile(t, dir, "b.yaml", hdr+"    B: {type: string}\n")
		aPath, bPath, cPath := filepath.Join(dir, "a.yaml"), filepath.Join(dir, "b.yaml"), filepath.Join(dir, "c.yaml")
		// Shared reaches X after Chained does, which leaves X's parent on Shared's
		// route: a chain read off the shared target would be Shared's.
		root := hdr +
			"    Direct: {$ref: \"" + bPath + "#/components/schemas/B\"}\n" +
			"    Chained: {$ref: \"" + cPath + "#/components/schemas/C\"}\n" +
			"    Shared: {$ref: \"" + aPath + "#/components/schemas/A2\"}\n"

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
		} {
			sch, ok := doc.Components.Schemas.Get(tc.name)
			require.True(t, ok)
			assert.Equal(t, tc.want, hopDocuments(sch), "%s: %s", tc.name, tc.why)
		}
	})

	t.Run("the default arm", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, hopDocuments(42))
		assert.Nil(t, hopDocuments(&soa.Info{}))
		assert.Nil(t, hopDocuments(nil))
	})
}

// TestHopsUsed_StopsOnMatchError pins hopsUsed's early return, which only a
// fabricated WalkItem reaches: a Match error ends the walk with what was
// collected so far.
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

// TestHopDocuments_StopsAtMaxResolutionHops proves the bounded-everything cap
// on both kinds of chain hopDocuments follows: one of a document past
// maxResolutionHops is cut at it rather than followed to its end.
func TestHopDocuments_StopsAtMaxResolutionHops(t *testing.T) {
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
			name: "a schema",
			end:  hdr + "paths: {}\ncomponents:\n  schemas:\n    S: {type: string}\n",
			link: hdr + "paths: {}\ncomponents:\n  schemas:\n    S: {$ref: \"%s#/components/schemas/S\"}\n",
			first: func(doc *soa.OpenAPI) any {
				sch, _ := doc.Components.Schemas.Get("S")
				return sch
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
			assert.Len(t, hopDocuments(first), maxResolutionHops,
				"a %d-document chain is capped at %d hops", chainLen, maxResolutionHops)
		})
	}
}

// respelledChain resolves a root whose /x starts a chain of n documents served
// over HTTP, each reached through an upper-case scheme and so missed by the
// resolver. Only the last holds anchored entries, so only reaching it matters;
// it returns /x's path item as resolved, and the diagnostics. With secondPass,
// the root's /t also reaches an anchored document that way, which sets the
// second pass off whatever the chain's length.
func respelledChain(t *testing.T, n int, secondPass bool) (*soa.PathItem, []ir.Diagnostic) {
	t.Helper()
	var base string
	mux := http.NewServeMux()
	mux.HandleFunc("/t.yaml", func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(anchoredExternalDoc))
		assert.NoError(t, err)
	})
	for i := range n {
		mux.HandleFunc(fmt.Sprintf("/f%d.yaml", i), func(w http.ResponseWriter, _ *http.Request) {
			body := anchoredExternalDoc
			if i < n-1 {
				next := fmt.Sprintf("HTTP://%s/f%d.yaml", strings.TrimPrefix(base, "http://"), i+1)
				body = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n" +
					"  /x: {$ref: \"" + next + "#/paths/~1x\"}\n"
			}
			_, err := w.Write([]byte(body))
			assert.NoError(t, err)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	base = srv.URL

	root := rootReferencing(respelled(srv.URL, "HTTP") + "/f0.yaml")
	if secondPass {
		root += "  /t: {$ref: \"" + respelled(srv.URL, "HTTP") + "/t.yaml#/paths/~1x\"}\n"
	}
	doc, diags, err := resolveSpec(t, root, "root.yaml")
	require.NoError(t, err)
	ref, ok := doc.Paths.Get("/x")
	require.True(t, ok)
	require.NotNil(t, ref.GetObject())
	return ref.GetObject(), diags
}

// TestResolveExternal_TheHopBoundIsWhereRecoveryEnds pins what the bound costs,
// from both sides: the last document of a chain of maxResolutionHops is
// recovered, and a chain one longer is not, with no report, because recovery
// never learns the last document was read. A second pass another $ref sets off
// resolves that one from its replayed bytes as the first pass did. The bound is
// for an absurd chain; this keeps a change to what happens at it deliberate.
func TestResolveExternal_TheHopBoundIsWhereRecoveryEnds(t *testing.T) {
	t.Parallel()

	t.Run("a chain at the bound is recovered", func(t *testing.T) {
		t.Parallel()
		item, diags := respelledChain(t, maxResolutionHops, false)

		assert.Empty(t, diags)
		require.NotNil(t, item.Get(), "the last document's anchored GET entry was folded")
		require.NotNil(t, item.Put())
	})

	for _, tc := range []struct {
		name       string
		secondPass bool
	}{
		{"a chain one past it is not", false},
		{"nor in a second pass another $ref sets off", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			item, diags := respelledChain(t, maxResolutionHops+1, tc.secondPass)

			assert.Empty(t, diags, "nothing reports the entries that were skipped")
			assert.Nil(t, item.Get(), "the last document is the resolver's own parse, which skips them")
			assert.Nil(t, item.Put())
		})
	}
}

// TestResolveExternal_ACanonicalDocumentPastTheHopBoundStaysPrepared covers a
// document no reference records: the last of a chain one document past
// maxResolutionHops, spelled canonically. Its request is answered with the bytes
// the first pass read, and the tree the first pass prepared is handed over
// under the key it was prepared under, which is the resolver's, so its anchored
// entries hold and nothing in the chain is fetched twice. ext.yaml is what sets
// the second pass off.
func TestResolveExternal_ACanonicalDocumentPastTheHopBoundStaysPrepared(t *testing.T) {
	t.Parallel()
	const n = maxResolutionHops + 1
	var base string
	var fetches [n]atomic.Int32
	mux := http.NewServeMux()
	for i := range n {
		mux.HandleFunc(fmt.Sprintf("/f%d.yaml", i), func(w http.ResponseWriter, _ *http.Request) {
			fetches[i].Add(1)
			body := anchoredExternalDoc
			if i < n-1 {
				body = rootReferencing(fmt.Sprintf("%s/f%d.yaml", base, i+1))
			}
			_, err := w.Write([]byte(body))
			assert.NoError(t, err)
		})
	}
	mux.HandleFunc("/ext.yaml", func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(anchoredExternalDoc))
		assert.NoError(t, err)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	base = srv.URL
	spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n" +
		"  /x: {$ref: \"" + respelled(srv.URL, "HTTP") + "/ext.yaml#/paths/~1x\"}\n" +
		"  /chain: {$ref: \"" + srv.URL + "/f0.yaml#/paths/~1x\"}\n"

	doc, diags, err := resolveSpec(t, spec, "root.yaml")

	require.NoError(t, err)
	assert.Empty(t, diags)
	assertRecoveredAt(t, doc, "/x")
	assertRecoveredAt(t, doc, "/chain")
	for i := range fetches {
		assert.Equal(t, int32(1), fetches[i].Load(), "f%d.yaml is fetched once", i)
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
		r := newExternalReads()
		tree := &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}
		r.recordTree("/tmp/doc.yaml", tree)

		got, ok := r.preparedFor("/tmp/doc.yaml")

		require.True(t, ok)
		assert.Same(t, tree, got)
	})

	t.Run("a file key http.NewRequest would respell", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads()
		tree := &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}
		r.recordTree("dir/with space/doc.yaml", tree)

		got, ok := r.preparedFor("dir/with space/doc.yaml")

		require.True(t, ok, "the path is its own key; the request spelling %20 is not")
		assert.Same(t, tree, got)
	})

	t.Run("a URL key found by its request spelling", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads()
		tree := &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}
		// The spelling external.Do actually stores under: the request's own URL.
		req, err := http.NewRequest(http.MethodGet, "HTTP://host/doc.yaml", nil)
		require.NoError(t, err)
		r.recordTree(req.URL.String(), tree)

		// The resolver's own, unnormalized key.
		got, ok := r.preparedFor("HTTP://host/doc.yaml")

		require.True(t, ok)
		assert.Same(t, tree, got)
	})

	t.Run("a URL key spelled with an empty port", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads()
		tree := &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}
		// http.NewRequest drops the empty port, so external.Do stores it without one.
		r.recordTree("http://host/doc.yaml", tree)

		got, ok := r.preparedFor("http://host:/doc.yaml")

		require.True(t, ok)
		assert.Same(t, tree, got)
	})

	t.Run("a key http.NewRequest rejects", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads()

		_, ok := r.preparedFor("http://host/\x00bad")

		assert.False(t, ok)
	})

	t.Run("a missing key", func(t *testing.T) {
		t.Parallel()
		r := newExternalReads()

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
		read.recordTree("k", tree) // the same pointer: ours

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

// TestExternalReads_ReplaysAnswersInOrder pins the replay's order: each key's
// answers come back as they were recorded, one per request, and then none.
func TestExternalReads_ReplaysAnswersInOrder(t *testing.T) {
	t.Parallel()
	r := newExternalReads()
	gone := answer{status: http.StatusNotFound}
	found := answer{status: http.StatusOK, body: []byte("doc")}
	other := answer{err: errors.New("reset")}
	r.recordAnswer("k", gone)
	r.recordAnswer("k", found)
	r.recordAnswer("other", other)

	var got []answer
	for {
		a, ok := r.nextAnswer("k")
		if !ok {
			break
		}
		got = append(got, a)
	}

	assert.Equal(t, []answer{gone, found}, got)
	a, ok := r.nextAnswer("other")
	require.True(t, ok, "each key keeps its own place")
	assert.Equal(t, other, a)
}

// TestExternalReads_IsSafeForConcurrentUse holds the record to what the readers
// sharing it promise: the resolver's interfaces do not rule out concurrent
// calls, and a write racing another is a fatal map fault that no recover can
// catch. Under -race, unsynchronized access fails here.
func TestExternalReads_IsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()
	r := newExternalReads()
	for i := range 8 {
		key := fmt.Sprintf("http://host/doc%d.yaml", i)
		t.Run(fmt.Sprintf("doc%d", i), func(t *testing.T) {
			t.Parallel()
			tree := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
			doc := &soa.OpenAPI{}
			doc.InitCache()
			for range 100 {
				r.recordTree(key, tree)
				got, ok := r.preparedFor(key)
				assert.True(t, ok)
				assert.Same(t, tree, got)
				assert.True(t, r.prepared(tree))
				r.recordAnswer(key, answer{status: http.StatusOK, body: []byte(key)})
				_, ok = r.nextAnswer(key)
				assert.True(t, ok)
				r.handOver(doc, nil)
			}
		})
	}
}
