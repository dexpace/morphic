// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package cache

import (
	"context"
	"io/fs"
	"time"
)

// NewDirLimited returns the Dir at path holding limit bytes, in place of
// MaxBytes.
func NewDirLimited(path string, limit int) *Dir {
	d := NewDir(path)
	d.limit = limit
	return d
}

// MarkWith makes d record a use of an entry through mark, so a test can fail it.
func MarkWith(d *Dir, mark func(name string, atime, mtime time.Time) error) { d.mark = mark }

// Prune exposes prune, so a test can fail each of its steps.
func Prune(dir string, limit int, list func(string) ([]fs.DirEntry, error), remove func(string) error) error {
	return prune(dir, limit, time.Now(), list, remove)
}

// BuildIDOf exposes buildID, so a test can fail finding the executable.
func BuildIDOf(ctx context.Context, executable func() (string, error)) (string, error) {
	return buildID(ctx, executable)
}

// LedgerName names the file a Dir tallies its Stores in.
const LedgerName = ledgerName

// MaxRecords is the most Stores a Dir lets pass between prunes.
const MaxRecords = maxRecords

// TallyDue exposes tallyDue, so a test can hand it ledgers no Store would read.
func TallyDue(data []byte, limit int) bool { return tallyDue(data, limit) }
