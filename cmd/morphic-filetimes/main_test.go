// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// windowFrom and windowTo are the window blobTime's doc comment states, spelled
// again here so that widening the code's constants does not move the test's.
var (
	windowFrom = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	windowTo   = time.Date(2009, 12, 29, 0, 0, 0, 0, time.UTC)
)

// TestMain unsets every inherited GIT_ variable before any test runs. A git
// hook, run by githooks(5), exports GIT_DIR and GIT_INDEX_FILE, and git -c
// exports GIT_CONFIG_PARAMETERS, so the tests' own git would otherwise act on
// the developer's repository and write fixture commits into it.
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !strings.HasPrefix(name, "GIT_") {
			continue
		}
		if err := os.Unsetenv(name); err != nil {
			fmt.Fprintf(os.Stderr, "cannot unset %s: %v\n", name, err)
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}

// hermeticEnv adds what a test's git needs beyond the clean environment
// TestMain leaves: no global or system config, and an author and committer.
func hermeticEnv() []string {
	return []string{
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	}
}

// useHermeticEnv puts hermeticEnv in the process, for a test that reaches git
// through run, which takes no environment of its own.
func useHermeticEnv(t *testing.T) {
	t.Helper()
	for _, kv := range hermeticEnv() {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
}

// repo is a git worktree in a test's temporary directory.
type repo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	r := &repo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", "main")
	return r
}

// gitOut runs git in the repository and returns its combined output.
func (r *repo) gitOut(args ...string) (string, error) {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", r.dir}, args...)...)
	cmd.Env = append(os.Environ(), hermeticEnv()...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	out, err := r.gitOut(args...)
	require.NoError(r.t, err, out)
	return out
}

func (r *repo) write(rel, content string) {
	r.t.Helper()
	p := filepath.Join(r.dir, filepath.FromSlash(rel))
	require.NoError(r.t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(r.t, os.WriteFile(p, []byte(content), 0o644))
}

func (r *repo) commit(msg string) {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "-m", msg)
}

// populate lays out files in directories of every shape the command meets:
// at the top, below a directory, below a directory holding only another
// directory, an executable, and two files with one content.
func (r *repo) populate() {
	r.t.Helper()
	r.write("README", "readme\n")
	r.write("pkg/a.go", "package a\n")
	r.write("svc/inner/x.go", "package x\n")
	r.write("svc/inner/same.go", "package a\n")
	r.write("bin/tool", "#!/bin/sh\n")
	require.NoError(r.t, os.Chmod(filepath.Join(r.dir, "bin/tool"), 0o755))
	r.commit("populate")
}

// system returns the command's view of the repository, with git as hermetic as
// the tests' own.
func (r *repo) system() system {
	return system{dir: r.dir, git: gitIn(r.dir, hermeticEnv(), maxGitOutput), chtimes: os.Chtimes}
}

// run executes the command on the repository.
func (r *repo) run() (code int, stdout, stderr string) {
	return runSystem(r.system())
}

func runSystem(sys system) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = execute(sys, &out, &errOut)
	return code, out.String(), errOut.String()
}

// scramble gives every file and directory below dir, bar .git, the time at.
func scramble(t *testing.T, dir string, at time.Time) {
	t.Helper()
	require.NoError(t, filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == ".git" {
			return fs.SkipDir
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		return os.Chtimes(p, at, at)
	}))
}

// snapshot is the modification time, in nanoseconds, of every file, symlink
// and directory below dir bar .git, keyed by slash path from dir.
func snapshot(t *testing.T, dir string) map[string]int64 {
	t.Helper()
	snap := make(map[string]int64)
	require.NoError(t, filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == ".git" {
			return fs.SkipDir
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		snap[filepath.ToSlash(rel)] = info.ModTime().UnixNano()
		return nil
	}))
	return snap
}

func inWindowNanos(ns int64) bool {
	at := time.Unix(0, ns)
	return !at.Before(windowFrom) && at.Before(windowTo)
}

