// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irtest

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
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

// ReadModuleFiles opens the main module's go.mod and go.sum, in this process.
//
// A test that reads a dependency's sources, which sit in the module cache, calls
// it so a bump reruns the test: go test rechecks only the files inside the
// module that a test opens, never the cache's, and a bump changes both of
// these. RequireOpensModuleFiles holds a caller to it.
func ReadModuleFiles(t *testing.T) {
	t.Helper()
	readModuleFiles(t, RepoRoot(t), os.ReadFile)
}

// readModuleFiles is ReadModuleFiles with the root and the read injected, so a
// test can drive its failure.
func readModuleFiles(t testingT, root string, read func(string) ([]byte, error)) {
	t.Helper()
	for _, name := range moduleFiles {
		if _, err := read(filepath.Join(root, name)); err != nil {
			t.Fatalf("irtest: reading the module's %s: %v", name, err)
		}
	}
}

// opensChildEnv marks a process RequireOpensModuleFiles started, which must not
// start another.
const opensChildEnv = "IRTEST_OPENS_CHILD"

// RequireOpensModuleFiles fails t unless running the top-level test named test
// opens the main module's go.mod and go.sum. It reads the open lines in go
// test's log, which the test cache keys on; a returned value cannot show that.
// The child runs until the parent's timeout, as a shorter bound under load
// would read a slow child as a hung one.
func RequireOpensModuleFiles(t *testing.T, test string) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "testlog")
	requireOpens(t, opensCheck{
		self:    t.Name(),
		test:    test,
		root:    RepoRoot(t),
		logPath: logPath,
		inChild: os.Getenv(opensChildEnv) != "",
		run: func() ([]byte, error) {
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+test+"$", "-test.testlogfile="+logPath)
			cmd.Env = append(os.Environ(), opensChildEnv+"=1")
			return cmd.CombinedOutput()
		},
		read: os.ReadFile,
	})
}

// opensCheck is what requireOpens needs: the calling test and the one to run,
// the module's root, where the child writes its log, whether this process is
// itself such a child, and the child's run and the log's read.
type opensCheck struct {
	self, test, root, logPath string
	inChild                   bool
	run                       func() ([]byte, error)
	read                      func(string) ([]byte, error)
}

// requireOpens is RequireOpensModuleFiles with its effects injected, so a test
// can drive each refusal without starting a child.
func requireOpens(t testingT, c opensCheck) {
	t.Helper()
	if c.inChild {
		t.Fatalf("irtest: %s runs in a child RequireOpensModuleFiles started, which would start another", c.self)
	}
	if c.test == c.self {
		t.Fatalf("irtest: %s names itself, which would run it again without end", c.self)
	}
	out, err := c.run()
	if err != nil {
		t.Fatalf("irtest: running %s to read its test log: %v\n%.1500s", c.test, err, out)
	}
	log, err := c.read(c.logPath)
	if err != nil {
		t.Fatalf("irtest: reading the test log of %s: %v", c.test, err)
	}
	if missing := unopened(log, c.root); len(missing) != 0 {
		t.Fatalf("irtest: %s does not open the module's %s, so a dependency bump would not rerun it:\n%.1500s",
			c.test, strings.Join(missing, " or "), out)
	}
}

// unopened returns the module files under root that log, go test's record of a
// run, holds no open line for.
func unopened(log []byte, root string) []string {
	lines := strings.Split(string(log), "\n")
	var missing []string
	for _, name := range moduleFiles {
		if !slices.Contains(lines, "open "+filepath.Join(root, name)) {
			missing = append(missing, name)
		}
	}
	return missing
}
