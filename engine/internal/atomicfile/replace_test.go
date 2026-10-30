// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package atomicfile_test

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine/internal/atomicfile"
)

// TestReplace_ReplacesAtomically pins the success path end to end: the
// destination gains the new bytes, keeps the mode it already had, and no
// temporary file is left beside it.
func TestReplace_ReplacesAtomically(t *testing.T) {
	t.Parallel()
	dest := newDest(t)
	seed(t, dest, "stale\n", 0o640)

	require.NoError(t, atomicfile.Replace(t.Context(), dest, writing("fresh\n")))

	assert.Equal(t, "fresh\n", contentOf(t, dest))
	assert.Equal(t, fs.FileMode(0o640), modeOf(t, dest),
		"replacing a file must preserve the mode it already carried")
	assert.Equal(t, []string{"ir.json"}, entriesIn(t, filepath.Dir(dest)),
		"no temporary file may survive a successful replace")
}

// processCreationMode is the mode a file requested at 0666 gets under this
// process's umask, found by creating one. Reading the umask directly would mean
// setting it, which is process-wide and would race the parallel tests.
func processCreationMode(t *testing.T) fs.FileMode {
	t.Helper()
	probe := filepath.Join(t.TempDir(), "probe")
	f, err := os.OpenFile(probe, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return modeOf(t, probe)
}

// TestReplace_NewFileTakesTheUmaskNarrowedMode pins that a destination which did
// not exist is created the way os.Create would create it: 0666 narrowed by the
// umask, which the temporary file holds while it is filled too. A replace that
// forced some other mode would make compile's -o file differ from what every
// other tool writes.
func TestReplace_NewFileTakesTheUmaskNarrowedMode(t *testing.T) {
	t.Parallel()
	dest := newDest(t)
	var filling fs.FileMode
	fill := func(w io.Writer) error {
		filling = modeOf(t, tempOf(t, dest))
		return writing("new\n")(w)
	}

	require.NoError(t, atomicfile.Replace(t.Context(), dest, fill))

	assert.Equal(t, "new\n", contentOf(t, dest))
	assert.Equal(t, processCreationMode(t), filling)
	assert.Equal(t, processCreationMode(t), modeOf(t, dest))
}

// TestReplace_ContentIsNeverWiderThanTheDestination pins that the new content
// is readable by no one the destination keeps it from, for as long as it sits
// in the temporary file: the file holds the destination's exact mode before
// the fill writes anything, a mode the umask would narrow included. Made at
// 0666 and set only before the rename, a private file's next content was open
// to every user on the machine meanwhile.
func TestReplace_ContentIsNeverWiderThanTheDestination(t *testing.T) {
	t.Parallel()
	for _, mode := range []fs.FileMode{0o400, 0o600, 0o640, 0o644, 0o666} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			dest := newDest(t)
			seed(t, dest, "old\n", mode)
			var filling fs.FileMode
			fill := func(w io.Writer) error {
				filling = modeOf(t, tempOf(t, dest))
				return writing("new\n")(w)
			}

			require.NoError(t, atomicfile.Replace(t.Context(), dest, fill))

			assert.Equal(t, mode, filling, "holding the destination's mode exactly while it is filled")
			assert.Equal(t, mode, modeOf(t, dest))
		})
	}
}

// TestReplace_TempFileIsCreatedBesideTheDestination pins where the temporary
// file goes: the destination's own directory, so the rename stays on one
// filesystem, never a temporary directory elsewhere, under a dot-prefixed name
// carrying the destination's base and a random number.
func TestReplace_TempFileIsCreatedBesideTheDestination(t *testing.T) {
	t.Parallel()
	dest := newDest(t)
	var temp string
	fill := func(w io.Writer) error {
		temp = tempOf(t, dest)
		return writing("fresh\n")(w)
	}

	require.NoError(t, atomicfile.Replace(t.Context(), dest, fill))

	assert.Equal(t, filepath.Dir(dest), filepath.Dir(temp))
	assert.Regexp(t, `^\.ir\.json[0-9]+$`, filepath.Base(temp))
}

// TestReplace_ReplacedSymlinkTakesItsTargetsMode pins that the mode kept is the
// mode of what the path resolves to. A link's own mode bits are not its
// target's, so reading the link instead would give the replacement file the
// umask's mode, which truncating the target in place would never have done.
func TestReplace_ReplacedSymlinkTakesItsTargetsMode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target, link := filepath.Join(dir, "target.json"), filepath.Join(dir, "link.json")
	seed(t, target, "PREVIOUS\n", 0o600)
	require.NoError(t, os.Symlink(target, link))

	require.NoError(t, atomicfile.Replace(t.Context(), link, writing("fresh\n")))

	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.Zero(t, info.Mode()&os.ModeSymlink, "the link is replaced by a regular file")
	assert.Equal(t, fs.FileMode(0o600), info.Mode().Perm())
}

