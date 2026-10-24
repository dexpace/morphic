package reader

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// ErrNotRegular exposes the error a read of a directory, FIFO, device or
// socket fails with, so the external tests assert on it rather than on its
// wording.
var ErrNotRegular = errNotRegular

// FilePathOf is filePathOf, for the external tests.
var FilePathOf = filePathOf

// ReadOpened is readOpened, for the external tests.
var ReadOpened = readOpened

// ResolvedDir returns a fresh temporary directory with its links resolved, so
// a path under it spells the directory the working directory reports where the
// system's temporary directory is itself a link.
func ResolvedDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}