// TestBlobTime_PinsTheDerivation holds the derivation to values written out by
// hand, so a change to it, which would move every file's time and so strand
// every cached result, is a decision and not a side effect.
// Mutants: return a constant; read another part of the hash; truncate to the
// second.
func TestBlobTime_PinsTheDerivation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, hash string
		want       time.Time
	}{
		{"all zero", strings.Repeat("0", 40), windowFrom},
		{"all zero, 32 bytes", strings.Repeat("0", 64), windowFrom},
		{"one nanosecond", strings.Repeat("0", 15) + "1" + strings.Repeat("0", 24), windowFrom.Add(1)},
		{"one second", strings.Repeat("0", 8) + "3b9aca00" + strings.Repeat("0", 24), windowFrom.Add(time.Second)},
		{"upper case", strings.Repeat("0", 15) + "A" + strings.Repeat("0", 24), windowFrom.Add(10)},
		{"all f", strings.Repeat("f", 40), time.Date(2004, 12, 8, 23, 34, 33, 709551615, time.UTC)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := blobTime(tc.hash)
			require.NoError(t, err)
			assert.True(t, tc.want.Equal(got), "got %s, want %s", got.Format(time.RFC3339Nano), tc.want.Format(time.RFC3339Nano))
		})
	}
}

// TestBlobTime_StaysInTheWindow is the property over many hashes, the edges
// among them. Mutants: widen windowSpan so the window reaches the present; use
// the clock or a length of the hash in the offset.
func TestBlobTime_StaysInTheWindow(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(1, 2))
	hashes := make([]string, 0, 6004)
	hashes = append(hashes, strings.Repeat("0", 40), strings.Repeat("f", 40), strings.Repeat("0", 64), strings.Repeat("f", 64))
	for i := range 6000 {
		raw := make([]byte, []int{20, 32}[i%2])
		for j := range raw {
			raw[j] = byte(rng.UintN(256))
		}
		hashes = append(hashes, hex.EncodeToString(raw))
	}

	seen := make(map[int64]string)
	withNanos := 0
	for _, h := range hashes {
		at, err := blobTime(h)
		require.NoError(t, err, h)
		assert.True(t, inWindowNanos(at.UnixNano()), "%s -> %s", h, at.Format(time.RFC3339Nano))
		assert.Greater(t, time.Since(at), 365*24*time.Hour, h)
		again, err := blobTime(strings.ToUpper(h))
		require.NoError(t, err)
		assert.True(t, at.Equal(again), h)
		if at.Nanosecond() != 0 {
			withNanos++
		}
		// The time reads the first eight bytes alone, so the all-zero and all-f
		// hashes of both lengths share theirs by design.
		if prior, dup := seen[at.UnixNano()]; dup && prior != h[:16] {
			t.Errorf("%s and %s share a time", prior, h[:16])
		}
		seen[at.UnixNano()] = h[:16]
	}
	assert.Greater(t, withNanos, len(hashes)*99/100, "the time keeps its nanoseconds")
}

func TestBlobTime_RefusesWhatIsNoObjectName(t *testing.T) {
	t.Parallel()
	for name, hash := range map[string]string{
		"empty":           "",
		"not hex":         strings.Repeat("g", 40),
		"odd length":      strings.Repeat("a", 39),
		"19 bytes":        strings.Repeat("a", 38),
		"21 bytes":        strings.Repeat("a", 42),
		"33 bytes":        strings.Repeat("a", 66),
		"prefixed":        "0x" + strings.Repeat("a", 38),
		"with whitespace": strings.Repeat("a", 39) + " ",
	} {
		_, err := blobTime(hash)
		assert.Error(t, err, name)
	}
}

func TestDirTime_IsInTheWindow(t *testing.T) {
	t.Parallel()
	assert.True(t, inWindowNanos(dirTime().UnixNano()))
	assert.True(t, windowFrom.Add(1825*24*time.Hour).Equal(dirTime()), "the midpoint of the window")
}

