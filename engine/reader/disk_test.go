package reader_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine/reader"
)

// diskTree builds the files the Disk tests read, under a fresh directory:
// in/ is the directory a confined request is held to, and every other file,
// directory and link is somewhere a read of it could wrongly reach.
func diskTree(t *testing.T) string {
	t.Helper()
	root := reader.ResolvedDir(t)
	writeFile(t, root, "in/a.yaml", "inside")
	writeFile(t, root, "in/sub/b.yaml", "nested")
	writeFile(t, root, "in/with space.yaml", "spaced")
	writeFile(t, root, "in-other/a.yaml", "sibling")
	writeFile(t, root, "outside.yaml", "outside")
	writeFile(t, root, "outdir/x.yaml", "outdir")
	symlink(t, "a.yaml", filepath.Join(root, "in", "link-in.yaml"))
	symlink(t, "sub", filepath.Join(root, "in", "dirlink"))
	symlink(t, filepath.Join("..", "outside.yaml"), filepath.Join(root, "in", "link-out.yaml"))
	symlink(t, filepath.Join("..", "outdir"), filepath.Join(root, "in", "outlink"))
	symlink(t, filepath.Join(root, "in", "a.yaml"), filepath.Join(root, "in", "abs-in.yaml"))
	symlink(t, filepath.Join(root, "outside.yaml"), filepath.Join(root, "in", "abs-out.yaml"))
	return root
}

// diskCase is one request to a Disk and what it must answer: the bytes want,
// or an error that wraps wantIs and names names. A row with fails set is
// refused with some error. Location, Within and names may hold the
// placeholders expander reads.
type diskCase struct {
	name, within, location string
	want                   string
	fails                  bool
	wantIs                 error
	names                  string
}

// check reads tc's request through a Disk and holds the answer to tc.
func (tc diskCase) check(t *testing.T, expand func(string) string) {
	t.Helper()
	req := compilers.DocumentRequest{Location: expand(tc.location), Within: expand(tc.within)}

	got, err := reader.Disk{}.ReadDocument(t.Context(), req)

	if !tc.fails && tc.wantIs == nil {
		require.NoError(t, err)
		assert.Equal(t, tc.want, string(got))
		return
	}
	require.Error(t, err)
	assert.Nil(t, got, "a refused read hands back no bytes")
	if tc.wantIs != nil {
		assert.ErrorIs(t, err, tc.wantIs)
	}
	if !errors.Is(tc.wantIs, fs.ErrNotExist) {
		assert.NotErrorIs(t, err, fs.ErrNotExist, "refused, not missing")
	}
	assert.Contains(t, err.Error(), expand(tc.names))
}

