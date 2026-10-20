package atomicfile_test

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine/internal/atomicfile"
)

// TestReplace_ReplacesAtomically pins the success path end to end through the
// zero Replacer, so every operating-system default is exercised at once: the
// destination gains the new bytes, keeps the mode it already had, and no
// temporary file is left beside it.
func TestReplace_ReplacesAtomically(t *testing.T) {
	t.Parallel()
	dest := newDest(t)
	seed(t, dest, "stale\n", 0o640)

	require.NoError(t, atomicfile.Replacer{}.Replace(dest, writing("fresh\n")))

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
// umask. A replace that forced some other mode would make compile's -o file
// differ from what every other tool writes.
func TestReplace_NewFileTakesTheUmaskNarrowedMode(t *testing.T) {
	t.Parallel()
	dest := newDest(t)

	require.NoError(t, atomicfile.Replacer{}.Replace(dest, writing("new\n")))

	assert.Equal(t, "new\n", contentOf(t, dest))
	assert.Equal(t, processCreationMode(t), modeOf(t, dest))
}

// TestReplace_ReplacedSymlinkTakesItsTargetsMode pins that the mode kept is the
// mode of what the path resolves to. A link's own mode bits are not its
// target's, so reading the link instead would give the replacement file the
// link's, which truncating the target in place would never have done.
func TestReplace_ReplacedSymlinkTakesItsTargetsMode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target, link := filepath.Join(dir, "target.json"), filepath.Join(dir, "link.json")
	seed(t, target, "PREVIOUS\n", 0o600)
	require.NoError(t, os.Symlink(target, link))

	require.NoError(t, atomicfile.Replacer{}.Replace(link, writing("fresh\n")))

	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.Zero(t, info.Mode()&os.ModeSymlink, "the link is replaced by a regular file")
	assert.Equal(t, fs.FileMode(0o600), info.Mode().Perm())
}

// The next three tests pin what replacing by rename gives up. None is a bug:
// each is a way to reach the destination's bytes other than through its own
// name, and honouring any of them means writing in place, the truncation Replace
// exists to remove. They are pinned so that changing one changes a stated
// contract instead of drifting.

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

	err := atomicfile.Replacer{}.Replace(dest, writing("fresh\n"))

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

	require.NoError(t, atomicfile.Replacer{}.Replace(link, writing("fresh\n")))

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

	require.NoError(t, atomicfile.Replacer{}.Replace(dest, writing("fresh\n")))

	assert.Equal(t, "fresh\n", contentOf(t, dest))
	assert.Equal(t, "PREVIOUS\n", contentOf(t, other), "the other link keeps the old inode's content")
}

