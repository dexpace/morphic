package reader

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
)

// TestLocate_ClassifiesARequestsLocation pins what a request's Location is
// read as: an http(s) or file URL, in any case of its scheme, or else a path,
// whatever else parses as a URL, so a Windows drive letter or a string
// url.Parse refuses is a file, not a malformed URL. An http(s) URL is kept as
// a request spells it, a file URI is read as the path it names, and a confined
// request refuses an http(s) URL.
func TestLocate_ClassifiesARequestsLocation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, location, within string
		windows                bool
		want                   location
		wantIs                 error
		wantErr                string
	}{
		{name: "a relative path, cleaned", location: "a/./b.yaml", want: location{path: filepath.FromSlash("a/b.yaml")}},
		{name: "a path climbing out is kept", location: "../a.yaml", want: location{path: filepath.FromSlash("../a.yaml")}},
		{name: "a Windows drive path", location: `C:\x\a.yaml`, want: location{path: filepath.Clean(`C:\x\a.yaml`)}},
		{name: "a drive path in slashes", location: "c:/x/a.yaml", want: location{path: filepath.Clean("c:/x/a.yaml")}},
		{name: "a string url.Parse refuses", location: "100%.yaml", want: location{path: "100%.yaml"}},
		{name: "a scheme no reader reads", location: "ftp://h/a.yaml", want: location{path: filepath.Clean("ftp://h/a.yaml")}},
		{name: "an http URL", location: "http://h/a.yaml", want: location{url: "http://h/a.yaml"}},
		{name: "an https URL", location: "https://h/a.yaml?v=1", want: location{url: "https://h/a.yaml?v=1"}},
		{name: "an http URL in capitals, its scheme lowered", location: "HTTP://H/a", want: location{url: "http://H/a"}},
		{name: "an http URL escaped as a request spells it", location: "http://h/a b/é.yaml",
			want: location{url: "http://h/a%20b/%C3%A9.yaml"}},
		{name: "an http URL a confined request refuses", location: "http://h/a.yaml", within: "d",
			wantIs: compilers.ErrOutsideWithin, wantErr: `"d"`},
		{name: "an https URL in capitals a confined request refuses", location: "HTTPS://h/a", within: "d",
			wantIs: compilers.ErrOutsideWithin},
		{name: "a path is judged by the read, not here", location: "a.yaml", within: "d",
			want: location{path: "a.yaml"}},
		{name: "a file URI", location: "file:///a/b.yaml", want: location{path: filepath.FromSlash("/a/b.yaml")}},
		{name: "a file URI in capitals", location: "File:///a", want: location{path: filepath.FromSlash("/a")}},
		{name: "a file URI's dot segments, cleaned", location: "file:///a/./c/../b.yaml",
			want: location{path: filepath.FromSlash("/a/b.yaml")}},
		{name: "a file URI a confined request leaves to the read", location: "file:///a", within: "d",
			want: location{path: filepath.FromSlash("/a")}},
		{name: "a file URI on Windows", location: "file:///C:/a/b.yaml", windows: true,
			want: location{path: filepath.FromSlash("C:/a/b.yaml")}},
		{name: "the same file URI elsewhere", location: "file:///C:/a/b.yaml",
			want: location{path: filepath.FromSlash("/C:/a/b.yaml")}},
		{name: "an escaped backslash on Windows", location: "file:///a%5Cb", windows: true, wantErr: "backslash"},
		{name: "an escaped backslash elsewhere", location: "file:///a%5Cb", want: location{path: filepath.Clean(`/a\b`)}},
		{name: "a file URI naming another host", location: "file://server/share/a.yaml",
			wantErr: `host "server"`},
		{name: "a file URI with a query, confined", location: "file:///a.yaml?v=1", within: "d", wantErr: "query"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := locate(compilers.DocumentRequest{Location: tc.location, Within: tc.within}, tc.windows)

			if tc.wantIs != nil || tc.wantErr != "" {
				require.Error(t, err)
				if tc.wantIs != nil {
					assert.ErrorIs(t, err, tc.wantIs)
				} else {
					assert.NotErrorIs(t, err, compilers.ErrOutsideWithin, "the URI is refused for its own reason")
				}
				assert.Contains(t, err.Error(), tc.wantErr)
				assert.Equal(t, location{}, got, "a refused location names nothing")
				return
			}
			require.NoError(t, err)
			assert.Empty(t, cmp.Diff(tc.want, got, cmp.AllowUnexported(location{})))
		})
	}
}

// TestReaders_AnEmptyLocationIsAnError pins that a request naming nothing is
// refused by locate and both readers, where cleaning the empty string would
// have read the working directory.
func TestReaders_AnEmptyLocationIsAnError(t *testing.T) {
	t.Parallel()
	mem, err := NewMemory(nil)
	require.NoError(t, err)
	req := compilers.DocumentRequest{}
	_, locErr := locate(req, false)
	_, diskErr := Disk{}.ReadDocument(t.Context(), req)
	_, memErr := mem.ReadDocument(t.Context(), req)

	for name, got := range map[string]error{"locate": locErr, "Disk": diskErr, "Memory": memErr} {
		assert.ErrorIs(t, got, errEmptyLocation, name)
	}
}

