package load

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
)

// fakeReader answers every request with the bytes and error it holds, and
// records each request and the value its context carried under ctxKey.
type fakeReader struct {
	data []byte
	err  error

	mu   sync.Mutex
	reqs []compilers.DocumentRequest
	ctxs []any
}

// ctxKey keys the value a test hangs on the context it hands the compiler.
type ctxKey struct{}

// ReadDocument records req and answers it.
func (f *fakeReader) ReadDocument(ctx context.Context, req compilers.DocumentRequest) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, req)
	f.ctxs = append(f.ctxs, ctx.Value(ctxKey{}))
	return f.data, f.err
}

// seen returns the requests the reader has answered, in order.
func (f *fakeReader) seen() []compilers.DocumentRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]compilers.DocumentRequest(nil), f.reqs...)
}

// contexts returns what each request's context carried under ctxKey.
func (f *fakeReader) contexts() []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]any(nil), f.ctxs...)
}

// externalOver returns an external reading through r under opts, over a fresh
// document.
func externalOver(ctx context.Context, opts Options, r compilers.DocumentReader) (external, *soa.OpenAPI) {
	doc := &soa.OpenAPI{}
	opts.Reader = r
	return newExternal(ctx, doc, opts, newExternalReads(sourceDocument{})), doc
}

// TestExternal_FetchWithNoReaderIsRefusedUnread pins that a compile handed no
// reader reads nothing: a document asked for through any of the resolver's
// doors fails with the reason, and no tree is stored for it.
func TestExternal_FetchWithNoReaderIsRefusedUnread(t *testing.T) {
	t.Parallel()
	const want = "external document %s refused: no reader was supplied to read it"
	get := func(ext external, key string) error {
		req, err := http.NewRequest(http.MethodGet, key, nil)
		require.NoError(t, err)
		resp, err := ext.Do(req)
		assert.Nil(t, resp)
		return err
	}
	tests := []struct {
		name string
		ask  func(ext external) error
		key  string
	}{
		{"fetch", func(ext external) error { return ext.fetch(t.Context(), "k", "loc.yaml").err }, "k"},
		{"Open", func(ext external) error { _, err := ext.Open("loc.yaml"); return err }, "loc.yaml"},
		{"Do", func(ext external) error { return get(ext, "http://host/doc.yaml") }, "http://host/doc.yaml"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ext, doc := externalOver(t.Context(), Options{}, nil)

			err := tc.ask(ext)

			require.Error(t, err)
			assert.Equal(t, fmt.Sprintf(want, tc.key), err.Error())
			_, cached := doc.GetCachedExternalDocument(tc.key)
			assert.False(t, cached, "nothing is stored for a document nothing read")
		})
	}
}

// TestExternal_ReadFailureMapsTheReadersSentinels pins how the compile reports
// what a reader refused: its own two sentinels, found through any wrapping,
// become the refusal that names the budget, the directory a path lies outside
// of, or the network a confined read does not reach, and any other error
// reaches the resolver as the reader spoke it, neither wrapped nor reworded.
func TestExternal_ReadFailureMapsTheReadersSentinels(t *testing.T) {
	t.Parallel()
	const (
		budget  = "external document %s refused: it is past the 16-byte budget"
		outside = `external document %s refused: it lies outside the source's directory "/srv/specs", ` +
			"which a read leaves only when external references are allowed"
		network = "external document %s refused: it is on the network, " +
			"which a read reaches only when external references are allowed"
	)
	const url = "http://host/a.yaml"
	tests := []struct {
		name string
		key  string
		err  error
		want string // "" means err itself, passed through
	}{
		{"too large", "k", compilers.ErrDocumentTooLarge, fmt.Sprintf(budget, "k")},
		{"too large, wrapped", "k", fmt.Errorf("read k: %w of 16 bytes", compilers.ErrDocumentTooLarge), fmt.Sprintf(budget, "k")},
		{"too large, a URL", url, compilers.ErrDocumentTooLarge, fmt.Sprintf(budget, url)},
		{"a path outside", "k", compilers.ErrOutsideWithin, fmt.Sprintf(outside, "k")},
		{"a path outside, wrapped", "k", fmt.Errorf("read k: %w %q", compilers.ErrOutsideWithin, "/srv/specs"), fmt.Sprintf(outside, "k")},
		{"a URL outside", url, compilers.ErrOutsideWithin, fmt.Sprintf(network, url)},
		{"a URL outside, wrapped", url, fmt.Errorf("read %s: %w", url, compilers.ErrOutsideWithin), fmt.Sprintf(network, url)},
		{"another failure", "k", errors.New("the disk is on fire"), ""},
		{"a missing file", "k", fmt.Errorf("read k: %w", fs.ErrNotExist), ""},
		{"another failure at a URL", url, errors.New("the network is on fire"), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := Options{Within: "/srv/specs", MaxSourceBytes: 16}
			ext, doc := externalOver(t.Context(), opts, &fakeReader{err: tc.err})

			got := ext.fetch(t.Context(), tc.key, tc.key).err

			require.Error(t, got)
			if tc.want == "" {
				assert.Same(t, tc.err, got, "the reader's error, passed through as it is")
			} else {
				assert.Equal(t, tc.want, got.Error())
			}
			_, cached := doc.GetCachedExternalDocument(tc.key)
			assert.False(t, cached, "nothing is stored for a document the reader refused")
		})
	}
}

