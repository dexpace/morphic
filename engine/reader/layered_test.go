// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reader_test

import (
	"context"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine/reader"
)

// recordingReader answers every request with "under" and records it, standing
// for the reader beneath a Layered. Each test reads through its own, in turn.
type recordingReader struct {
	reqs []compilers.DocumentRequest
}

func (r *recordingReader) ReadDocument(_ context.Context, req compilers.DocumentRequest) ([]byte, error) {
	r.reqs = append(r.reqs, req)
	return []byte("under"), nil
}

// TestLayered_ReadsWhatHeldHoldsAndTheRestFromDisk pins the default Under: a
// document Held holds is served from memory though a file of other content
// sits at its path, named as Memory names it, with or without dot segments or
// as a file URI, while one it does not hold is read from disk, and one neither
// holds is missing as Disk reports it.
func TestLayered_ReadsWhatHeldHoldsAndTheRestFromDisk(t *testing.T) {
	t.Parallel()
	root := reader.ResolvedDir(t)
	spec := writeFile(t, root, "specs/spec.yaml", "on disk")
	writeFile(t, root, "specs/common.yaml", "common")
	l := reader.Layered{Held: newMemory(t, []compilers.Source{{Path: spec, Data: []byte("held")}})}
	tests := []struct {
		name, location string
		want           string
	}{
		{"the held path", spec, "held"},
		{"the held path with dot segments", filepath.Join(root, "specs", ".", "spec.yaml"), "held"},
		{"the held path as a file URI", fileURI(spec), "held"},
		{"a document beside it", filepath.Join(root, "specs", "common.yaml"), "common"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := l.ReadDocument(t.Context(), compilers.DocumentRequest{Location: tc.location})

			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}

	got, err := l.ReadDocument(t.Context(), compilers.DocumentRequest{Location: filepath.Join(root, "missing.yaml")})
	require.ErrorIs(t, err, fs.ErrNotExist)
	assert.Nil(t, got)
}

// TestLayered_EachRequestIsJudgedByTheReaderServingIt pins that holding is
// decided before confinement and limit: a held document is confined and
// limited as Memory judges it, and Under never sees the request, while a
// document Held does not hold reaches Under as it was made, confinement and
// limit included, though Memory's lexical test would refuse it.
func TestLayered_EachRequestIsJudgedByTheReaderServingIt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		req       compilers.DocumentRequest
		want      string
		wantIs    error
		wantUnder bool
	}{
		{name: "a held document inside", req: compilers.DocumentRequest{Location: "specs/a.yaml", Within: "specs"},
			want: "alpha"},
		{name: "a held document outside", req: compilers.DocumentRequest{Location: "specs/a.yaml", Within: "other"},
			wantIs: compilers.ErrOutsideWithin},
		{name: "a held document past the limit", req: compilers.DocumentRequest{Location: "specs/a.yaml", Limit: 2},
			wantIs: compilers.ErrDocumentTooLarge},
		{name: "another document outside", req: compilers.DocumentRequest{Location: "other/b.yaml", Within: "specs"},
			want: "under", wantUnder: true},
		{name: "another document past the limit", req: compilers.DocumentRequest{Location: "other/b.yaml", Limit: 2},
			want: "under", wantUnder: true},
		{name: "a URL held nowhere", req: compilers.DocumentRequest{Location: "https://example.com/c.yaml"},
			want: "under", wantUnder: true},
		{name: "a location no source can be named by", req: compilers.DocumentRequest{Location: "file:a.yaml"},
			want: "under", wantUnder: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			under := &recordingReader{}
			l := reader.Layered{Held: newMemory(t, memorySources()[:1]), Under: under}

			got, err := l.ReadDocument(t.Context(), tc.req)

			if tc.wantIs != nil {
				require.ErrorIs(t, err, tc.wantIs)
				assert.Nil(t, got)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.want, string(got))
			}
			var want []compilers.DocumentRequest
			if tc.wantUnder {
				want = []compilers.DocumentRequest{tc.req}
			}
			assert.Equal(t, want, under.reqs)
		})
	}
}

// TestLayered_NilHeldHoldsNothing pins the zero Held: every request reaches
// Under, a request for a path no source could have named among them.
func TestLayered_NilHeldHoldsNothing(t *testing.T) {
	t.Parallel()
	under := &recordingReader{}
	l := reader.Layered{Under: under}

	for _, location := range []string{"specs/a.yaml", "file:a.yaml", ""} {
		got, err := l.ReadDocument(t.Context(), compilers.DocumentRequest{Location: location})
		require.NoError(t, err, location)
		assert.Equal(t, "under", string(got), location)
	}
	assert.Len(t, under.reqs, 3)
}

// TestLayered_ACancelledContextStopsEitherRead pins that a cancelled context
// stops a read whichever reader would serve it, so holding the spec does not
// let its read outlive the run.
func TestLayered_ACancelledContextStopsEitherRead(t *testing.T) {
	t.Parallel()
	root := reader.ResolvedDir(t)
	beside := writeFile(t, root, "beside.yaml", "beside")
	l := reader.Layered{Held: newMemory(t, memorySources()[:1])}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	for _, location := range []string{"specs/a.yaml", beside} {
		got, err := l.ReadDocument(ctx, compilers.DocumentRequest{Location: location})
		require.ErrorIs(t, err, context.Canceled, location)
		assert.Nil(t, got, location)
	}
}
