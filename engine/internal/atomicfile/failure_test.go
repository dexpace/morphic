package atomicfile_test

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine/internal/atomicfile"
)

// TestReplace_WriteErrorClosesFile pins that a write failing inside fill, the
// disk filling mid-write, still closes the temporary file and removes it. A
// leaked descriptor and a stray file are the two things a failed replace must
// not leave behind.
func TestReplace_WriteErrorClosesFile(t *testing.T) {
	t.Parallel()
	dest := newDest(t)
	f := &flakyFile{writeErr: errDisk}

	err := atomicfile.Replacer{Create: f.create}.Replace(dest, writing("x"))

	require.ErrorIs(t, err, errDisk)
	assert.Equal(t, 1, f.closes, "the file must be closed exactly once after a write error")
	assert.Empty(t, entriesIn(t, filepath.Dir(dest)), "a failed write leaves no temporary file")
}

// TestReplace_WriteErrorPreservesDestination is the guard against a failed
// replace destroying the previous content. The Create below truncates whatever
// path it is handed, as the os.Create this design replaced did, and then fails
// the write; so a Replace that ever opened the destination directly would really
// lose the old bytes, and the assertion would really fire.
func TestReplace_WriteErrorPreservesDestination(t *testing.T) {
	t.Parallel()
	const existing = `{"name":"Existing"}` + "\n"
	dest := newDest(t)
	seed(t, dest, existing, 0o644)
	var opened string
	r := atomicfile.Replacer{Create: func(name string, perm fs.FileMode) (atomicfile.File, error) {
		opened = name
		file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
		if err != nil {
			return nil, err
		}
		return &flakyFile{File: file, writeErr: errDisk}, nil
	}}

	err := r.Replace(dest, writing("x"))

	require.ErrorIs(t, err, errDisk)
	assert.NotEqual(t, dest, opened, "the bytes must go to a temporary file, never the destination")
	assert.Equal(t, existing, contentOf(t, dest), "a failed write must not disturb the previous content")
}

// TestReplace_FillErrorLeavesDestinationUntouched pins both halves of filling a
// temporary file instead of preparing the bytes first: the file exists before
// fill is known to succeed, so a fill that fails, before writing or part way
// through, must leave nothing beside the destination, leave the destination
// alone, and surface its own error unwrapped.
func TestReplace_FillErrorLeavesDestinationUntouched(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		fill func(io.Writer) error
	}{
		{"fails before writing", func(io.Writer) error { return errFill }},
		{"fails after a partial write", func(w io.Writer) error {
			if _, err := w.Write([]byte("partial")); err != nil {
				return err
			}
			return errFill
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dest := newDest(t)
			seed(t, dest, "PREVIOUS\n", 0o640)

			err := atomicfile.Replacer{}.Replace(dest, tt.fill)

			require.ErrorIs(t, err, errFill)
			assert.EqualError(t, err, "fill refused", "fill's error comes back unwrapped")
			assert.Equal(t, "PREVIOUS\n", contentOf(t, dest))
			assert.Equal(t, fs.FileMode(0o640), modeOf(t, dest))
			assert.Equal(t, []string{"ir.json"}, entriesIn(t, filepath.Dir(dest)),
				"a failed fill leaves no temporary file beside the destination")
		})
	}
}

// TestReplace_SyncErrorPreservesDestination drives the branch that makes the
// rename safe to trust: a filesystem that takes the bytes but cannot flush them
// must abort before publishing. A sync failure that fell through to the rename
// would publish a file a crash could still lose. The message names the
// destination the caller asked for, not the temporary file it never heard of.
func TestReplace_SyncErrorPreservesDestination(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		closeErr error
		want     []error
	}{
		{"sync fails", nil, []error{errSync}},
		{"sync and close fail", errClose, []error{errSync, errClose}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dest := newDest(t)
			seed(t, dest, "old\n", 0o644)
			tr := &trace{}
			f := &flakyFile{syncErr: errSync, closeErr: tt.closeErr, trace: tr}

			err := atomicfile.Replacer{Create: f.create}.Replace(dest, writing("new\n"))

			for _, want := range tt.want {
				require.ErrorIs(t, err, want)
			}
			assert.ErrorContains(t, err, fmt.Sprintf("sync output %q: sync refused", dest))
			assert.NotContains(t, err.Error(), f.name, "the message names the destination, not the temporary file")
			assert.Equal(t, 1, f.closes, "a failed sync must still close the temporary file")
			assert.Equal(t, []string{"chmod", "write", "sync", "close"}, tr.steps,
				"the replaced file's mode first, then the bytes")
			assert.Equal(t, "old\n", contentOf(t, dest), "a failed sync must not disturb the destination")
			assert.Equal(t, []string{"ir.json"}, entriesIn(t, filepath.Dir(dest)),
				"a failed sync must leave no temporary file beside the destination")
		})
	}
}