// TestDisk_ReadDocumentConfined pins a read confined to a directory: a file
// inside is read, a path outside as written or once its links are followed is
// refused with ErrOutsideWithin, and a link that stays inside is read, an
// absolute one included. A URL is outside every directory and never reaches
// the network, whatever the case of its scheme.
func TestDisk_ReadDocumentConfined(t *testing.T) {
	t.Parallel()
	root := diskTree(t)
	srv, hits := countingServer(t, serveBody(t, http.StatusOK, "network"))
	t.Cleanup(func() { assert.Zero(t, hits.Load(), "a confined read reaches no server") })
	expand := expander(root, srv.URL)
	const in = "{root}/in"
	tests := []diskCase{
		{name: "a file inside", within: in, location: in + "/a.yaml", want: "inside"},
		{name: "a file in a directory inside", within: in, location: in + "/sub/b.yaml", want: "nested"},
		{name: "a directory spelled with dot segments", within: "{root}/in/../in/./", location: in + "/a.yaml",
			want: "inside"},
		{name: "a path climbing out", within: in, location: in + "/../outside.yaml", wantIs: compilers.ErrOutsideWithin},
		{name: "a sibling sharing the name prefix", within: in, location: "{root}/in-other/a.yaml",
			wantIs: compilers.ErrOutsideWithin},
		{name: "a file elsewhere", within: in, location: "{root}/outside.yaml", wantIs: compilers.ErrOutsideWithin},
		{name: "the directory itself", within: in, location: in, wantIs: reader.ErrNotRegular, names: in},
		{name: "a missing file", within: in, location: in + "/missing.yaml", wantIs: fs.ErrNotExist},
		{name: "a missing directory to be held to", within: "{root}/nope", location: "{root}/nope/a.yaml",
			wantIs: fs.ErrNotExist},
		{name: "a link inside to a file inside", within: in, location: in + "/link-in.yaml", want: "inside"},
		{name: "a link inside to a directory inside", within: in, location: in + "/dirlink/b.yaml", want: "nested"},
		{name: "a link out to a file", within: in, location: in + "/link-out.yaml",
			wantIs: compilers.ErrOutsideWithin, names: in + "/link-out.yaml"},
		{name: "a link out to a directory", within: in, location: in + "/outlink/x.yaml",
			wantIs: compilers.ErrOutsideWithin, names: in + "/outlink/x.yaml"},
		{name: "an absolute link to a file inside", within: in, location: in + "/abs-in.yaml", want: "inside"},
		{name: "an absolute link to a file outside", within: in, location: in + "/abs-out.yaml",
			wantIs: compilers.ErrOutsideWithin, names: in + "/abs-out.yaml"},
		{name: "an http URL", within: in, location: "{srv}/doc.yaml", wantIs: compilers.ErrOutsideWithin},
		{name: "an http URL in capitals", within: in, location: "{SRV}/doc.yaml", wantIs: compilers.ErrOutsideWithin},
		{name: "a file URI inside", within: in, location: "{rooturi}/in/a.yaml", want: "inside"},
		{name: "a file URI in capitals inside", within: in, location: "{ROOTURI}/in/a.yaml", want: "inside"},
		{name: "a file URI outside", within: in, location: "{rooturi}/outside.yaml", wantIs: compilers.ErrOutsideWithin},
		{name: "a file URI naming another host", within: in, location: "file://server/share/a.yaml", fails: true},
		{name: "a file URI with a query", within: in, location: "{rooturi}/in/a.yaml?v=1", fails: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.check(t, expand)
		})
	}
}

// TestDisk_ReadDocumentConfinedToTheWorkingDirectory pins how a confined read
// treats a relative directory or file: both relative, both are read against
// the working directory alike; one absolute and one not, the working
// directory is what makes them comparable. It changes directory, so it runs
// alone.
func TestDisk_ReadDocumentConfinedToTheWorkingDirectory(t *testing.T) {
	root := diskTree(t)
	t.Chdir(root)
	expand := expander(root, "")
	tests := []diskCase{
		{name: "a relative directory, an absolute file", within: "in", location: "{root}/in/a.yaml", want: "inside"},
		{name: "an absolute directory, a relative file", within: "{root}/in", location: "in/a.yaml", want: "inside"},
		{name: "both relative", within: "in", location: "in/a.yaml", want: "inside"},
		{name: "both relative, climbing out", within: "in", location: "in/../outside.yaml",
			wantIs: compilers.ErrOutsideWithin},
		{name: "both relative, a sibling", within: "in", location: "in-other/a.yaml", wantIs: compilers.ErrOutsideWithin},
		{name: "a relative directory, an absolute file outside", within: "in", location: "{root}/outside.yaml",
			wantIs: compilers.ErrOutsideWithin},
		{name: "an absolute directory, a relative file outside", within: "{root}/in", location: "outside.yaml",
			wantIs: compilers.ErrOutsideWithin},
		{name: "the working directory itself", within: ".", location: "outside.yaml", want: "outside"},
		{name: "a relative link out", within: "in", location: "in/link-out.yaml", fails: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.check(t, expand)
		})
	}
}

// TestDisk_ReadDocumentConfinedBelowALinkedWorkingDirectory pins a confined
// read from a working directory reached through a symbolic link: a relative
// directory and an absolute file inside it are inside, though the file's links
// resolve and the directory's, being relative, only below the working
// directory. It changes directory, so it runs alone.
func TestDisk_ReadDocumentConfinedBelowALinkedWorkingDirectory(t *testing.T) {
	root := diskTree(t)
	link := filepath.Join(reader.ResolvedDir(t), "link")
	symlink(t, root, link)
	t.Chdir(link)
	expand := expander(link, "")
	tests := []diskCase{
		{name: "a relative directory, an absolute file", within: "in", location: "{root}/in/a.yaml", want: "inside"},
		{name: "an absolute directory, a relative file", within: "{root}/in", location: "in/a.yaml", want: "inside"},
		{name: "a relative directory, an absolute file outside", within: "in", location: "{root}/outside.yaml",
			wantIs: compilers.ErrOutsideWithin},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.check(t, expand)
		})
	}
}

