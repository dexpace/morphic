package load

import (
	"errors"
	"fmt"
	"io"
	"math"
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

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// TestExternal_OpenOfAMissingFilePassesTheOSErrorThrough pins that Open adds
// nothing of its own to a file that is not there: the caller sees exactly what
// os.Open would have told it.
func TestExternal_OpenOfAMissingFilePassesTheOSErrorThrough(t *testing.T) {
	t.Parallel()
	name := filepath.Join(t.TempDir(), "missing.yaml")

	_, err := newExternal(&soa.OpenAPI{}, Options{}, newExternalReads()).Open(name)

	require.Error(t, err)
	assert.True(t, os.IsNotExist(err), "the error is os.Open's own, unwrapped: %v", err)
}

// TestExternal_OpenOfADirectoryFailsOnTheRead covers the read-error return in
// prepare and the errors.Join(err, f.Close()) branch in Open: os.Open succeeds
// on a directory, and reading it is what fails (EISDIR on Linux and macOS).
func TestExternal_OpenOfADirectoryFailsOnTheRead(t *testing.T) {
	t.Parallel()

	_, err := newExternal(&soa.OpenAPI{}, Options{}, newExternalReads()).Open(t.TempDir())

	require.Error(t, err, "a directory opens but does not read")
}

// TestExternal_OpenPreparesTheFileAndCachesItsTree pins the success path: the
// file's own bytes come back through Read, Stat and Close answer for the real
// file, and the tree stored under the name Open was given has had its anchors
// released while its alias still stands for what it named.
func TestExternal_OpenPreparesTheFileAndCachesItsTree(t *testing.T) {
	t.Parallel()
	const body = "a: &x 1\nb: *x\n"
	path := filepath.Join(t.TempDir(), "doc.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	doc := &soa.OpenAPI{}
	f, err := newExternal(doc, Options{}, newExternalReads()).Open(path)
	require.NoError(t, err)

	got, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, body, string(got), "Read serves exactly the file's own bytes")

	info, err := f.Stat()
	require.NoError(t, err)
	assert.Equal(t, "doc.yaml", info.Name(), "Stat answers for the real file")
	assert.NoError(t, f.Close())

	cached, ok := doc.GetCachedExternalDocument(path)
	require.True(t, ok, "the tree is stored under the name Open was given")
	root, ok := cached.(*yaml.Node)
	require.True(t, ok, "stored as the parsed *yaml.Node")
	assertAnchorsReleasedAliasesStand(t, root)
}

// TestExternal_OpenOverTheByteBudgetIsRefused pins that a file past the byte
// budget is refused, naming the file and the budget it crossed.
func TestExternal_OpenOverTheByteBudgetIsRefused(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "big.yaml")
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("a", 32)), 0o600))

	_, err := newExternal(&soa.OpenAPI{}, Options{MaxSourceBytes: 16}, newExternalReads()).Open(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "external document "+path+" refused")
	assert.Contains(t, err.Error(), "16-byte budget")
}

// TestExternal_OpenRefusesARefusedFileAgainUnread pins that a refusal is kept.
// The resolver asks for a document again on every reference into it until it
// has built something from it, and the second ask is refused without the file
// being opened, which it could not be here: it is removed after the first.
func TestExternal_OpenRefusesARefusedFileAgainUnread(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "big.yaml")
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("a", 32)), 0o600))
	reader := newExternal(&soa.OpenAPI{}, Options{MaxSourceBytes: 16}, newExternalReads())
	_, first := reader.Open(path)
	require.Error(t, first)
	require.NoError(t, os.Remove(path))

	_, again := reader.Open(path)

	require.Error(t, again)
	assert.Equal(t, first.Error(), again.Error(), "the same refusal, not the missing file")
}

// serveThenFail serves 'x' bytes up to max in total and fails on any read past
// that. Wrapped behind prepare's io.LimitReader at the byte budget, it is never
// asked for more than the budget allows and so never reaches its own failure;
// unbounded, io.ReadAll drives it all the way to max before prepare ever judges
// the length. It is what makes the wrap observable: remove it and this reader's
// own error replaces the budget's.
type serveThenFail struct {
	served, max int
}

func (r *serveThenFail) Read(p []byte) (int, error) {
	if r.served >= r.max {
		return 0, errors.New("serveThenFail: read past its own limit")
	}
	n := min(len(p), r.max-r.served)
	for i := range n {
		p[i] = 'x'
	}
	r.served += n
	return n, nil
}