// TestReplace_NearLimitNameFails pins the one limitation that is a new failure
// rather than a lost capability: the temporary name is 21 characters longer than
// the destination's, so a basename within 21 of the filesystem's limit cannot be
// replaced. The longest base that fits succeeds, which fixes the overhead at
// exactly 21; the next one fails although writing that name in place succeeds.
func TestReplace_NearLimitNameFails(t *testing.T) {
	t.Parallel()
	// nameMax is the POSIX floor every filesystem Morphic targets meets or
	// exceeds; probing the real limit would tie the test to the filesystem
	// under the temporary directory.
	const nameMax, tempOverhead = 255, 21
	tests := []struct {
		name    string
		baseLen int
		wantErr bool
	}{
		{"longest base whose temporary name fits", nameMax - tempOverhead, false},
		{"one character longer", nameMax - tempOverhead + 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dest := filepath.Join(t.TempDir(), strings.Repeat("a", tt.baseLen))
			seed(t, dest, "PREVIOUS\n", 0o644) // precondition: this name is writable in place

			err := atomicfile.Replacer{}.Replace(dest, writing("fresh\n"))

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

// TestReplace_StatError pins that a destination that cannot be examined stops
// the replace before any file is created. A regular file used as a parent
// directory makes Stat fail with ENOTDIR, an error that is not "does not exist".
func TestReplace_StatError(t *testing.T) {
	t.Parallel()
	notDir := newDest(t)
	seed(t, notDir, "x", 0o644)
	dest := filepath.Join(notDir, "ir.json")
	creates := 0
	r := atomicfile.Replacer{Create: func(name string, perm fs.FileMode) (atomicfile.File, error) {
		creates++
		return createExclusive(name, perm)
	}}

	err := r.Replace(dest, writing("fresh\n"))

	require.Error(t, err)
	assert.ErrorContains(t, err, fmt.Sprintf("stat output %q", dest))
	assert.Zero(t, creates, "a failed stat must stop the replace before it creates a file")
}

// TestReplace_TempFileIsCreatedBesideTheDestination pins the temporary name's
// shape and mode: the same directory, so the rename stays on one filesystem, a
// dot-prefixed name carrying the destination's base and the 16-digit suffix, and
// the 0666 os.Create requests so the umask narrows it identically.
func TestReplace_TempFileIsCreatedBesideTheDestination(t *testing.T) {
	t.Parallel()
	dest := newDest(t)
	var gotName string
	var gotPerm fs.FileMode
	r := atomicfile.Replacer{
		Create: func(name string, perm fs.FileMode) (atomicfile.File, error) {
			gotName, gotPerm = name, perm
			return createExclusive(name, perm)
		},
		Suffix: func() uint64 { return 0xab },
	}

	require.NoError(t, r.Replace(dest, writing("fresh\n")))

	assert.Equal(t, tempName(dest, 0xab), gotName)
	assert.Equal(t, ".ir.json.tmp00000000000000ab", filepath.Base(gotName))
	assert.Equal(t, fs.FileMode(0o666), gotPerm)
}

// TestReplace_StepsRunInOrder pins the sequence that makes the rename safe to
// trust: the bytes are written, flushed with Sync and closed before anything
// publishes them, and a destination that already exists has its mode applied to
// the temporary file before it holds a byte.
func TestReplace_StepsRunInOrder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		existing bool
		want     []string
	}{
		{"new file", false, []string{"write", "sync", "close", "rename"}},
		{"replacing a file", true, []string{"chmod", "write", "sync", "close", "rename"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dest := newDest(t)
			if tt.existing {
				seed(t, dest, "old\n", 0o640)
			}
			tr := &trace{}
			f := &flakyFile{trace: tr}
			r := atomicfile.Replacer{
				Create: f.create,
				Rename: func(from, to string) error { tr.add("rename"); return os.Rename(from, to) },
			}

			require.NoError(t, r.Replace(dest, writing("new\n")))

			assert.Equal(t, tt.want, tr.steps)
			assert.Equal(t, "new\n", contentOf(t, dest))
		})
	}
}

// TestReplace_ChmodOnlyWhenReplacing pins when the mode is set: a replaced
// destination's mode goes onto the temporary file, through the open file, and
// a new file is left with the mode it was created with. Chmod-ing a new file
// would override the umask with a mode nobody asked for.
func TestReplace_ChmodOnlyWhenReplacing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		existing bool
		want     []fs.FileMode
	}{
		{"new file", false, nil},
		{"replacing a 0640 file", true, []fs.FileMode{0o640}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dest := newDest(t)
			if tt.existing {
				seed(t, dest, "old\n", 0o640)
			}
			f := &flakyFile{}
			r := atomicfile.Replacer{Create: f.create}

			require.NoError(t, r.Replace(dest, writing("new\n")))

			assert.Equal(t, tt.want, f.chmods)
		})
	}
}

// TestReplace_ContentIsNeverWiderThanTheDestination pins that the new content
// is readable by no one the destination keeps it from, for as long as it sits
// in the temporary file: created with the destination's mode, which the umask
// can only narrow, and set to it exactly before the fill writes anything. Made
// at 0666 and set only before the rename, a private file's next content was
// open to every user on the machine meanwhile.
func TestReplace_ContentIsNeverWiderThanTheDestination(t *testing.T) {
	t.Parallel()
	for _, mode := range []fs.FileMode{0o600, 0o640, 0o644, 0o666} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			dest := newDest(t)
			seed(t, dest, "old\n", mode)
			var created, filling fs.FileMode
			r := atomicfile.Replacer{Create: func(name string, perm fs.FileMode) (atomicfile.File, error) {
				created = perm
				return createExclusive(name, perm)
			}}
			fill := func(w io.Writer) error {
				f, ok := w.(*os.File)
				require.True(t, ok, "the default Create hands the fill the file itself")
				info, err := f.Stat()
				require.NoError(t, err)
				filling = info.Mode().Perm()
				return writing("new\n")(w)
			}

			require.NoError(t, r.Replace(dest, fill))

			assert.Equal(t, mode, created, "created with the destination's mode")
			assert.Equal(t, mode, filling, "holding that mode exactly while it is filled")
			assert.Equal(t, mode, modeOf(t, dest))
		})
	}
}