// TestExternal_FetchRefusesBytesPastTheBudgetAReaderReturned pins the backstop:
// a reader that breaks its contract and returns more than the byte budget is
// refused as one that reported the document too large would be, and the budget
// is exact, since a document of exactly that size is prepared. Without it, one
// reader could lift a budget the compile set.
func TestExternal_FetchRefusesBytesPastTheBudgetAReaderReturned(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		size   int
		limit  int
		refuse bool
	}{
		{"one byte past", 17, 16, true},
		{"far past", 4096, 16, true},
		{"exactly the budget", 16, 16, false},
		{"under the budget", 8, 16, false},
		{"no budget", 4096, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := []byte("a: " + strings.Repeat("x", tc.size-len("a: ")-1) + "\n")
			require.Len(t, body, tc.size)
			ext, doc := externalOver(t.Context(), Options{MaxSourceBytes: tc.limit}, &fakeReader{data: body})

			got := ext.fetch(t.Context(), "k", "k")

			_, cached := doc.GetCachedExternalDocument("k")
			if tc.refuse {
				require.Error(t, got.err)
				assert.Equal(t, "external document k refused: it is past the 16-byte budget", got.err.Error())
				assert.False(t, cached, "bytes past the budget are never prepared")
				return
			}
			require.NoError(t, got.err)
			assert.Equal(t, body, got.body)
			assert.True(t, cached, "bytes within the budget are prepared")
		})
	}
}

// TestExternal_ReadsAskTheReaderForTheCompilesBounds pins what each door sends
// the reader: the document as that door names it — Open's path cleaned, Do's
// URL as net/url spells it — with the compile's confinement and byte budget,
// and the context the read runs under. An unconfined compile sends no
// directory. These are the bounds the reader enforces, so a request that
// dropped one would lift it silently.
func TestExternal_ReadsAskTheReaderForTheCompilesBounds(t *testing.T) {
	t.Parallel()
	type door struct {
		name string
		ask  func(t *testing.T, ctx context.Context, ext external)
		want string
	}
	doors := []door{
		{"fetch", func(_ *testing.T, ctx context.Context, ext external) { ext.fetch(ctx, "k", "dir/./a.yaml") }, "dir/./a.yaml"},
		{"Open", func(_ *testing.T, _ context.Context, ext external) { _, _ = ext.Open("dir/../b/./a.yaml") }, "b/a.yaml"},
		{"Do", func(t *testing.T, ctx context.Context, ext external) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://host:80/a.yaml?v=1", nil)
			require.NoError(t, err)
			_, _ = ext.Do(req)
		}, "http://host:80/a.yaml?v=1"},
	}
	for _, d := range doors {
		for _, within := range []string{"/srv/specs", ""} {
			t.Run(fmt.Sprintf("%s within %q", d.name, within), func(t *testing.T) {
				t.Parallel()
				ctx := context.WithValue(t.Context(), ctxKey{}, "the read's context")
				fake := &fakeReader{err: errors.New("stop")}
				ext, _ := externalOver(ctx, Options{Within: within, MaxSourceBytes: 123}, fake)

				d.ask(t, ctx, ext)

				want := []compilers.DocumentRequest{{Location: d.want, Within: within, Limit: 123}}
				assert.Empty(t, cmp.Diff(want, fake.seen()))
				assert.Equal(t, []any{"the read's context"}, fake.contexts(), "the read runs under the caller's context")
			})
		}
	}
}

