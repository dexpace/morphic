// Package atomicfile replaces a file's content atomically: the new content is
// written to a temporary file beside the destination, flushed, and renamed over
// it, so a reader sees the whole previous content or the whole new one, and a
// failure leaves the previous content in place.
package atomicfile

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
)

// File is the temporary file a replace writes through. Sync is part of the
// contract rather than an implementation detail: Close returns once the bytes
// reach the page cache, so without an explicit Sync the publishing rename can
// expose a file the filesystem has not taken yet. Chmod sets the mode through
// the open file, so no other file a path could name by then is changed.
type File interface {
	io.Writer
	Chmod(mode fs.FileMode) error
	Sync() error
	Close() error
}

// Replacer replaces files. The zero value replaces through the operating
// system. Each field, when set, stands in for one step of a replace, which is
// how a test reaches a failure only a failing or racing filesystem produces; a
// Replacer holds no other state, so one value serves concurrent replaces of
// different paths.
type Replacer struct {
	// Create creates the file name for writing with mode perm, before the
	// umask, and fails with an error matching fs.ErrExist when name exists.
	// Nil is os.OpenFile with O_EXCL.
	Create func(name string, perm fs.FileMode) (File, error)
	// Rename renames oldpath over newpath. Nil is os.Rename.
	Rename func(oldpath, newpath string) error
	// Suffix draws the random part of a temporary file's name. Nil draws from
	// math/rand/v2.
	Suffix func() uint64
}

// maxTempAttempts bounds the search for an unused temporary name. Each attempt
// draws a fresh suffix, so exhausting it means the destination's directory is
// unusable rather than that the names happened to collide.
const maxTempAttempts = 10

// newFilePerm is the mode a temporary file for a new destination is created
// with: the same 0666 os.Create requests, so the umask narrows it identically.
// One replacing a destination is created with that destination's mode instead
// (see destination.createPerm).
const newFilePerm fs.FileMode = 0o666

// destination is what a replace learned of the file it replaces.
type destination struct {
	path string
	// perm is the existing file's mode, read when exists is true.
	perm   fs.FileMode
	exists bool
}

// createPerm is the mode the temporary file is created with. One replacing an
// existing file is created with that file's mode, which the umask can only
// narrow, so the content it is about to hold is never readable by anyone the
// destination keeps it from.
func (d destination) createPerm() fs.FileMode {
	if d.exists {
		return d.perm
	}
	return newFilePerm
}

// Replace atomically replaces path with what fill writes, through a temporary
// file in path's directory, which keeps the rename on one filesystem. fill's
// error is joined with any failed cleanup, so errors.Is finds it.
//
// Never writing in place costs four things. The directory must take a new
// entry, so /dev/null and a file in a read-only directory fail. A symlink or
// FIFO at path is replaced, and other hard links keep the old content. The
// temporary name is 21 characters longer than path's base. The directory is
// not synced, so a crash can leave the previous content, never part of one.
func (r Replacer) Replace(path string, fill func(io.Writer) error) error {
	r = r.withDefaults()
	dest, err := destOf(path)
	if err != nil {
		return err
	}

	tmp, err := r.writeTemp(dest, fill)
	if err != nil {
		return err
	}

	if err := r.Rename(tmp, path); err != nil {
		return errors.Join(fmt.Errorf("replace output %q: %w", path, err), os.Remove(tmp))
	}
	return nil
}

// withDefaults returns r with every step left unset taken from the operating
// system.
func (r Replacer) withDefaults() Replacer {
	if r.Create == nil {
		r.Create = func(name string, perm fs.FileMode) (File, error) {
			return os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
		}
	}
	if r.Rename == nil {
		r.Rename = os.Rename
	}
	if r.Suffix == nil {
		r.Suffix = rand.Uint64
	}
	return r
}

// destOf reads what a replace of path needs to know of it: the mode an
// existing destination carries, and whether there is one at all. A missing
// destination is the ordinary case, not an error. A symlink is followed, so a
// replaced link's file takes its target's mode.
func destOf(path string) (destination, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return destination{path: path}, nil
	}
	if err != nil {
		return destination{}, fmt.Errorf("stat output %q: %w", path, err)
	}
	return destination{path: path, perm: info.Mode().Perm(), exists: true}, nil
}

// writeTemp runs fill into a newly created file beside dest and returns that
// file's name. Creation is exclusive under a random name, so it neither
// clobbers an existing file nor follows a symlink planted at the name it drew;
// that makes the suffix's unpredictability a convenience, not a security
// boundary.
func (r Replacer) writeTemp(dest destination, fill func(io.Writer) error) (string, error) {
	dir, base := filepath.Dir(dest.path), filepath.Base(dest.path)
	for range maxTempAttempts {
		tmp := filepath.Join(dir, fmt.Sprintf(".%s.tmp%016x", base, r.Suffix()))
		f, err := r.Create(tmp, dest.createPerm())
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("create output %q: %w", dest.path, err)
		}
		if err := fillTemp(f, tmp, dest, fill); err != nil {
			return "", err
		}
		return tmp, nil
	}
	return "", fmt.Errorf("create output %q: no unused temporary name in %d attempts", dest.path, maxTempAttempts)
}

// fillTemp gives f, the temporary file tmp beside dest, the mode it will
// publish with, runs fill into it, flushes it to the filesystem and closes it,
// removing tmp if any step fails, so a failed replace leaves nothing beside the
// destination, even a fill that fails after writing part of its output.
//
// A replacing file takes the destination's exact mode, which the umask may
// have narrowed at creation, before it holds a byte. The Sync lets the rename
// publish durable bytes: without it a crash before writeback can leave the
// destination short or empty.
func fillTemp(f File, tmp string, dest destination, fill func(io.Writer) error) error {
	path := dest.path
	if dest.exists {
		if err := f.Chmod(dest.perm); err != nil {
			return errors.Join(fmt.Errorf("chmod output %q: %w", path, err), f.Close(), os.Remove(tmp))
		}
	}
	if err := fill(f); err != nil {
		return errors.Join(err, f.Close(), os.Remove(tmp))
	}
	if err := f.Sync(); err != nil {
		return errors.Join(fmt.Errorf("sync output %q: %w", path, err), f.Close(), os.Remove(tmp))
	}
	if err := f.Close(); err != nil {
		return errors.Join(fmt.Errorf("close output %q: %w", path, err), os.Remove(tmp))
	}
	return nil
}
