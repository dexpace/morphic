package archtest_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
)

// walkGoFiles parses, comments included, every Go file under root that the go
// tool would build, tests among them, and calls visit with each file's path
// relative to root, slash-separated. It skips the directories the go tool skips
// (see ignoredByGoTool) but never root itself, whatever it is named.
func walkGoFiles(root string, visit func(rel string, fset *token.FileSet, f *ast.File)) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && ignoredByGoTool(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(p) != ".go" {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		visit(filepath.ToSlash(rel), fset, f)
		return nil
	})
}

// ignoredByGoTool reports whether the go tool skips a directory by name: testdata
// holds fixtures rather than packages, and a leading "." or "_" hides a directory
// from package patterns such as ./... — which is what keeps the gitignored
// reference checkouts under .claude out of the walk.
func ignoredByGoTool(name string) bool {
	return name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// TestWalkGoFiles_ReadsOnlyWhatTheGoToolBuilds plants a Go file in each place
// the go tool reads or skips, beside a file it never reads, and expects the walk
// to visit exactly the ones it reads.
func TestWalkGoFiles_ReadsOnlyWhatTheGoToolBuilds(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, rel := range []string{
		"a/p.go", "a/p_test.go", "a/b/q.go", // read: tests and nested directories included
		"a/testdata/fixture.go", ".hidden/h.go", "_skipped/s.go", // skipped by name
		"a/notes.txt", // not Go
	} {
		writeDocFile(t, filepath.Join(root, rel), "package p\n")
	}

	var got []string
	require.NoError(t, walkGoFiles(root, func(rel string, _ *token.FileSet, _ *ast.File) {
		got = append(got, rel)
	}))

	want := []string{"a/b/q.go", "a/p.go", "a/p_test.go"}
	if d := cmp.Diff(want, got); d != "" {
		t.Errorf("visited files differ (-want +got):\n%s", d)
	}
}

// TestWalkGoFiles_RootIsNeverSkipped plants a tree whose root carries a name the
// go tool skips beneath it. The walk is asked about that directory, not about a
// child of it, so the skip rule must not empty the walk before it starts.
func TestWalkGoFiles_RootIsNeverSkipped(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "testdata")
	writeDocFile(t, filepath.Join(root, "p.go"), "package p\n")

	var got []string
	require.NoError(t, walkGoFiles(root, func(rel string, _ *token.FileSet, _ *ast.File) {
		got = append(got, rel)
	}))
	if d := cmp.Diff([]string{"p.go"}, got); d != "" {
		t.Errorf("the root's own name must not skip it (-want +got):\n%s", d)
	}
}

// TestWalkGoFiles_FailuresAreReturnedNotSkipped holds that a file that does not
// parse and a root that cannot be read each end the walk with an error. A check
// built on the walk must not pass over what it could not read.
func TestWalkGoFiles_FailuresAreReturnedNotSkipped(t *testing.T) {
	t.Parallel()
	visit := func(string, *token.FileSet, *ast.File) {}

	broken := t.TempDir()
	writeDocFile(t, filepath.Join(broken, "p.go"), "package p\n\nfunc {")
	require.Error(t, walkGoFiles(broken, visit), "a syntax error")

	missing := filepath.Join(t.TempDir(), "absent")
	require.ErrorIs(t, walkGoFiles(missing, visit), os.ErrNotExist, "a root that does not exist")
}
