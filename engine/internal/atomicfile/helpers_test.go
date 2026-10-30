// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package atomicfile_test

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// errFill is the cause a failing fill returns, matched with errors.Is rather
// than by text.
var errFill = errors.New("fill refused")

// writing returns a fill that writes data and succeeds. It calls Write itself:
// io.WriteString would take a file's own WriteString, past a wrapper's Write.
func writing(data string) func(io.Writer) error {
	return func(w io.Writer) error {
		_, err := w.Write([]byte(data))
		return err
	}
}

// newDest returns a destination path in a fresh directory, without creating it.
func newDest(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "ir.json")
}

// seed writes content to path with exactly mode, whatever the umask narrows.
func seed(t *testing.T, path, content string, mode fs.FileMode) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), mode))
	require.NoError(t, os.Chmod(path, mode))
}

// contentOf returns the content of the file at path.
func contentOf(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(raw)
}

// modeOf returns the permission bits of the file at path, following links.
func modeOf(t *testing.T, path string) fs.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.Mode().Perm()
}

// entriesIn lists the names in dir, in the order os.ReadDir returns them.
func entriesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// otherEntries lists the names in dest's directory other than dest's own.
func otherEntries(t *testing.T, dest string) []string {
	t.Helper()
	var others []string
	for _, name := range entriesIn(t, filepath.Dir(dest)) {
		if name != filepath.Base(dest) {
			others = append(others, name)
		}
	}
	return others
}

// tempOf returns the path of the one temporary file beside dest while a
// replace of dest runs: the entry of dest's directory that is not dest.
func tempOf(t *testing.T, dest string) string {
	t.Helper()
	found := otherEntries(t, dest)
	require.Len(t, found, 1, "one temporary file beside the destination")
	return filepath.Join(filepath.Dir(dest), found[0])
}