// TestReplace_TempNameCollisionIsRetried pins what a taken temporary name
// means: the next suffix is drawn and the squatter is left exactly as it was.
// The squatters are real files, through the default Create, so a creation that
// is not exclusive would truncate the regular file and write through either
// symlink, into its target or into one that does not yet exist.
func TestReplace_TempNameCollisionIsRetried(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		plant func(t *testing.T, squatter, victim string)
		// squatterContent is what reading the squatter must still give, or ""
		// when it cannot be read.
		squatterContent string
	}{
		{"regular file", func(t *testing.T, squatter, _ string) {
			seed(t, squatter, "squatter\n", 0o600)
		}, "squatter\n"},
		{"symlink to an existing file", func(t *testing.T, squatter, victim string) {
			require.NoError(t, os.Symlink(victim, squatter))
		}, "victim\n"},
		{"dangling symlink", func(t *testing.T, squatter, victim string) {
			require.NoError(t, os.Symlink(victim+".missing", squatter))
		}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dest := newDest(t)
			dir := filepath.Dir(dest)
			victim := filepath.Join(dir, "victim.json")
			seed(t, victim, "victim\n", 0o644)
			squatter := tempName(dest, 1)
			tt.plant(t, squatter, victim)
			draws := 0
			r := atomicfile.Replacer{Suffix: func() uint64 {
				draws++
				return uint64(draws)
			}}

			require.NoError(t, r.Replace(dest, writing("fresh\n")))

			assert.Equal(t, 2, draws, "a taken name is retried once, with a fresh draw")
			assert.Equal(t, "fresh\n", contentOf(t, dest))
			assert.Equal(t, "victim\n", contentOf(t, victim), "nothing is written through the squatter")
			_, err := os.Lstat(victim + ".missing")
			assert.ErrorIs(t, err, fs.ErrNotExist, "nothing is created behind a dangling link")
			if tt.squatterContent != "" {
				assert.Equal(t, tt.squatterContent, contentOf(t, squatter))
			}
			assert.Equal(t, []string{filepath.Base(squatter), "ir.json", "victim.json"}, entriesIn(t, dir),
				"the squatter stays, and the name actually used is gone")
		})
	}
}

// TestReplace_NamesAreExhausted pins that the search for a temporary name is
// bounded: a suffix source that only ever yields a taken name fails after
// exactly ten draws, naming the destination, and disturbs neither the squatter
// nor the destination.
func TestReplace_NamesAreExhausted(t *testing.T) {
	t.Parallel()
	dest := newDest(t)
	dir := filepath.Dir(dest)
	seed(t, dest, "PREVIOUS\n", 0o644)
	squatter := tempName(dest, 5)
	seed(t, squatter, "squatter\n", 0o600)
	draws := 0
	r := atomicfile.Replacer{Suffix: func() uint64 {
		draws++
		return 5
	}}

	err := r.Replace(dest, writing("fresh\n"))

	assert.EqualError(t, err, fmt.Sprintf("create output %q: no unused temporary name in 10 attempts", dest))
	assert.Equal(t, 10, draws, "the name search must be bounded at ten attempts")
	assert.Equal(t, "PREVIOUS\n", contentOf(t, dest))
	assert.Equal(t, "squatter\n", contentOf(t, squatter))
	assert.Equal(t, []string{filepath.Base(squatter), "ir.json"}, entriesIn(t, dir))
}

// TestReplace_CreateFailureIsNotRetried pins that only a taken name is retried.
// Any other creation failure, one a Create reports or one the filesystem does
// for a missing directory, ends the replace after a single draw, naming the
// destination and wrapping the cause.
func TestReplace_CreateFailureIsNotRetried(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		dest    func(t *testing.T) string
		create  func(string, fs.FileMode) (atomicfile.File, error)
		wantErr error
	}{
		{"Create refuses", newDest,
			func(string, fs.FileMode) (atomicfile.File, error) { return nil, errCreate }, errCreate},
		{"parent directory is missing",
			func(t *testing.T) string { return filepath.Join(newDest(t), "missing", "ir.json") },
			nil, fs.ErrNotExist},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dest := tt.dest(t)
			draws := 0
			r := atomicfile.Replacer{
				Create: tt.create,
				Suffix: func() uint64 {
					draws++
					return 1
				},
			}

			err := r.Replace(dest, writing("fresh\n"))

			require.ErrorIs(t, err, tt.wantErr)
			assert.ErrorContains(t, err, fmt.Sprintf("create output %q: ", dest))
			assert.Equal(t, 1, draws, "a creation failure that is not a taken name is not retried")
		})
	}
}

// TestReplace_DefaultSuffixDrawsFreshNames pins that the default suffix source
// is random: a replace repeated at one destination creates its temporary file
// under a different name each time. A constant default would make a leftover
// from a crashed run block every later one.
func TestReplace_DefaultSuffixDrawsFreshNames(t *testing.T) {
	t.Parallel()
	const rounds = 8
	dest := newDest(t)
	var names []string
	r := atomicfile.Replacer{Create: func(name string, perm fs.FileMode) (atomicfile.File, error) {
		names = append(names, name)
		return createExclusive(name, perm)
	}}

	for range rounds {
		require.NoError(t, r.Replace(dest, writing("fresh\n")))
	}

	slices.Sort(names)
	require.Len(t, names, rounds)
	assert.Len(t, slices.Compact(names), rounds, "each replace must draw its own temporary name")
}
