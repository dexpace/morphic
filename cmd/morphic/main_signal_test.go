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

// laterSignals bounds how many signals a test sends a child that did not stop
// at the first, ten milliseconds apart.
const laterSignals = 500

// stoppedByInterrupt is what a run SIGINT stopped reports on stderr.
const stoppedByInterrupt = "morphic: stopped: interrupt signal received\n"

// stoppedBy returns a context cancelled as a stop signal cancels the CLI's,
// with sig as its cause.
func stoppedBy(t *testing.T, sig os.Signal) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(&signalled{sig: sig})
	return ctx
}

// TestRun_AStopSignalWritesNothing pins what a run a stop signal cancelled
// before it compiled leaves: no document anywhere, an -o destination as it
// was, the signal named on stderr as the one reason, and the status a shell
// gives a process SIGINT ended. A misuse it met on the way is not reported.
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

			code := run(stoppedBy(t, os.Interrupt), tt.args(dest), &stdout, &stderr)

			assert.Equal(t, 130, code)
			assert.Equal(t, stoppedByInterrupt, stderr.String())
			assert.Empty(t, stdout.String())
			assert.Equal(t, "PREVIOUS\n", readFile(t, dest))
			assert.Equal(t, []string{"ir.json"}, namesIn(t, dir))
		})
	}
}

// TestRun_ACancelledRunIsAFailure pins a run whose context a caller other than
// a stop signal cancelled: it is not the spec's fault, so it fails with exit 2
// and the cancellation as its reason, and writes nothing.
func TestRun_ACancelledRunIsAFailure(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	dest := filepath.Join(t.TempDir(), "ir.json")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var stdout, stderr bytes.Buffer

	code := run(ctx, []string{"compile", spec, "-o", dest}, &stdout, &stderr)

	assert.Equal(t, exitFailure, code)
	assert.Contains(t, stderr.String(), "context canceled")
	assert.NoFileExists(t, dest)
}

// cancelOnWrite is a stream that cancels the run, as SIGINT does, when it is
// written to, and keeps what it is given.
type cancelOnWrite struct {
	cancel context.CancelCauseFunc
	buf    bytes.Buffer
}

func (w *cancelOnWrite) Write(p []byte) (int, error) {
	w.cancel(&signalled{sig: os.Interrupt})
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

			assert.Equal(t, 130, code)
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

// signalOf returns the signal that ended the child whose Wait returned err,
// failing the test when the child exited instead.
func signalOf(t *testing.T, err error) syscall.Signal {
	t.Helper()
	exit, ok := errors.AsType[*exec.ExitError](err)
	require.True(t, ok, "the child exited cleanly or did not run: %v", err)
	status, ok := exit.Sys().(syscall.WaitStatus)
	require.True(t, ok, "no wait status: %T", exit.Sys())
	require.True(t, status.Signaled(), "the child exited with %d rather than being ended by a signal", status.ExitStatus())
	return status.Signal()
}

// writingToStdout starts cmd, a run of the CLI that writes a document to
// stdout, with stdout a pipe this test reads, and returns once the child has
// written the document's first byte: the run is past every step before the
// write, and a document larger than the pipe holds keeps it in the write until
// the pipe is read. It returns that byte and the pipe.
func writingToStdout(t *testing.T, cmd *exec.Cmd) (first []byte, stdout *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, r.Close()) })
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = w, &stderr
	require.NoError(t, cmd.Start())
	require.NoError(t, w.Close())

	first = make([]byte, 1)
	_, err = io.ReadFull(r, first)
	require.NoError(t, err, "the child wrote no document: %s", &stderr)
	return first, r
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
// check that a signal stops -o's write: the destination keeps its previous
// content, the temporary file is gone, the run reports the signal, and the
// process ends by it.
//
// The child is caught in the write by watching for its temporary file and
// stopping it with SIGSTOP the moment one appears. A write can end before
// that, which is a miss rather than a failure, so the case is tried again.
func TestMain_AStopSignalDuringTheOutputWriteKeepsTheDestination(t *testing.T) {
	t.Parallel()
	spec := specOf(t, 500)
	const attempts = 3
	for attempt := 1; attempt <= attempts; attempt++ {
		if interruptWrite(t, spec) {
			return
		}
		t.Logf("attempt %d: the write ended before the child was stopped in it", attempt)
	}
	require.Fail(t, "every write ended before the child could be stopped in it", "%d attempts", attempts)
}

