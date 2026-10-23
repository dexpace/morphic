package load

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

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
// pass's hop-by-hop walk (resolutionTrail) recorded.
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
	assert.Equal(t, int32(1), flaky.Load(), "a failed status is the document's answer")
	assert.Equal(t, int32(1), refused.Load(), "a refusal is the document's answer")
	assert.Equal(t, int32(1), unfound.Load(), "a document's bytes are its answer")
	assert.Equal(t, int32(1), cut.Load(), "a transport error is the document's answer")
}

// TestResolveExternal_EveryReferenceGetsTheOneAnswerItsDocumentGot pins that a
// document is read once a compile, whichever reference asks and in either
// pass: the references to a URL, spelled alike or as the resolver keys apart,
// all fail with the 503 its one request got, or all resolve. A second request
// would have succeeded, so a reference that asked again would resolve what
// another failed on. A respelled $ref to another server sets the second pass
// off in each case.
func TestResolveExternal_EveryReferenceGetsTheOneAnswerItsDocumentGot(t *testing.T) {
	t.Parallel()
	trigger, _ := countingServer(t, anchoredExternalDoc)
	for _, tc := range []struct {
		name      string
		schemes   []string
		failFirst bool
	}{
		{"three spellings, failed", []string{"HTTP", "Http", "hTTP"}, true},
		{"three spellings, served", []string{"HTTP", "Http", "hTTP"}, false},
		{"one spelling, failed", []string{"http", "http"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var requests atomic.Int32
			failing := int32(2)
			if tc.failFirst {
				failing = 1
			}
			srv := newTestServer(t, failingHandler(t, failing, &requests))
			spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n" +
				"  /x: {$ref: \"" + respelled(trigger.URL, "HTTP") + "/ext.yaml#/paths/~1x\"}\n"
			for i, scheme := range tc.schemes {
				spec += fmt.Sprintf("  /p%d: {$ref: \"%s/ext.yaml#/paths/~1x\"}\n", i, respelled(srv.URL, scheme))
			}

			doc, diags, err := resolveSpec(t, spec, "root.yaml")

			require.NoError(t, err)
			assertRecoveredAt(t, doc, "/x")
			assert.Equal(t, int32(1), requests.Load(), "one request, in neither pass sent again")
			if !tc.failFirst {
				assert.Empty(t, diags)
				for i := range tc.schemes {
					assertRecoveredAt(t, doc, fmt.Sprintf("/p%d", i))
				}
				return
			}
			require.Len(t, diags, len(tc.schemes), "every reference fails, each once")
			for _, d := range diags {
				assert.Equal(t, diag.UnresolvedRef, d.Code)
				assert.Contains(t, d.Message, "503")
			}
		})
	}
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

// TestResolveExternal_TheSecondPassResolvesASelfReferenceAsTheFirstDid pins
// that both passes answer a $ref naming the source's own file from the source
// as held (GitHub #759): /b reaches /a's own declaration whether or not another
// $ref is respelled, which is what decides whether a second pass runs.
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
			assert.Same(t, a.GetObject(), b.GetObject(),
				"/b reaches the source's own declaration, as /a does")
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

// combinedRootSpec is a source with two ordinary ghost references beside the
// $ref to other.yaml at url, so the diagnostic list the test below compares is
// more than the one entry the recovery pass touches.
func combinedRootSpec(url string) string {
	return "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n" +
		"  /a:\n    parameters:\n      - {$ref: '#/components/parameters/GhostParam'}\n" +
		"    get: {operationId: getA, responses: {\"200\": {description: ok}}}\n" +
		"  /x: {$ref: \"" + url + "#/paths/~1x\"}\n" +
		"components:\n  schemas:\n    S: {$ref: '#/components/schemas/GhostSchema'}\n"
}

// TestResolveExternal_ASecondPassReportsWhatOnePassWould pins how per-reference
// reporting meets resolveExternal's two passes (see its doc comment): the
// second pass REPLACES the first pass's diagnostics rather than adding to them,
// and replacing must neither double nor lose a report. The two ordinary ghost
// references in combinedRootSpec are what makes that a claim about the whole
// diagnostic list a compile reports, not only about the one entry the recovery
// touches.
func TestResolveExternal_ASecondPassReportsWhatOnePassWould(t *testing.T) {
	t.Parallel()
	srv, requests := countingServer(t, otherDocWithAnchoredGetAndFinding)

	run := func(url string) (*Document, []ir.Diagnostic) {
		requests.Store(0)
		src := compilers.Source{Path: "root.yaml", Data: []byte(combinedRootSpec(url))}
		got, diags, err := Load(t.Context(), 0, src, Options{Reader: reader.Disk{}})
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, int32(1), requests.Load(), "one request per compile")
		return got, diags
	}

	respelledURL := respelled(srv.URL, "HTTP") + "/other.yaml"
	recovered, twoPassDiags := run(respelledURL)
	_, onePassDiags := run(srv.URL + "/other.yaml")

	if d := cmp.Diff(spelledAs(onePassDiags, srv.URL, respelled(srv.URL, "HTTP")), twoPassDiags); d != "" {
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

// twoRefsTarget is an external document with an anchored get, so that a
// respelling of its URL forces the rebuild. /x's put has a parameter missing
// its in, a finding the unmarshal makes rather than validation; /w is clean.
const twoRefsTarget = `openapi: 3.1.0
info: {title: O, version: "1"}
paths:
  /w:
    get: {operationId: W, responses: {"200": {description: ok}}}
  /x:
    get: &g
      operationId: G
      responses: {"200": {description: ok}}
    put:
      operationId: P
      parameters:
        - {name: p, schema: {type: string}}
      responses: {"200": {description: ok}}
`

// TestResolveExternal_TwoRefsToOneTargetReportItsFindingOnce pins that a finding
// in a target two $refs reach is reported once, at its node, with or without a
// rebuild. /w reads the document first, so the library builds /x's
// target from bytes it already holds, and caches no object built that way: it
// builds the target again for /y, which the reached check's identity of a
// finding keeps from a second report.
func TestResolveExternal_TwoRefsToOneTargetReportItsFindingOnce(t *testing.T) {
	t.Parallel()
	srv, _ := countingServer(t, twoRefsTarget)
	run := func(base string, wantRebuilds int32) []ir.Diagnostic {
		doc := base + "/other.yaml#/paths/"
		src := compilers.Source{Path: "root.yaml", Data: []byte("openapi: 3.1.0\n" +
			"info: {title: T, version: \"1\"}\npaths:\n" +
			"  /w: {$ref: \"" + doc + "~1w\"}\n" +
			"  /x: {$ref: \"" + doc + "~1x\"}\n  /y: {$ref: \"" + doc + "~1x\"}\n")}
		var rebuilds atomic.Int32
		rebuild := func(ctx context.Context, data []byte, root *yaml.Node) (*soa.OpenAPI, error) {
			rebuilds.Add(1)
			return unmarshal(ctx, data, root)
		}
		_, diags, err := Load(t.Context(), 0, src, Options{Reader: reader.Disk{}, rebuildDoc: rebuild})
		require.NoError(t, err)
		require.Equal(t, wantRebuilds, rebuilds.Load(), "%s takes the path this case names", base)
		return diags
	}

	onePass := run(srv.URL, 0)
	rebuilt := run(respelled(srv.URL, "HTTP"), 1)

	if d := cmp.Diff(spelledAs(onePass, srv.URL, respelled(srv.URL, "HTTP")), rebuilt); d != "" {
		t.Errorf("two $refs to one target must report alike with and without a rebuild (-onePass +rebuilt):\n%s", d)
	}
	for name, diags := range map[string][]ir.Diagnostic{"one pass": onePass, "rebuilt": rebuilt} {
		var sites []jsontext.Pointer
		for _, d := range diags {
			if d.Code == diag.Validation+"/validation-required-field" {
				sites = append(sites, d.Provenance.Pointer)
			}
		}
		assert.Equal(t, []jsontext.Pointer{"/paths/~1x/put/parameters/0"}, sites,
			"%s: the finding is reported once, at its node", name)
	}
}

// spelledAs returns diags with every mention of from in a message spelled as
// to, so two runs whose $refs spell one URL differently compare on the rest:
// a finding's message names its document as the $ref spelled it.
func spelledAs(diags []ir.Diagnostic, from, to string) []ir.Diagnostic {
	out := make([]ir.Diagnostic, len(diags))
	for i, d := range diags {
		d.Message = strings.ReplaceAll(d.Message, from, to)
		out[i] = d
	}
	return out
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
	assert.Equal(t, jsontext.Pointer("/paths/~1y"), diags[0].Provenance.Pointer,
		"reported at the reference that read it")
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
// resolves that one from the bytes its one read got, as the first pass did. The bound is
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