func TestParseIndex_ReadsRecords(t *testing.T) {
	t.Parallel()
	h := strings.Repeat("ab", 20)
	out := "H 100644 " + h + " 0\ta b.go\x00" + "H 100755 " + h + " 0\tbin/tool\x00" + "H 120000 " + h + " 0\tlink\x00" +
		"H 100644 " + h + " 2\tconflict\x00" + "H 100644 " + h + " 0\tnew\nline\x00"
	got, err := parseIndex([]byte(out))
	require.NoError(t, err)
	want := []entry{
		{"H", "100644", h, 0, "a b.go"},
		{"H", "100755", h, 0, "bin/tool"},
		{"H", "120000", h, 0, "link"},
		{"H", "100644", h, 2, "conflict"},
		{"H", "100644", h, 0, "new\nline"},
	}
	assert.Empty(t, cmp.Diff(want, got, cmp.AllowUnexported(entry{})))

	got, err = parseIndex(nil)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestParseIndex_RefusesWhatIsNotARecord(t *testing.T) {
	t.Parallel()
	h := strings.Repeat("ab", 20)
	for name, out := range map[string]string{
		"no tab":          "H 100644 " + h + " 0 a.go\x00",
		"three fields":    "H 100644 " + h + "\ta.go\x00",
		"no tag":          "100644 " + h + " 0\ta.go\x00",
		"five fields":     "H 100644 " + h + " 0 0\ta.go\x00",
		"stage not a int": "H 100644 " + h + " x\ta.go\x00",
		"empty path":      "H 100644 " + h + " 0\t\x00",
		"dot":             "H 100644 " + h + " 0\t.\x00",
		"absolute":        "H 100644 " + h + " 0\t/etc/passwd\x00",
		"parent":          "H 100644 " + h + " 0\t../x\x00",
		"inner parent":    "H 100644 " + h + " 0\ta/../../x\x00",
		"trailing slash":  "H 100644 " + h + " 0\ta/\x00",
	} {
		_, err := parseIndex([]byte(out))
		assert.Error(t, err, name)
	}
}

// TestUndatable_NamesEveryEntryItCannotDate. Mutants: drop the mode test; drop
// the stage test; drop the tag test; admit 120000 or 160000 as a regular file.
func TestUndatable_NamesEveryEntryItCannotDate(t *testing.T) {
	t.Parallel()
	entries := []entry{
		{tag: "H", mode: "100644", path: "ok"},
		{tag: "H", mode: "100755", path: "exec"},
		{tag: "H", mode: "120000", path: "link"},
		{tag: "H", mode: "160000", path: "sub"},
		{tag: "H", mode: "100664", path: "odd"},
		{tag: "M", mode: "100644", stage: 1, path: "base"},
		{tag: "M", mode: "100644", stage: 3, path: "theirs"},
		{tag: "M", mode: "120000", stage: 2, path: "both"},
		{tag: "h", mode: "100644", path: "assumed"},
		{tag: "S", mode: "100644", path: "sparse"},
		{tag: "", mode: "100644", path: "untagged"},
	}
	got := undatable(entries)
	want := []string{
		`"link" has mode 120000, not a regular file`,
		`"sub" has mode 160000, not a regular file`,
		`"odd" has mode 100664, not a regular file`,
		`"base" is at index stage 1`,
		`"theirs" is at index stage 3`,
		`"both" is at index stage 2`,
		`"assumed" is tagged "h", which hides changes to it from the status`,
		`"sparse" is tagged "S", which hides changes to it from the status`,
		`"untagged" is tagged "", which hides changes to it from the status`,
	}
	assert.Equal(t, want, got)
	assert.Empty(t, undatable(entries[:2]))
}

func TestDirtyPaths_ListsEveryRecord(t *testing.T) {
	t.Parallel()
	out := " M a.go\x00M  b go\x00?? c\x00A  d\nx\x00AM e\x00odd\x00\x00"
	assert.Equal(t, []string{` M "a.go"`, `M  "b go"`, `?? "c"`, `A  "d\nx"`, `AM "e"`, `"odd"`}, dirtyPaths([]byte(out)))
	assert.Empty(t, dirtyPaths(nil))
	assert.Equal(t, []string{`"abc"`}, dirtyPaths([]byte("abc")), "a record too short to hold a path")
}

// TestSchedule_DatesFilesByContentAndEveryDirectoryAbove. Mutants: mix the path
// into a file's time; leave out the top level; leave out a directory that
// holds only a directory.
func TestSchedule_DatesFilesByContentAndEveryDirectoryAbove(t *testing.T) {
	t.Parallel()
	one, two := strings.Repeat("11", 20), strings.Repeat("22", 20)
	j, err := schedule([]entry{
		{hash: one, path: "a/b/c.go"},
		{hash: one, path: "a/b/d.go"},
		{hash: two, path: "e.go"},
		{hash: one, path: "z/f.go"},
	})
	require.NoError(t, err)

	assert.Equal(t, []string{".", "a", "a/b", "z"}, j.dirs)
	require.Len(t, j.files, 4)
	for _, i := range []int{1, 3} {
		assert.True(t, j.files[0].at.Equal(j.files[i].at), "one content, one time: %s", j.files[i].path)
	}
	assert.False(t, j.files[0].at.Equal(j.files[2].at), "two contents, two times")
	for _, s := range j.files {
		want, err := blobTime(map[string]string{"a/b/c.go": one, "a/b/d.go": one, "e.go": two, "z/f.go": one}[s.path])
		require.NoError(t, err)
		assert.True(t, want.Equal(s.at), s.path)
	}
}

func TestSchedule_TerminatesAndRefuses(t *testing.T) {
	t.Parallel()
	j, err := schedule(nil)
	require.NoError(t, err)
	assert.Empty(t, j.files)
	assert.Empty(t, j.dirs, "no tracked file, no directory to date")

	j, err = schedule([]entry{{hash: strings.Repeat("11", 20), path: "/abs/f"}})
	require.NoError(t, err, "a path parseIndex would refuse still ends the walk")
	assert.Equal(t, []string{"/", "/abs"}, j.dirs)

	_, err = schedule([]entry{{hash: strings.Repeat("11", 20), path: "ok"}, {hash: "zz", path: "bad name"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"bad name"`)
}

// TestRefusalError_BoundsTheListing. Mutant: list every item; compare against
// the cap with >=.
func TestRefusalError_BoundsTheListing(t *testing.T) {
	t.Parallel()
	items := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("item%02d", i)
		}
		return out
	}

	atCap := (&refusalError{header: "bad", items: items(maxReported)}).Error()
	assert.Equal(t, maxReported+1, strings.Count(atCap, "\n")+1)
	assert.Contains(t, atCap, "item24")
	assert.NotContains(t, atCap, "more")

	over := (&refusalError{header: "bad", items: items(maxReported + 5)}).Error()
	assert.Contains(t, over, "item24")
	assert.NotContains(t, over, "item25")
	assert.True(t, strings.HasSuffix(over, "... and 5 more"), over)
}

// TestExecute_TwoCheckoutsEndIdentical is the command's reason to exist: two
// copies of one commit, made and scrambled differently, end with the same time
// on every file and directory. Mutants: mix the clock into a file's time or the
// directory time; set only the files.
func TestExecute_TwoCheckoutsEndIdentical(t *testing.T) {
	t.Parallel()
	a := newRepo(t)
	a.populate()
	b := &repo{t: t, dir: t.TempDir()}
	a.git("clone", "-q", "--no-hardlinks", a.dir, b.dir)
	scramble(t, a.dir, time.Date(2021, 3, 4, 5, 6, 7, 8, time.UTC))
	scramble(t, b.dir, time.Date(2023, 9, 8, 7, 6, 5, 4, time.UTC))

	codeA, outA, errA := a.run()
	codeB, outB, errB := b.run()
	require.Equal(t, 0, codeA, errA)
	require.Equal(t, 0, codeB, errB)
	assert.Equal(t, "morphic-filetimes: dated 5 files and 5 directories\n", outA)
	assert.Equal(t, outA, outB)

	snapA, snapB := snapshot(t, a.dir), snapshot(t, b.dir)
	assert.Empty(t, cmp.Diff(snapA, snapB))
	for _, p := range []string{".", "pkg", "svc", "svc/inner", "bin"} {
		assert.Equal(t, dirTime().UnixNano(), snapA[p], "directory %q", p)
	}
	for _, p := range []string{"README", "pkg/a.go", "svc/inner/x.go", "svc/inner/same.go", "bin/tool"} {
		assert.True(t, inWindowNanos(snapA[p]), "file %q", p)
	}

	// Content, not path: two files of one content share a time, and the rest differ.
	assert.Equal(t, snapA["pkg/a.go"], snapA["svc/inner/same.go"])
	assert.NotEqual(t, snapA["pkg/a.go"], snapA["README"])

	// The worktree is clean after, so a second run does the same again.
	code, _, errText := a.run()
	require.Equal(t, 0, code, errText)
	assert.Empty(t, cmp.Diff(snapA, snapshot(t, a.dir)))
}

// TestExecute_DatesGitLast holds the root's .git entry to the directory time
// and its dating to the run's last call, so that nothing the command does
// follows it. Mutants: delete the call; move it ahead of the directory loop.
func TestExecute_DatesGitLast(t *testing.T) {
	t.Parallel()
	r := newRepo(t)
	r.populate()
	scramble(t, r.dir, time.Date(2021, 3, 4, 5, 6, 7, 8, time.UTC))
	require.NoError(t, os.Chtimes(filepath.Join(r.dir, ".git"), time.Now(), time.Now()))

	var names []string
	sys := r.system()
	sys.chtimes = func(name string, atime, mtime time.Time) error {
		names = append(names, name)
		return os.Chtimes(name, atime, mtime)
	}
	code, stdout, stderr := runSystem(sys)
	require.Equal(t, 0, code, stderr)

	assert.Equal(t, "morphic-filetimes: dated 5 files and 5 directories\n", stdout)
	info, err := os.Lstat(filepath.Join(r.dir, ".git"))
	require.NoError(t, err)
	assert.Equal(t, dirTime().UnixNano(), info.ModTime().UnixNano())
	require.NotEmpty(t, names)
	assert.Equal(t, onDisk(r.dir, ".git"), names[len(names)-1])
}

// TestExecute_DatesALinkedWorktreesGitFile. In a linked worktree .git is a
// file. Mutant: date .git only when Lstat says it is a directory.
func TestExecute_DatesALinkedWorktreesGitFile(t *testing.T) {
	t.Parallel()
	r := newRepo(t)
	r.populate()
	linked := &repo{t: t, dir: filepath.Join(t.TempDir(), "linked")}
	r.git("worktree", "add", "-q", "-b", "linked", linked.dir)
	info, err := os.Lstat(filepath.Join(linked.dir, ".git"))
	require.NoError(t, err)
	require.True(t, info.Mode().IsRegular(), "a linked worktree's .git is a file")

	code, _, stderr := linked.run()
	require.Equal(t, 0, code, stderr)

	info, err = os.Lstat(filepath.Join(linked.dir, ".git"))
	require.NoError(t, err)
	assert.Equal(t, dirTime().UnixNano(), info.ModTime().UnixNano())
}

// TestExecute_SkipsAnAbsentGit. With GIT_DIR elsewhere the root holds no .git
// entry, so there is nothing to date. Mutant: return the not-exist error.
func TestExecute_SkipsAnAbsentGit(t *testing.T) {
	t.Parallel()
	work, gitDir := t.TempDir(), filepath.Join(t.TempDir(), "repo.git")
	env := append(hermeticEnv(), "GIT_DIR="+gitDir, "GIT_WORK_TREE="+work)
	git := gitIn(work, env, maxGitOutput)
	_, err := git("init", "-q", "-b", "main")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(work, "f"), []byte("f\n"), 0o644))
	_, err = git("add", "-A")
	require.NoError(t, err)
	_, err = git("commit", "-q", "-m", "f")
	require.NoError(t, err)
	_, err = os.Lstat(filepath.Join(work, ".git"))
	require.ErrorIs(t, err, fs.ErrNotExist)

	code, stdout, stderr := runSystem(system{dir: work, git: git, chtimes: os.Chtimes})

	assert.Equal(t, 0, code, stderr)
	assert.Equal(t, "morphic-filetimes: dated 1 files and 1 directories\n", stdout)
}

// TestExecute_ChangedContentChangesTheTime. Mutant: return a constant.
func TestExecute_ChangedContentChangesTheTime(t *testing.T) {
	t.Parallel()
	r := newRepo(t)
	r.populate()
	code, _, errText := r.run()
	require.Equal(t, 0, code, errText)
	before := snapshot(t, r.dir)

	r.write("pkg/a.go", "package a // changed\n")
	r.commit("change")
	code, _, errText = r.run()
	require.Equal(t, 0, code, errText)
	after := snapshot(t, r.dir)

	assert.NotEqual(t, before["pkg/a.go"], after["pkg/a.go"])
	assert.Equal(t, before["README"], after["README"], "an untouched file keeps its time")
	assert.Equal(t, before["."], after["."])
}

// TestExecute_RefusesADirtyWorktree. Mutants: drop the status check; ask for
// untracked files "no"; read the status after the files are dated.
func TestExecute_RefusesADirtyWorktree(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(r *repo)
		names  string
	}{
		{"modified", func(r *repo) { r.write("README", "changed\n") }, `"README"`},
		{"staged", func(r *repo) { r.write("README", "changed\n"); r.git("add", "README") }, `"README"`},
		{"staged and reverted in the tree", func(r *repo) {
			r.write("README", "changed\n")
			r.git("add", "README")
			r.write("README", "readme\n")
		}, `"README"`},
		{"staged new file", func(r *repo) { r.write("fresh", "x\n"); r.git("add", "fresh") }, `"fresh"`},
		{"untracked", func(r *repo) { r.write("stray.txt", "x\n") }, `"stray.txt"`},
		{"untracked in a new directory", func(r *repo) { r.write("newdir/stray.txt", "x\n") }, `"newdir/stray.txt"`},
		{"staged rename", func(r *repo) { r.git("mv", "README", "moved") }, `A  "moved"`},
		{"deleted", func(r *repo) { require.NoError(r.t, os.Remove(filepath.Join(r.dir, "README"))) }, `"README"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newRepo(t)
			r.populate()
			scramble(t, r.dir, time.Date(2022, 2, 2, 2, 2, 2, 2, time.UTC))
			tc.mutate(r)
			before := snapshot(t, r.dir)

			code, stdout, stderr := r.run()

			assert.Equal(t, 1, code)
			assert.Empty(t, stdout)
			assert.Contains(t, stderr, "the worktree is not clean")
			assert.Contains(t, stderr, tc.names)
			assert.Empty(t, cmp.Diff(before, snapshot(t, r.dir)), "no time moved")
		})
	}
}

// TestExecute_LeavesWhatGitIgnores. Mutant: ask git for the ignored files too.
func TestExecute_LeavesWhatGitIgnores(t *testing.T) {
	t.Parallel()
	r := newRepo(t)
	r.write(".gitignore", "*.log\nbuild/\n")
	r.write("README", "readme\n")
	r.commit("ignore")
	r.write("run.log", "x\n")
	r.write("build/out", "x\n")
	scramble(t, r.dir, time.Date(2022, 2, 2, 2, 2, 2, 2, time.UTC))
	before := snapshot(t, r.dir)

	code, stdout, stderr := r.run()

	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "morphic-filetimes: dated 2 files and 1 directories\n", stdout)
	after := snapshot(t, r.dir)
	for _, p := range []string{"run.log", "build", "build/out"} {
		assert.Equal(t, before[p], after[p], "%q is not tracked, so its time stays", p)
	}
	assert.Equal(t, dirTime().UnixNano(), after["."])
}

