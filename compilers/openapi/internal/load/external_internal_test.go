// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"errors"
	"fmt"
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
	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/engine/reader"
)

// diskReader is a reader of the documents on disk and on the network, under
// opts, that has read none.
func diskReader(opts Options) *external {
	opts.Reader = reader.Disk{}
	return newExternal("", opts, newExternalReads(sourceDocument{}, opts.MaxExternalDocuments))
}

// TestExternal_DocumentOfAMissingFilePassesTheReadersErrorThrough pins that
// document adds nothing of its own to a file the reader cannot find: the
// caller sees exactly what the reader told it.
func TestExternal_DocumentOfAMissingFilePassesTheReadersErrorThrough(t *testing.T) {
	t.Parallel()
	name := filepath.Join(t.TempDir(), "missing.yaml")
	_, told := reader.Disk{}.ReadDocument(t.Context(), compilers.DocumentRequest{Location: name})
	require.ErrorIs(t, told, fs.ErrNotExist, "sanity: the reader reports the file missing")

	_, err := diskReader(Options{}).document(t.Context(), name)

	require.Error(t, err)
	assert.Equal(t, told.Error(), err.Error(), "the reader's own error, unwrapped")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

// TestExternal_DocumentPreparesTheFile pins the success path: the tree a file
// parses to comes back with its anchors released while its alias still stands
// for what it named, recorded under the name it was asked for by.
func TestExternal_DocumentPreparesTheFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "doc.yaml")
	require.NoError(t, os.WriteFile(path, []byte("a: &x 1\nb: *x\n"), 0o600))
	ext := diskReader(Options{})

	tree, err := ext.document(t.Context(), path)

	require.NoError(t, err)
	assertAnchorsReleasedAliasesStand(t, tree)
	recorded, ok := ext.read.preparedFor(path)
	require.True(t, ok)
	assert.Same(t, tree, recorded)
}

// TestExternal_DocumentOverTheByteBudgetIsRefused pins that a file past the
// byte budget is refused, naming the file and the budget it crossed.
func TestExternal_DocumentOverTheByteBudgetIsRefused(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "big.yaml")
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("a", 32)), 0o600))

	_, err := diskReader(Options{MaxSourceBytes: budget.AtMost(16)}).document(t.Context(), path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "external document "+path+" refused")
	assert.Contains(t, err.Error(), "16-byte budget")
}

// TestExternal_DocumentRefusesARefusedFileAgainUnread pins that a refusal is the
// file's answer: a second ask is refused without the file being opened, which
// it could not be here, as it is removed after the first.
func TestExternal_DocumentRefusesARefusedFileAgainUnread(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "big.yaml")
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("a", 32)), 0o600))
	ext := diskReader(Options{MaxSourceBytes: budget.AtMost(16)})
	_, first := ext.document(t.Context(), path)
	require.Error(t, first)
	require.NoError(t, os.Remove(path))

	_, again := ext.document(t.Context(), path)

	require.Error(t, again)
	assert.Equal(t, first.Error(), again.Error(), "the same refusal, not the missing file")
}

// TestExternal_DocumentReadsAFileOnce pins that a file read once is served from
// that read whenever it is asked for again: the second ask gets the same tree
// though the file is gone.
func TestExternal_DocumentReadsAFileOnce(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "doc.yaml")
	require.NoError(t, os.WriteFile(path, []byte("a: 1\n"), 0o600))
	ext := diskReader(Options{})
	first, err := ext.document(t.Context(), path)
	require.NoError(t, err)
	require.NoError(t, os.Remove(path))

	again, err := ext.document(t.Context(), path)

	require.NoError(t, err, "served from the first read, not opened")
	assert.Same(t, first, again, "the tree prepared the first time stands")
}

// TestExternal_PrepareRefusesBytesThatDoNotParse pins that bytes which will
// not parse are refused with the parser's reason, line included, and that no
// tree is recorded for them.
func TestExternal_PrepareRefusesBytesThatDoNotParse(t *testing.T) {
	t.Parallel()
	read := newExternalReads(sourceDocument{}, budget.Unbounded())

	data, err := newExternal("", Options{}, read).prepare(t.Context(), "k", "k", []byte("a: [\n"))

	var unparsable *bind.UnparsableError
	require.ErrorAs(t, err, &unparsable)
	assert.Equal(t, "yaml: line 1: did not find expected node content", unparsable.Err.Error())
	assert.Nil(t, data)
	_, ok := read.preparedFor("k")
	assert.False(t, ok, "no tree is recorded")
}