// TestExternal_PrepareStopsReadingAtTheByteBudget proves the byte budget bounds
// the read itself and not only the verdict reached afterward: a reader that
// would happily serve ten times the budget before failing on its own never gets
// asked for more than the budget, so prepare reports the budget crossed rather
// than the reader's own error.
func TestExternal_PrepareStopsReadingAtTheByteBudget(t *testing.T) {
	t.Parallel()
	const limit = 5
	reader := newExternal(&soa.OpenAPI{}, Options{MaxSourceBytes: limit}, newExternalReads())

	_, err := reader.prepare("k", &serveThenFail{max: 10 * limit})

	require.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("%d-byte budget", limit),
		"the reader was never driven far enough to reach its own failure")
}

// TestExternal_PrepareUnderAMaxIntByteBudgetReadsTheDocument pins the largest
// budget a caller can set. No document can pass math.MaxInt bytes, and the
// byte read past a budget to tell a document over it would overflow there: a
// read bounded by the overflowed count ends before the document begins, which
// left every external reference naming an empty document.
func TestExternal_PrepareUnderAMaxIntByteBudgetReadsTheDocument(t *testing.T) {
	t.Parallel()
	const body = "a: 1\n"
	doc := &soa.OpenAPI{}
	reader := newExternal(doc, Options{MaxSourceBytes: math.MaxInt}, newExternalReads())

	data, err := reader.prepare("k", strings.NewReader(body))

	require.NoError(t, err)
	assert.Equal(t, body, string(data))
	_, ok := doc.GetCachedExternalDocument("k")
	assert.True(t, ok, "the document is prepared")
}

// TestExternal_PrepareParsesUnchangedBytesOnce pins that a document the
// resolver reads again, as it does on every reference into it until it has
// built something from it, is not parsed again while its bytes are the same:
// they come back as read, and the tree the resolver looks up is still the one
// stored the first time.
func TestExternal_PrepareParsesUnchangedBytesOnce(t *testing.T) {
	t.Parallel()
	const body = "a: &x 1\nb: *x\n"
	doc := &soa.OpenAPI{}
	reader := newExternal(doc, Options{}, newExternalReads())
	_, err := reader.prepare("k", strings.NewReader(body))
	require.NoError(t, err)
	first, ok := doc.GetCachedExternalDocument("k")
	require.True(t, ok)

	data, err := reader.prepare("k", strings.NewReader(body))

	require.NoError(t, err)
	assert.Equal(t, body, string(data), "the bytes read are handed over again")
	again, ok := doc.GetCachedExternalDocument("k")
	require.True(t, ok)
	assert.Same(t, first, again, "the tree stored the first time stands")
}

// TestExternal_PrepareJudgesChangedBytesAgain pins that a document whose bytes
// changed since it was prepared is held to the refusals again. The resolver
// parses what it reads itself wherever it misses the prepared tree (GitHub
// #538), so skipping them would let the new bytes reach the parser unjudged.
func TestExternal_PrepareJudgesChangedBytesAgain(t *testing.T) {
	t.Parallel()
	reader := newExternal(&soa.OpenAPI{}, Options{}, newExternalReads())
	_, err := reader.prepare("k", strings.NewReader("a: 1\n"))
	require.NoError(t, err)

	_, err = reader.prepare("k", strings.NewReader("p: &a [*a]\n"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "recursive YAML anchor")
}

// TestExternal_PrepareSkipsOnlyWhatItPrepared pins that a refusal is never
// taken for a preparation: a document refused under the key before is judged
// again, so bytes the reader has not admitted cannot reach the resolver. Open
// and Do refuse such a key before reading at all; this holds prepare to it on
// its own.
func TestExternal_PrepareSkipsOnlyWhatItPrepared(t *testing.T) {
	t.Parallel()
	doc := &soa.OpenAPI{}
	reader := newExternal(doc, Options{MaxSourceNodes: 5}, newExternalReads())
	_, err := reader.prepare("k", strings.NewReader("a: [1, 2, 3, 4, 5]\n"))
	require.Error(t, err, "sanity: the first document is past the node budget")

	_, err = reader.prepare("k", strings.NewReader("p: &a [*a]\n"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "recursive YAML anchor")
	_, ok := doc.GetCachedExternalDocument("k")
	assert.False(t, ok, "nothing is stored for a document refused")
}

// TestExternal_PrepareLeavesUnparseableBytesForTheResolver pins the overlay
// path's own rule extended to an external document: bytes that will not parse
// are handed back unprepared and uncached, and the resolver parses them itself
// and reports its own reason.
func TestExternal_PrepareLeavesUnparseableBytesForTheResolver(t *testing.T) {
	t.Parallel()
	const bad = "a: [\n"
	doc := &soa.OpenAPI{}

	data, err := newExternal(doc, Options{}, newExternalReads()).prepare("k", strings.NewReader(bad))

	require.NoError(t, err)
	assert.Equal(t, bad, string(data))
	_, ok := doc.GetCachedExternalDocument("k")
	assert.False(t, ok, "nothing not admitted is cached")
}

// TestExternal_AdmitRefusesARecursiveAnchor pins the refusal that makes
// releasing anchor names safe: a recursive anchor is caught by the same scan
// the source is held to, before its name is ever cleared.
func TestExternal_AdmitRefusesARecursiveAnchor(t *testing.T) {
	t.Parallel()
	root, parsed := parseTree([]byte("p: &a [*a]\n"))
	require.True(t, parsed)

	err := newExternal(&soa.OpenAPI{}, Options{}, newExternalReads()).admit("k", root)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "refused")
	assert.Contains(t, err.Error(), `recursive YAML anchor "a" references an ancestor node`)
}

// TestExternal_AdmitJoinsTwoFindings pins that a document tripping more than
// one pre-parse refusal reports every reason, joined rather than replaced —
// the same behavior refusals gives the source, carried into one error string
// since an external document's findings have no diagnostic list of their own
// to land in.
func TestExternal_AdmitJoinsTwoFindings(t *testing.T) {
	t.Parallel()
	root, parsed := parseTree([]byte("p: &a [*a]\nq: !x {b: 1}\n"))
	require.True(t, parsed)

	err := newExternal(&soa.OpenAPI{}, Options{}, newExternalReads()).admit("k", root)

	require.Error(t, err)
	assert.Contains(t, err.Error(),
		`recursive YAML anchor "a" references an ancestor node; mapping is tagged`,
		"both reasons appear, joined by \"; \"")
}

// TestExternal_AdmitOverTheNodeBudgetIsRefused pins the node budget, which admit
// holds an external document to once the pre-parse refusals have cleared it.
func TestExternal_AdmitOverTheNodeBudgetIsRefused(t *testing.T) {
	t.Parallel()
	root, parsed := parseTree([]byte("a: 1\n"))
	require.True(t, parsed)
	nodes := nodeCount(root)
	require.Greater(t, nodes, 3, "sanity: the fixture crosses the budget below")

	err := newExternal(&soa.OpenAPI{}, Options{MaxSourceNodes: 3}, newExternalReads()).admit("k", root)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "refused")
	assert.Contains(t, err.Error(), fmt.Sprintf("%d nodes, past the 3-node budget", nodes))
}

