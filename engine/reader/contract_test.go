// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reader_test

import (
	"io/fs"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine/reader"
)

// TestReaders_AnswerAlikeThroughTheContract pins what Memory promises, that
// it reads as Disk would were each source a file at its Path: the same
// requests, made through compilers.DocumentReader, get the same bytes or the
// same sentinel from a Disk over real files and a Memory over the same paths,
// for a file, a file URI, a confined read, a URL, a limit and a missing file.
func TestReaders_AnswerAlikeThroughTheContract(t *testing.T) {
	t.Parallel()
	root := reader.ResolvedDir(t)
	inside := writeFile(t, root, "in/a.yaml", "alpha")
	outside := writeFile(t, root, "outside.yaml", "omega")
	srv, hits := countingServer(t, serveBody(t, http.StatusOK, "network"))
	t.Cleanup(func() { assert.Zero(t, hits.Load(), "no confined read reaches the server") })
	memory := newMemory(t, []compilers.Source{
		{Path: inside, Data: []byte("alpha")},
		{Path: outside, Data: []byte("omega")},
		{Path: srv.URL + "/doc.yaml", Data: []byte("network")},
	})
	readers := map[string]compilers.DocumentReader{"Disk": reader.Disk{}, "Memory": memory}
	dir := filepath.Dir(inside)
	tests := []struct {
		name   string
		req    compilers.DocumentRequest
		want   string
		wantIs error
	}{
		{name: "a file", req: compilers.DocumentRequest{Location: inside}, want: "alpha"},
		{name: "a file URI", req: compilers.DocumentRequest{Location: fileURI(inside)}, want: "alpha"},
		{name: "a file with dot segments", req: compilers.DocumentRequest{Location: dir + "/../outside.yaml"}, want: "omega"},
		{name: "a file inside", req: compilers.DocumentRequest{Location: inside, Within: dir}, want: "alpha"},
		{name: "a file outside", req: compilers.DocumentRequest{Location: outside, Within: dir},
			wantIs: compilers.ErrOutsideWithin},
		{name: "a file URI outside", req: compilers.DocumentRequest{Location: fileURI(outside), Within: dir},
			wantIs: compilers.ErrOutsideWithin},
		{name: "a path climbing out", req: compilers.DocumentRequest{Location: dir + "/../outside.yaml", Within: dir},
			wantIs: compilers.ErrOutsideWithin},
		{name: "a URL, confined", req: compilers.DocumentRequest{Location: srv.URL + "/doc.yaml", Within: dir},
			wantIs: compilers.ErrOutsideWithin},
		{name: "a limit met", req: compilers.DocumentRequest{Location: inside, Limit: 5}, want: "alpha"},
		{name: "a limit passed", req: compilers.DocumentRequest{Location: inside, Limit: 4},
			wantIs: compilers.ErrDocumentTooLarge},
		{name: "a missing file", req: compilers.DocumentRequest{Location: filepath.Join(dir, "missing.yaml")},
			wantIs: fs.ErrNotExist},
	}
	for name, r := range readers {
		for _, tc := range tests {
			t.Run(name+", "+tc.name, func(t *testing.T) {
				t.Parallel()

				got, err := r.ReadDocument(t.Context(), tc.req)

				if tc.wantIs != nil {
					require.ErrorIs(t, err, tc.wantIs)
					assert.Nil(t, got)
					return
				}
				require.NoError(t, err)
				assert.Equal(t, tc.want, string(got))
			})
		}
	}
}
