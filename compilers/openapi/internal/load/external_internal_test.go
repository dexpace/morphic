package load

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
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
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/engine/reader"
)

// TestExternal_OpenOfAMissingFilePassesTheReadersErrorThrough pins that Open
// adds nothing of its own to a file the reader cannot find: the caller sees
// exactly what the reader told it.
func TestExternal_OpenOfAMissingFilePassesTheReadersErrorThrough(t *testing.T) {
	t.Parallel()
	name := filepath.Join(t.TempDir(), "missing.yaml")
	_, told := reader.Disk{}.ReadDocument(t.Context(), compilers.DocumentRequest{Location: name})
	require.ErrorIs(t, told, fs.ErrNotExist, "sanity: the reader reports the file missing")

	_, err := newExternal(t.Context(), &soa.OpenAPI{}, Options{Reader: reader.Disk{}}, newExternalReads(sourceDocument{}, budget.Unbounded())).Open(name)

	require.Error(t, err)
	assert.Equal(t, told.Error(), err.Error(), "the reader's own error, unwrapped")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

// TestExternal_OpenPreparesTheFileAndCachesItsTree pins the success path: the
// file's own bytes come back through Read, served from its read, so Stat
// refuses and Close releases nothing, and the tree stored under the name Open
// was given has had its anchors released while its alias still stands for what
// it named.
func TestExternal_OpenPreparesTheFileAndCachesItsTree(t *testing.T) {
	t.Parallel()
	const body = "a: &x 1\nb: *x\n"
	path := filepath.Join(t.TempDir(), "doc.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	doc := &soa.OpenAPI{}
	f, err := newExternal(t.Context(), doc, Options{Reader: reader.Disk{}}, newExternalReads(sourceDocument{}, budget.Unbounded())).Open(path)
	require.NoError(t, err)

	got, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, body, string(got), "Read serves exactly the file's own bytes")

	_, err = f.Stat()
	assert.ErrorIs(t, err, fs.ErrInvalid, "no file is open to answer for")
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

	opts := Options{Reader: reader.Disk{}, MaxSourceBytes: budget.AtMost(16)}

	_, err := newExternal(t.Context(), &soa.OpenAPI{}, opts, newExternalReads(sourceDocument{}, budget.Unbounded())).Open(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "external document "+path+" refused")
	assert.Contains(t, err.Error(), "16-byte budget")
}

// TestExternal_OpenRefusesARefusedFileAgainUnread pins that a refusal is the
// file's answer. The resolver asks for a document again on every reference
// into it until it has built something from it, and the second ask is refused
// without the file being opened, which it could not be here: it is removed
// after the first.
func TestExternal_OpenRefusesARefusedFileAgainUnread(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "big.yaml")
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("a", 32)), 0o600))
	opts := Options{Reader: reader.Disk{}, MaxSourceBytes: budget.AtMost(16)}
	ext := newExternal(t.Context(), &soa.OpenAPI{}, opts, newExternalReads(sourceDocument{}, budget.Unbounded()))
	_, first := ext.Open(path)
	require.Error(t, first)
	require.NoError(t, os.Remove(path))

	_, again := ext.Open(path)

	require.Error(t, again)
	assert.Equal(t, first.Error(), again.Error(), "the same refusal, not the missing file")
}

// TestExternal_OpenReadsAFileOnce pins that a file read once is served from
// that read whenever it is asked for again, as the resolver does after a
// pointer into it names nothing: the second ask gets the same bytes though the
// file is gone, and the tree stored the first time stands.
func TestExternal_OpenReadsAFileOnce(t *testing.T) {
	t.Parallel()
	const body = "a: &x 1\nb: *x\n"
	path := filepath.Join(t.TempDir(), "doc.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	doc := &soa.OpenAPI{}
	ext := newExternal(t.Context(), doc, Options{Reader: reader.Disk{}}, newExternalReads(sourceDocument{}, budget.Unbounded()))
	_, err := ext.Open(path)
	require.NoError(t, err)
	first, ok := doc.GetCachedExternalDocument(path)
	require.True(t, ok)
	require.NoError(t, os.Remove(path))

	f, err := ext.Open(path)

	require.NoError(t, err, "served from the first read, not opened")
	got, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, body, string(got))
	again, ok := doc.GetCachedExternalDocument(path)
	require.True(t, ok)
	assert.Same(t, first, again, "the tree stored the first time stands")
}