// TestReplace_CloseError pins that a close failing after the bytes were written
// is a failed replace: it names the destination, leaves the previous content,
// and removes the temporary file. Close is where a delayed write error can
// surface, so ignoring it would publish bytes the filesystem refused.
func TestReplace_CloseError(t *testing.T) {
	t.Parallel()
	dest := newDest(t)
	seed(t, dest, "old\n", 0o644)
	f := &flakyFile{closeErr: errClose}

	err := atomicfile.Replacer{Create: f.create}.Replace(dest, writing("payload\n"))

	require.ErrorIs(t, err, errClose)
	assert.EqualError(t, err, fmt.Sprintf("close output %q: close refused", dest))
	assert.Equal(t, "payload\n", f.written.String(), "the bytes are written before the close fails")
	assert.Equal(t, 1, f.closes)
	assert.Equal(t, "old\n", contentOf(t, dest))
	assert.Equal(t, []string{"ir.json"}, entriesIn(t, filepath.Dir(dest)))
}

// TestReplace_ChmodError pins that failing to carry the destination's mode onto
// the replacement stops the replace before a byte is written. Publishing the
// file anyway would silently widen or narrow a file the caller asked to keep
// as it was.
func TestReplace_ChmodError(t *testing.T) {
	t.Parallel()
	dest := newDest(t)
	seed(t, dest, "old\n", 0o640)
	f := &flakyFile{chmodErr: errChmod}
	r := atomicfile.Replacer{Create: f.create}

	err := r.Replace(dest, writing("new\n"))

	require.ErrorIs(t, err, errChmod)
	assert.Empty(t, f.written.String(), "the mode is set before any byte is written")
	assert.Equal(t, 1, f.closes, "a failed chmod still closes the temporary file")
	assert.EqualError(t, err, fmt.Sprintf("chmod output %q: chmod refused", dest))
	assert.Equal(t, "old\n", contentOf(t, dest), "a failed chmod must not disturb the destination")
	assert.Equal(t, fs.FileMode(0o640), modeOf(t, dest))
	assert.Equal(t, []string{"ir.json"}, entriesIn(t, filepath.Dir(dest)),
		"a failed chmod must remove the temporary file")
}

// TestReplace_RenameError pins that a rename failing, the last step and the one
// that publishes, removes the temporary file it leaves behind, for a new
// destination and a replaced one alike, and leaves the destination as it was.
func TestReplace_RenameError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		existing bool
		want     []string
	}{
		{"new file", false, nil},
		{"replacing a file", true, []string{"ir.json"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dest := newDest(t)
			if tt.existing {
				seed(t, dest, "old\n", 0o644)
			}
			r := atomicfile.Replacer{Rename: func(string, string) error { return errRename }}

			err := r.Replace(dest, writing("new\n"))

			require.ErrorIs(t, err, errRename)
			assert.EqualError(t, err, fmt.Sprintf("replace output %q: rename refused", dest))
			assert.Equal(t, tt.want, nonEmpty(entriesIn(t, filepath.Dir(dest))),
				"a failed rename removes the temporary file it left behind")
			if tt.existing {
				assert.Equal(t, "old\n", contentOf(t, dest))
			}
		})
	}
}

// nonEmpty returns names, or nil for none, so a table can say "no entries" as
// nil and compare with assert.Equal.
func nonEmpty(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	return names
}

// TestReplace_CleanupFailureIsReportedWithTheCause pins that every failing step
// reports a failed cleanup beside its own error, rather than dropping either:
// the step fails after taking the temporary file away, so removing it fails
// with ErrNotExist, and the caller must see both the cause and that.
func TestReplace_CleanupFailureIsReportedWithTheCause(t *testing.T) {
	t.Parallel()
	const suffix = 7
	fixed := func() uint64 { return suffix }
	tests := []struct {
		name  string
		build func(remove func()) (atomicfile.Replacer, func(io.Writer) error, error)
	}{
		{"fill fails", func(remove func()) (atomicfile.Replacer, func(io.Writer) error, error) {
			fill := func(io.Writer) error { remove(); return errFill }
			return atomicfile.Replacer{Suffix: fixed}, fill, errFill
		}},
		{"sync fails", func(remove func()) (atomicfile.Replacer, func(io.Writer) error, error) {
			f := &flakyFile{syncErr: errSync, onFail: remove}
			return atomicfile.Replacer{Create: f.create, Suffix: fixed}, writing("x"), errSync
		}},
		{"close fails", func(remove func()) (atomicfile.Replacer, func(io.Writer) error, error) {
			f := &flakyFile{closeErr: errClose, onFail: remove}
			return atomicfile.Replacer{Create: f.create, Suffix: fixed}, writing("x"), errClose
		}},
		{"chmod fails", func(remove func()) (atomicfile.Replacer, func(io.Writer) error, error) {
			f := &flakyFile{chmodErr: errChmod, onFail: remove}
			return atomicfile.Replacer{Create: f.create, Suffix: fixed}, writing("x"), errChmod
		}},
		{"rename fails", func(remove func()) (atomicfile.Replacer, func(io.Writer) error, error) {
			rename := func(string, string) error { remove(); return errRename }
			return atomicfile.Replacer{Rename: rename, Suffix: fixed}, writing("x"), errRename
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dest := newDest(t)
			seed(t, dest, "old\n", 0o644)
			remove := func() { assert.NoError(t, os.Remove(tempName(dest, suffix))) }
			r, fill, cause := tt.build(remove)

			err := r.Replace(dest, fill)

			require.ErrorIs(t, err, cause)
			assert.ErrorIs(t, err, fs.ErrNotExist, "the failed cleanup is reported with the cause")
			assert.Equal(t, "old\n", contentOf(t, dest))
		})
	}
}
