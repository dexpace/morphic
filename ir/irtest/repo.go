// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dexpace/morphic/internal/exectest"
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

// opensChildEnv marks a process RequireOpens started, which must not start
// another.
const opensChildEnv = "IRTEST_OPENS_CHILD"

// RequireOpensModuleFiles fails t unless running the top-level test named test
// opens the main module's go.mod and go.sum. It is RequireOpens for those two.
func RequireOpensModuleFiles(t *testing.T, test string) {
	t.Helper()
	root := RepoRoot(t)
	paths := make([]string, 0, len(moduleFiles))
	for _, name := range moduleFiles {
		paths = append(paths, filepath.Join(root, name))
	}
	RequireOpens(t, test, paths...)
}

// RequireOpens fails t unless running the top-level test named test opens each
// of paths, absolute and as the log writes them. It reads the open lines in go
// test's log, which the test cache keys on; a returned value cannot show that.
// The child takes the parent's remaining time as its own -test.timeout, so it
// ends when its parent does, and gets no shorter bound, as one under load
// would read a slow child as a hung one.
func RequireOpens(t *testing.T, test string, paths ...string) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "testlog")
	deadline, bounded := t.Deadline()
	requireOpens(t, opensCheck{
		self:      t.Name(),
		test:      test,
		paths:     paths,
		logPath:   logPath,
		inChild:   os.Getenv(opensChildEnv) != "",
		remaining: time.Until(deadline),
		bounded:   bounded,
		run: func(args []string) ([]byte, error) {
			cmd := exectest.Command(t.Context(), t, []string{opensChildEnv + "=1"}, os.Args[0], args...)
			return cmd.CombinedOutput()
		},
		read: os.ReadFile,
	})
}

// opensCheck is what requireOpens needs: the calling test and the one to run,
// the paths the child must open, where it writes its log, whether this process
// is itself such a child, the time left to this process and whether it has a
// limit at all, and the child's run, given its arguments, and the log's read.
type opensCheck struct {
	self, test, logPath string
	paths               []string
	inChild             bool
	remaining           time.Duration
	bounded             bool
	run                 func(args []string) ([]byte, error)
	read                func(string) ([]byte, error)
}

// requireOpens is RequireOpens with its effects injected, so a test can drive
// each refusal without starting a child.
func requireOpens(t testingT, c opensCheck) {
	t.Helper()
	if c.inChild {
		t.Fatalf("irtest: %s runs in a child RequireOpens started, which would start another", c.self)
	}
	if c.test == c.self {
		t.Fatalf("irtest: %s names itself, which would run it again without end", c.self)
	}
	if len(c.paths) == 0 {
		t.Fatalf("irtest: %s names no path for %s to open, so its log would show nothing", c.self, c.test)
	}
	if c.bounded && c.remaining <= 0 {
		// testing arms its timer only for a value above zero.
		t.Fatalf("irtest: %s has no time left (%v), and a child given none would run without a timeout",
			c.self, c.remaining)
	}
	out, err := c.run(childArgs(c.test, c.logPath, c.remaining, c.bounded))
	if err != nil {
		t.Fatalf("irtest: running %s to read its test log: %v\n%.1500s", c.test, err, out)
	}
	log, err := c.read(c.logPath)
	if err != nil {
		t.Fatalf("irtest: reading the test log of %s: %v", c.test, err)
	}
	if missing := unopened(log, c.paths); len(missing) != 0 {
		t.Fatalf("irtest: %s does not open %.1500s, so a change there would not rerun it:\n%.1500s",
			c.test, strings.Join(missing, ", "), out)
	}
}

// childArgs returns the arguments that run the top-level test named test and
// write go test's log to logPath. A bounded parent gives the child what it has
// left as -test.timeout; an unbounded one gives none.
func childArgs(test, logPath string, remaining time.Duration, bounded bool) []string {
	args := []string{"-test.run=^" + test + "$", "-test.testlogfile=" + logPath}
	if bounded {
		args = append(args, "-test.timeout="+remaining.String())
	}
	return args
}

// unopened returns the paths that log, go test's record of a run, holds no open
// line for, in the order given.
func unopened(log []byte, paths []string) []string {
	opened := map[string]bool{}
	for line := range strings.SplitSeq(string(log), "\n") {
		if path, ok := strings.CutPrefix(line, "open "); ok {
			opened[path] = true
		}
	}
	var missing []string
	for _, path := range paths {
		if !opened[path] {
			missing = append(missing, path)
		}
	}
	return missing
}