// TestExecute_RefusesAnEntryItCannotDate. Mutants: drop the mode refusal; drop
// the stage refusal; date before refusing.
func TestExecute_RefusesAnEntryItCannotDate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(r *repo)
		want  string
	}{
		{"a symlink", func(r *repo) {
			require.NoError(r.t, os.Symlink("README", filepath.Join(r.dir, "link")))
			r.commit("link")
		}, `"link" has mode 120000, not a regular file`},
		{"a submodule", func(r *repo) {
			head := strings.TrimSpace(r.git("rev-parse", "HEAD"))
			r.git("update-index", "--add", "--cacheinfo", "160000,"+head+",sub")
		}, `"sub" has mode 160000, not a regular file`},
		{"an assume-unchanged file", func(r *repo) {
			r.git("update-index", "--assume-unchanged", "README")
			r.write("README", "changed behind git's back\n")
		}, `"README" is tagged "h", which hides changes to it from the status`},
		{"a skip-worktree file", func(r *repo) {
			r.git("update-index", "--skip-worktree", "README")
			r.write("README", "changed behind git's back\n")
		}, `"README" is tagged "S", which hides changes to it from the status`},
		{"an unresolved merge", func(r *repo) {
			r.git("checkout", "-q", "-b", "side")
			r.write("README", "side\n")
			r.commit("side")
			r.git("checkout", "-q", "main")
			r.write("README", "main\n")
			r.commit("main")
			_, err := r.gitOut("merge", "side")
			require.Error(r.t, err, "the merge must conflict")
		}, `"README" is at index stage 1`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newRepo(t)
			r.populate()
			tc.setup(r)
			scramble(t, r.dir, time.Date(2022, 2, 2, 2, 2, 2, 2, time.UTC))
			before := snapshot(t, r.dir)

			code, stdout, stderr := r.run()

			assert.Equal(t, 1, code)
			assert.Empty(t, stdout)
			assert.Contains(t, stderr, "cannot be dated")
			assert.Contains(t, stderr, tc.want)
			assert.Empty(t, cmp.Diff(before, snapshot(t, r.dir)), "no time moved")
		})
	}
}

