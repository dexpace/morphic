// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irtest_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/exectest"
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

// parentDeadlineEnv carries the deadline of the test that starts a child to the
// child, which cannot read it from its parent.
const parentDeadlineEnv = "IRTEST_TEST_PARENT_DEADLINE"

// childStartupSlack is how far past its parent's deadline a child's own may
// fall, since the child's clock starts when the process does.
const childStartupSlack = time.Minute

// TestBoundedChild_OpensTheModuleFilesOnlyWithinItsParentsTime opens the
// module's files only in a marked child whose own deadline exists and falls
// within childStartupSlack of its parent's, so the log of
// TestRequireOpensModuleFiles_BoundsTheChildByItsParent shows whether the child
// was bounded by it. A child with no limit, or a longer one, leaves the log bare.
func TestBoundedChild_OpensTheModuleFilesOnlyWithinItsParentsTime(t *testing.T) {
	t.Parallel()
	if os.Getenv(irtest.OpensChildEnv) == "" {
		return
	}
	parent, err := time.Parse(time.RFC3339Nano, os.Getenv(parentDeadlineEnv))
	if err != nil {
		return
	}
	own, bounded := t.Deadline()
	if !bounded || own.After(parent.Add(childStartupSlack)) {
		return
	}
	irtest.ReadModuleFiles(t)
}

// TestRequireOpensModuleFiles_BoundsTheChildByItsParent holds the deadline
// RequireOpensModuleFiles reads from t to the child it starts: the test above
// opens the files only if the child's -test.timeout ends it with its parent,
// so one given none, or a longer one, fails here. It is not parallel, as
// t.Setenv forbids, and skips when the run has no deadline to pass on.
func TestRequireOpensModuleFiles_BoundsTheChildByItsParent(t *testing.T) {
	deadline, bounded := t.Deadline()
	if !bounded {
		t.Skip("the run has no deadline (-timeout 0) for the child to inherit")
	}
	t.Setenv(parentDeadlineEnv, deadline.Format(time.RFC3339Nano))
	irtest.RequireOpensModuleFiles(t, "TestBoundedChild_OpensTheModuleFilesOnlyWithinItsParentsTime")
}

// subjectEnv names the test TestRefusalSubject_RunsWhatItIsGiven passes to
// RequireOpensModuleFiles.
const subjectEnv = "IRTEST_TEST_SUBJECT"

// TestRefusalSubject_RunsWhatItIsGiven calls RequireOpensModuleFiles on the test
// subjectEnv names, if it is set, so the test below can run the call in a
// process of its own, where a refusal ends that process rather than the suite.
func TestRefusalSubject_RunsWhatItIsGiven(t *testing.T) {
	t.Parallel()
	if subject := os.Getenv(subjectEnv); subject != "" {
		irtest.RequireOpensModuleFiles(t, subject)
	}
}

// TestRequireOpensModuleFiles_RefusesThroughARealChild holds the values
// RequireOpensModuleFiles builds for its refusals, which the injected tests
// cannot: a caller naming itself, one inside a marked child, and a child whose
// log opens nothing must each fail the process that made the call, for the
// reason named. It also holds the module files the function requires. Without
// them the call recurses without end or passes on any log.
func TestRequireOpensModuleFiles_RefusesThroughARealChild(t *testing.T) {
	t.Parallel()
	const self = "TestRefusalSubject_RunsWhatItIsGiven"
	root := irtest.RepoRoot(t)
	tests := []struct {
		name    string
		subject string
		marked  bool
		want    string
	}{
		{"a caller naming itself", self, false, self + " names itself"},
		{"a caller inside a marked child", "TestReadModuleFiles_OpensGoModAndGoSum", true,
			self + " runs in a child RequireOpens started"},
		{"a child that opens nothing", "TestUnopened_NamesTheModuleFilesTheLogLacks", false,
			"TestUnopened_NamesTheModuleFilesTheLogLacks does not open " +
				filepath.Join(root, "go.mod") + ", " + filepath.Join(root, "go.sum") + ", so"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := []string{subjectEnv + "=" + tc.subject, "PWD=" + os.Getenv("PWD")}
			if tc.marked {
				env = append(env, irtest.OpensChildEnv+"=1")
			}
			cmd := exectest.Command(t.Context(), t, env, os.Args[0],
				"-test.run=^"+self+"$", "-test.timeout=2m")
			out, err := cmd.CombinedOutput()
			require.Error(t, err, "the call must fail its process:\n%.1500s", out)
			assert.Contains(t, string(out), tc.want)
		})
	}
}

