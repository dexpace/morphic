// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/dexpace/morphic/engine/internal/atomicfile"
	"github.com/dexpace/morphic/engine/reader"
)

// MaxBytes bounds a Dir's entries, and so one entry: room for eleven of the 96 MB
// document the golden spec lowers to (spec @319dc756), so a spec revised in a
// loop cannot fill a disk with its past documents. Between prunes the entries can
// pass it by an eighth (ledgerName), and by what a Store in progress holds.
const MaxBytes = 1 << 30

// entrySuffix ends every entry's file name.
const entrySuffix = ".entry"

// dirPerm is the mode a new directory is created with: its entries restate the
// specs compiled, which may be private.
const dirPerm fs.FileMode = 0o700

// errNoDirectory reports a Dir naming no directory: its entries would land in
// whatever the working directory is.
var errNoDirectory = errors.New("no directory to keep entries in")

// Dir stores entries as files in a directory, created on the first Store. It
// implements engine.Cache, and overlapping calls are safe, from several
// processes too: a Store publishes by rename, so a Load sees a whole entry or
// none. Past MaxBytes a Store removes the entries unused longest. The zero Dir
// names no directory, and refuses every call.
type Dir struct {
	path  string
	limit int
	ops   fileOps
}

// fileOps are the calls a Dir makes on its directory's files beyond writing an
// entry, a field each so a test can fail one.
type fileOps struct {
	list   func(dir string) ([]fs.DirEntry, error)
	remove func(name string) error
	mark   func(name string, atime, mtime time.Time) error
}

// NewDir returns the Dir at path, which the first Store creates. An empty path
// is refused, for the reason errNoDirectory gives.
func NewDir(path string) (*Dir, error) {
	if path == "" {
		return nil, errNoDirectory
	}
	return &Dir{path: path, limit: MaxBytes, ops: fileOps{list: os.ReadDir, remove: os.Remove, mark: os.Chtimes}}, nil
}

// fileOf is the path of the entry named key.
func (d *Dir) fileOf(key Key) string {
	return filepath.Join(d.path, key.String()+entrySuffix)
}

// Load returns the entry named key, and false when there is none, and marks it
// used. An entry past the limit is an error: no Store wrote one, so something
// else did.
func (d *Dir) Load(ctx context.Context, key Key) ([]byte, bool, error) {
	if d.path == "" {
		return nil, false, errNoDirectory
	}
	entry, err := reader.ReadSource(ctx, d.fileOf(key), d.limit)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read entry: %w", err)
	}
	if len(entry) > d.limit {
		return nil, false, fmt.Errorf("entry %s: past the limit of %d bytes", key, d.limit)
	}
	// The mark is all eviction reads of a use. Its error is deliberately dropped:
	// it means a read-only directory or another user's entry, which still
	// answers, and the mark only orders what goes first.
	now := time.Now()
	_ = d.ops.mark(d.fileOf(key), now, now)
	return entry, true, nil
}

// Store publishes entry as the one named key, then, when a prune is due
// (ledgerName), removes the entries unused longest past the limit. An entry past
// the limit alone is refused, since keeping it would mean removing every other.
func (d *Dir) Store(ctx context.Context, key Key, entry []byte) error {
	if d.path == "" {
		return errNoDirectory
	}
	if len(entry) > d.limit {
		return fmt.Errorf("entry %s: %d bytes, past the limit of %d", key, len(entry), d.limit)
	}
	if err := os.MkdirAll(d.path, dirPerm); err != nil {
		return fmt.Errorf("make directory: %w", err)
	}
	err := atomicfile.Replace(ctx, d.fileOf(key), func(w io.Writer) error {
		_, err := w.Write(entry)
		return err
	})
	if err != nil {
		return fmt.Errorf("write entry: %w", err)
	}
	if !d.pruneDue(ctx, len(entry)) {
		return nil
	}
	return errors.Join(d.resetLedger(), d.prune(time.Now()))
}
