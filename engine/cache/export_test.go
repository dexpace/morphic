// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package cache

import (
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
	return &Dir{path: path, limit: limit, operations: fileOperations{list: os.ReadDir, remove: os.Remove, mark: os.Chtimes}}
}

// Operations are a Dir's file operations a test replaces, a nil one keeping the
// system's.
type Operations struct {
	List   func(dir string) ([]fs.DirEntry, error)
	Remove func(name string) error
	Mark   func(name string, atime, mtime time.Time) error
}

// UseOperations makes dir call the operations operations sets in place of the
// system's.
func UseOperations(dir *Dir, operations Operations) {
	if operations.List != nil {
		dir.operations.list = operations.List
	}
	if operations.Remove != nil {
		dir.operations.remove = operations.Remove
	}
	if operations.Mark != nil {
		dir.operations.mark = operations.Mark
	}
}

// Prune prunes dir now, whatever its ledger says.
func Prune(dir *Dir) error { return dir.prune(time.Now()) }

// TallyDue exposes tallyDue, so a test can hand it ledgers no Store would read.
func TallyDue(data []byte, limit int) bool { return tallyDue(data, limit) }

// BuildIDOn exposes buildID, so a test can take each system's way to the image
// and fail finding the executable.
var BuildIDOn = buildID