// TestDisk_ReadDocumentUnconfined pins a read with no Within: any file is
// read wherever it lies, links followed, by path or as the file URI that
// names it, which is read percent-decoded and with dot segments removed. A
// path no file answers to fails as the file system says, and a file URI
// naming no local file fails with the reason.
func TestDisk_ReadDocumentUnconfined(t *testing.T) {
	t.Parallel()
	root := diskTree(t)
	expand := expander(root, "")
	tests := []diskCase{
		{name: "an absolute path", location: "{root}/outside.yaml", want: "outside"},
		{name: "a path with dot segments", location: "{root}/in/../outside.yaml", want: "outside"},
		{name: "a link out is followed", location: "{root}/in/link-out.yaml", want: "outside"},
		{name: "an absolute link is followed", location: "{root}/in/abs-in.yaml", want: "inside"},
		{name: "a file URI", location: "{rooturi}/outside.yaml", want: "outside"},
		{name: "a file URI at localhost", location: "{localuri}/outside.yaml", want: "outside"},
		{name: "a file URI in capitals", location: "{ROOTURI}/outside.yaml", want: "outside"},
		{name: "a percent-encoded file URI", location: "{rooturi}/in/with%20space.yaml", want: "spaced"},
		{name: "a file URI with dot segments", location: "{rooturi}/in/../outside.yaml", want: "outside"},
		{name: "a missing file", location: "{root}/missing.yaml", wantIs: fs.ErrNotExist},
		{name: "a missing file by URI", location: "{rooturi}/missing.yaml", wantIs: fs.ErrNotExist},
		{name: "a directory", location: "{root}/in", wantIs: reader.ErrNotRegular, names: "{root}/in"},
		{name: "a directory by URI", location: "{rooturi}/in", wantIs: reader.ErrNotRegular, names: "{root}/in"},
		{name: "a file URI naming another host", location: "file://server/share/a.yaml", fails: true},
		{name: "a file URI with a query", location: "{rooturi}/outside.yaml?v=1", fails: true},
		{name: "a relative file URI", location: "file:outside.yaml", fails: true},
	}
	if runtime.GOOS != "windows" {
		writeFile(t, root, "in/back\\slash.yaml", "backslashed")
		tests = append(tests, diskCase{name: "an escaped backslash is a name here",
			location: "{rooturi}/in/back%5Cslash.yaml", want: "backslashed"})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.check(t, expand)
		})
	}
}

// TestDisk_ReadDocumentReadsARelativePathAgainstTheWorkingDirectory pins that
// an unconfined relative path is read from the working directory. It changes
// directory, so it runs alone.
func TestDisk_ReadDocumentReadsARelativePathAgainstTheWorkingDirectory(t *testing.T) {
	root := diskTree(t)
	t.Chdir(root)

	got, err := reader.Disk{}.ReadDocument(t.Context(), compilers.DocumentRequest{Location: "in/./a.yaml"})

	require.NoError(t, err)
	assert.Equal(t, "inside", string(got))
}

