// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package cache

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/dexpace/morphic/engine/internal/atomicfile"
	"github.com/dexpace/morphic/engine/reader"
)

// MaxBytes bounds what a Dir holds, and so one entry: room for eleven of the
// 96 MB document the golden spec lowers to (spec @319dc756), so a spec revised
// in a loop cannot fill a disk with its past documents.
const MaxBytes = 1 << 30

// entrySuffix ends every entry's file name.
const entrySuffix = ".entry"

// dirPerm is the mode a new directory is created with: its entries restate the
// specs compiled, which may be private.
const dirPerm fs.FileMode = 0o700

// staleAfter is how old a temporary file must be before it is taken for what a
// killed Store left. A younger one may be another process's Store, whose rename
// would fail were it removed, so it is neither counted nor touched.
const staleAfter = time.Hour

// ownName matches the files a Dir may remove: an entry, and what an interrupted
// atomic replace of one left, a dot before its name and digits after. Nothing
// else is touched, so a directory shared with other files loses none of them.
var ownName = regexp.MustCompile(`^\.?[0-9a-f]{64}` + regexp.QuoteMeta(entrySuffix) + `[0-9]*$`)

// errNoDirectory reports a Dir naming no directory: its entries would land in
// whatever the working directory is.
var errNoDirectory = errors.New("no directory to keep entries in")

// Dir stores entries as files in a directory, created on the first Store. It
// implements engine.Cache, and overlapping calls are safe, from several
// processes too: a Store publishes by rename, so a Load sees a whole entry or
// none. Past MaxBytes a Store removes the oldest entries first. The zero Dir
// names no directory, and refuses every call.
type Dir struct {
	path  string
	limit int
}

// NewDir returns the Dir at path.
func NewDir(path string) *Dir {
	return &Dir{path: path, limit: MaxBytes}
}

// fileOf is the path of the entry named key.
func (d *Dir) fileOf(key Key) string {
	return filepath.Join(d.path, key.String()+entrySuffix)
}

// Load returns the entry named key, and false when there is none. An entry past
// the limit is an error: no Store wrote one, so something else did.
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
	return entry, true, nil
}

// Store publishes entry as the one named key, then removes the oldest entries
// past the limit. An entry past the limit alone is refused, since keeping it
// would mean removing every other.
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
	return prune(d.path, d.limit, time.Now(), os.ReadDir, os.Remove)
}

// held is one file a Dir owns.
type held struct {
	name     string
	size     int
	modified time.Time
}

// prune removes the oldest files of dir's own until the rest fit limit, each
// step handed in so a test can fail it. Another process may prune alongside,
// so a file already gone is not a failure.
func prune(dir string, limit int, now time.Time, list func(string) ([]fs.DirEntry, error),
	remove func(string) error,
) error {
	listed, err := list(dir)
	if err != nil {
		return fmt.Errorf("prune: %w", err)
	}
	files, err := heldIn(listed, now)
	if err != nil {
		return err
	}
	total := 0
	for _, f := range files {
		total += f.size
	}
	for _, f := range files {
		if total <= limit {
			return nil
		}
		if err := remove(filepath.Join(dir, f.name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("prune: %w", err)
		}
		total -= f.size
	}
	return nil
}

// heldIn returns the regular files of listed a Dir owns and may remove at now,
// oldest first, ties in name order so two runs agree.
func heldIn(listed []fs.DirEntry, now time.Time) ([]held, error) {
	files := make([]held, 0, len(listed))
	for _, entry := range listed {
		if !entry.Type().IsRegular() || !ownName.MatchString(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("prune: %w", err)
		}
		if strings.HasPrefix(entry.Name(), ".") && now.Sub(info.ModTime()) < staleAfter {
			continue
		}
		files = append(files, held{name: entry.Name(), size: int(info.Size()), modified: info.ModTime()})
	}
	slices.SortFunc(files, func(a, b held) int {
		return cmp.Or(a.modified.Compare(b.modified), strings.Compare(a.name, b.name))
	})
	return files, nil
}
