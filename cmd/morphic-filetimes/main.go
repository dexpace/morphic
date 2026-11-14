// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	// windowStartUnix is 2000-01-01T00:00:00Z, the first instant of the window
	// every time this command sets lies in.
	windowStartUnix = 946684800

	// windowSpan is the window's length, 3650 days, so it ends at
	// 2009-12-29T00:00:00Z and stays far behind any run's clock.
	windowSpan = 3650 * 24 * time.Hour

	// maxGitOutput bounds what the command reads from git on stdout. The index
	// of a repository this size is a few hundred kilobytes, so the bound is a
	// stop for a runaway git, not a limit anything reaches.
	maxGitOutput = 64 << 20

	// gitTimeout bounds one git call. The calls read an index and a few
	// refs, which take well under a second, so the bound is a stop for a git
	// that hangs, not a limit anything reaches.
	gitTimeout = 2 * time.Minute

	// maxGitStderr bounds the stderr kept to explain a git failure.
	maxGitStderr = 4 << 10

	// maxReported bounds a refusal's listing, as the first screenful is what
	// gets read.
	maxReported = 25

	// hashPrefixBytes is how much of a blob hash the derivation reads: enough
	// for 64 bits, which is uniform in a cryptographic hash.
	hashPrefixBytes = 8

	sha1Bytes   = 20
	sha256Bytes = 32

	// tagCached is the tag git ls-files -v gives an entry nothing hides from
	// status. Assume-unchanged and skip-worktree entries get others.
	tagCached = "H"

	modeFile       = "100644"
	modeExecutable = "100755"

	// gitEntry is the name of the top level's repository entry: a directory,
	// or a file in a linked worktree.
	gitEntry = ".git"
)

const usage = "usage: morphic-filetimes"

// osExit is os.Exit, a package var so tests can drive main without terminating
// the test process.
var osExit = os.Exit

func main() {
	osExit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// system is what the command reaches outside itself through: the worktree's
// directory, git, and the call that sets a time. A test replaces either call to
// reach a failure a real one seldom shows.
type system struct {
	dir     string
	git     func(args ...string) ([]byte, error)
	chtimes func(name string, atime, mtime time.Time) error
}

// run dates the worktree whose top level is the current directory and returns
// the process exit code. It takes no argument.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		emitf(stderr, "%s\n", usage)
		return 2
	}
	return execute(system{dir: ".", git: gitIn(".", nil, maxGitOutput, gitTimeout), chtimes: os.Chtimes}, stdout, stderr)
}

// execute prepares the job and then applies it, so nothing changes unless the
// whole worktree passed every check.
func execute(sys system, stdout, stderr io.Writer) int {
	j, err := prepare(sys)
	if err != nil {
		emitf(stderr, "morphic-filetimes: %v\n", err)
		return 1
	}
	if err := apply(sys, j); err != nil {
		emitf(stderr, "morphic-filetimes: %v\n", err)
		return 1
	}
	emitf(stdout, "morphic-filetimes: dated %d files and %d directories\n", len(j.files), len(j.dirs))
	return 0
}

// entry is one line of the index: git's tag for it, a file's mode, blob hash,
// stage and path.
type entry struct {
	tag        string
	mode, hash string
	stage      int
	path       string
}

// stamp is a file and the time it is to be given.
type stamp struct {
	path string
	at   time.Time
}

// job is everything to set: each file with its time, and each directory, in
// slash form relative to the worktree's top level, which is ".".
type job struct {
	files []stamp
	dirs  []string
}

// refusalError is a worktree the command declines to date: what is wrong, and
// the paths that make it so.
type refusalError struct {
	header string
	items  []string
}

// Error lists at most maxReported items, so a refusal over a broken tree stays
// a screenful.
func (r *refusalError) Error() string {
	var b strings.Builder
	b.WriteString(r.header)
	for _, it := range r.items[:min(len(r.items), maxReported)] {
		b.WriteString("\n  " + it)
	}
	if more := len(r.items) - maxReported; more > 0 {
		fmt.Fprintf(&b, "\n  ... and %d more", more)
	}
	return b.String()
}