// TestDisk_ReadDocumentHTTP pins what an http(s) location reads: the body of
// a success, in any case of the scheme, a redirect followed; and an error
// naming the status code and the URL for anything else, 1xx and 3xx included,
// since no document is read from them.
func TestDisk_ReadDocumentHTTP(t *testing.T) {
	t.Parallel()
	redirect := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/final.yaml" {
			_, err := io.WriteString(w, "followed")
			assert.NoError(t, err)
			return
		}
		http.Redirect(w, r, "/final.yaml", http.StatusFound)
	}
	tests := []struct {
		name     string
		handler  http.Handler
		location string
		want     string
		wantCode string
	}{
		{name: "a 200", handler: serveBody(t, http.StatusOK, "net: 1"), want: "net: 1"},
		{name: "a 200 with the scheme in capitals", handler: serveBody(t, http.StatusOK, "net: 1"),
			location: "{SRV}/doc.yaml", want: "net: 1"},
		{name: "a 204 with no body", handler: serveBody(t, http.StatusNoContent, "")},
		{name: "the last success status", handler: serveBody(t, 299, "edge"), want: "edge"},
		{name: "a redirect, followed", handler: http.HandlerFunc(redirect), want: "followed"},
		{name: "a protocol switch", handler: serveBody(t, http.StatusSwitchingProtocols, ""),
			wantCode: "101"},
		{name: "the first status past success", handler: serveBody(t, 300, "multiple"),
			wantCode: "300"},
		{name: "a redirect with nowhere to go", handler: serveBody(t, http.StatusFound, "moved"),
			wantCode: "302"},
		{name: "a 404", handler: serveBody(t, http.StatusNotFound, "nope"), wantCode: "404"},
		{name: "a 500", handler: serveBody(t, http.StatusInternalServerError, "oops"),
			wantCode: "500"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, hits := countingServer(t, tc.handler)
			location := cmpOr(tc.location, "{srv}/doc.yaml")

			got, err := reader.Disk{}.ReadDocument(t.Context(),
				compilers.DocumentRequest{Location: expander("", srv.URL)(location)})

			assert.Positive(t, hits.Load())
			if tc.wantCode != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantCode)
				assert.Contains(t, err.Error(), srv.URL+"/doc.yaml", "the URL asked for")
				assert.Nil(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}
}

// cmpOr returns s, or fallback where s is empty.
func cmpOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// TestDisk_ReadDocumentHoldsTheLimit pins the size limit for every kind of
// location a Disk reads: exactly the limit is read, one byte more fails with
// ErrDocumentTooLarge, and zero, a negative limit and math.MaxInt read the
// document whole.
func TestDisk_ReadDocumentHoldsTheLimit(t *testing.T) {
	t.Parallel()
	const body = "12345"
	root := reader.ResolvedDir(t)
	file := writeFile(t, root, "doc.yaml", body)
	srv, _ := countingServer(t, serveBody(t, http.StatusOK, body))
	locations := map[string]string{"a path": file, "a file URI": fileURI(file), "a URL": srv.URL + "/doc.yaml"}
	limits := []struct {
		name  string
		limit int
		over  bool
	}{
		{name: "the exact size", limit: 5},
		{name: "one byte under", limit: 4, over: true},
		{name: "room to spare", limit: 6},
		{name: "zero", limit: 0},
		{name: "negative", limit: -1},
		{name: "math.MaxInt", limit: math.MaxInt},
	}
	for kind, location := range locations {
		for _, tc := range limits {
			t.Run(kind+", "+tc.name, func(t *testing.T) {
				t.Parallel()

				got, err := reader.Disk{}.ReadDocument(t.Context(),
					compilers.DocumentRequest{Location: location, Limit: tc.limit})

				if tc.over {
					require.ErrorIs(t, err, compilers.ErrDocumentTooLarge)
					assert.Contains(t, err.Error(), fmt.Sprintf("%d bytes", tc.limit))
					assert.Nil(t, got)
					return
				}
				require.NoError(t, err)
				assert.Equal(t, body, string(got))
			})
		}
	}
}

// TestDisk_ReadDocumentHoldsTheLimitWithinADirectory pins the same limit for a
// confined read, which opens the file through a different path.
func TestDisk_ReadDocumentHoldsTheLimitWithinADirectory(t *testing.T) {
	t.Parallel()
	root := reader.ResolvedDir(t)
	file := writeFile(t, root, "doc.yaml", "12345")
	for limit, wantOver := range map[int]bool{5: false, 4: true, 0: false, -1: false, math.MaxInt: false} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			t.Parallel()

			got, err := reader.Disk{}.ReadDocument(t.Context(),
				compilers.DocumentRequest{Location: file, Within: root, Limit: limit})

			if wantOver {
				require.ErrorIs(t, err, compilers.ErrDocumentTooLarge)
				assert.Nil(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "12345", string(got))
		})
	}
}