// TestExternal_PrepareRefusesADocumentHoldingNothing pins that bytes which
// parse to no node, white space or comments alone, are refused as an empty
// document, with nothing recorded, while a document holding null is prepared
// as any other.
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
			read := newExternalReads(sourceDocument{}, budget.Unbounded())

			_, err := newExternal("", Options{}, read).prepare(t.Context(), "k", "k", []byte(tc.body))

			_, recorded := read.preparedFor("k")
			if !tc.empty {
				require.NoError(t, err)
				assert.True(t, recorded)
				return
			}
			require.ErrorIs(t, err, bind.ErrEmptyDocument)
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

	err = newExternal("", Options{}, newExternalReads(sourceDocument{}, budget.Unbounded())).admit(t.Context(), "k", root)

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

	err = newExternal("", Options{}, newExternalReads(sourceDocument{}, budget.Unbounded())).admit(t.Context(), "k", root)

	require.Error(t, err)
	assert.Contains(t, err.Error(),
		`recursive YAML anchor "a" references an ancestor node; mapping is tagged`,
		"both reasons appear, joined by \"; \"")
}

// TestExternal_AdmitOverTheNodeBudgetIsRefused pins the node budget, which admit
// holds an external document to once the pre-parse refusals have cleared it:
// a document of the budget's nodes is admitted, and one node fewer of budget
// refuses it.
func TestExternal_AdmitOverTheNodeBudgetIsRefused(t *testing.T) {
	t.Parallel()
	root, err := parseTree(t.Context(), []byte("a: 1\n"))
	require.NoError(t, err)
	nodes := nodeCount(t.Context(), root)
	admit := func(limit int) error {
		return newExternal("", Options{MaxSourceNodes: budget.AtMost(limit)},
			newExternalReads(sourceDocument{}, budget.Unbounded())).admit(t.Context(), "k", root)
	}

	require.NoError(t, admit(nodes), "a document of the budget's nodes is within it")
	err = admit(nodes - 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refused")
	assert.Contains(t, err.Error(), fmt.Sprintf("%d nodes, past the %d-node budget", nodes, nodes-1))
}

// TestExternal_AdmitHoldsTheAliasBudget pins that an external document is held
// to MaxAliasSurplus as the source is, on a document well inside the ratio
// every document is held to whatever the budget.
func TestExternal_AdmitHoldsTheAliasBudget(t *testing.T) {
	t.Parallel()
	root, err := parseTree(t.Context(), []byte("a: &x {b: 1}\nc: *x\n"))
	require.NoError(t, err)

	err = newExternal("", Options{MaxAliasSurplus: budget.AtMost(1)}, newExternalReads(sourceDocument{}, budget.Unbounded())).admit(t.Context(), "k", root)

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

// TestExternal_DocumentFetchesAURL pins the network half of the success path:
// the tree the body parses to, anchors released and aliases standing,
// recorded under the request's own URL.
func TestExternal_DocumentFetchesAURL(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte("a: &x 1\nb: *x\n"))
		assert.NoError(t, err)
	})
	key := srv.URL + "/doc.yaml"
	ext := diskReader(Options{})

	tree, err := ext.document(t.Context(), key)

	require.NoError(t, err)
	assertAnchorsReleasedAliasesStand(t, tree)
	_, ok := ext.read.preparedFor(key)
	assert.True(t, ok, "recorded under the request's own URL")
}

// TestExternal_DocumentPassesAReadFailureThrough pins that a URL the reader
// cannot fetch fails with the reader's own error, and that the reader is asked
// once, for the request's URL. The reader fails as told: two dials of a closed
// port need not fail alike.
func TestExternal_DocumentPassesAReadFailureThrough(t *testing.T) {
	t.Parallel()
	const key = "http://host/doc.yaml"
	told := errors.New("dial tcp 127.0.0.1:9: connect: connection refused")
	failing := &fakeReader{err: told}
	ext := newExternal("", Options{Reader: failing}, newExternalReads(sourceDocument{}, budget.Unbounded()))

	_, err := ext.document(t.Context(), key)

	require.Error(t, err)
	assert.ErrorIs(t, err, told)
	assert.Equal(t, told.Error(), err.Error(), "the reader's own error, as it spoke it")
	assert.Equal(t, []compilers.DocumentRequest{{Location: key}}, failing.seen(), "asked once, for the request's URL")
}

