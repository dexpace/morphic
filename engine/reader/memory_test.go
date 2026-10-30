// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reader_test

import (
	"context"
	"fmt"
	"io/fs"
	"math"
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine/reader"
)

// memorySources returns the sources the Memory tests serve: relative and
// absolute paths, a path spelled with dot segments, a file URI, a URL, and a
// source with no Path, which nothing can name.
func memorySources() []compilers.Source {
	return []compilers.Source{
		{Path: "specs/a.yaml", Data: []byte("alpha")},
		{Path: "/abs/b.yaml", Data: []byte("bravo")},
		{Path: "https://example.com/c.yaml", Data: []byte("charlie")},
		{Path: "https://example.com/d e.yaml", Data: []byte("delta")},
		{Path: "x/./y.yaml", Data: []byte("yankee")},
		{Path: "file:///abs/e.yaml", Data: []byte("echo")},
		{Path: "", Data: []byte("nameless")},
	}
}

// newMemory returns a Memory serving sources, failing the test where it
// cannot.
func newMemory(t *testing.T, sources []compilers.Source) *reader.Memory {
	t.Helper()
	m, err := reader.NewMemory(sources)
	require.NoError(t, err)
	require.NotNil(t, m)
	return m
}

// TestMemory_ReadDocumentFindsASourceByTheDocumentItNames pins how a request
// finds a source: by path with its dot segments removed, by the file URI
// naming that path, or by the URL as a request spells it, whatever case its
// scheme is in and whether what it escapes is written escaped. Identity is
// otherwise lexical: a relative and an absolute path, or a URL whose host is
// in other capitals, are other documents, and a document no source holds is
// fs.ErrNotExist.
func TestMemory_ReadDocumentFindsASourceByTheDocumentItNames(t *testing.T) {
	t.Parallel()
	m := newMemory(t, memorySources())
	tests := []struct {
		name, location string
		want           string
		wantIs         error
		fails          bool
	}{
		{name: "a path as spelled", location: "specs/a.yaml", want: "alpha"},
		{name: "a path with dot segments", location: "specs/./a.yaml", want: "alpha"},
		{name: "a path climbing out and back", location: "specs/sub/../a.yaml", want: "alpha"},
		{name: "a source spelled with dot segments", location: "x/y.yaml", want: "yankee"},
		{name: "a source spelled with dot segments, as spelled", location: "x/./y.yaml", want: "yankee"},
		{name: "an absolute path", location: "/abs/b.yaml", want: "bravo"},
		{name: "a file URI naming the path", location: "file:///abs/b.yaml", want: "bravo"},
		{name: "a file URI at localhost", location: "file://localhost/abs/b.yaml", want: "bravo"},
		{name: "a file URI in capitals", location: "FILE:///abs/b.yaml", want: "bravo"},
		{name: "a source added as a file URI, by path", location: "/abs/e.yaml", want: "echo"},
		{name: "a URL as written", location: "https://example.com/c.yaml", want: "charlie"},
		{name: "a URL whose scheme is in capitals", location: "HTTPS://example.com/c.yaml", want: "charlie"},
		{name: "a URL whose host is in capitals", location: "https://EXAMPLE.com/c.yaml", wantIs: fs.ErrNotExist},
		{name: "a URL as its source spells it", location: "https://example.com/d e.yaml", want: "delta"},
		{name: "a URL as a request spells it", location: "https://example.com/d%20e.yaml", want: "delta"},
		{name: "a relative path for an absolute source", location: "abs/b.yaml", wantIs: fs.ErrNotExist},
		{name: "an absolute path for a relative source", location: "/specs/a.yaml", wantIs: fs.ErrNotExist},
		{name: "a path no source holds", location: "missing.yaml", wantIs: fs.ErrNotExist},
		{name: "the directory a source lies in", location: "specs", wantIs: fs.ErrNotExist},
		{name: "an empty location, which no source answers to", location: "", fails: true},
		{name: "a file URI naming another host", location: "file://server/abs/b.yaml", fails: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := m.ReadDocument(t.Context(), compilers.DocumentRequest{Location: tc.location})

			if tc.wantIs == nil && !tc.fails {
				require.NoError(t, err)
				assert.Equal(t, tc.want, string(got))
				return
			}
			require.Error(t, err)
			assert.Nil(t, got)
			if tc.wantIs != nil {
				require.ErrorIs(t, err, tc.wantIs)
				assert.Contains(t, err.Error(), tc.location, "the document asked for")
				return
			}
			assert.NotErrorIs(t, err, fs.ErrNotExist, "refused, not missing")
		})
	}
}

