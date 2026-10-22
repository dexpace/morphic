package atomicfile

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/renameio/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReplace_AChmodFailureLeavesNothingBehind pins a filesystem refusing the
// destination's mode, as some network and FAT mounts do: the replace fails
// naming the destination and the cause, removes the temporary file it made,
// runs no fill, and leaves the destination as it was. A destination that does
// not exist yet is never chmod-ed.
func TestReplace_AChmodFailureLeavesNothingBehind(t *testing.T) {
	t.Parallel()
	refused := errors.New("chmod refused")
	failing := func(*renameio.PendingFile, fs.FileMode) error { return refused }
	filled := false
	fill := func(w io.Writer) error {
		filled = true
		_, err := w.Write([]byte("new\n"))
		return err
	}
	dir := t.TempDir()
	dest := filepath.Join(dir, "ir.json")
	require.NoError(t, os.WriteFile(dest, []byte("old\n"), 0o600))

	err := replace(dest, fill, failing)

	require.ErrorIs(t, err, refused)
	assert.ErrorContains(t, err, fmt.Sprintf("chmod output %q", dest))
	assert.False(t, filled, "nothing is filled into a file whose mode is wrong")
	entries, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	require.Len(t, entries, 1, "the temporary file is removed")
	raw, readErr := os.ReadFile(dest)
	require.NoError(t, readErr)
	assert.Equal(t, "old\n", string(raw))

	fresh := filepath.Join(t.TempDir(), "ir.json")
	require.NoError(t, replace(fresh, fill, failing), "a new destination is not chmod-ed")
}