// TestExternal_AdmitHoldsTheAliasBudget pins that an external document is held
// to MaxAliasSurplus as the source is, on a document well inside the ratio
// every document is held to whatever the budget.
func TestExternal_AdmitHoldsTheAliasBudget(t *testing.T) {
	t.Parallel()
	root, parsed := parseTree([]byte("a: &x {b: 1}\nc: *x\n"))
	require.True(t, parsed)

	err := newExternal(&soa.OpenAPI{}, Options{MaxAliasSurplus: 1}, newExternalReads()).admit("k", root)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "refused")
	assert.Contains(t, err.Error(), "past the 1-node alias budget")
}

// TestExternal_AdmitRefusesWhereTheScanCannotFinish pins the rule admit states:
// any finding refuses an external document, the scan's report that it could
// not finish included, though the source is only warned about that one. A
// merge-key chain deeper than the scan expands is a document that draws it.
func TestExternal_AdmitRefusesWhereTheScanCannotFinish(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("m0: &m0 {k: v}\n")
	for i := 1; i <= nodeview.MergeDepthLimit+2; i++ {
		fmt.Fprintf(&b, "m%d: &m%d {<<: *m%d}\n", i, i, i-1)
	}
	root, parsed := parseTree([]byte(b.String()))
	require.True(t, parsed)

	err := newExternal(&soa.OpenAPI{}, Options{}, newExternalReads()).admit("k", root)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "refused")
	assert.Contains(t, err.Error(), "merge-key expansion bound")
}

// newTestServer starts an httptest.Server for handler, closed when the test
// ends.
func newTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// TestExternal_DoCachesASuccessfulResponsesBody pins the HTTP half of the
// success path: the body reads back unchanged, and the tree is cached under the
// request's own URL, which is the key the resolver looks it up by for a $ref it
// spelled canonically.
func TestExternal_DoCachesASuccessfulResponsesBody(t *testing.T) {
	t.Parallel()
	const body = "a: &x 1\nb: *x\n"
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(body))
		assert.NoError(t, err)
	})
	key := srv.URL + "/doc.yaml"
	req, err := http.NewRequest(http.MethodGet, key, nil)
	require.NoError(t, err)

	doc := &soa.OpenAPI{}
	resp, err := newExternal(doc, Options{}, newExternalReads()).Do(req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	t.Cleanup(func() { assert.NoError(t, resp.Body.Close()) })
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, body, string(got))

	_, ok := doc.GetCachedExternalDocument(key)
	assert.True(t, ok, "cached under the request's own URL")
}

