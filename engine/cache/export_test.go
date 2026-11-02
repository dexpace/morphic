// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package cache

import (
	"context"
	"io/fs"
	"os"
	"time"
)

// LedgerName names the file a Dir tallies its Stores in.
const LedgerName = ledgerName

// MaxRecords is the most Stores a Dir lets pass between prunes.
const MaxRecords = maxRecords

// NewDirLimited returns the Dir at path holding limit bytes, in place of
// MaxBytes.
func NewDirLimited(path string, limit int) *Dir {
	return &Dir{path: path, limit: limit, ops: fileOps{list: os.ReadDir, remove: os.Remove, mark: os.Chtimes}}
}

// Ops are a Dir's file operations a test replaces, a nil one keeping the
// system's.
type Ops struct {
	List   func(dir string) ([]fs.DirEntry, error)
	Remove func(name string) error
	Mark   func(name string, atime, mtime time.Time) error
}

// UseOps makes d call the operations ops sets in place of the system's.
func UseOps(d *Dir, ops Ops) {
	if ops.List != nil {
		d.ops.list = ops.List
	}
	if ops.Remove != nil {
		d.ops.remove = ops.Remove
	}
	if ops.Mark != nil {
		d.ops.mark = ops.Mark
	}
}

// Prune prunes d now, whatever its ledger says.
func Prune(d *Dir) error { return d.prune(time.Now()) }

// TallyDue exposes tallyDue, so a test can hand it ledgers no Store would read.
func TallyDue(data []byte, limit int) bool { return tallyDue(data, limit) }

// BuildIDOf exposes buildID, so a test can fail finding the executable.
func BuildIDOf(ctx context.Context, executable func() (string, error)) (string, error) {
	return buildID(ctx, executable)
}
