package main

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/ir"
)

// childTimeout bounds each child the tests below start: it is killed once the
// time is up, so a defect that keeps one running fails the test rather than
// hanging the suite.
const childTimeout = 2 * time.Minute

// laterSignals is how many signals a test sends, ten milliseconds apart, after
// the one that stopped a run.
const laterSignals = 50

// stoppedBy returns what a run sig stopped reports on stderr.
func stoppedBy(sig os.Signal) string {
	return "morphic: stopped: " + sig.String() + " signal received\n"
}

// stoppedByInterrupt is what a run SIGINT stopped reports on stderr.
var stoppedByInterrupt = stoppedBy(os.Interrupt)

// errInterrupted is the cause signal.NotifyContext gives a context SIGINT
// cancelled, for a test that runs the CLI in this process and so cannot send
// one.
var errInterrupted = errors.New("interrupt signal received")

// interrupted returns a context cancelled as SIGINT cancels the CLI's.
func interrupted(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(errInterrupted)
	return ctx
}

// TestRun_AStopSignalWritesNothing pins what a run a stop signal cancelled
// before it compiled leaves: no document anywhere, an -o destination as it
// was, the signal named on stderr as the one reason, and exit 2. A misuse it
// met on the way is not reported.
func TestRun_AStopSignalWritesNothing(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	tests := []struct {
		name string
		args func(dest string) []string
	}{
		{"compile -o", func(dest string) []string { return []string{"compile", spec, "-o", dest} }},
		{"compile to stdout", func(string) []string { return []string{"compile", spec} }},
		{"compile --explain", func(string) []string { return []string{"compile", spec, "--explain", ""} }},
		{"validate", func(string) []string { return []string{"validate", spec} }},
		{"a misuse", func(string) []string { return []string{"compile"} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			dest := filepath.Join(dir, "ir.json")
			require.NoError(t, os.WriteFile(dest, []byte("PREVIOUS\n"), 0o644))
			var stdout, stderr bytes.Buffer

			code := run(interrupted(t), tt.args(dest), &stdout, &stderr)

			assert.Equal(t, exitFailure, code)
			assert.Equal(t, stoppedByInterrupt, stderr.String())
			assert.Empty(t, stdout.String())
			assert.Equal(t, "PREVIOUS\n", readFile(t, dest))
			assert.Equal(t, []string{"ir.json"}, namesIn(t, dir))
		})
	}
}

// TestRun_ACancelledRunIsAFailure pins a run whose context a caller other than
// a stop signal cancelled: it is stopped as a signal stops it, with exit 2 and
// the cancellation as its reason, and writes nothing.
func TestRun_ACancelledRunIsAFailure(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	dest := filepath.Join(t.TempDir(), "ir.json")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var stdout, stderr bytes.Buffer

	code := run(ctx, []string{"compile", spec, "-o", dest}, &stdout, &stderr)

	assert.Equal(t, exitFailure, code)
	assert.Equal(t, "morphic: stopped: context canceled\n", stderr.String())
	assert.Empty(t, stdout.String())
	assert.NoFileExists(t, dest)
}

// cancelOnWrite is a stream that cancels the run, as SIGINT does, when it is
// written to, and keeps what it is given.
type cancelOnWrite struct {
	cancel context.CancelCauseFunc
	buf    bytes.Buffer
}

func (w *cancelOnWrite) Write(p []byte) (int, error) {
	w.cancel(errInterrupted)
	return w.buf.Write(p)
}

// TestRun_AStopSignalDuringTheReportAbandonsTheDocument pins a signal that
// arrives once the run's output has begun: the diagnostics being written are
// finished, and the document after them is not written, to stdout or to -o's
// destination.
func TestRun_AStopSignalDuringTheReportAbandonsTheDocument(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", unresolvedRefSpec)
	for _, toFile := range []bool{false, true} {
		t.Run(fmt.Sprintf("to a file %t", toFile), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			dest := filepath.Join(dir, "ir.json")
			require.NoError(t, os.WriteFile(dest, []byte("PREVIOUS\n"), 0o644))
			args := []string{"compile", spec}
			if toFile {
				args = append(args, "-o", dest)
			}
			ctx, cancel := context.WithCancelCause(t.Context())
			stderr := &cancelOnWrite{cancel: cancel}
			var stdout bytes.Buffer

			code := run(ctx, args, &stdout, stderr)

			assert.Equal(t, exitFailure, code)
			assert.Contains(t, stderr.buf.String(), "openapi/unresolved-ref", "the report under way is finished")
			assert.True(t, strings.HasSuffix(stderr.buf.String(), stoppedByInterrupt), "stderr: %s", &stderr.buf)
			assert.Empty(t, stdout.String())
			assert.Equal(t, "PREVIOUS\n", readFile(t, dest))
			assert.Equal(t, []string{"ir.json"}, namesIn(t, dir))
		})
	}
}