// TestDisk_ReadDocumentWithACancelledContext pins that a context already
// cancelled fails the read before it starts, whatever the location is: no
// file is read and no request is sent.
func TestDisk_ReadDocumentWithACancelledContext(t *testing.T) {
	t.Parallel()
	root := diskTree(t)
	srv, hits := countingServer(t, serveBody(t, http.StatusOK, "network"))
	for name, req := range map[string]compilers.DocumentRequest{
		"a path":              {Location: filepath.Join(root, "outside.yaml")},
		"a confined path":     {Location: filepath.Join(root, "in", "a.yaml"), Within: filepath.Join(root, "in")},
		"a file URI":          {Location: fileURI(filepath.Join(root, "outside.yaml"))},
		"a URL":               {Location: srv.URL + "/doc.yaml"},
		"an empty location":   {},
		"a location outside":  {Location: srv.URL + "/doc.yaml", Within: root},
		"a missing file path": {Location: filepath.Join(root, "missing.yaml")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := reader.Disk{}.ReadDocument(cancelled(t), req)

			require.ErrorIs(t, err, context.Canceled)
			assert.Nil(t, got)
		})
	}
	t.Cleanup(func() { assert.Zero(t, hits.Load(), "no request is sent under a cancelled context") })
}

// TestDisk_ReadDocumentStopsWhenTheContextIsCancelledMidRequest pins that the
// request carries the context: the server cancels it once it has the request,
// and the read fails with the cancellation instead of waiting on a response.
func TestDisk_ReadDocumentStopsWhenTheContextIsCancelledMidRequest(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	srv, _ := countingServer(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		cancel()
		<-r.Context().Done()
	}))

	got, err := reader.Disk{}.ReadDocument(ctx, compilers.DocumentRequest{Location: srv.URL + "/doc.yaml"})

	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, got)
}

// ctxKey keys the value TestDisk_ReadDocumentUsesItsClient puts on its context.
type ctxKey struct{}

// TestDisk_ReadDocumentUsesItsClient pins that a Disk's Client sends the
// request, a GET for the URL as written, under the caller's context: the
// transport counts what it is asked, and no server is reached.
func TestDisk_ReadDocumentUsesItsClient(t *testing.T) {
	t.Parallel()
	var asked atomic.Int32
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		asked.Add(1)
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "http://spec.invalid/doc.yaml", r.URL.String())
		assert.Equal(t, "carried", r.Context().Value(ctxKey{}), "the caller's context rides the request")
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader("served"))}, nil
	})
	ctx := context.WithValue(t.Context(), ctxKey{}, "carried")

	got, err := reader.Disk{Client: &http.Client{Transport: transport}}.ReadDocument(ctx,
		compilers.DocumentRequest{Location: "http://spec.invalid/doc.yaml"})

	require.NoError(t, err)
	assert.Equal(t, "served", string(got))
	assert.Equal(t, int32(1), asked.Load())
}

// TestDisk_ReadDocumentWithANilClientUsesTheDefaultClient pins that a zero
// Disk sends its request through http.DefaultClient, observed by swapping that
// client's transport for one that counts. It changes a package variable, so it
// runs alone.
func TestDisk_ReadDocumentWithANilClientUsesTheDefaultClient(t *testing.T) {
	var asked atomic.Int32
	previous := http.DefaultClient.Transport
	http.DefaultClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		asked.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader("default"))}, nil
	})
	t.Cleanup(func() { http.DefaultClient.Transport = previous })

	got, err := reader.Disk{}.ReadDocument(t.Context(), compilers.DocumentRequest{Location: "http://spec.invalid/doc.yaml"})

	require.NoError(t, err)
	assert.Equal(t, "default", string(got))
	assert.Equal(t, int32(1), asked.Load())
}