// TestExternal_DoLeavesANonSuccessResponseUntouched pins that a status the
// resolver already treats as failure is left for it to report: the response is
// handed back exactly as received, and nothing is prepared or cached from a
// body this reader never reads.
func TestExternal_DoLeavesANonSuccessResponseUntouched(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	})
	key := srv.URL + "/missing.yaml"
	req, err := http.NewRequest(http.MethodGet, key, nil)
	require.NoError(t, err)

	doc := &soa.OpenAPI{}
	resp, err := newExternal(doc, Options{}, newExternalReads()).Do(req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	t.Cleanup(func() { assert.NoError(t, resp.Body.Close()) })
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	_, ok := doc.GetCachedExternalDocument(key)
	assert.False(t, ok)
}

// TestExternal_DoPassesATransportErrorThrough pins that a request the default
// client cannot even complete — nothing is listening — fails exactly as
// http.DefaultClient.Do would; Do adds nothing of its own to a transport error.
func TestExternal_DoPassesATransportErrorThrough(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/doc.yaml", nil)
	require.NoError(t, err)

	resp, err := newExternal(&soa.OpenAPI{}, Options{}, newExternalReads()).Do(req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// TestExternal_DoFailsOnATruncatedBody covers Do's errors.Join(err,
// resp.Body.Close()) branch: a body that ends before its declared
// Content-Length fails mid-read, and that failure — not a short, silent
// document — is what Do reports.
func TestExternal_DoFailsOnATruncatedBody(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, err := w.Write([]byte("0123456789"))
		assert.NoError(t, err)
	})
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/doc.yaml", nil)
	require.NoError(t, err)

	resp, err := newExternal(&soa.OpenAPI{}, Options{}, newExternalReads()).Do(req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// TestExternal_DoFailsOnARefusedBody pins that a document a 200 response
// carries is still held to the pre-parse refusals: a recursive anchor fails Do
// exactly as it fails a file Open reads.
func TestExternal_DoFailsOnARefusedBody(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte("p: &a [*a]\n"))
		assert.NoError(t, err)
	})
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/doc.yaml", nil)
	require.NoError(t, err)

	resp, err := newExternal(&soa.OpenAPI{}, Options{}, newExternalReads()).Do(req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "refused")
}

// TestExternal_DoRefusesARefusedURLAgainUnfetched is the HTTP half of
// TestExternal_OpenRefusesARefusedFileAgainUnread: a second request for a
// refused document fails as the first did, and reaches no server.
func TestExternal_DoRefusesARefusedURLAgainUnfetched(t *testing.T) {
	t.Parallel()
	var fetches atomic.Int32
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		_, err := w.Write([]byte("p: &a [*a]\n"))
		assert.NoError(t, err)
	})
	reader := newExternal(&soa.OpenAPI{}, Options{}, newExternalReads())
	fetch := func() error {
		req, err := http.NewRequest(http.MethodGet, srv.URL+"/doc.yaml", nil)
		require.NoError(t, err)
		resp, err := reader.Do(req)
		assert.Nil(t, resp)
		return err
	}

	first, again := fetch(), fetch()

	require.Error(t, first)
	require.Error(t, again)
	assert.Equal(t, first.Error(), again.Error(), "the same refusal")
	assert.Equal(t, int32(1), fetches.Load(), "fetched once")
}

// TestNewExternal_InitializesTheCacheOnAZeroDocument pins why newExternal calls
// InitCache rather than trusting the caller to have: storing into a *soa.OpenAPI
// whose caches were never set up faults inside the library, and a zero value is
// exactly that.
func TestNewExternal_InitializesTheCacheOnAZeroDocument(t *testing.T) {
	t.Parallel()
	doc := &soa.OpenAPI{}
	e := newExternal(doc, Options{}, newExternalReads())

	require.NotPanics(t, func() {
		_, err := e.prepare("k", strings.NewReader("a: 1\n"))
		require.NoError(t, err)
	})
	_, ok := doc.GetCachedExternalDocument("k")
	assert.True(t, ok)
}

// assertAnchorsReleasedAliasesStand walks every node reachable from root and
// fails if any carries an anchor name, while requiring at least one alias node
// whose Alias still points at its target — the two halves of "released, but an
// alias still stands for what it named" that a cached external tree must show.
func assertAnchorsReleasedAliasesStand(t *testing.T, root *yaml.Node) {
	t.Helper()
	var sawAlias bool
	stack := []*yaml.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		assert.Empty(t, n.Anchor, "anchors are released before the tree is cached")
		if n.Kind == yaml.AliasNode {
			sawAlias = true
			assert.NotNil(t, n.Alias, "an alias still stands for its target")
		}
		stack = append(stack, n.Content...)
	}
	assert.True(t, sawAlias, "sanity: the fixture carries an alias")
}
