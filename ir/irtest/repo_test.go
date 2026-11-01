// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irtest_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir/irtest"
)

// TestRepoRoot_FindsTheModuleAboveTheWorkingDirectory pins the walk on the
// real tree: the root holds go.mod, and the working directory lies under it.
func TestRepoRoot_FindsTheModuleAboveTheWorkingDirectory(t *testing.T) {
	t.Parallel()
	root := irtest.RepoRoot(t)
	_, err := os.Stat(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	wd, err := os.Getwd()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(wd, root), "%s lies under %s", wd, root)
}

// runRootFrom drives rootFrom in a goroutine so a Goexit from Fatalf unwinds
// only that goroutine, then returns the recorder and what rootFrom returned.
func runRootFrom(getwd func() (string, error)) (*recordingT, string) {
	rec := &recordingT{}
	var got string
	done := make(chan struct{})
	go func() {
		defer close(done)
		got = irtest.RootFrom(rec, getwd)
	}()
	<-done
	return rec, got
}

// TestRootFrom_RefusesAWorkingDirectoryItCannotRead pins the first failure:
// the walk has nowhere to start, so the test stops rather than guessing one.
func TestRootFrom_RefusesAWorkingDirectoryItCannotRead(t *testing.T) {
	t.Parallel()
	rec, got := runRootFrom(func() (string, error) { return "", errors.New("gone") })
	assert.True(t, rec.aborted)
	assert.Contains(t, rec.fatalf, "reading the working directory: gone")
	assert.Empty(t, got)
	assert.GreaterOrEqual(t, rec.helperCalls, 1, "rootFrom marks itself a helper")
}

// TestRootFrom_RefusesADirectoryOutsideAnyModule pins the second: a walk that
// reaches the filesystem root without a go.mod stops there.
func TestRootFrom_RefusesADirectoryOutsideAnyModule(t *testing.T) {
	t.Parallel()
	outside := t.TempDir()
	rec, got := runRootFrom(func() (string, error) { return outside, nil })
	assert.True(t, rec.aborted)
	assert.Contains(t, rec.fatalf, "no go.mod in or above the working directory")
	assert.Empty(t, got)
	assert.GreaterOrEqual(t, rec.helperCalls, 1, "rootFrom marks itself a helper")
}

// TestReadModuleFiles_OpensGoModAndGoSum is the test
// TestRequireOpensModuleFiles_ReadsTheChildsLog runs in a child. Alone it holds
// nothing: it fails only if a file cannot be opened.
func TestReadModuleFiles_OpensGoModAndGoSum(t *testing.T) {
	t.Parallel()
	irtest.ReadModuleFiles(t)
}

// TestRequireOpensModuleFiles_ReadsTheChildsLog runs the test above in a real
// child, whose go test log must hold an open line for each module file.
func TestRequireOpensModuleFiles_ReadsTheChildsLog(t *testing.T) {
	t.Parallel()
	irtest.RequireOpensModuleFiles(t, "TestReadModuleFiles_OpensGoModAndGoSum")
}

// TestMarkedChild_OpensTheModuleFilesOnlyWhenMarked opens the module's files
// only when the process carries the marker RequireOpensModuleFiles gives its
// child, so the log of TestRequireOpensModuleFiles_MarksTheChildItStarts shows
// whether the child had it.
func TestMarkedChild_OpensTheModuleFilesOnlyWhenMarked(t *testing.T) {
	t.Parallel()
	if os.Getenv(irtest.OpensChildEnv) != "" {
		irtest.ReadModuleFiles(t)
	}
}

// TestRequireOpensModuleFiles_MarksTheChildItStarts holds the marker that bounds
// the re-execution: the test above opens the files only in a marked child, so a
// child started without it leaves the log bare.
func TestRequireOpensModuleFiles_MarksTheChildItStarts(t *testing.T) {
	t.Parallel()
	irtest.RequireOpensModuleFiles(t, "TestMarkedChild_OpensTheModuleFilesOnlyWhenMarked")
}

// TestUnopened_NamesTheModuleFilesTheLogLacks pins the log's reading: only an
// exact open line under the root counts, so a stat, another file and another
// root do not.
func TestUnopened_NamesTheModuleFilesTheLogLacks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		log  string
		want []string
	}{
		{"both", "# test log\nopen /m/go.mod\nopen /m/go.sum\n", nil},
		{"no go.mod", "open /m/go.sum\n", []string{"go.mod"}},
		{"no go.sum", "open /m/go.mod\n", []string{"go.sum"}},
		{"stat is not open", "stat /m/go.mod\nopen /m/go.sum\n", []string{"go.mod"}},
		{"another root", "open /other/go.mod\nopen /m/go.sum\n", []string{"go.mod"}},
		{"a longer name", "open /m/go.mod.bak\nopen /m/go.sum\n", []string{"go.mod"}},
		{"empty", "", []string{"go.mod", "go.sum"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, irtest.UnopenedIn([]byte(tc.log), "/m"))
		})
	}
}

// opensRun is one drive of requireOpens: the caller, the test it names, whether
// the process is a child, and the child's run and the log's read.
type opensRun struct {
	self, test string
	inChild    bool
	run        func() ([]byte, error)
	read       func(string) ([]byte, error)
}