// TestDisk_ReadDocumentPassesATransportErrorThrough pins that a request the
// client cannot complete fails the read with the client's own error, wrapped,
// whether nothing is listening or the transport fails with an error of its own.
func TestDisk_ReadDocumentPassesATransportErrorThrough(t *testing.T) {
	t.Parallel()
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closed.Close()
	boom := errors.New("boom")

	t.Run("nothing is listening", func(t *testing.T) {
		t.Parallel()

		got, err := reader.Disk{}.ReadDocument(t.Context(), compilers.DocumentRequest{Location: closed.URL + "/doc.yaml"})

		var urlErr *url.Error
		require.ErrorAs(t, err, &urlErr, "the client's error, not a flattened copy of it")
		assert.Nil(t, got)
	})
	t.Run("the transport fails", func(t *testing.T) {
		t.Parallel()
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, boom })}

		got, err := reader.Disk{Client: client}.ReadDocument(t.Context(),
			compilers.DocumentRequest{Location: "http://spec.invalid/doc.yaml"})

		require.ErrorIs(t, err, boom)
		assert.Nil(t, got)
	})
}

// TestDisk_ReadDocumentFailsOnATruncatedBody pins that a body ending before
// its declared Content-Length fails the read mid-way, rather than yielding a
// short document that reads as a whole one.
func TestDisk_ReadDocumentFailsOnATruncatedBody(t *testing.T) {
	t.Parallel()
	srv, _ := countingServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, err := io.WriteString(w, "0123456789")
		assert.NoError(t, err)
	}))

	got, err := reader.Disk{}.ReadDocument(t.Context(), compilers.DocumentRequest{Location: srv.URL + "/doc.yaml"})

	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	assert.Nil(t, got)
}

// closeRecorder is a response body that counts its closes and fails them with
// closeErr.
type closeRecorder struct {
	io.Reader
	closeErr error
	closes   int
}

func (b *closeRecorder) Close() error {
	b.closes++
	return b.closeErr
}

// TestDisk_ReadDocumentClosesTheResponseBody pins that the response body is
// closed exactly once however the read ends, on a document read, a failed
// status, a document too large and a failed read, and that a close which
// fails is reported with the outcome, a read that succeeded included, whose
// bytes it then withholds.
func TestDisk_ReadDocumentClosesTheResponseBody(t *testing.T) {
	t.Parallel()
	readFailed := errors.New("read failed")
	closeFailed := errors.New("close failed")
	tests := []struct {
		name     string
		status   int
		body     io.Reader
		closeErr error
		limit    int
		want     string
		wantIs   []error
		wantText string
	}{
		{name: "a document read", status: 200, body: strings.NewReader("12345"), want: "12345"},
		{name: "a failed status", status: 404, body: strings.NewReader("nope"), wantText: "404"},
		{name: "a document too large", status: 200, body: strings.NewReader("12345"), limit: 4,
			wantIs: []error{compilers.ErrDocumentTooLarge}},
		{name: "a read that fails", status: 200, body: iotest.ErrReader(readFailed), wantIs: []error{readFailed}},
		{name: "a close that fails after a read", status: 200, body: strings.NewReader("12345"),
			closeErr: closeFailed, wantIs: []error{closeFailed}},
		{name: "a close that fails after a failed status", status: 500, body: strings.NewReader("oops"),
			closeErr: closeFailed, wantIs: []error{closeFailed}, wantText: "500"},
		{name: "a close that fails after a failed read", status: 200, body: iotest.ErrReader(readFailed),
			closeErr: closeFailed, wantIs: []error{readFailed, closeFailed}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := &closeRecorder{Reader: tc.body, closeErr: tc.closeErr}
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Status: fmt.Sprintf("%d x", tc.status), Body: body}, nil
			})}

			got, err := reader.Disk{Client: client}.ReadDocument(t.Context(),
				compilers.DocumentRequest{Location: "http://spec.invalid/doc.yaml", Limit: tc.limit})

			assert.Equal(t, 1, body.closes, "closed once")
			if len(tc.wantIs) == 0 && tc.wantText == "" {
				require.NoError(t, err)
				assert.Equal(t, tc.want, string(got))
				return
			}
			require.Error(t, err)
			for _, want := range tc.wantIs {
				assert.ErrorIs(t, err, want)
			}
			assert.Contains(t, err.Error(), tc.wantText)
			assert.Nil(t, got)
		})
	}
}