// TestReplace_StatError pins that a destination that cannot be examined stops
// the replace before any file is created. A regular file used as a parent
// directory makes Stat fail with ENOTDIR, an error that is not "does not exist".
func TestReplace_StatError(t *testing.T) {
	t.Parallel()
	notDir := newDest(t)
	seed(t, notDir, "x", 0o644)
	dest := filepath.Join(notDir, "ir.json")

	err := atomicfile.Replace(t.Context(), dest, writing("fresh\n"))

	require.Error(t, err)
	assert.ErrorContains(t, err, fmt.Sprintf("stat output %q", dest))
	assert.Equal(t, []string{"ir.json"}, entriesIn(t, filepath.Dir(notDir)), "nothing is created")
}

// The next four tests pin what replacing by rename gives up. None is a bug:
// each is a way to reach the destination's bytes other than through its own
// name, or a name the temporary file cannot take, and honouring any of them
// means writing in place, the truncation Replace exists to remove. They are
// pinned so that changing one changes a stated contract instead of drifting.

// TestReplace_ReadOnlyDirFails pins that replacing needs permission on the
// destination's directory, not merely on the destination. Truncating a writable
// file in a read-only directory succeeds; renaming a new one over it cannot.
func TestReplace_ReadOnlyDirFails(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission checks, so the replace would succeed")
	}
	dest := newDest(t)
	dir := filepath.Dir(dest)
	seed(t, dest, "PREVIOUS\n", 0o644)
	require.NoError(t, os.Chmod(dir, 0o555))
	// Restore write permission so t.TempDir's own cleanup can remove the tree.
	t.Cleanup(func() { assert.NoError(t, os.Chmod(dir, 0o755)) })

	err := atomicfile.Replace(t.Context(), dest, writing("fresh\n"))

	require.ErrorIs(t, err, fs.ErrPermission)
	assert.ErrorContains(t, err, "create output")
	assert.Equal(t, "PREVIOUS\n", contentOf(t, dest),
		"a destination that cannot be replaced keeps its previous content")
}

// TestReplace_SymlinkIsReplaced pins that a symlink destination is replaced by
// a regular file rather than followed and written through, so its target keeps
// its old content.
func TestReplace_SymlinkIsReplaced(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target, link := filepath.Join(dir, "target.json"), filepath.Join(dir, "link.json")
	seed(t, target, "PREVIOUS\n", 0o644)
	require.NoError(t, os.Symlink(target, link))

	require.NoError(t, atomicfile.Replace(t.Context(), link, writing("fresh\n")))

	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.Zero(t, info.Mode()&os.ModeSymlink, "the symlink must be replaced, not followed")
	assert.Equal(t, "fresh\n", contentOf(t, link))
	assert.Equal(t, "PREVIOUS\n", contentOf(t, target), "the symlink's former target stays as it was")
}

// TestReplace_HardLinkIsBroken pins that another hard link to the destination
// keeps pointing at the old inode, and so keeps the old content, instead of
// observing the new bytes.
func TestReplace_HardLinkIsBroken(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dest, other := filepath.Join(dir, "a.json"), filepath.Join(dir, "b.json")
	seed(t, dest, "PREVIOUS\n", 0o644)
	require.NoError(t, os.Link(dest, other))

	require.NoError(t, atomicfile.Replace(t.Context(), dest, writing("fresh\n")))

	assert.Equal(t, "fresh\n", contentOf(t, dest))
	assert.Equal(t, "PREVIOUS\n", contentOf(t, other), "the other link keeps the old inode's content")
}

// TestReplace_NearLimitNameFails pins the one limitation that is a new failure
// rather than a lost capability: the temporary name is the base behind a dot
// and before a random number of up to 19 digits. A base 20 short of the
// filesystem's limit always fits; one 1 short never does, though writing that
// name in place succeeds.
func TestReplace_NearLimitNameFails(t *testing.T) {
	t.Parallel()
	// nameMax is the POSIX floor every filesystem Morphic targets meets or
	// exceeds; probing the real limit would tie the test to the filesystem
	// under the temporary directory.
	const nameMax, maxOverhead = 255, 20
	tests := []struct {
		name    string
		baseLen int
		wantErr bool
	}{
		{"the longest base every temporary name fits", nameMax - maxOverhead, false},
		{"a base no temporary name fits", nameMax - 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dest := filepath.Join(t.TempDir(), strings.Repeat("a", tt.baseLen))
			seed(t, dest, "PREVIOUS\n", 0o644) // precondition: this name is writable in place

			err := atomicfile.Replace(t.Context(), dest, writing("fresh\n"))

			if !tt.wantErr {
				require.NoError(t, err)
				assert.Equal(t, "fresh\n", contentOf(t, dest))
				return
			}
			require.Error(t, err, "the temporary name must not fit where the destination does")
			assert.ErrorContains(t, err, "create output")
			assert.Equal(t, "PREVIOUS\n", contentOf(t, dest),
				"the destination keeps its previous content when the temporary name will not fit")
		})
	}
}
