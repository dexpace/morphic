// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pinnedspecs_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/exectest"
	"github.com/dexpace/morphic/internal/harness/pinnedspecs"
)

// fetchScript is the script that fetches the listed specs, from this
// package's directory.
const fetchScript = "../../../scripts/fetch-pinned-specs.sh"

// stubCurl stands in for curl as the fetch script calls it, reaching no
// network: it writes the URL it is asked for to the file --output names.
const stubCurl = `#!/bin/sh
output=
while [ "$#" -gt 1 ]; do
	if [ "$1" = --output ]; then
		output="$2"
		shift
	fi
	shift
done
printf '%s\n' "$1" >"$output"
`

// TestReadList_ReadsAListAsTheFetchScriptDoes runs ReadList over one table of
// lists and, on each it accepts, the fetch script, with stubCurl for curl,
// which must fetch the same specs, each from the URL its fields name into the
// file Spec.File names. The rows ReadList refuses, as
// TestReadList_RefusesWhatItCannotFetchSafely pins, are lists the script reads
// another way, so a ReadList that came to accept one fails here.
func TestReadList_ReadsAListAsTheFetchScriptDoes(t *testing.T) {
	t.Parallel()
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "curl"), []byte(stubCurl), 0o755))
	script, err := filepath.Abs(fetchScript)
	require.NoError(t, err)
	line := func(name, repository, path string) string {
		return strings.Join([]string{name, repository, commit, path, sum}, " ") + "\n"
	}
	good := line("a", "o/r", "a.yaml")
	for _, tc := range []struct{ name, list string }{
		{"a spec", good},
		{"two specs", good + line("b", "o/r.x", ".github/d/b.yaml")},
		{"fields between runs of spaces and tabs", "a \t o/r\t\t" + commit + "   a.yaml\t" + sum + "\n"},
		{"white space around a line", " \ta o/r " + commit + " a.yaml " + sum + "\t \n"},
		{"comment and blank lines", "# c\n\n \t\n   # indented\n#a o/r " + commit + " a.yaml " + sum + "\n" + good},
		{"a comment ending in a backslash", "# c \\\n" + good},
		{"no newline at the end", good + strings.TrimSuffix(line("b", "o/r", "b.yaml"), "\n")},
		{"a no-break space between fields", strings.ReplaceAll(good, " ", "\u00a0")},
		{"a next-line character between fields", strings.ReplaceAll(good, " ", "\u0085")},
		{"a vertical tab between fields", strings.ReplaceAll(good, " ", "\v")},
		{"a form feed between fields", strings.ReplaceAll(good, " ", "\f")},
		{"a blank line of a carriage return", "\r\n" + good},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			specs, err := pinnedspecs.ReadList([]byte(tc.list))
			if err != nil {
				return
			}
			dir := t.TempDir()
			want := map[string]string{}
			for _, spec := range specs {
				want[spec.File(dir)] = "https://raw.githubusercontent.com/" +
					spec.Repository + "/" + spec.Commit + "/" + spec.Path + "\n"
			}
			assert.Equal(t, want, runFetchScript(t, script, bin, dir, tc.list))
		})
	}
}

// runFetchScript runs script, with the curl in bin first on its PATH, to
// fetch the specs list names into dir, and returns what it wrote, by file.
func runFetchScript(t *testing.T, script, bin, dir, list string) map[string]string {
	t.Helper()
	listFile := filepath.Join(t.TempDir(), "specs.txt")
	require.NoError(t, os.WriteFile(listFile, []byte(list), 0o644))
	command := exectest.Command(t.Context(), t, []string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH")},
		"bash", script, dir, listFile)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "the fetch script: %s", output)

	written := map[string]string{}
	require.NoError(t, filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		written[path] = readFile(t, path)
		return nil
	}))
	return written
}