// TestExecute_RefusesWhereItIsNotATopLevel. Mutants: accept a subdirectory;
// accept the inside of .git.
func TestExecute_RefusesWhereItIsNotATopLevel(t *testing.T) {
	t.Parallel()
	r := newRepo(t)
	r.populate()
	scramble(t, r.dir, time.Date(2022, 2, 2, 2, 2, 2, 2, time.UTC))
	before := snapshot(t, r.dir)

	tests := []struct{ name, dir, want string }{
		{"a subdirectory", filepath.Join(r.dir, "pkg"), "not at the worktree's top level: run it from ../"},
		{"the git directory", filepath.Join(r.dir, ".git"), "not inside a git worktree"},
	}
	for _, tc := range tests {
		sys := r.system()
		sys.dir, sys.git = tc.dir, gitIn(tc.dir, hermeticEnv(), maxGitOutput)
		code, stdout, stderr := runSystem(sys)
		assert.Equal(t, 1, code, tc.name)
		assert.Empty(t, stdout, tc.name)
		assert.Contains(t, stderr, tc.want, tc.name)
	}
	assert.Empty(t, cmp.Diff(before, snapshot(t, r.dir)), "no time moved")
}

func TestExecute_RefusesOutsideAWorktree(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	env := append(hermeticEnv(), "GIT_CEILING_DIRECTORIES="+filepath.Dir(dir))
	code, stdout, stderr := runSystem(system{dir: dir, git: gitIn(dir, env, maxGitOutput), chtimes: os.Chtimes})
	assert.Equal(t, 1, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "not in a git worktree: git rev-parse: fatal: not a git repository")
}