// TestUnopened_NamesTheModuleFilesTheLogLacks pins the log's reading: only an
// exact open line for a path counts, so a stat, another file, another root and
// a longer name do not, and the answer keeps the order the paths came in.
func TestUnopened_NamesTheModuleFilesTheLogLacks(t *testing.T) {
	t.Parallel()
	files := []string{"/m/go.mod", "/m/go.sum"}
	tests := []struct {
		name  string
		log   string
		paths []string
		want  []string
	}{
		{"both", "# test log\nopen /m/go.mod\nopen /m/go.sum\n", files, nil},
		{"no go.mod", "open /m/go.sum\n", files, []string{"/m/go.mod"}},
		{"no go.sum", "open /m/go.mod\n", files, []string{"/m/go.sum"}},
		{"stat is not open", "stat /m/go.mod\nopen /m/go.sum\n", files, []string{"/m/go.mod"}},
		{"another root", "open /other/go.mod\nopen /m/go.sum\n", files, []string{"/m/go.mod"}},
		{"a longer name", "open /m/go.mod.bak\nopen /m/go.sum\n", files, []string{"/m/go.mod"}},
		{"empty", "", files, files},
		{"a directory", "open /m\nopen /m/a/b\n", []string{"/m", "/m/a", "/m/a/b"}, []string{"/m/a"}},
		{"no paths", "open /m/go.mod\n", nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, irtest.UnopenedIn([]byte(tc.log), tc.paths))
		})
	}
}

// opensRun is one drive of requireOpens: the caller, the test it names, whether
// the process is a child, the time left and whether it is limited, and the
// child's run and the log's read.
type opensRun struct {
	self, test string
	paths      []string
	inChild    bool
	remaining  time.Duration
	bounded    bool
	run        func(args []string) ([]byte, error)
	read       func(string) ([]byte, error)
}

// goodOpens is a drive whose child exits cleanly and whose log opens both
// module files; a test changes the one thing it pins.
func goodOpens() opensRun {
	return opensRun{
		self: "TestCaller", test: "TestNamed", paths: []string{"/m/go.mod", "/m/go.sum"},
		run:  func([]string) ([]byte, error) { return []byte("ok"), nil },
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
		irtest.RequireOpensOn(rec, o.self, o.test, o.paths, o.inChild, o.remaining, o.bounded,
			func(args []string) ([]byte, error) { started = true; return o.run(args) }, o.read)
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
			o.run = func([]string) ([]byte, error) { return []byte("boom output"), errors.New("exit status 1") }
		}, "running TestNamed to read its test log: exit status 1\nboom output"},
		{"the log is unreadable", func(o *opensRun) {
			o.read = func(string) ([]byte, error) { return nil, errors.New("gone") }
		}, "reading the test log of TestNamed: gone"},
		{"no go.mod opened", func(o *opensRun) { o.read = logOf("open /m/go.sum\n") },
			"TestNamed does not open /m/go.mod, so a change there would not rerun it:\nok"},
		{"no go.sum opened", func(o *opensRun) { o.read = logOf("open /m/go.mod\n") },
			"TestNamed does not open /m/go.sum, so a change there would not rerun it:\nok"},
		{"neither opened", func(o *opensRun) { o.read = logOf("") },
			"TestNamed does not open /m/go.mod, /m/go.sum, so a change there"},
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