// prepare checks the worktree and returns what to set. It reads the index and
// the status and writes nothing. The index is read first, so an unresolved merge
// is reported as one rather than as a dirty file.
func prepare(sys system) (job, error) {
	if err := requireTopLevel(sys); err != nil {
		return job{}, err
	}
	out, err := sys.git("ls-files", "--stage", "-v", "-z")
	if err != nil {
		return job{}, fmt.Errorf("list the index: %w", err)
	}
	entries, err := parseIndex(out)
	if err != nil {
		return job{}, fmt.Errorf("read the index: %w", err)
	}
	if bad := undatable(entries); len(bad) > 0 {
		return job{}, &refusalError{header: "the index holds entries that cannot be dated", items: bad}
	}
	j, err := schedule(entries)
	if err != nil {
		return job{}, err
	}
	// The hash is the index's, so a file that differs from it would get the
	// time of content it does not hold.
	out, err = sys.git("status", "--porcelain=v1", "-z", "--no-renames", "--untracked-files=all")
	if err != nil {
		return job{}, fmt.Errorf("read the status: %w", err)
	}
	if dirty := dirtyPaths(out); len(dirty) > 0 {
		return job{}, &refusalError{header: "the worktree is not clean", items: dirty}
	}
	return j, nil
}

// requireTopLevel refuses a directory that is not the top level of a worktree:
// the index lists paths from the top, and a directory below it would date the
// wrong files.
func requireTopLevel(sys system) error {
	out, err := sys.git("rev-parse", "--is-inside-work-tree", "--show-cdup")
	if err != nil {
		return fmt.Errorf("not in a git worktree: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 || fields[0] != "true" {
		return errors.New("not inside a git worktree")
	}
	if len(fields) > 1 {
		return fmt.Errorf("not at the worktree's top level: run it from %s", fields[1])
	}
	return nil
}

// apply sets every file's time, then every directory's, then the top level's
// .git entry. A directory after its files, as a write to its entries is what
// moves its own time on some systems. .git is last, and dated because Go hashes
// every entry of a directory a test lists, the root among them; any later git
// status moves it again. A root with no .git entry is skipped, and a symlinked
// one has its target dated, not the link, which is out of scope.
func apply(sys system, j job) error {
	for _, s := range j.files {
		if err := sys.chtimes(onDisk(sys.dir, s.path), s.at, s.at); err != nil {
			return fmt.Errorf("date %q: %w", s.path, err)
		}
	}
	at := dirTime()
	for _, d := range j.dirs {
		if err := sys.chtimes(onDisk(sys.dir, d), at, at); err != nil {
			return fmt.Errorf("date directory %q: %w", d, err)
		}
	}
	if err := sys.chtimes(onDisk(sys.dir, gitEntry), at, at); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("date %q: %w", gitEntry, err)
	}
	return nil
}

// onDisk is the file system path of rel, a slash path below the top level dir.
func onDisk(dir, rel string) string {
	return filepath.Join(dir, filepath.FromSlash(rel))
}

// parseIndex reads the output of git ls-files --stage -v -z: one NUL-ended
// "tag mode hash stage<TAB>path" record per entry.
func parseIndex(out []byte) ([]entry, error) {
	var entries []entry
	for rec := range strings.SplitSeq(string(out), "\x00") {
		if rec == "" {
			continue
		}
		meta, p, found := strings.Cut(rec, "\t")
		fields := strings.Fields(meta)
		if !found || len(fields) != 4 {
			return nil, fmt.Errorf("malformed record %q", rec)
		}
		stage, err := strconv.Atoi(fields[3])
		if err != nil {
			return nil, fmt.Errorf("record %q: stage: %w", rec, err)
		}
		if !fs.ValidPath(p) || p == "." {
			return nil, fmt.Errorf("record %q: the path is not valid UTF-8 slash form below the top level", rec)
		}
		entries = append(entries, entry{tag: fields[0], mode: fields[1], hash: fields[2], stage: stage, path: p})
	}
	return entries, nil
}

// undatable names the entries that cannot be dated as the content they hold:
// one at a stage other than 0, an unresolved merge with no single blob, and one
// whose tag is not git's plain "cached", which marks it assume-unchanged or
// skip-worktree, so that status does not vouch that the file matches its blob.
func undatable(entries []entry) []string {
	var bad []string
	for _, e := range entries {
		switch {
		case e.stage != 0:
			bad = append(bad, fmt.Sprintf("%q is at index stage %d", e.path, e.stage))
		case e.tag != tagCached:
			bad = append(bad, fmt.Sprintf("%q is tagged %q, which hides changes to it from the status", e.path, e.tag))
		}
	}
	return bad
}

// dirtyPaths lists, one per record, what git status --porcelain=v1 -z
// --no-renames reports: a staged or unstaged change, or an untracked file that
// is not ignored. Each record is "XY path".
func dirtyPaths(out []byte) []string {
	var dirty []string
	for rec := range strings.SplitSeq(string(out), "\x00") {
		switch {
		case rec == "":
			continue
		case len(rec) > 3 && rec[2] == ' ':
			dirty = append(dirty, fmt.Sprintf("%s %q", rec[:2], rec[3:]))
		default:
			dirty = append(dirty, fmt.Sprintf("%q", rec))
		}
	}
	return dirty
}