// readFile returns the content of the file at path.
func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(raw)
}

// namesIn lists the names in dir.
func namesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// The tests below run main in a child, as the CLI's own process, and signal
// it. Each child is this test binary with asMorphic set: TestMain runs main
// in it and nothing else.

// morphic returns a command running the CLI with args in a child, which ctx
// kills when it ends.
func morphic(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, os.Args[0], args...)
	cmd.Env = append(os.Environ(), asMorphic+"=1")
	return cmd
}

// childContext returns the context a test's children run under: its own,
// bounded by childTimeout.
func childContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), childTimeout)
	t.Cleanup(cancel)
	return ctx
}

// specOf writes a spec of n object schemas, each with a long description and
// ten described properties, and returns its path. Its document is several
// times its size, so a large n makes a write that takes a while.
func specOf(t *testing.T, n int) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: Big, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n")
	for i := range n {
		fmt.Fprintf(&b, "    S%d:\n      type: object\n      description: %s\n      properties:\n",
			i, strings.Repeat("d", 200))
		for j := range 10 {
			fmt.Fprintf(&b, "        p%d: {type: string, description: %s}\n", j, strings.Repeat("x", 50))
		}
	}
	return writeFile(t, "spec.yaml", b.String())
}

// exitCodeOf returns the code the child whose Wait returned err exited with,
// failing the test when it exited 0, did not run, or a signal ended it.
func exitCodeOf(t *testing.T, err error) int {
	t.Helper()
	exit, ok := errors.AsType[*exec.ExitError](err)
	require.True(t, ok, "the child exited 0 or did not run: %v", err)
	require.NotEqual(t, -1, exit.ExitCode(), "a signal ended the child: %v", err)
	return exit.ExitCode()
}

// writingToStdout starts cmd, a run of the CLI that writes a document to
// stdout, with stdout a pipe this test reads, and returns once the child has
// written the document's first byte: the run is past every step before the
// write, and a document larger than the pipe holds keeps it in the write until
// the pipe is read. It returns that byte, the pipe, and what the child writes
// to stderr, whole once cmd.Wait returns.
func writingToStdout(t *testing.T, cmd *exec.Cmd) (first []byte, stdout *os.File, stderr *bytes.Buffer) {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, r.Close()) })
	stderr = &bytes.Buffer{}
	cmd.Stdout, cmd.Stderr = w, stderr
	require.NoError(t, cmd.Start())
	require.NoError(t, w.Close())

	first = make([]byte, 1)
	_, err = io.ReadFull(r, first)
	require.NoError(t, err, "the child wrote no document: %s", stderr)
	return first, r, stderr
}

// requireWholeDocument fails the test unless doc is a whole IR document as
// compile writes it to stdout.
func requireWholeDocument(t *testing.T, doc []byte) {
	t.Helper()
	require.True(t, bytes.HasSuffix(doc, []byte("}\n")), "the document was cut short at %d bytes", len(doc))
	var got ir.Document
	require.NoError(t, json.Unmarshal(doc, &got), "the document was cut short")
}

// TestMain_AStopSignalDuringTheOutputWriteKeepsTheDestination is the end-to-end
// check that each signal morphic catches stops -o's write: the destination
// keeps its previous content, the temporary file is gone, and the run reports
// the signal and exits 2 rather than being ended by it.
//
// The child is caught in the write by watching for its temporary file and
// stopping it with SIGSTOP the moment one appears. A write can end before
// that, which is a miss rather than a failure, so the case is tried again.
func TestMain_AStopSignalDuringTheOutputWriteKeepsTheDestination(t *testing.T) {
	t.Parallel()
	spec := specOf(t, 500)
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			t.Parallel()
			const attempts = 3
			for attempt := 1; attempt <= attempts; attempt++ {
				if interruptWrite(t, spec, sig) {
					return
				}
				t.Logf("attempt %d: the write ended before the child was stopped in it", attempt)
			}
			require.Fail(t, "every write ended before the child could be stopped in it", "%d attempts", attempts)
		})
	}
}