// interruptWrite runs one attempt of the case above, and reports whether the
// child was caught in its write. Only a caught child's outcome is asserted.
func interruptWrite(t *testing.T, spec string) bool {
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
	require.NoError(t, cmd.Process.Signal(syscall.SIGINT))
	err := cmd.Wait()

	assert.Equal(t, syscall.SIGINT, signalOf(t, err), "stderr: %s", &stderr)
	assert.Equal(t, "PREVIOUS\n", readFile(t, dest), "the destination keeps its previous content")
	assert.Equal(t, []string{"ir.json"}, namesIn(t, dir), "the temporary file %s is removed", temp)
	assert.True(t, strings.HasSuffix(stderr.String(), stoppedByInterrupt), "stderr: %s", &stderr)
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

// TestMain_AStopSignalEndsTheProcessByItself pins, for each signal morphic
// catches, that the signal is caught rather than ending the process at once,
// and that the process still ends by it once the run has stopped. The child is
// blocked writing to stdout, which a signal does not interrupt, so it finishes
// the document first: a whole document shows the signal was caught.
func TestMain_AStopSignalEndsTheProcessByItself(t *testing.T) {
	t.Parallel()
	spec := specOf(t, 100)
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			t.Parallel()
			cmd := morphic(childContext(t), "compile", spec)
			first, stdout := writingToStdout(t, cmd)

			require.NoError(t, cmd.Process.Signal(sig))
			rest, err := io.ReadAll(stdout)
			require.NoError(t, err)

			assert.Equal(t, sig, signalOf(t, cmd.Wait()))
			requireWholeDocument(t, append(first, rest...))
		})
	}
}

// TestMain_ASecondSignalEndsARunThatHasNotStopped pins the way out of a run
// that does not stop: the first signal is caught, and releases the signals,
// so a later one ends the process at once. The child is blocked writing to
// stdout, which nothing here reads, so it never stops on its own.
func TestMain_ASecondSignalEndsARunThatHasNotStopped(t *testing.T) {
	t.Parallel()
	cmd := morphic(childContext(t), "compile", specOf(t, 100))
	writingToStdout(t, cmd)
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	require.NoError(t, cmd.Process.Signal(syscall.SIGINT))
	select {
	case err := <-waited:
		require.FailNow(t, "the first signal ended the process", "%v", err)
	case <-time.After(settle):
	}
	// The first signal is released as soon as it is caught; sending until one
	// lands keeps the test from depending on how soon that is. A child the last
	// one ended may already be gone, which the wait below then reports.
	var err error
	for range laterSignals {
		if sent := cmd.Process.Signal(syscall.SIGINT); sent != nil {
			require.ErrorIs(t, sent, os.ErrProcessDone)
		}
		select {
		case err = <-waited:
		case <-time.After(10 * time.Millisecond):
			continue
		}
		break
	}

	require.Error(t, err, "no later signal ended the process")
	assert.Equal(t, syscall.SIGINT, signalOf(t, err))
}

// TestMain_ASignalTheShellIgnoredStaysIgnored pins that morphic catches no
// signal it was started ignoring, as a shell starts a background job with
// SIGINT and nohup a command with SIGHUP: catching one would undo that. The
// run ignores both and ends as if neither had been sent.
func TestMain_ASignalTheShellIgnoredStaysIgnored(t *testing.T) {
	t.Parallel()
	ctx := childContext(t)
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", `trap '' INT HUP; exec "$0" "$@"`,
		os.Args[0], "compile", specOf(t, 100))
	cmd.Env = append(os.Environ(), asMorphic+"=1")
	first, stdout := writingToStdout(t, cmd)

	require.NoError(t, cmd.Process.Signal(syscall.SIGINT))
	require.NoError(t, cmd.Process.Signal(syscall.SIGHUP))
	rest, err := io.ReadAll(stdout)
	require.NoError(t, err)

	require.NoError(t, cmd.Wait(), "the ignored signals stopped the run")
	requireWholeDocument(t, append(first, rest...))
}
