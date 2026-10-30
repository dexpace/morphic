// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package keydoc_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/keydoc"
)

// writeTree writes each file under root, by its slash-separated path.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for path, text := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(text), 0o600))
	}
}

// TestCitations_ReadsEachKindOfFile plants citations in each kind of file
// read, one on a later line of a block comment, and in what is not read: a Go
// string, a YAML value, a text file, a directory whose name starts with a dot,
// and a link another document makes to its own entries.
func TestCitations_ReadsEachKindOfFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"a/a.go": "package a\n\n// docs/d.md#one and d.md#two\n" +
			"const s = \"d.md#unread-string\" /* d.md#three */\n\n/* A block,\n   citing d.md#seven. */\nvar t = 1\n",
		"a/a.yaml":      "x: 'd.md#unread-value' # d.md#four\n",
		"a/a.md":        "First line.\nSee [it](../d.md#five).\n",
		"a/a.txt":       "d.md#unread-text\n",
		".hidden/h.go":  "package h // d.md#unread-hidden\n",
		"d.md":          "### `one`\n\nAs [`one`](#one) says.\n",
		"other/e.md":    "As [`e`](#unread-own-link) says.\n",
		"other/f.yml":   "# d.md#six\n",
		"other/.e/g.md": "d.md#unread-nested-hidden\n",
	})
	got, err := keydoc.Citations(root, "d.md", "d.md")
	require.NoError(t, err)
	want := []keydoc.Citation{
		{Path: "a/a.go", Line: 3, Key: "one"},
		{Path: "a/a.go", Line: 3, Key: "two"},
		{Path: "a/a.go", Line: 4, Key: "three"},
		{Path: "a/a.go", Line: 7, Key: "seven"},
		{Path: "a/a.md", Line: 2, Key: "five"},
		{Path: "a/a.yaml", Line: 1, Key: "four"},
		{Path: "d.md", Line: 3, Key: "one"},
		{Path: "other/f.yml", Line: 1, Key: "six"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("citations (-want +got):\n%s", diff)
	}
}

// TestCitations_FailsOnAFileItCannotRead requires an error, not a shorter
// list, for a Go file that does not parse and for a Markdown or YAML file that
// cannot be read, and for a root that does not exist.
//
// The unreadable files are symbolic links to nothing rather than files with no
// permission, since permission bits are advisory to root, and a link with no
// target fails to read whoever asks.
func TestCitations_FailsOnAFileItCannotRead(t *testing.T) {
	t.Parallel()
	_, err := keydoc.Citations(filepath.Join(t.TempDir(), "absent"), "d.md", "d.md")
	require.Error(t, err)

	broken := t.TempDir()
	writeTree(t, broken, map[string]string{"a.go": "package\n"})
	_, err = keydoc.Citations(broken, "d.md", "d.md")
	require.Error(t, err)

	for _, name := range []string{"a.md", "a.yaml"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			require.NoError(t, os.Symlink(filepath.Join(dir, "absent"), filepath.Join(dir, name)))
			_, err := keydoc.Citations(dir, "d.md", "d.md")
			require.ErrorIs(t, err, fs.ErrNotExist)
		})
	}
}

// TestComments_ReadsGoAndYAMLOnly reads each comment of a Go file, a line or
// a block each, and each of a YAML file, and nothing of any other.
func TestComments_ReadsGoAndYAMLOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"a.go":   "// Package a.\npackage a\n\n/* A block,\nover two lines. */\nvar x = 1 // after\n",
		"a.yaml": "# whole line\nkey: v # after\n",
		"a.md":   "# A heading, which is no comment\n",
	})
	got, err := keydoc.Comments(filepath.Join(root, "a.go"))
	require.NoError(t, err)
	assert.Equal(t, []keydoc.Comment{
		{Text: "// Package a.", Line: 1},
		{Text: "/* A block,\nover two lines. */", Line: 4},
		{Text: "// after", Line: 6},
	}, got)
	got, err = keydoc.Comments(filepath.Join(root, "a.yaml"))
	require.NoError(t, err)
	assert.Equal(t, []keydoc.Comment{{Text: " whole line", Line: 1}, {Text: " after", Line: 2}}, got)
	got, err = keydoc.Comments(filepath.Join(root, "a.md"))
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestWalk_ReadsATreeWhateverItIsNamed requires the tree a walk starts at to
// be read even where its own name starts with a dot, which only a directory
// below it is skipped for.
func TestWalk_ReadsATreeWhateverItIsNamed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTree(t, root, map[string]string{".tree/a.go": "", ".tree/.below/b.go": "", "c.go": ""})
	var seen []string
	require.NoError(t, keydoc.Walk(root, ".tree", func(path string) error {
		seen = append(seen, keydoc.Rel(root, path))
		return nil
	}))
	assert.Equal(t, []string{".tree/a.go"}, seen)
}

// TestRel spells a path below the root relative to it, and leaves one it
// cannot relate as it is.
func TestRel(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "a/b.go", keydoc.Rel("/r", "/r/a/b.go"))
	assert.Equal(t, "b.go", keydoc.Rel("/r", "b.go"))
}