// TestExternal_DocumentRefusesARefusedURLAgainUnfetched pins that a document a
// response carries is held to the pre-parse refusals, and that a second ask for
// a refused one fails as the first did and reaches no server.
func TestExternal_DocumentRefusesARefusedURLAgainUnfetched(t *testing.T) {
	t.Parallel()
	var fetches atomic.Int32
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		_, err := w.Write([]byte("p: &a [*a]\n"))
		assert.NoError(t, err)
	})
	ext := diskReader(Options{})

	_, first := ext.document(t.Context(), srv.URL+"/doc.yaml")
	_, again := ext.document(t.Context(), srv.URL+"/doc.yaml")

	require.Error(t, first)
	assert.Contains(t, first.Error(), "refused")
	require.Error(t, again)
	assert.Equal(t, first.Error(), again.Error(), "the same refusal")
	assert.Equal(t, int32(1), fetches.Load(), "fetched once")
}

// TestExternal_DocumentFetchesAURLOnce pins, for every kind of answer, that a
// second ask for a URL gets the first one's tree, or the failure the reader
// gave it, and reaches no server.
func TestExternal_DocumentFetchesAURLOnce(t *testing.T) {
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
	ext := diskReader(Options{})

	first, err := ext.document(t.Context(), srv.URL+"/doc.yaml")
	require.NoError(t, err)
	for range 2 {
		again, err := ext.document(t.Context(), srv.URL+"/doc.yaml")
		require.NoError(t, err)
		assert.Same(t, first, again)
	}
	_, refused := ext.document(t.Context(), srv.URL+"/gone.yaml")
	_, refusedAgain := ext.document(t.Context(), srv.URL+"/gone.yaml")
	_, failed := ext.document(t.Context(), closed.URL+"/doc.yaml")
	_, failedAgain := ext.document(t.Context(), closed.URL+"/doc.yaml")

	require.Error(t, refused)
	assert.Same(t, refused, refusedAgain, "the failure the one request got, an unsuccessful status")
	require.Error(t, failed)
	assert.Same(t, failed, failedAgain, "the failure the one request got, a transport error")
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
	ext := diskReader(Options{MaxExternalDocuments: budget.AtMost(2)})
	_, err := ext.document(t.Context(), filepath.Join(dir, "a.yaml"))
	require.NoError(t, err, "the first document is within the budget")
	_, err = ext.document(t.Context(), srv.URL+"/a.yaml")
	require.NoError(t, err, "the second is too, though it is a URL")

	_, file := ext.document(t.Context(), filepath.Join(dir, "b.yaml"))
	_, url := ext.document(t.Context(), srv.URL+"/b.yaml")

	require.Error(t, file)
	assert.Equal(t, "external document "+filepath.Join(dir, "b.yaml")+" refused: it is past the 2-document budget",
		file.Error())
	require.Error(t, url)
	assert.Contains(t, url.Error(), "external document "+srv.URL+"/b.yaml refused: it is past the 2-document budget")
	assert.Equal(t, int32(1), fetches.Load(), "nothing past the budget is fetched")
}

// assertAnchorsReleasedAliasesStand walks every node reachable from root and
// fails if any carries an anchor name, while requiring at least one alias node
// whose Alias still points at its target — the two halves of "released, but an
// alias still stands for what it named" that a prepared tree must show.
func assertAnchorsReleasedAliasesStand(t *testing.T, root *yaml.Node) {
	t.Helper()
	var sawAlias bool
	stack := []*yaml.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		assert.Empty(t, n.Anchor, "anchors are released before the tree is recorded")
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

// TestExternal_DocumentReadsTheDocumentThePathNames pins that the file a $ref
// reads is the one its path names once cleaned, however the $ref is spelled,
// rather than one the operating system reaches by walking it: through a
// directory that is not there it would fail, and through a link read another
// file. Both spellings of one path read one file (GitHub #780).
func TestExternal_DocumentReadsTheDocumentThePathNames(t *testing.T) {
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
				resp := refscope.Object[soa.Response](got.Bindings, response200(t, got.Doc, path))
				require.NotNil(t, resp, path)
				assert.Equal(t, "beside root", resp.GetDescription(), "%s reads other.yaml beside root.yaml", path)
			}
		})
	}
}