// TestRelativeTo_ReadsTheLexicalPath pins the containment test: dot segments
// removed first, a directory inside itself, and a sibling that merely shares
// a name prefix outside it. Both paths relative or both absolute, so the
// working directory is not read.
func TestRelativeTo_ReadsTheLexicalPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tests := []struct {
		name, dir, path string
		want            string
		wantOK          bool
	}{
		{name: "a file inside", dir: "d", path: "d/a.yaml", want: "a.yaml", wantOK: true},
		{name: "a file deeper inside", dir: "d", path: "d/x/a.yaml", want: "x/a.yaml", wantOK: true},
		{name: "the directory itself", dir: "d", path: "d", want: ".", wantOK: true},
		{name: "dot segments in the path", dir: "d", path: "d/x/../a.yaml", want: "a.yaml", wantOK: true},
		{name: "dot segments in the directory", dir: "d/../e", path: "e/a.yaml", want: "a.yaml", wantOK: true},
		{name: "a path climbing out", dir: "d", path: "d/../e.yaml"},
		{name: "a sibling sharing the name prefix", dir: "d", path: "dd/a.yaml"},
		{name: "the parent", dir: "d/e", path: "d"},
		{name: "absolute, inside", dir: "@/d", path: "@/d/a.yaml", want: "a.yaml", wantOK: true},
		{name: "absolute, outside", dir: "@/d", path: "@/e/a.yaml"},
		{name: "absolute, climbing out", dir: "@/d", path: "@/d/../e.yaml"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := relativeTo(under(root, tc.dir), under(root, tc.path))

			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.want, filepath.ToSlash(got))
		})
	}
}

// under returns name with a leading "@" read as root, and any other name as
// written.
func under(root, name string) string {
	if rest, ok := strings.CutPrefix(name, "@"); ok {
		return root + rest
	}
	return name
}

// TestRelativeTo_ReadsTheWorkingDirectoryOnlyForAMixedPair pins that the
// working directory is consulted when exactly one of the directory and the
// path is absolute, and never otherwise: with the working directory named by
// no one, a mixed pair is outside and a like pair still reads.
func TestRelativeTo_ReadsTheWorkingDirectoryOnlyForAMixedPair(t *testing.T) {
	root := ResolvedDir(t)
	t.Chdir(root)
	abs := filepath.Join(root, "d", "a.yaml")

	rel, ok := relativeTo("d", abs)
	assert.True(t, ok, "a relative directory against an absolute path")
	assert.Equal(t, "a.yaml", rel)

	rel, ok = relativeTo(filepath.Join(root, "d"), filepath.Join("d", "a.yaml"))
	assert.True(t, ok, "an absolute directory against a relative path")
	assert.Equal(t, "a.yaml", rel)

	_, ok = relativeTo("d", filepath.Join(root, "e", "a.yaml"))
	assert.False(t, ok, "an absolute path outside the relative directory")

	VanishWorkingDirectory(t)

	rel, ok = relativeTo("d", filepath.Join("d", "a.yaml"))
	assert.True(t, ok, "two relative paths need no working directory")
	assert.Equal(t, "a.yaml", rel)

	_, ok = relativeTo("d", abs)
	assert.False(t, ok, "a relative directory against an absolute path, no working directory")

	_, ok = relativeTo(filepath.Dir(abs), filepath.Join("d", "a.yaml"))
	assert.False(t, ok, "an absolute directory against a relative path, no working directory")
}

// TestReadLimited_HoldsTheLimit pins the size a document may have: exactly
// the limit passes, one byte past fails with ErrDocumentTooLarge naming the
// limit, a limit of zero or less reads any size, and math.MaxInt, where the
// byte past the limit would overflow, reads the document whole.
func TestReadLimited_HoldsTheLimit(t *testing.T) {
	t.Parallel()
	const body = "12345"
	tests := []struct {
		name  string
		limit int
		over  bool
	}{
		{name: "the exact size", limit: 5},
		{name: "one byte past", limit: 4, over: true},
		{name: "well past", limit: 1, over: true},
		{name: "room to spare", limit: 6},
		{name: "zero is unbounded", limit: 0},
		{name: "negative is unbounded", limit: -1},
		{name: "a limit one under the overflow", limit: math.MaxInt - 1},
		{name: "a limit at the overflow", limit: math.MaxInt},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := readLimited(t.Context(), "doc", strings.NewReader(body), tc.limit, 0)

			if tc.over {
				require.ErrorIs(t, err, compilers.ErrDocumentTooLarge)
				assert.Contains(t, err.Error(), "doc")
				assert.Contains(t, err.Error(), fmt.Sprintf("%d bytes", tc.limit))
				assert.Nil(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, body, string(got))
		})
	}
}