// TestRequireOpens_RefusesToRunAgainstItselfInAChildOrWithoutTime pins the
// depth and time bounds: each refusal comes before any child starts.
func TestRequireOpens_RefusesToRunAgainstItselfInAChildOrWithoutTime(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		edit  func(*opensRun)
		fatal string
	}{
		{"the test names itself", func(o *opensRun) { o.test = o.self },
			"TestCaller names itself, which would run it again without end"},
		{"it runs in a child", func(o *opensRun) { o.inChild = true },
			"TestCaller runs in a child RequireOpens started, which would start another"},
		{"no path is named", func(o *opensRun) { o.paths = nil },
			"TestCaller names no path for TestNamed to open, so its log would show nothing"},
		{"no time is left", func(o *opensRun) { o.bounded = true },
			"TestCaller has no time left (0s), and a child given none would run without a timeout"},
		{"the time has passed", func(o *opensRun) { o.bounded, o.remaining = true, -time.Second },
			"TestCaller has no time left (-1s), and a child given none would run without a timeout"},
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

// TestRequireOpens_GivesTheChildTheTimeLeft pins the wiring: the arguments
// requireOpens builds reach the child's run, bounded by what the parent has
// left when it has a limit.
func TestRequireOpens_GivesTheChildTheTimeLeft(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		bounded   bool
		remaining time.Duration
		want      []string
	}{
		{"a limited parent", true, 90 * time.Second,
			[]string{"-test.run=^TestNamed$", "-test.testlogfile=/log", "-test.timeout=1m30s"}},
		{"an unlimited parent", false, 0,
			[]string{"-test.run=^TestNamed$", "-test.testlogfile=/log"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			o := goodOpens()
			o.bounded, o.remaining = tc.bounded, tc.remaining
			o.run = func(args []string) ([]byte, error) { got = args; return []byte("ok"), nil }
			rec, started := o.drive()
			assert.True(t, started)
			assert.False(t, rec.aborted)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestChildArgs_BoundsTheChildOnlyWhenTheParentIsBounded pins the argument
// list: the timeout round-trips to the time left, and an unlimited parent
// gives none, since the child would otherwise be cut short.
func TestChildArgs_BoundsTheChildOnlyWhenTheParentIsBounded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		bounded   bool
		remaining time.Duration
	}{
		{"a whole second", true, time.Second},
		{"minutes", true, 119*time.Second + 999*time.Millisecond},
		{"one nanosecond", true, time.Nanosecond},
		{"unlimited", false, 0},
		{"unlimited with time given", false, time.Hour},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			args := irtest.ChildArgs("T", "/some/log", tc.remaining, tc.bounded)
			require.GreaterOrEqual(t, len(args), 2)
			assert.Equal(t, "-test.run=^T$", args[0])
			assert.Equal(t, "-test.testlogfile=/some/log", args[1])
			if !tc.bounded {
				assert.Len(t, args, 2, "no -test.timeout")
				return
			}
			require.Len(t, args, 3)
			value, ok := strings.CutPrefix(args[2], "-test.timeout=")
			require.True(t, ok, "%q", args[2])
			got, err := time.ParseDuration(value)
			require.NoError(t, err)
			assert.Equal(t, tc.remaining, got)
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

// TestRequireOpensModuleFiles_ReadsAChildThroughASymlinkedCheckout runs the check
// from a directory reached through a symlink, where this process spells its paths
// as $PWD does. The child must spell its own alike, or the log names none of them.
func TestRequireOpensModuleFiles_ReadsAChildThroughASymlinkedCheckout(t *testing.T) {
	link := filepath.Join(t.TempDir(), "checkout")
	require.NoError(t, os.Symlink(irtest.RepoRoot(t), link))
	t.Chdir(filepath.Join(link, "ir", "irtest"))
	irtest.RequireOpensModuleFiles(t, "TestReadModuleFiles_OpensGoModAndGoSum")
}