// TestExternal_OpenKeepsAnEmptyNameAnError pins that cleaning a name does not
// turn the empty one, which names no file, into the current directory: the
// reader is asked for the empty location, not for ".".
func TestExternal_OpenKeepsAnEmptyNameAnError(t *testing.T) {
	t.Parallel()
	fake := &fakeReader{err: fs.ErrNotExist}
	ext, _ := externalOver(t.Context(), Options{}, fake)

	_, err := ext.Open("")

	assert.ErrorIs(t, err, fs.ErrNotExist, "no file, passed through from the reader")
	assert.Empty(t, cmp.Diff([]compilers.DocumentRequest{{Location: ""}}, fake.seen()))
}

// TestExternal_OpenServesWhatTheReaderReturned pins the success path with no
// disk in it: the bytes a reader returns are what the resolver reads back,
// whole, and the tree prepared from them is stored under the name Open was
// given.
func TestExternal_OpenServesWhatTheReaderReturned(t *testing.T) {
	t.Parallel()
	const body = "a: 1\n"
	fake := &fakeReader{data: []byte(body)}
	ext, doc := externalOver(t.Context(), Options{}, fake)

	f, err := ext.Open("virtual/a.yaml")
	require.NoError(t, err)

	got, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, body, string(got))
	_, cached := doc.GetCachedExternalDocument("virtual/a.yaml")
	assert.True(t, cached)
}

// TestExternal_AConfinedReadRefusesByHowItIsNamed pins what a read confined to
// the source's directory refuses unread, each with its reason and naming the
// document: one a file URI names, and one an absolute path names, respelled for
// the resolver (see escapes.forResolver). A key that is an absolute path is
// read, for it says nothing of how a $ref spells it, and an unconfined read
// refuses none.
func TestExternal_AConfinedReadRefusesByHowItIsNamed(t *testing.T) {
	t.Parallel()
	const allowed = ", which a read follows only when external references are allowed"
	for _, tc := range []struct{ name, key, within, want string }{
		{"a file URI", "file:///r/a.yaml", "/r", "external document file:///r/a.yaml refused: it is named by a file URI" + allowed},
		{"a file URI in capitals", "FILE:///r/a.yaml", "/r", "external document FILE:///r/a.yaml refused: it is named by a file URI" + allowed},
		{"an absolute path, respelled", "x-morphic-absolute:%2Fr%2Fa%20b.yaml", "/r",
			"external document /r/a b.yaml refused: the $ref names it by an absolute path" + allowed},
		{"an absolute key", "/r/a.yaml", "/r", ""},
		{"a relative path", "a.yaml", "/r", ""},
		{"a file URI, unconfined", "file:///r/a.yaml", "", ""},
		{"an absolute path respelled, unconfined", "x-morphic-absolute:%2Fr%2Fa.yaml", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeReader{data: []byte("X: {}\n")}
			ext, _ := externalOver(t.Context(), Options{Within: tc.within}, fake)

			a := ext.fetch(t.Context(), tc.key, tc.key)

			if tc.want == "" {
				require.NoError(t, a.err)
				assert.Equal(t, "X: {}\n", string(a.body))
				return
			}
			require.EqualError(t, a.err, tc.want)
			assert.Empty(t, fake.reqs, "the reader is never asked")
		})
	}
}

// TestByAbsolutePath_RefusesAnAbsolutePathToAnotherDocument pins what a
// confined read refuses by a $ref's spelling: an absolute file path, wherever
// it points, unless it names the source. A relative path, a URL and a file URI,
// which unnamed refuses itself, are not, and a read confined nowhere, or with
// no reader, refuses none.
func TestByAbsolutePath_RefusesAnAbsolutePathToAnotherDocument(t *testing.T) {
	t.Parallel()
	confined := Options{Within: "/r", Reader: &fakeReader{}}
	refuses := byAbsolutePath(confined, "/r/root.yaml")
	require.NotNil(t, refuses)
	for uri, want := range map[string]bool{
		"/r/a.yaml": true, "/elsewhere/a.yaml": true, "/r/x/../a.yaml": true,
		"/r/root.yaml": false, "/r/./root.yaml": false,
		"a.yaml": false, "../a.yaml": false, "https://h/a.yaml": false, "file:///r/a.yaml": false,
	} {
		assert.Equal(t, want, refuses(uri), uri)
	}
	assert.True(t, byAbsolutePath(confined, "")("/r/root.yaml"), "a source with no path is named by none")
	assert.Nil(t, byAbsolutePath(Options{Reader: &fakeReader{}}, "/r/root.yaml"))
	assert.Nil(t, byAbsolutePath(Options{Within: "/r"}, "/r/root.yaml"))
}
