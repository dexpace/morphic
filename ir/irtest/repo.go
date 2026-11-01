// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

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

// moduleFiles are the files whose change marks a dependency moving: go.mod
// names each version and go.sum its hashes.
var moduleFiles = []string{"go.mod", "go.sum"}

// ReadModuleFiles opens the main module's go.mod and go.sum, in this process,
// and returns their paths.
//
// A test that reads a dependency's sources, which sit in the module cache, calls
// it so a bump reruns the test: go test rechecks only the files inside the
// module that a test opens, never the cache's, and a bump changes both of
// these.
func ReadModuleFiles(t *testing.T) []string {
	t.Helper()
	return readModuleFiles(t, RepoRoot(t), os.ReadFile)
}

// readModuleFiles is ReadModuleFiles with the root and the read injected, so a
// test can drive its failure.
func readModuleFiles(t testingT, root string, read func(string) ([]byte, error)) []string {
	t.Helper()
	paths := make([]string, 0, len(moduleFiles))
	for _, name := range moduleFiles {
		path := filepath.Join(root, name)
		if _, err := read(path); err != nil {
			t.Fatalf("irtest: reading the module's %s: %v", name, err)
		}
		paths = append(paths, path)
	}
	return paths
}
