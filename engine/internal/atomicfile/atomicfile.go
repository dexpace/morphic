// Package atomicfile replaces a file's content atomically: the new content is
// written to a temporary file beside the destination, flushed, and renamed over
// it, so a reader sees the whole previous content or the whole new one, and a
// failure, or a cancellation before the rename, leaves the previous content in
// place.
//
// github.com/google/renameio/v2 creates, syncs and renames the file. This
// package fixes where the temporary file goes and the mode it holds, which
// renameio leaves to options, and names the destination in every error. The
// library builds for no Windows target, so neither does this package.
package atomicfile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/google/renameio/v2"
)

// newFilePerm is the mode a new destination is created with: the same 0666
// os.Create requests, so the umask narrows it identically.
const newFilePerm fs.FileMode = 0o666

// Replace atomically replaces path with what fill writes. An empty path, a
// nil fill and a nil or done ctx are refused first. fill's error comes back
// joined with any failed cleanup, and a ctx done when fill returns publishes
// nothing: fill reads ctx to stop sooner.
//
// Renaming needs path's directory to take a new entry, replaces a symlink or
// FIFO at path, leaves other hard links the old content, and does not sync the
// directory. The temporary name adds a dot and up to 19 digits, so a base
// within 20 of the name limit fails on some runs.
func Replace(ctx context.Context, path string, fill func(io.Writer) error) error {
	return replace(ctx, path, fill, (*renameio.PendingFile).Chmod)
}

// replace is Replace with the step that gives the temporary file an existing
// destination's mode handed in, so a test can fail it.
func replace(ctx context.Context, path string, fill func(io.Writer) error,
	chmod func(*renameio.PendingFile, fs.FileMode) error,
) error {
	if path == "" {
		return errors.New("replace output: no path to replace")
	}
	if fill == nil {
		return fmt.Errorf("replace output %q: nothing to fill it with", path)
	}
	if ctx == nil {
		return fmt.Errorf("replace output %q: no context to replace it under", path)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("replace output %q: %w", path, err)
	}
	f, err := pendingFor(path, chmod)
	if err != nil {
		return err
	}
	if err := fill(f); err != nil {
		return abandon(f, path, err)
	}
	// The last point a cancellation can stop the replace: the rename publishes.
	if err := ctx.Err(); err != nil {
		return abandon(f, path, err)
	}
	if err := f.CloseAtomicallyReplace(); err != nil {
		return abandon(f, path, err)
	}
	return nil
}

// abandon removes f, the temporary file replacing path, and returns cause
// naming path, joined with any failure to remove it.
func abandon(f *renameio.PendingFile, path string, cause error) error {
	return errors.Join(fmt.Errorf("replace output %q: %w", path, cause), f.Cleanup())
}

// pendingFor creates the temporary file replacing path, holding the mode it
// publishes with before fill writes a byte. An existing destination's mode is
// kept exactly, a symlink followed to its target's: renameio creates the file
// owner-only and chmod sets that mode, the file removed if it cannot be. A new
// destination's file is created at newFilePerm, narrowed by the umask.
//
// renameio's own mode options set it inside NewPendingFile, which keeps the
// file when that fails, and its WithExistingPermissions reads a link rather
// than its target.
func pendingFor(path string, chmod func(*renameio.PendingFile, fs.FileMode) error) (*renameio.PendingFile, error) {
	// Beside path, so the rename stays on one filesystem.
	opts := []renameio.Option{renameio.WithTempDir(filepath.Dir(path))}
	info, err := os.Stat(path)
	exists := err == nil
	switch {
	case errors.Is(err, fs.ErrNotExist):
		opts = append(opts, renameio.WithPermissions(newFilePerm))
	case err != nil:
		return nil, fmt.Errorf("stat output %q: %w", path, err)
	}
	f, err := renameio.NewPendingFile(path, opts...)
	if err != nil {
		return nil, fmt.Errorf("create output %q: %w", path, err)
	}
	if exists {
		if err := chmod(f, info.Mode().Perm()); err != nil {
			return nil, errors.Join(fmt.Errorf("chmod output %q: %w", path, err), f.Cleanup())
		}
	}
	return f, nil
}