// TestExternal_PrepareRefusesBytesThatDoNotParse pins that bytes which will
// not parse are refused with the parser's reason, line included, as the
// resolver, parsing them with the same call, would refuse them, and that
// nothing is cached or recorded for them.
func TestExternal_PrepareRefusesBytesThatDoNotParse(t *testing.T) {
	t.Parallel()
	doc := &soa.OpenAPI{}
	read := newExternalReads(sourceDocument{}, budget.Unbounded())

	data, err := newExternal(t.Context(), doc, Options{}, read).prepare(t.Context(), "k", []byte("a: [\n"))

	var unparsable *bind.UnparsableError
	require.ErrorAs(t, err, &unparsable)
	assert.Equal(t, "yaml: line 1: did not find expected node content", unparsable.Err.Error())
	assert.Nil(t, data)
	_, ok := doc.GetCachedExternalDocument("k")
	assert.False(t, ok, "nothing not admitted is cached")
	_, ok = read.preparedFor("k")
	assert.False(t, ok, "and no tree is recorded")
}

// TestExternal_PrepareRefusesADocumentHoldingNothing pins that bytes which
// parse to no node, white space or comments alone, are refused as an empty
// document, with nothing cached or recorded, while a document holding null is
// prepared as any other.
func TestExternal_PrepareRefusesADocumentHoldingNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body string
		empty      bool
	}{
		{"no bytes", "", true}, {"a blank line", "\n", true}, {"a comment", "# nothing yet\n", true},
		{"null", "---\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := &soa.OpenAPI{}
			read := newExternalReads(sourceDocument{}, budget.Unbounded())

			_, err := newExternal(t.Context(), doc, Options{}, read).prepare(t.Context(), "k", []byte(tc.body))

			_, recorded := read.preparedFor("k")
			if !tc.empty {
				require.NoError(t, err)
				assert.True(t, recorded)
				return
			}
			require.ErrorIs(t, err, bind.ErrEmptyDocument)
			_, cached := doc.GetCachedExternalDocument("k")
			assert.False(t, cached)
			assert.False(t, recorded)
		})
	}
}

// TestExternal_AdmitRefusesARecursiveAnchor pins the refusal that makes
// releasing anchor names safe: a recursive anchor is caught by the same scan
// the source is held to, before its name is ever cleared.
func TestExternal_AdmitRefusesARecursiveAnchor(t *testing.T) {
	t.Parallel()
	root, err := parseTree(t.Context(), []byte("p: &a [*a]\n"))
	require.NoError(t, err)

	err = newExternal(t.Context(), &soa.OpenAPI{}, Options{}, newExternalReads(sourceDocument{}, budget.Unbounded())).admit(t.Context(), "k", root)

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
	root, err := parseTree(t.Context(), []byte("p: &a [*a]\nq: !x {b: 1}\n"))
	require.NoError(t, err)

	err = newExternal(t.Context(), &soa.OpenAPI{}, Options{}, newExternalReads(sourceDocument{}, budget.Unbounded())).admit(t.Context(), "k", root)

	require.Error(t, err)
	assert.Contains(t, err.Error(),
		`recursive YAML anchor "a" references an ancestor node; mapping is tagged`,
		"both reasons appear, joined by \"; \"")
}

// TestExternal_AdmitOverTheNodeBudgetIsRefused pins the node budget, which admit
// holds an external document to once the pre-parse refusals have cleared it.
func TestExternal_AdmitOverTheNodeBudgetIsRefused(t *testing.T) {
	t.Parallel()
	root, err := parseTree(t.Context(), []byte("a: 1\n"))
	require.NoError(t, err)
	nodes := nodeCount(t.Context(), root)
	require.Greater(t, nodes, 3, "sanity: the fixture crosses the budget below")

	err = newExternal(t.Context(), &soa.OpenAPI{}, Options{MaxSourceNodes: budget.AtMost(3)}, newExternalReads(sourceDocument{}, budget.Unbounded())).admit(t.Context(), "k", root)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "refused")
	assert.Contains(t, err.Error(), fmt.Sprintf("%d nodes, past the 3-node budget", nodes))
}

// TestExternal_AdmitHoldsTheAliasBudget pins that an external document is held
// to MaxAliasSurplus as the source is, on a document well inside the ratio
// every document is held to whatever the budget.
func TestExternal_AdmitHoldsTheAliasBudget(t *testing.T) {
	t.Parallel()
	root, err := parseTree(t.Context(), []byte("a: &x {b: 1}\nc: *x\n"))
	require.NoError(t, err)

	err = newExternal(t.Context(), &soa.OpenAPI{}, Options{MaxAliasSurplus: budget.AtMost(1)}, newExternalReads(sourceDocument{}, budget.Unbounded())).admit(t.Context(), "k", root)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "refused")
	assert.Contains(t, err.Error(), "past the 1-node alias budget")
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
	resp, err := newExternal(t.Context(), doc, Options{Reader: reader.Disk{}}, newExternalReads(sourceDocument{}, budget.Unbounded())).Do(req)

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

