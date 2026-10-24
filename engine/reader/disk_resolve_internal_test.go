package reader

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
)

// TestReadRooted_RefusesALinkSwappedInAfterTheCheck pins what the os.Root open
// adds to resolveWithin's check: a file the check placed inside, replaced by a
// link out before the open, is still not read. Without it, a link swapped in
// between the two would lead the read outside the directory.
func TestReadRooted_RefusesALinkSwappedInAfterTheCheck(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("creating a symbolic link needs a privilege Windows grants few users")
	}
	top := t.TempDir()
	in := filepath.Join(top, "in")
	require.NoError(t, os.Mkdir(in, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(top, "secret.yaml"), []byte("secret"), 0o644))
	path := filepath.Join(in, "a.yaml")
	require.NoError(t, os.WriteFile(path, []byte("inside"), 0o644))

	dir, rel, err := resolveWithin(in, path)
	require.NoError(t, err, "the check places the file inside")
	require.NoError(t, os.Remove(path))
	require.NoError(t, os.Symlink(filepath.Join("..", "secret.yaml"), path))
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, root.Close()) })

	got, err := readRooted(root, rel, path, 0)

	require.Error(t, err)
	assert.Nil(t, got, "the link's target is never read")
}

// TestResolveWithin_RefusesAMixedPairWithNoWorkingDirectory pins a confined
// read of a relative directory and an absolute file where the working
// directory cannot be read: the two cannot be put alike, so the file is
// outside, as relativeTo answers for the same pair.
func TestResolveWithin_RefusesAMixedPairWithNoWorkingDirectory(t *testing.T) {
	root := ResolvedDir(t)
	t.Chdir(root)
	abs := filepath.Join(root, "d", "a.yaml")
	vanishWorkingDirectory(t)

	_, _, err := resolveWithin("d", abs)

	require.ErrorIs(t, err, compilers.ErrOutsideWithin)
}