// TestExecute_ReportsWhatFailsBeyondTheCommand reaches, through the system's
// seams, the failures a healthy git and file system do not show. Each fails the
// run before or at the step it names, and the step is the one named.
func TestExecute_ReportsWhatFailsBeyondTheCommand(t *testing.T) {
	t.Parallel()
	r := newRepo(t)
	r.populate()
	healthy := r.system()
	boom := errors.New("boom")
	// gitAnswering is the real git except for one verb, which answers out, err.
	gitAnswering := func(verb, out string, err error) system {
		sys := healthy
		sys.git = func(args ...string) ([]byte, error) {
			if args[0] == verb {
				return []byte(out), err
			}
			return healthy.git(args...)
		}
		return sys
	}
	chtimesFailing := func(rel string) system {
		sys := healthy
		sys.chtimes = func(name string, atime, mtime time.Time) error {
			if name == onDisk(r.dir, rel) {
				return boom
			}
			return os.Chtimes(name, atime, mtime)
		}
		return sys
	}

	tests := []struct {
		name string
		sys  system
		want string
	}{
		{"rev-parse fails", gitAnswering("rev-parse", "", boom), "not in a git worktree: boom"},
		{"rev-parse answers nothing", gitAnswering("rev-parse", "", nil), "not inside a git worktree"},
		{"rev-parse says false", gitAnswering("rev-parse", "false\n", nil), "not inside a git worktree"},
		{"ls-files fails", gitAnswering("ls-files", "", boom), "list the index: boom"},
		{"ls-files answers nonsense", gitAnswering("ls-files", "nonsense\x00", nil), `read the index: malformed record "nonsense"`},
		{"ls-files names no object", gitAnswering("ls-files", "H 100644 zz 0\tf\x00", nil), `"f": object name "zz"`},
		{"status fails", gitAnswering("status", "", boom), "read the status: boom"},
		{"a file cannot be dated", chtimesFailing("pkg/a.go"), `date "pkg/a.go": boom`},
		{"a directory cannot be dated", chtimesFailing("svc"), `date directory "svc": boom`},
		{"the top level cannot be dated", chtimesFailing("."), `date directory ".": boom`},
		{"git cannot be dated", chtimesFailing(".git"), `date ".git": boom`},
	}
	for _, tc := range tests {
		code, stdout, stderr := runSystem(tc.sys)
		assert.Equal(t, 1, code, tc.name)
		assert.Empty(t, stdout, tc.name)
		assert.Contains(t, stderr, "morphic-filetimes: ", tc.name)
		assert.Contains(t, stderr, tc.want, tc.name)
	}
}

