// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package cache_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine/cache"
)

// newDir returns the Dir at path, which the test requires to be one.
func newDir(t *testing.T, path string) *cache.Dir {
	t.Helper()
	dir, err := cache.NewDir(path)
	require.NoError(t, err)
	return dir
}

// keyN is a key that differs for each n.
func keyN(n int) cache.Key { return cache.KeyOf("entry", string(rune('a'+n))) }

// fileOf is the file a Dir at dir holds key's entry in.
func fileOf(dir string, key cache.Key) string { return filepath.Join(dir, key.String()+".entry") }

// aged writes data as key's entry, last changed age ago.
func aged(t *testing.T, dir string, key cache.Key, data string, age time.Duration) {
	t.Helper()
	path := fileOf(dir, key)
	require.NoError(t, os.WriteFile(path, []byte(data), 0o600))
	when := time.Now().Add(-age)
	require.NoError(t, os.Chtimes(path, when, when))
}

// temporary writes the file a Store of key would rename into place, last
// changed age ago.
func temporary(t *testing.T, path string, key cache.Key, age time.Duration) string {
	t.Helper()
	name := filepath.Join(path, "."+key.String()+".entry1234567890123456789")
	require.NoError(t, os.WriteFile(name, []byte("xxxxxxxx"), 0o600))
	when := time.Now().Add(-age)
	require.NoError(t, os.Chtimes(name, when, when))
	return name
}

// listedEntry is a directory entry whose Info answers as a test says.
type listedEntry struct {
	name    string
	size    int64
	infoErr error
}

func (e listedEntry) Name() string               { return e.name }
func (listedEntry) IsDir() bool                  { return false }
func (listedEntry) Type() fs.FileMode            { return 0 }
func (e listedEntry) Info() (fs.FileInfo, error) { return listedInfo(e), e.infoErr }

// listedInfo is the fs.FileInfo of a listedEntry.
type listedInfo listedEntry

func (i listedInfo) Name() string     { return i.name }
func (i listedInfo) Size() int64      { return i.size }
func (listedInfo) Mode() fs.FileMode  { return 0 }
func (listedInfo) ModTime() time.Time { return time.Unix(0, 0) }
func (listedInfo) IsDir() bool        { return false }
func (listedInfo) Sys() any           { return nil }

func listing(entries ...fs.DirEntry) func(string) ([]fs.DirEntry, error) {
	return func(string) ([]fs.DirEntry, error) { return entries, nil }
}