// serveThenFail serves 'x' bytes up to max in total and fails on any read
// past that. Bounded at a limit, a read is never asked for more than the limit
// allows and so never reaches its own failure; unbounded, io.ReadAll drives it
// to max before the length is judged.
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

// TestReadLimited_StopsReadingAtTheLimit proves the limit bounds the read
// itself and not only the verdict reached afterward: a reader that would serve
// ten times the limit before failing on its own is never asked for more than
// the limit, so the document is reported too large, not the reader's failure.
func TestReadLimited_StopsReadingAtTheLimit(t *testing.T) {
	t.Parallel()
	const limit = 5

	_, err := readLimited(t.Context(), "doc", &serveThenFail{max: 10 * limit}, limit, 0)

	assert.ErrorIs(t, err, compilers.ErrDocumentTooLarge,
		"the reader was never driven far enough to reach its own failure")
}

// TestReadLimited_ReportsAReadFailure pins that a read that fails is reported
// with the document's name, wrapping the failure, and is not mistaken for a
// document too large.
func TestReadLimited_ReportsAReadFailure(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")

	got, err := readLimited(t.Context(), "doc", iotest.ErrReader(boom), 10, 0)

	require.ErrorIs(t, err, boom)
	assert.NotErrorIs(t, err, compilers.ErrDocumentTooLarge)
	assert.Contains(t, err.Error(), "doc")
	assert.Nil(t, got)
}

// TestReadUpTo_SizesTheBufferFromWhatTheDocumentReports pins the buffer a read
// starts with: room for a document of the size it reports and the end after
// it, so the read never grows it, and never more than the bytes the read may
// take or maxInitial, so neither a size past the limit nor a sparse file's
// allocates what no byte read backs.
func TestReadUpTo_SizesTheBufferFromWhatTheDocumentReports(t *testing.T) {
	t.Parallel()
	body := strings.Repeat("x", 3000)
	tests := []struct {
		name    string
		n       int
		size    int64
		wantLen int
		wantCap int
	}{
		{name: "a reported size, unbounded", n: 0, size: 3000, wantLen: 3000, wantCap: 3000 + minRead},
		{name: "a reported size past the bound", n: 100, size: 3000, wantLen: 100, wantCap: 100},
		{name: "a reported size within the bound", n: 5000, size: 3000, wantLen: 3000, wantCap: 3000 + minRead},
		{name: "no size, a bound under one buffer", n: 100, wantLen: 100, wantCap: 100},
		{name: "a size past the largest first buffer", n: 0, size: 2 * maxInitial, wantLen: 3000,
			wantCap: maxInitial + minRead},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := readUpTo(t.Context(), "doc", strings.NewReader(body), tc.n, tc.size)

			require.NoError(t, err)
			assert.Len(t, got, tc.wantLen)
			assert.Equal(t, tc.wantCap, cap(got))
		})
	}
}

// TestReadUpTo_GrowsNoFurtherThanTheBound pins growth: a buffer grown as it
// fills, whether the document reported no size or one it then outgrew, as a
// file growing while it is read does, is never asked to grow past the bound,
// and the read takes exactly the bytes the bound allows, however much the
// reader has left.
func TestReadUpTo_GrowsNoFurtherThanTheBound(t *testing.T) {
	t.Parallel()
	const n = 10<<20 + 7
	for _, size := range []int64{0, 8 << 20} {
		t.Run(fmt.Sprintf("reporting %d bytes", size), func(t *testing.T) {
			t.Parallel()
			r := &serveThenFail{max: 2 * n}

			got, err := readUpTo(t.Context(), "doc", r, n, size)

			require.NoError(t, err)
			assert.Len(t, got, n)
			assert.Equal(t, n, r.served, "nothing past the bound is read")
			assert.LessOrEqual(t, cap(got), n, "the buffer grew to the bound, not past it")
		})
	}
}

// TestReadUpTo_AsksTheContextBetweenChunks pins how often a read asks ctx: a
// reader serving without end is read a chunk at a time, so a cancel landing
// during the first read ends the read with the context's error before a
// second.
func TestReadUpTo_AsksTheContextBetweenChunks(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	r := &cancelOnRead{cancel: cancel}

	got, err := readUpTo(ctx, "doc", r, 0, 1<<40)

	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, got)
	assert.Equal(t, 1, r.reads, "the read stopped at the first ask after the cancel")
	assert.Equal(t, readChunk, r.asked, "and that one read asked for no more than a chunk")
}

// cancelOnRead serves every byte it is asked for, cancelling its context on the
// first read and recording how many bytes that read asked for.
type cancelOnRead struct {
	cancel       context.CancelFunc
	reads, asked int
}

func (r *cancelOnRead) Read(p []byte) (int, error) {
	r.cancel()
	r.reads++
	r.asked = len(p)
	return len(p), nil
}