// TestMemory_ReadDocumentConfined pins a confined request, judged lexically
// since memory holds no link to follow: inside is read, anything the lexical
// test puts outside is ErrOutsideWithin though a source holds it, a URL is
// outside every directory, and a document inside that no source holds is
// missing, not outside.
func TestMemory_ReadDocumentConfined(t *testing.T) {
	t.Parallel()
	m := newMemory(t, memorySources())
	tests := []struct {
		name, within, location string
		want                   string
		wantIs                 error
	}{
		{name: "a path inside", within: "specs", location: "specs/a.yaml", want: "alpha"},
		{name: "a directory spelled with a trailing slash", within: "specs/", location: "specs/./a.yaml", want: "alpha"},
		{name: "a source spelled with dot segments", within: "x", location: "x/y.yaml", want: "yankee"},
		{name: "an absolute path inside", within: "/abs", location: "/abs/b.yaml", want: "bravo"},
		{name: "a file URI inside", within: "/abs", location: "file:///abs/b.yaml", want: "bravo"},
		{name: "a path climbing out", within: "specs", location: "specs/../x/y.yaml", wantIs: compilers.ErrOutsideWithin},
		{name: "a sibling sharing the name prefix", within: "spec", location: "specs/a.yaml",
			wantIs: compilers.ErrOutsideWithin},
		{name: "an absolute path for a relative directory", within: "specs", location: "/abs/b.yaml",
			wantIs: compilers.ErrOutsideWithin},
		{name: "a relative path for an absolute directory", within: "/abs", location: "specs/a.yaml",
			wantIs: compilers.ErrOutsideWithin},
		{name: "an absolute path elsewhere", within: "/abs", location: "/specs/a.yaml", wantIs: compilers.ErrOutsideWithin},
		{name: "a file URI elsewhere", within: "/abs", location: "file:///other/b.yaml", wantIs: compilers.ErrOutsideWithin},
		{name: "a URL a source holds", within: "specs", location: "https://example.com/c.yaml",
			wantIs: compilers.ErrOutsideWithin},
		{name: "a missing path inside", within: "specs", location: "specs/missing.yaml", wantIs: fs.ErrNotExist},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := m.ReadDocument(t.Context(),
				compilers.DocumentRequest{Location: tc.location, Within: tc.within})

			if tc.wantIs == nil {
				require.NoError(t, err)
				assert.Equal(t, tc.want, string(got))
				return
			}
			require.ErrorIs(t, err, tc.wantIs)
			assert.Nil(t, got)
		})
	}
}

// TestMemory_ReadDocumentHoldsTheLimit pins the size limit as Disk holds it:
// exactly the limit is read, one byte more fails with ErrDocumentTooLarge
// naming the limit, and zero, a negative limit and math.MaxInt read the
// document whole.
func TestMemory_ReadDocumentHoldsTheLimit(t *testing.T) {
	t.Parallel()
	m := newMemory(t, memorySources())
	tests := []struct {
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
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := m.ReadDocument(t.Context(),
				compilers.DocumentRequest{Location: "specs/a.yaml", Limit: tc.limit})

			if tc.over {
				require.ErrorIs(t, err, compilers.ErrDocumentTooLarge)
				assert.Contains(t, err.Error(), fmt.Sprintf("%d bytes", tc.limit))
				assert.Nil(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "alpha", string(got))
		})
	}
}

// TestMemory_ReadDocumentHandsBackACopy pins that every read is handed bytes
// of its own: a caller that overwrites what it got changes neither the next
// read nor the source's own bytes.
func TestMemory_ReadDocumentHandsBackACopy(t *testing.T) {
	t.Parallel()
	sources := memorySources()
	m := newMemory(t, sources)
	req := compilers.DocumentRequest{Location: "specs/a.yaml"}
	first, err := m.ReadDocument(t.Context(), req)
	require.NoError(t, err)

	first[0] = 'X'

	second, err := m.ReadDocument(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, "alpha", string(second), "the next read")
	assert.Equal(t, "alpha", string(sources[0].Data), "the source's own bytes")
}

// TestMemory_ReadDocumentWithACancelledContext pins that a cancelled context
// fails the read before the document is looked up, one a source holds
// included.
func TestMemory_ReadDocumentWithACancelledContext(t *testing.T) {
	t.Parallel()
	m := newMemory(t, memorySources())

	got, err := m.ReadDocument(cancelled(t), compilers.DocumentRequest{Location: "specs/a.yaml"})

	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, got)
}

