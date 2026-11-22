// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pinnedspecs_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/harness/pinnedspecs"
)

// A commit and a SHA-256 of the forms a list holds.
const (
	commit = "0e9cf29230f0e546ec6e03e53e77bb828b8bda4a"
	sum    = "ead26a54153a18ab272dfb7c42c1db1a24c88cee84c0006c5aeab924309b7721"
)

// TestReadList_ReadsEachSpecInOrder pins the shape of a list: a spec a line,
// each line ending in a newline, its fields between any run of spaces or tabs,
// comment and blank lines skipped.
func TestReadList_ReadsEachSpecInOrder(t *testing.T) {
	t.Parallel()
	list := "# a comment\n\nb-1  owner/repo.x  " + commit + "  dir/b.yaml  " + sum + "\n" +
		"  a\tOwner-2/r_e-p\t" + commit + "\ta.json\t" + sum + "\n"
	specs, err := pinnedspecs.ReadList([]byte(list))
	require.NoError(t, err)
	assert.Equal(t, []pinnedspecs.Spec{
		{Name: "b-1", Repository: "owner/repo.x", Commit: commit, Path: "dir/b.yaml", SHA256: sum},
		{Name: "a", Repository: "Owner-2/r_e-p", Commit: commit, Path: "a.json", SHA256: sum},
	}, specs)
}

// TestReadList_RefusesWhatItCannotFetchSafely pins each line a list refuses,
// each line read as the fetch script reads it and each field held to a form
// that names one file of one revision and stays inside the directory a fetch
// writes to, and the line it is on.
func TestReadList_RefusesWhatItCannotFetchSafely(t *testing.T) {
	t.Parallel()
	line := func(name, repository, commit, path, sum string) string {
		return strings.Join([]string{name, repository, commit, path, sum}, " ") + "\n"
	}
	good := line("a", "o/r", commit, "a.yaml", sum)
	for _, tc := range []struct{ name, list, want string }{
		{"a field missing", "# c\n" + "a o/r " + commit + " a.yaml\n", "list line 2: want name, repository, commit, path and SHA-256"},
		{"a field more", "a o/r " + commit + " a.yaml " + sum + " x\n", "list line 1: want name"},
		{"no newline at the end", good + strings.TrimSuffix(good, "\n"), "list line 2: want a newline at its end"},
		{"a comment with no newline at the end", good + "# c", "list line 2: want a newline at its end"},
		{"a no-break space between fields", strings.ReplaceAll(good, " ", "\u00a0"), "list line 1: want name"},
		{"a vertical tab between fields", strings.ReplaceAll(good, " ", "\v"), "list line 1: want name"},
		{"a next-line character between fields", strings.ReplaceAll(good, " ", "\u0085"), "list line 1: want name"},
		{"a form feed between fields", strings.ReplaceAll(good, " ", "\f"), "list line 1: want name"},
		{"a blank line of a carriage return", "\r\n" + good, `list line 1: want name, repository, commit, path and SHA-256, got "\r"`},
		{"a line ending in a carriage return", strings.ReplaceAll(good, "\n", "\r\n"), `a: SHA-256 "` + sum + `\r"`},
		{"a name with a slash", line("a/b", "o/r", commit, "a.yaml", sum), `name "a/b"`},
		{"a name climbing out", line("..", "o/r", commit, "a.yaml", sum), `name ".."`},
		{"a name in capitals", line("A", "o/r", commit, "a.yaml", sum), `name "A"`},
		{"a repository with no owner", line("a", "r", commit, "a.yaml", sum), `a: repository "r"`},
		{"a repository with a path", line("a", "o/r/x", commit, "a.yaml", sum), `repository "o/r/x"`},
		{"a short commit", line("a", "o/r", commit[:7], "a.yaml", sum), `a: commit "0e9cf29"`},
		{"a branch for a commit", line("a", "o/r", "main", "a.yaml", sum), `commit "main"`},
		{"an absolute path", line("a", "o/r", commit, "/a.yaml", sum), `a: path "/a.yaml"`},
		{"a path climbing out", line("a", "o/r", commit, "../a.yaml", sum), `path "../a.yaml"`},
		{"a path of the parent", line("a", "o/r", commit, "..", sum), `path ".."`},
		{"an unclean path", line("a", "o/r", commit, "d//a.yaml", sum), `path "d//a.yaml"`},
		{"a short SHA-256", line("a", "o/r", commit, "a.yaml", sum[:63]), `a: SHA-256 "` + sum[:63] + `"`},
		{"a SHA-256 in capitals", line("a", "o/r", commit, "a.yaml", strings.ToUpper(sum)), "SHA-256"},
		{"a name listed twice", good + good, `list line 2: "a" is listed twice`},
		{"no spec", "# only a comment\n\n", "the list names no spec"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := pinnedspecs.ReadList([]byte(tc.list))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestSpec_FileKeepsSpecsOfOneFileNameApart pins where a fetch writes a spec:
// two specs whose files share a name land apart.
func TestSpec_FileKeepsSpecsOfOneFileNameApart(t *testing.T) {
	t.Parallel()
	first := pinnedspecs.Spec{Name: "one", Path: "specs.yaml"}
	second := pinnedspecs.Spec{Name: "two", Path: "specs/specs.yaml"}
	assert.Equal(t, filepath.Join("d", "one", "specs.yaml"), first.File("d"))
	assert.Equal(t, filepath.Join("d", "two", "specs.yaml"), second.File("d"))
}

// TestSpec_ReadReturnsOnlyThePinnedBytes pins that Read returns a copy whose
// SHA-256 is the one pinned, and refuses one of another, a Spec of a form no
// list holds, no directory, and a copy that is missing.
func TestSpec_ReadReturnsOnlyThePinnedBytes(t *testing.T) {
	t.Parallel()
	const name = "fixture"
	data := []byte(readFile(t, "testdata/fixtures/fetched/quiet/quiet.yaml"))
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, name), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name, "quiet.yaml"), data, 0o644))
	pinned := pinnedspecs.Spec{Name: name, Repository: "o/r", Commit: commit, Path: "x/quiet.yaml",
		SHA256: sha256Hex(t, "testdata/fixtures/fetched/quiet/quiet.yaml")}

	got, err := pinned.Read(dir)
	require.NoError(t, err)
	assert.Equal(t, data, got)

	other := pinned
	other.SHA256 = sum
	unformed := pinned
	unformed.Commit = "HEAD"
	missing := pinned
	missing.Name = "missing"
	for _, tc := range []struct {
		name string
		spec pinnedspecs.Spec
		dir  string
		want string
	}{
		{"another hash", other, dir, "has SHA-256 " + pinned.SHA256 + ", where the list pins " + sum},
		{"a form no list holds", unformed, dir, `commit "HEAD"`},
		{"no directory", pinned, "", "fixture: no directory to read it from"},
		{"a missing copy", missing, dir, "no such file or directory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := tc.spec.Read(tc.dir)
			require.Error(t, err)
			assert.Nil(t, got)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}