// TestGitIn_BoundsAndExplains. Mutants: compare the limit with >=; keep reading
// past it; drop git's explanation from the error.
func TestGitIn_BoundsAndExplains(t *testing.T) {
	t.Parallel()
	r := newRepo(t)

	out, err := gitIn(r.dir, hermeticEnv(), 5)("rev-parse", "--git-dir")
	require.NoError(t, err, "five bytes fit a limit of five")
	assert.Equal(t, ".git\n", string(out))

	_, err = gitIn(r.dir, hermeticEnv(), 4)("rev-parse", "--git-dir")
	require.Error(t, err)
	assert.Equal(t, "git rev-parse: output is over 4 bytes", err.Error())

	_, err = gitIn(r.dir, hermeticEnv(), maxGitOutput)("no-such-verb")
	require.Error(t, err)
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	assert.Contains(t, err.Error(), "git no-such-verb: git: 'no-such-verb' is not a git command")
	assert.NotContains(t, err.Error(), "\n")
}

func TestCapture_KeepsTheFirstBytesAndConsumesAll(t *testing.T) {
	t.Parallel()
	c := &capture{limit: 3}
	for _, p := range []string{"ab", "cd", "ef", ""} {
		n, err := c.Write([]byte(p))
		require.NoError(t, err)
		assert.Equal(t, len(p), n)
	}
	assert.Equal(t, "abc", c.String())
	assert.True(t, c.truncated)

	exact := &capture{limit: 3}
	_, err := exact.Write([]byte("abc"))
	require.NoError(t, err)
	assert.False(t, exact.truncated, "filling the limit exactly drops nothing")
}