// TestExternal_DoPassesAReadFailureThrough pins that a request the reader
// cannot complete — nothing is listening — fails with the reader's own error,
// no response beside it; Do adds nothing of its own to it.
func TestExternal_DoPassesAReadFailureThrough(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/doc.yaml", nil)
	require.NoError(t, err)
	_, told := reader.Disk{}.ReadDocument(t.Context(), compilers.DocumentRequest{Location: srv.URL + "/doc.yaml"})
	require.Error(t, told, "sanity: nothing is listening")

	resp, err := newExternal(t.Context(), &soa.OpenAPI{}, Options{Reader: reader.Disk{}}, newExternalReads(sourceDocument{}, budget.Unbounded())).Do(req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, told.Error(), err.Error(), "the reader's own error, as it spoke it")
	assert.NotContains(t, err.Error(), "external document", "not wrapped as a refusal")
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

	resp, err := newExternal(t.Context(), &soa.OpenAPI{}, Options{Reader: reader.Disk{}}, newExternalReads(sourceDocument{}, budget.Unbounded())).Do(req)

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
	ext := newExternal(t.Context(), &soa.OpenAPI{}, Options{Reader: reader.Disk{}}, newExternalReads(sourceDocument{}, budget.Unbounded()))
	fetch := func() error {
		req, err := http.NewRequest(http.MethodGet, srv.URL+"/doc.yaml", nil)
		require.NoError(t, err)
		resp, err := ext.Do(req)
		assert.Nil(t, resp)
		return err
	}

	first, again := fetch(), fetch()

	require.Error(t, first)
	require.Error(t, again)
	assert.Equal(t, first.Error(), again.Error(), "the same refusal")
	assert.Equal(t, int32(1), fetches.Load(), "fetched once")
}

// TestExternal_DoFetchesAURLOnce is the HTTP half of
// TestExternal_OpenReadsAFileOnce, for every kind of answer: a second request
// for a URL gets the first one's body, or the failure the reader gave it, and
// reaches no server.
func TestExternal_DoFetchesAURLOnce(t *testing.T) {
	t.Parallel()
	var fetches atomic.Int32
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if fetches.Add(1) > 2 {
			http.Error(w, "asked again", http.StatusTeapot)
			return
		}
		if r.URL.Path == "/gone.yaml" {
			http.Error(w, "nope", http.StatusNotFound)
			return
		}
		_, err := w.Write([]byte("a: 1\n"))
		assert.NoError(t, err)
	})
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closed.Close()
	ext := newExternal(t.Context(), &soa.OpenAPI{}, Options{Reader: reader.Disk{}}, newExternalReads(sourceDocument{}, budget.Unbounded()))
	do := func(url string) (int, string, error) {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		require.NoError(t, err)
		resp, err := ext.Do(req)
		if err != nil {
			return 0, "", err
		}
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		return resp.StatusCode, string(body), nil
	}

	for range 3 {
		status, body, err := do(srv.URL + "/doc.yaml")
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, status)
		assert.Equal(t, "a: 1\n", body)
	}
	_, _, refused := do(srv.URL + "/gone.yaml")
	_, _, refusedAgain := do(srv.URL + "/gone.yaml")
	_, _, first := do(closed.URL + "/doc.yaml")
	_, _, again := do(closed.URL + "/doc.yaml")

	require.ErrorAs(t, refused, new(unread), "a failed read, worded as the binder words one")
	assert.Same(t, errors.Unwrap(refused), errors.Unwrap(refusedAgain), "the failure the one request got, an unsuccessful status")
	require.ErrorAs(t, first, new(unread))
	assert.Same(t, errors.Unwrap(first), errors.Unwrap(again), "the failure the one request got, a transport error")
	assert.Equal(t, int32(2), fetches.Load(), "each URL fetched once")
}

// TestExternal_ReadsPastTheDocumentBudgetAreRefusedUnread pins the budget at
// the reader, over both kinds of document, which share one count: within it a
// file and a URL are read, and past it each is refused with the budget named
// and never opened or fetched, though both are there to read.
func TestExternal_ReadsPastTheDocumentBudgetAreRefusedUnread(t *testing.T) {
	t.Parallel()
	var fetches atomic.Int32
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		_, err := w.Write([]byte("a: 1\n"))
		assert.NoError(t, err)
	})
	dir := externalDir(t, map[string]string{"a.yaml": "a: 1\n", "b.yaml": "b: 1\n"})
	opts := Options{Reader: reader.Disk{}, MaxExternalDocuments: budget.AtMost(2)}
	ext := newExternal(t.Context(), &soa.OpenAPI{}, opts, newExternalReads(sourceDocument{}, opts.MaxExternalDocuments))
	get := func(name string) error {
		req, err := http.NewRequest(http.MethodGet, srv.URL+"/"+name, nil)
		require.NoError(t, err)
		resp, err := ext.Do(req)
		if err == nil {
			require.NoError(t, resp.Body.Close())
		}
		return err
	}
	_, err := ext.Open(filepath.Join(dir, "a.yaml"))
	require.NoError(t, err, "the first document is within the budget")
	require.NoError(t, get("a.yaml"), "the second is too, though it is a URL")

	_, file := ext.Open(filepath.Join(dir, "b.yaml"))
	url := get("b.yaml")

	require.Error(t, file)
	assert.Equal(t, "external document "+filepath.Join(dir, "b.yaml")+" refused: it is past the 2-document budget",
		file.Error())
	require.Error(t, url)
	assert.Contains(t, url.Error(), "external document "+srv.URL+"/b.yaml refused: it is past the 2-document budget")
	assert.Equal(t, int32(1), fetches.Load(), "nothing past the budget is fetched")
}

