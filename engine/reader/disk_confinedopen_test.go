// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reader_test

import (
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine/reader"
)

// TestDisk_ReadDocumentConfinedOpenFailureNamesTheRequestedPath pins that a
// confined read whose file cannot be opened fails as os.Open would: with an
// *fs.PathError whose Op is "open" and whose Path is the path the request
// named, wrapping the cause. os.Root names the file relative to the directory,
// which means nothing to a caller that never spelled it so.
func TestDisk_ReadDocumentConfinedOpenFailureNamesTheRequestedPath(t *testing.T) {
	t.Parallel()
	root := diskTree(t)
	in := filepath.Join(root, "in")
	tests := []struct {
		name, location string
		wantIs         error
	}{
		{name: "a missing file", location: filepath.Join(in, "missing.yaml"), wantIs: fs.ErrNotExist},
		{name: "a missing file in a directory", location: filepath.Join(in, "sub", "missing.yaml"), wantIs: fs.ErrNotExist},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := reader.Disk{}.ReadDocument(t.Context(),
				compilers.DocumentRequest{Location: tc.location, Within: in})

			require.Error(t, err)
			assert.Nil(t, got)
			if tc.wantIs != nil {
				assert.ErrorIs(t, err, tc.wantIs)
			}
			var pathErr *fs.PathError
			require.ErrorAs(t, err, &pathErr)
			assert.Equal(t, "open", pathErr.Op)
			assert.Equal(t, tc.location, pathErr.Path, "the path the request named, not the one inside the directory")
			assert.Contains(t, err.Error(), tc.location)
		})
	}
}