// interruptWrite runs one attempt of the case above with sig, and reports
// whether the child was caught in its write. Only a caught child's outcome is
// asserted.
func interruptWrite(t *testing.T, spec string, sig syscall.Signal) bool {
	t.Helper()
	dir := t.TempDir()
	dest := filepath.Join(dir, "ir.json")
	require.NoError(t, os.WriteFile(dest, []byte("PREVIOUS\n"), 0o644))
	cmd := morphic(childContext(t), "compile", spec, "-o", dest)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Start())

	temp, caught := frozenInWrite(t, cmd, dir, dest)
	// The child is continued before it is signalled: on macOS a signal sent
	// to a stopped Go process is not handled until after the write ends.
	require.NoError(t, cmd.Process.Signal(syscall.SIGCONT))
	if !caught {
		require.NoError(t, cmd.Wait(), "a child the test did not signal: %s", &stderr)
		return false
	}
	require.NoError(t, cmd.Process.Signal(sig))
	err := cmd.Wait()

	assert.Equal(t, exitFailure, exitCodeOf(t, err), "stderr: %s", &stderr)
	assert.Equal(t, "PREVIOUS\n", readFile(t, dest), "the destination keeps its previous content")
	assert.Equal(t, []string{"ir.json"}, namesIn(t, dir), "the temporary file %s is removed", temp)
	assert.Equal(t, stoppedBy(sig), stderr.String())
	return true
}

// frozenInWrite waits for the child's temporary file to appear beside dest,
// stops the child with SIGSTOP, and reports whether it was stopped in the
// write: the temporary file still there and dest not yet replaced. A child
// whose write ends first leaves no temporary file to see; it is stopped too,
// so the caller continues it either way.
func frozenInWrite(t *testing.T, cmd *exec.Cmd, dir, dest string) (string, bool) {
	t.Helper()
	ctx := childContext(t)
	for ctx.Err() == nil {
		names := namesIn(t, dir)
		if len(names) > 1 {
			require.NoError(t, cmd.Process.Signal(syscall.SIGSTOP))
			temp := filepath.Join(dir, otherThan(names, filepath.Base(dest)))
			_, err := os.Stat(temp)
			return temp, err == nil && readFile(t, dest) == "PREVIOUS\n"
		}
		if readFile(t, dest) != "PREVIOUS\n" {
			require.NoError(t, cmd.Process.Signal(syscall.SIGSTOP))
			return "", false
		}
		time.Sleep(100 * time.Microsecond)
	}
	require.FailNow(t, "the child wrote nothing within its time")
	return "", false
}

// otherThan returns the first of names that is not name.
func otherThan(names []string, name string) string {
	for _, n := range names {
		if n != name {
			return n
		}
	}
	return ""
}

// TestMain_LaterSignalsDoNotEndARunThatIsStopping pins that the first signal
// releases none: later ones, of every kind morphic catches, are caught too, as
// a copy a shell or a wrapper sends would be, so none cuts the stopping run
// short. The child is blocked writing to stdout until the test reads it, then
// finishes and exits 2 naming the first signal.
func TestMain_LaterSignalsDoNotEndARunThatIsStopping(t *testing.T) {
	t.Parallel()
	cmd := morphic(childContext(t), "compile", specOf(t, 100))
	first, stdout, stderr := writingToStdout(t, cmd)
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	require.NoError(t, cmd.Process.Signal(syscall.SIGINT))
	caught := []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP}
	for i := range laterSignals {
		sig := caught[i%len(caught)]
		require.NoError(t, cmd.Process.Signal(sig), "later signal %d", i+1)
		select {
		case err := <-waited:
			require.FailNow(t, "a later signal ended the run", "signal %d, %s: %v", i+1, sig, err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	rest, err := io.ReadAll(stdout)
	require.NoError(t, err)

	assert.Equal(t, exitFailure, exitCodeOf(t, <-waited), "stderr: %s", stderr)
	assert.Equal(t, stoppedByInterrupt, stderr.String())
	requireWholeDocument(t, append(first, rest...))
}

// TestMain_ASignalTheShellIgnoredStaysIgnored pins that morphic catches no
// signal it was started ignoring, as a script's shell starts a background job
// with SIGINT and nohup a command with SIGHUP: catching one would undo that.
// The run ignores both and ends as if neither had been sent.
func TestMain_ASignalTheShellIgnoredStaysIgnored(t *testing.T) {
	t.Parallel()
	ctx := childContext(t)
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", `trap '' INT HUP; exec "$0" "$@"`,
		os.Args[0], "compile", specOf(t, 100))
	cmd.Env = append(os.Environ(), asMorphic+"=1")
	first, stdout, stderr := writingToStdout(t, cmd)

	require.NoError(t, cmd.Process.Signal(syscall.SIGINT))
	require.NoError(t, cmd.Process.Signal(syscall.SIGHUP))
	rest, err := io.ReadAll(stdout)
	require.NoError(t, err)

	require.NoError(t, cmd.Wait(), "the ignored signals stopped the run: %s", stderr)
	requireWholeDocument(t, append(first, rest...))
}