// TestNewExternal_InitializesTheCacheOnAZeroDocument pins why newExternal calls
// InitCache rather than trusting the caller to have: storing into a *soa.OpenAPI
// whose caches were never set up faults inside the library, and a zero value is
// exactly that.
func TestNewExternal_InitializesTheCacheOnAZeroDocument(t *testing.T) {
	t.Parallel()
	doc := &soa.OpenAPI{}
	e := newExternal(t.Context(), doc, Options{}, newExternalReads(sourceDocument{}, budget.Unbounded()))

	require.NotPanics(t, func() {
		_, err := e.prepare(t.Context(), "k", []byte("a: 1\n"))
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

// lexicalRoot is a source whose /abs and /rel responses are both a $ref to R in
// other.yaml, the first spelled by an absolute path through through and the
// second by a relative one.
func lexicalRoot(dir, through string) string {
	return "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n" +
		"  /abs: {get: {responses: {\"200\": {$ref: '" + dir + "/" + through + "/../other.yaml#/components/responses/R'}}}}\n" +
		"  /rel: {get: {responses: {\"200\": {$ref: '" + through + "/../other.yaml#/components/responses/R'}}}}\n"
}

// TestExternal_OpenReadsTheDocumentThePathNames pins that the file a $ref reads
// is the one its path names once cleaned, however the $ref is spelled. A
// relative path is cleaned by the resolver before it asks, and an absolute one
// is not, so the operating system was left to walk it: through a directory that
// is not there it failed, and through a link it read another file. Both
// spellings of one path read one file (GitHub #780).
func TestExternal_OpenReadsTheDocumentThePathNames(t *testing.T) {
	t.Parallel()
	for _, through := range []string{"nope", "link"} {
		t.Run(through, func(t *testing.T) {
			t.Parallel()
			dir := externalDir(t, map[string]string{
				"other.yaml": "components:\n  responses:\n    R: {description: beside root}\n",
			})
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub", "deep"), 0o750))
			writeFile(t, dir, filepath.Join("sub", "other.yaml"), "components:\n  responses:\n    R: {content: {}}\n")
			if through == "link" {
				if err := os.Symlink(filepath.Join(dir, "sub", "deep"), filepath.Join(dir, "link")); err != nil {
					t.Skipf("a link cannot be made here: %v", err)
				}
			}

			got, diags := loadExternal(t, dir, lexicalRoot(dir, through), Options{})

			assert.Empty(t, diags, "neither spelling fails, nor reads sub/other.yaml, whose R has no description")
			for _, path := range []string{"/abs", "/rel"} {
				resp := response200(t, got.Doc, path).GetObject()
				require.NotNil(t, resp, path)
				assert.Equal(t, "beside root", resp.GetDescription(), "%s reads other.yaml beside root.yaml", path)
			}
		})
	}
}

// TestExternal_OpenHoldsTheSourceItIsAskedFor pins what Open does with the
// source spelled as no key holds it: it holds the source under that spelling,
// so the next $ref spelling it so finds the source's own objects, and serves
// the bytes held for it, which the resolver reads none of, its tree being held.
func TestExternal_OpenHoldsTheSourceItIsAskedFor(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	doc, pass := heldResolution(t, backRoot(backPath("/a", "#/components/responses/R")), filepath.Join(dir, "root.yaml"))
	spelled := dir + "/./root.yaml"

	f, err := pass.reader.Open(spelled)

	require.NoError(t, err, "no root.yaml is on disk, and none is read")
	data, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Empty(t, data)
	_, err = f.Stat()
	require.ErrorIs(t, err, fs.ErrInvalid)
	require.NoError(t, f.Close())
	tree, ok := doc.GetCachedExternalDocument(spelled)
	require.True(t, ok)
	assert.Same(t, pass.reader.read.self.root, tree)
	held, ok := doc.GetCachedReferencedObject(spelled + "#/components/responses/R")
	require.True(t, ok)
	r, ok := doc.Components.Responses.Get("R")
	require.True(t, ok)
	assert.Same(t, r, held, "the source's own object, a response being no reference")
}