// TestGitIn_ReportsAMissingGit has no PATH to find git on, so the command
// has no stderr to quote and says what the system said.
func TestGitIn_ReportsAMissingGit(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := gitIn(".", nil, maxGitOutput)("rev-parse")
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), "git rev-parse: exec: "), err.Error())
	assert.ErrorIs(t, err, exec.ErrNotFound)
}

func TestRun_RefusesArguments(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	assert.Equal(t, 2, run([]string{"extra"}, &stdout, &stderr))
	assert.Empty(t, stdout.String())
	assert.Equal(t, usage+"\n", stderr.String())
}

// TestRun_DatesTheCurrentDirectory drives the command as main does, in the
// process's own directory and environment.
func TestRun_DatesTheCurrentDirectory(t *testing.T) {
	useHermeticEnv(t)
	r := newRepo(t)
	r.populate()
	scramble(t, r.dir, time.Date(2022, 2, 2, 2, 2, 2, 2, time.UTC))
	t.Chdir(r.dir)

	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)

	require.Equal(t, 0, code, stderr.String())
	assert.Equal(t, "morphic-filetimes: dated 5 files and 5 directories\n", stdout.String())
	snap := snapshot(t, r.dir)
	assert.Equal(t, dirTime().UnixNano(), snap["."])
	assert.True(t, inWindowNanos(snap["README"]))
}

func TestRun_RefusesWhereThereIsNoWorktree(t *testing.T) {
	useHermeticEnv(t)
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	t.Chdir(dir)

	var stdout, stderr bytes.Buffer
	assert.Equal(t, 1, run(nil, &stdout, &stderr))
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "not in a git worktree")
}

func TestMain_ExitCode(t *testing.T) {
	origArgs, origExit := os.Args, osExit
	t.Cleanup(func() { os.Args, osExit = origArgs, origExit })

	var got int
	osExit = func(code int) { got = code }
	os.Args = []string{"morphic-filetimes", "extra"} // an argument → usage → exit 2

	main()

	assert.Equal(t, 2, got)
}