// goodOpens is a drive whose child exits cleanly and whose log opens both
// module files; a test changes the one thing it pins.
func goodOpens() opensRun {
	return opensRun{
		self: "TestCaller", test: "TestNamed",
		run:  func() ([]byte, error) { return []byte("ok"), nil },
		read: func(string) ([]byte, error) { return []byte("open /m/go.mod\nopen /m/go.sum\n"), nil },
	}
}

// drive runs requireOpens in a goroutine so a Goexit from Fatalf unwinds only
// that goroutine, then returns the recorder and whether the child started.
func (o opensRun) drive() (*recordingT, bool) {
	rec := &recordingT{}
	started := false
	done := make(chan struct{})
	go func() {
		defer close(done)
		irtest.RequireOpensOn(rec, o.self, o.test, o.inChild,
			func() ([]byte, error) { started = true; return o.run() }, o.read)
	}()
	<-done
	return rec, started
}

// TestRequireOpens_AcceptsALogOpeningBothFiles pins the passing arm: a child
// runs, and nothing is reported.
func TestRequireOpens_AcceptsALogOpeningBothFiles(t *testing.T) {
	t.Parallel()
	rec, started := goodOpens().drive()
	assert.True(t, started)
	assert.False(t, rec.aborted)
	assert.Empty(t, rec.fatalf)
	assert.GreaterOrEqual(t, rec.helperCalls, 1, "requireOpens marks itself a helper")
}

// TestRequireOpens_FailsWhenTheChildOrItsLogDoes pins each failure after the
// child starts by the text it reports.
func TestRequireOpens_FailsWhenTheChildOrItsLogDoes(t *testing.T) {
	t.Parallel()
	logOf := func(s string) func(string) ([]byte, error) {
		return func(string) ([]byte, error) { return []byte(s), nil }
	}
	tests := []struct {
		name  string
		edit  func(*opensRun)
		fatal string
	}{
		{"the child fails", func(o *opensRun) {
			o.run = func() ([]byte, error) { return []byte("boom output"), errors.New("exit status 1") }
		}, "running TestNamed to read its test log: exit status 1\nboom output"},
		{"the log is unreadable", func(o *opensRun) {
			o.read = func(string) ([]byte, error) { return nil, errors.New("gone") }
		}, "reading the test log of TestNamed: gone"},
		{"no go.mod opened", func(o *opensRun) { o.read = logOf("open /m/go.sum\n") },
			"TestNamed does not open the module's go.mod, so a dependency bump would not rerun it:\nok"},
		{"no go.sum opened", func(o *opensRun) { o.read = logOf("open /m/go.mod\n") },
			"TestNamed does not open the module's go.sum, so a dependency bump would not rerun it:\nok"},
		{"neither opened", func(o *opensRun) { o.read = logOf("") },
			"TestNamed does not open the module's go.mod or go.sum, so"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			o := goodOpens()
			tc.edit(&o)
			rec, started := o.drive()
			assert.True(t, started)
			assert.True(t, rec.aborted)
			assert.Contains(t, rec.fatalf, tc.fatal)
			assert.GreaterOrEqual(t, rec.helperCalls, 1, "requireOpens marks itself a helper")
		})
	}
}

// TestRequireOpens_RefusesToRunAgainstItselfOrInAChild pins the depth bound:
// each refusal comes before any child starts.
func TestRequireOpens_RefusesToRunAgainstItselfOrInAChild(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		edit  func(*opensRun)
		fatal string
	}{
		{"the test names itself", func(o *opensRun) { o.test = o.self },
			"TestCaller names itself, which would run it again without end"},
		{"it runs in a child", func(o *opensRun) { o.inChild = true },
			"TestCaller runs in a child RequireOpensModuleFiles started, which would start another"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			o := goodOpens()
			tc.edit(&o)
			rec, started := o.drive()
			assert.False(t, started, "no child starts")
			assert.True(t, rec.aborted)
			assert.Contains(t, rec.fatalf, tc.fatal)
			assert.GreaterOrEqual(t, rec.helperCalls, 1, "requireOpens marks itself a helper")
		})
	}
}

// TestReadModuleFiles_RefusesAFileItCannotRead pins the failure for each file:
// a module whose go.mod or go.sum cannot be opened stops the test, since the
// rerun it promises would otherwise not happen.
func TestReadModuleFiles_RefusesAFileItCannotRead(t *testing.T) {
	t.Parallel()
	for _, missing := range []string{"go.mod", "go.sum"} {
		t.Run(missing, func(t *testing.T) {
			t.Parallel()
			rec := &recordingT{}
			done := make(chan struct{})
			go func() {
				defer close(done)
				irtest.ReadModuleFilesOn(rec, "/module", func(path string) ([]byte, error) {
					if filepath.Base(path) == missing {
						return nil, errors.New("gone")
					}
					return nil, nil
				})
			}()
			<-done
			assert.True(t, rec.aborted)
			assert.Contains(t, rec.fatalf, "reading the module's "+missing+": gone")
			assert.GreaterOrEqual(t, rec.helperCalls, 1, "readModuleFiles marks itself a helper")
		})
	}
}
