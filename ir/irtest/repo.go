package irtest

import (
	"os"
	"path/filepath"
	"testing"
)

// RepoRoot returns the directory holding the module's go.mod, walking up from
// the working directory, which go test sets to the package under test's own.
// It does not start from a source file's path: under -trimpath,
// runtime.Caller reports a file under its import path, not on disk.
func RepoRoot(t *testing.T) string {
	t.Helper()
	return rootFrom(t, os.Getwd)
}

// rootFrom is RepoRoot with the working directory's lookup injected, so a test
// can drive both of its failures.
func rootFrom(t testingT, getwd func() (string, error)) string {
	t.Helper()
	dir, err := getwd()
	if err != nil {
		t.Fatalf("irtest: reading the working directory: %v", err)
	}
	for !holdsGoMod(dir) {
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("irtest: no go.mod in or above the working directory")
		}
		dir = parent
	}
	return dir
}

// holdsGoMod reports whether dir has a go.mod of its own.
func holdsGoMod(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "go.mod"))
	return err == nil
}
