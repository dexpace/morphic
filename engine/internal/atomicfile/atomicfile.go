// Package atomicfile replaces a file's content atomically: the new content is
// written to a temporary file beside the destination, flushed, and renamed over
// it, so a reader sees the whole previous content or the whole new one, and a
// failure leaves the previous content in place.
//
// github.com/google/renameio/v2 creates, syncs and renames the file. This
// package fixes where the temporary file goes and the mode it holds, which
// renameio leaves to options, and names the destination in every error. The
// library builds for no Windows target, so neither does this package.
package atomicfile

import (
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

// Replace atomically replaces path with what fill writes, through a temporary
// file in path's directory, which keeps the rename on one filesystem. fill's
// error is joined with any failed cleanup, so errors.Is finds it. An empty
// path or a nil fill is refused before anything is created.
//
// Never writing in place has costs. The directory must take a new entry, so
// /dev/null and a read-only directory fail. A symlink or FIFO at path is
// replaced, and other hard links keep the old content. The temporary name is
// up to 20 characters longer than path's base. The directory is not synced.
func Replace(path string, fill func(io.Writer) error) error {
	if path == "" {
		return errors.New("replace output: no path to replace")
	}
	if fill == nil {
		return fmt.Errorf("replace output %q: nothing to fill it with", path)
	}
	mode, err := modeFor(path)
	if err != nil {
		return err
	}
	f, err := renameio.NewPendingFile(path, renameio.WithTempDir(filepath.Dir(path)), mode)
	if err != nil {
		return fmt.Errorf("create output %q: %w", path, err)
	}
	if err := fill(f); err != nil {
		return errors.Join(err, f.Cleanup())
	}
	if err := f.CloseAtomicallyReplace(); err != nil {
		return errors.Join(fmt.Errorf("replace output %q: %w", path, err), f.Cleanup())
	}
	return nil
}

// modeFor returns the option that gives the file replacing path its mode. An
// existing destination's mode is kept exactly, a symlink followed to its
// target's: renameio creates the file owner-only and sets that mode through
// the open file before fill writes a byte, so the content is never readable
// by anyone the destination keeps it from. A new destination takes
// newFilePerm, narrowed by the umask.
//
// renameio's own WithExistingPermissions reads the link rather than its
// target, so a private target replaced through a link would come back with the
// umask's mode.
func modeFor(path string) (renameio.Option, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return renameio.WithPermissions(newFilePerm), nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat output %q: %w", path, err)
	}
	return renameio.WithStaticPermissions(info.Mode().Perm()), nil
}
