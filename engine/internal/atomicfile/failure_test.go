package atomicfile_test

import (
	"context"
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

// TestReplace_FillErrorLeavesDestinationUntouched pins both halves of filling a
// temporary file instead of preparing the bytes first: the file exists before
// fill is known to succeed, so a fill that fails, before writing or part way
// through, must leave nothing beside the destination, leave the destination
// alone, and surface its own error naming the destination, not the temporary
// file a failed write names.
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

			err := atomicfile.Replace(t.Context(), dest, tt.fill)

			require.ErrorIs(t, err, errFill)
			assert.EqualError(t, err, fmt.Sprintf("replace output %q: fill refused", dest),
				"fill's error comes back naming the destination")
			assert.Equal(t, "PREVIOUS\n", contentOf(t, dest))
			assert.Equal(t, fs.FileMode(0o640), modeOf(t, dest))
			assert.Equal(t, []string{"ir.json"}, entriesIn(t, filepath.Dir(dest)),
				"a failed fill leaves no temporary file beside the destination")
		})
	}
}

// TestReplace_RenameErrorLeavesNothingBehind pins a publish the filesystem
// refuses: a directory at the destination takes no file renamed over it. The
// error names the destination, the temporary file is removed, and the
// directory stays as it was.
func TestReplace_RenameErrorLeavesNothingBehind(t *testing.T) {
	t.Parallel()
	dest := newDest(t)
	require.NoError(t, os.Mkdir(dest, 0o755))
	seed(t, filepath.Join(dest, "inside"), "kept\n", 0o644)

	err := atomicfile.Replace(t.Context(), dest, writing("new\n"))

	require.Error(t, err)
	assert.ErrorContains(t, err, fmt.Sprintf("replace output %q: ", dest))
	assert.Equal(t, []string{"ir.json"}, entriesIn(t, filepath.Dir(dest)),
		"a failed rename removes the temporary file it left behind")
	assert.Equal(t, "kept\n", contentOf(t, filepath.Join(dest, "inside")))
}

// TestReplace_CleanupFailureIsReportedWithTheCause pins that a failing step
// reports a failed cleanup beside its own error, rather than dropping either.
// The fill takes the temporary file away, so removing it fails with
// ErrNotExist, and the caller sees both that and the cause: the fill's own
// error, the context's when the fill ends it, or the rename's when the fill
// succeeds but leaves nothing to rename.
func TestReplace_CleanupFailureIsReportedWithTheCause(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		end       func(cancel context.CancelFunc) error
		wantCause error
	}{
		{"fill fails", func(context.CancelFunc) error { return errFill }, errFill},
		{"context ends", func(cancel context.CancelFunc) error { cancel(); return nil }, context.Canceled},
		{"rename fails", func(context.CancelFunc) error { return nil }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			dest := newDest(t)
			seed(t, dest, "old\n", 0o644)
			fill := func(io.Writer) error {
				require.NoError(t, os.Remove(tempOf(t, dest)))
				return tt.end(cancel)
			}

			err := atomicfile.Replace(ctx, dest, fill)

			assert.ErrorContains(t, err, fmt.Sprintf("replace output %q: ", dest))
			if tt.wantCause != nil {
				assert.ErrorIs(t, err, tt.wantCause)
			}
			assert.ErrorIs(t, err, fs.ErrNotExist, "the failed cleanup is reported with the cause")
			assert.Equal(t, "old\n", contentOf(t, dest))
		})
	}
}

// TestReplace_RefusesAnUnusableCall pins the arguments refused before anything
// is created: an empty path, which would put the temporary file in the working
// directory and run the whole fill before the rename failed, a nil fill, which
// would panic with that file left behind, and a nil ctx, which would panic
// before it.
func TestReplace_RefusesAnUnusableCall(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filled := false
	fill := func(io.Writer) error { filled = true; return nil }
	var noContext context.Context // nil, to reach the guard

	assert.EqualError(t, atomicfile.Replace(t.Context(), "", fill), "replace output: no path to replace")
	dest := filepath.Join(dir, "ir.json")
	assert.EqualError(t, atomicfile.Replace(t.Context(), dest, nil),
		fmt.Sprintf("replace output %q: nothing to fill it with", dest))
	assert.EqualError(t, atomicfile.Replace(noContext, dest, fill),
		fmt.Sprintf("replace output %q: no context to replace it under", dest))
	assert.False(t, filled, "the fill does not run for a refused call")
	assert.Empty(t, entriesIn(t, dir), "nothing is created for any")
}
