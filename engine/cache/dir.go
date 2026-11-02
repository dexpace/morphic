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
	"strconv"
	"strings"
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

// staleAfter is how old a temporary file must be before it is taken for what a
// killed Store left. A younger one may be another process's Store, whose rename
// would fail were it removed, so it is neither counted nor touched.
const staleAfter = time.Hour

// ledgerName names the file each Store appends its entry's size to. Listing the
// directory costs each Store what the entries number, so a Store prunes only once
// the bytes stored since the last prune pass an eighth of the limit, or once
// maxRecords Stores have passed, and removes the ledger first.
const ledgerName = "stored.ledger"

// recordWidth is one ledger record: a size in twenty digits and a newline, one
// short write, which lands whole beside another process's.
const recordWidth = 21

// maxRecords bounds the ledger, so reading it stays cheap, and a prune comes at
// least once in that many Stores.
const maxRecords = 1 << 12

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
// none. Past MaxBytes a Store removes the entries unused longest. The zero Dir
// names no directory, and refuses every call.
type Dir struct {
	path  string
	limit int
	// mark records a use of an entry's file, os.Chtimes outside a test.
	mark func(name string, atime, mtime time.Time) error
}

// NewDir returns the Dir at path.
func NewDir(path string) *Dir {
	return &Dir{path: path, limit: MaxBytes, mark: os.Chtimes}
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
	_ = d.mark(d.fileOf(key), now, now)
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
	reset := os.Remove(d.ledger())
	if errors.Is(reset, fs.ErrNotExist) {
		reset = nil
	}
	if reset != nil {
		reset = fmt.Errorf("reset the ledger: %w", reset)
	}
	return errors.Join(reset, prune(d.path, d.limit, time.Now(), os.ReadDir, os.Remove))
}

// ledger is the path of d's ledger.
func (d *Dir) ledger() string { return filepath.Join(d.path, ledgerName) }

// pruneDue records size in the ledger and reports whether a prune is due by what
// the ledger then holds (tallyDue). A ledger it cannot write or read is taken as
// due, so a fault costs a scan and never the bound.
func (d *Dir) pruneDue(ctx context.Context, size int) bool {
	if err := appendRecord(d.ledger(), size); err != nil {
		return true
	}
	data, err := reader.ReadSource(ctx, d.ledger(), maxRecords*recordWidth)
	return err != nil || tallyDue(data, d.limit)
}

// tallyDue reports whether a ledger holding data calls for a prune under limit:
// it tallies more than an eighth of limit, or holds more than maxRecords records,
// or anything but whole records of sizes limit allows. A read that caught another
// process mid-write ends in part of a record, which is due, never a short record
// indexed past its end.
func tallyDue(data []byte, limit int) bool {
	if len(data) > maxRecords*recordWidth || len(data)%recordWidth != 0 {
		return true
	}
	total := 0
	for record := range slices.Chunk(data, recordWidth) {
		n, err := strconv.Atoi(string(record[:recordWidth-1]))
		if err != nil || n < 0 || n > limit || record[recordWidth-1] != '\n' {
			return true
		}
		total += n
	}
	return total > limit/8
}

// appendRecord appends size to the ledger at path, creating it if need be.
func appendRecord(path string, size int) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(fmt.Appendf(nil, "%0*d\n", recordWidth-1, size))
	return errors.Join(err, f.Close())
}

// held is one file a Dir owns.
type held struct {
	name     string
	size     int
	modified time.Time
}

// prune removes the files of dir's own unused longest until the rest fit limit,
// each step handed in so a test can fail it. Another process may prune alongside,
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
// unused longest first, ties in name order so two runs agree.
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