// TestNewMemory_RefusesSourcesNamingOneDocument pins that two sources whose
// Paths name one document are refused, naming the second, however the Paths
// are spelled: with dot segments, as a path and its file URI, as one URL
// twice, or as one URL in two spellings a request reads alike. Paths that name
// two documents, and sources with no Path, are not duplicates.
func TestNewMemory_RefusesSourcesNamingOneDocument(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		paths []string
		names string
	}{
		{name: "dot segments", paths: []string{"a/./b.yaml", "a/b.yaml"}, names: "a/b.yaml"},
		{name: "a path and its file URI", paths: []string{"/abs/b.yaml", "file:///abs/b.yaml"},
			names: "file:///abs/b.yaml"},
		{name: "a URL twice", paths: []string{"https://h/a.yaml", "x.yaml", "https://h/a.yaml"},
			names: "https://h/a.yaml"},
		{name: "a relative and an absolute path", paths: []string{"a.yaml", "/a.yaml"}},
		{name: "a URL whose scheme is in other capitals", paths: []string{"https://h/a", "HTTPS://h/a"},
			names: "HTTPS://h/a"},
		{name: "a URL escaped and not", paths: []string{"https://h/a b", "https://h/a%20b"}, names: "https://h/a%20b"},
		{name: "a URL whose host is in other capitals", paths: []string{"https://h/a", "https://H/a"}},
		{name: "two sources with no Path", paths: []string{"", "", "a.yaml"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sources := make([]compilers.Source, len(tc.paths))
			for i, p := range tc.paths {
				sources[i] = compilers.Source{Path: p, Data: []byte(p)}
			}

			m, err := reader.NewMemory(sources)

			if tc.names == "" {
				require.NoError(t, err)
				assert.NotNil(t, m)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.names, "the Path of the second source")
			assert.Nil(t, m)
		})
	}
}

// TestNewMemory_RefusesAFileURINamingNoLocalFile pins that a source whose Path
// is a file URI no reader could read as a file is refused, naming that Path,
// not served under a name nothing can ask for.
func TestNewMemory_RefusesAFileURINamingNoLocalFile(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"file://server/share/a.yaml", "file:a.yaml", "file:///a.yaml?v=1"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			m, err := reader.NewMemory([]compilers.Source{{Path: "ok.yaml"}, {Path: path}})

			require.Error(t, err)
			assert.Contains(t, err.Error(), path)
			assert.Nil(t, m)
		})
	}
}

// TestNewMemory_ServesNothingFromNoSources pins that no sources make a Memory
// every request of which is a document that does not exist.
func TestNewMemory_ServesNothingFromNoSources(t *testing.T) {
	t.Parallel()
	m := newMemory(t, nil)

	got, err := m.ReadDocument(t.Context(), compilers.DocumentRequest{Location: "a.yaml"})

	require.ErrorIs(t, err, fs.ErrNotExist)
	assert.Nil(t, got)
}

// TestMemory_ReadsAFileURIAsTheHostSystemDoes pins that a Memory reads file
// URIs by the rules of the system it runs on, as Disk does: where a backslash
// is an ordinary name character, a source and a request may each spell one as
// %5C, and a request by path finds it.
func TestMemory_ReadsAFileURIAsTheHostSystemDoes(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("a backslash is a separator here, so no file URI may carry one")
	}
	m := newMemory(t, []compilers.Source{{Path: "file:///abs/c%5Cd.yaml", Data: []byte("slashed")}})
	for _, location := range []string{`/abs/c\d.yaml`, "file:///abs/c%5Cd.yaml"} {
		t.Run(location, func(t *testing.T) {
			t.Parallel()

			got, err := m.ReadDocument(t.Context(), compilers.DocumentRequest{Location: location})

			require.NoError(t, err)
			assert.Equal(t, "slashed", string(got))
		})
	}
}

// TestMemory_ConcurrentReadsAreSafe reads one Memory from several goroutines
// at once, each overwriting the bytes it is handed, which is a race under
// -race unless every read has bytes of its own, and requires every read to
// return the document.
func TestMemory_ConcurrentReadsAreSafe(t *testing.T) {
	t.Parallel()
	const workers, rounds = 8, 50
	m := newMemory(t, memorySources())
	want := map[string]string{"specs/a.yaml": "alpha", "/abs/b.yaml": "bravo", "https://example.com/c.yaml": "charlie"}
	var wg sync.WaitGroup

	for range workers {
		wg.Go(func() {
			for range rounds {
				for location, doc := range want {
					got, err := m.ReadDocument(t.Context(), compilers.DocumentRequest{Location: location})
					if !assert.NoError(t, err) {
						return
					}
					assert.Equal(t, doc, string(got))
					got[0] = 'X'
				}
			}
		})
	}
	wg.Wait()
}