// schedule derives the time of every regular file and the set of directories to
// date: every directory above an entry, the top level "." included, once each and
// in sorted order. A symlink or a submodule is no file to date, but its
// directories are. A regular file's hash that is no blob name fails the whole
// schedule.
func schedule(entries []entry) (job, error) {
	var j job
	seen := make(map[string]bool)
	for _, e := range entries {
		// A symlink's own time is out of os.Chtimes's reach and a submodule holds no
		// blob. Left undated, its entry differs per checkout, so a test listing its
		// directory reruns: a miss, never a replay.
		if e.mode == modeFile || e.mode == modeExecutable {
			at, err := blobTime(e.hash)
			if err != nil {
				return job{}, fmt.Errorf("%q: %w", e.path, err)
			}
			j.files = append(j.files, stamp{path: e.path, at: at})
		}
		// A directory seen already has its parents scheduled, so the walk
		// ends there, and path.Dir reaches "." or "/" and then repeats.
		for d := path.Dir(e.path); !seen[d]; d = path.Dir(d) {
			seen[d] = true
			j.dirs = append(j.dirs, d)
		}
	}
	slices.Sort(j.dirs)
	return j, nil
}

// blobTime returns the time to give the file whose index blob has the object
// name hash, in lower or upper case hex, 20 or 32 bytes long. Three properties
// hold, and the tests hold the function to each:
//
//   - It depends on hash alone, never on the path, the clock or the machine.
//   - It lies in [2000-01-01T00:00:00Z, 2009-12-29T00:00:00Z), a window in the
//     past by far more than the 2s before a run that Go's test cache treats as
//     too new to trust, and never in the future.
//   - It carries nanoseconds, since Go hashes a file's time at full precision.
func blobTime(hash string) (time.Time, error) {
	raw, err := hex.DecodeString(hash)
	if err != nil {
		return time.Time{}, fmt.Errorf("object name %q: %w", hash, err)
	}
	if len(raw) != sha1Bytes && len(raw) != sha256Bytes {
		return time.Time{}, fmt.Errorf("object name %q is %d bytes, want %d or %d", hash, len(raw), sha1Bytes, sha256Bytes)
	}
	offset := binary.BigEndian.Uint64(raw[:hashPrefixBytes]) % uint64(windowSpan)
	return inWindow(time.Duration(offset)), nil
}

// dirTime is the one time every directory is given: the window's midpoint.
func dirTime() time.Time {
	return inWindow(windowSpan / 2)
}

// inWindow is the instant offset after the window's start.
func inWindow(offset time.Duration) time.Time {
	return time.Unix(windowStartUnix, 0).UTC().Add(offset)
}

// gitIn returns a function running git in dir with env added to the process's
// environment. It reads at most limit bytes of stdout, ends a git still running
// after timeout, and fails past either, and a failure carries git's first line
// of explanation.
func gitIn(dir string, env []string, limit int, timeout time.Duration) func(args ...string) ([]byte, error) {
	return func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), env...)
		// A git that forked a child holding its pipes outlives its kill, so
		// the call ends once the pipes have had as long again to close.
		cmd.WaitDelay = timeout
		out, errOut := &capture{limit: limit}, &capture{limit: maxGitStderr}
		cmd.Stdout, cmd.Stderr = out, errOut
		if err := cmd.Run(); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, fmt.Errorf("git %s: not done within %v: %w", args[0], timeout, ctxErr)
			}
			detail, _, _ := strings.Cut(strings.TrimSpace(errOut.String()), "\n")
			if detail == "" {
				return nil, fmt.Errorf("git %s: %w", args[0], err)
			}
			return nil, fmt.Errorf("git %s: %s: %w", args[0], detail, err)
		}
		if out.truncated {
			return nil, fmt.Errorf("git %s: output is over %d bytes", args[0], limit)
		}
		return out.buf.Bytes(), nil
	}
}

// capture is a writer that keeps the first limit bytes written to it and notes
// that it dropped the rest. It still accepts them, so the process writing is
// never blocked on a full pipe.
type capture struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

// Write keeps what fits under the limit and reports every byte consumed.
func (c *capture) Write(p []byte) (int, error) {
	n := len(p)
	room := max(c.limit-c.buf.Len(), 0)
	if n > room {
		c.truncated = true
		p = p[:room]
	}
	// A bytes.Buffer's Write never fails.
	_, _ = c.buf.Write(p)
	return n, nil
}

// String is what was kept.
func (c *capture) String() string { return c.buf.String() }

// emitf writes a formatted message to w; write errors on a human-facing stream
// are unactionable and deliberately discarded.
func emitf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}
