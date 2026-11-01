// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

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
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine"
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

// previous is what a test's -o destination holds before the run.
const previous = "PREVIOUS\n"

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

// destination returns a new directory holding one file, ir.json, whose
// content is previous, and that file's path, for a run to write with -o.
func destination(t *testing.T) (dir, dest string) {
	t.Helper()
	dir = t.TempDir()
	dest = filepath.Join(dir, "ir.json")
	require.NoError(t, os.WriteFile(dest, []byte(previous), 0o644))
	return dir, dest
}

// assertKept asserts that the -o destination dest still holds previous, and
// that nothing was left beside it in dir.
func assertKept(t *testing.T, dir, dest string) {
	t.Helper()
	assert.Equal(t, previous, readFile(t, dest), "the destination keeps its previous content")
	assert.Equal(t, []string{"ir.json"}, namesIn(t, dir), "no temporary file is left")
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
			dir, dest := destination(t)
			var stdout, stderr bytes.Buffer

			code := run(interrupted(t), tt.args(dest), &stdout, &stderr)

			assert.Equal(t, exitFailure, code)
			assert.Equal(t, stoppedByInterrupt, stderr.String())
			assert.Empty(t, stdout.String())
			assertKept(t, dir, dest)
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

// capturingCompiler lowers as findingCompiler does, without the finding, so a
// run writing to this process's own stderr writes nothing there. It keeps the
// Err of the context it was handed, to ask later whether that context has
// ended.
type capturingCompiler struct {
	findingCompiler
	ranErr *func() error
}

func (c capturingCompiler) Compile(ctx context.Context, sources []compilers.Source, opts compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	*c.ranErr = ctx.Err
	doc, _, err := c.findingCompiler.Compile(ctx, sources, opts)
	return doc, nil, err
}

// TestMain_KeepsTheSignalWatchUntilItHasExited pins main's watch over the
// context it runs the CLI under: still on when main exits, so a signal then
// cannot end the process by its default action, and ended once an osExit that
// returns, as this test's does, hands control back.
func TestMain_KeepsTheSignalWatchUntilItHasExited(t *testing.T) {
	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	var ranErr func() error
	useEngine(t, engine.WithCompilers(capturingCompiler{ranErr: &ranErr}))
	origArgs, origExit := os.Args, osExit
	t.Cleanup(func() { os.Args, osExit = origArgs, origExit })
	atExit := errors.New("osExit was not called after the compile")
	osExit = func(int) {
		if ranErr != nil {
			atExit = ranErr()
		}
	}
	os.Args = []string{"morphic", "validate", spec}

	main()

	require.NotNil(t, ranErr, "the compiler ran under main's context")
	assert.NoError(t, atExit, "the watch was on when main exited")
	assert.ErrorIs(t, ranErr(), context.Canceled, "the watch ends once osExit returns")
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

// TestRun_AStopSignalDuringTheReportAbandonsTheOutputAfterIt pins a signal
// that arrives once compile's diagnostics have begun: they are finished, and
// what would follow them is not written, whether the document, to stdout or
// to -o's destination, or an --explain report. The run is stopped.
func TestRun_AStopSignalDuringTheReportAbandonsTheOutputAfterIt(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", unresolvedRefSpec)
	tests := []struct {
		name string
		args func(dest string) []string
	}{
		{"the document to stdout", func(string) []string { return []string{"compile", spec} }},
		{"the document to a file", func(dest string) []string { return []string{"compile", spec, "-o", dest} }},
		{"an --explain report", func(string) []string { return []string{"compile", spec, "--explain", ""} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir, dest := destination(t)
			ctx, cancel := context.WithCancelCause(t.Context())
			stderr := &cancelOnWrite{cancel: cancel}
			var stdout bytes.Buffer

			code := run(ctx, tt.args(dest), &stdout, stderr)

			assert.Equal(t, exitFailure, code)
			assert.Contains(t, stderr.buf.String(), "openapi/unresolved-ref", "the report under way is finished")
			assert.True(t, strings.HasSuffix(stderr.buf.String(), stoppedByInterrupt), "stderr: %s", &stderr.buf)
			assert.Empty(t, stdout.String())
			assertKept(t, dir, dest)
		})
	}
}

// TestRun_AStopSignalOnceTheLastOutputHasBegunComesTooLate pins a signal that
// arrives once the run is writing its last output, after the last step that
// looks at the context: the run finishes, and ends exactly as a run no signal
// reached does. That output is validate's report, compile's report of a spec
// that lowered to no document, or compile's document on stdout or its
// --explain report.
func TestRun_AStopSignalOnceTheLastOutputHasBegunComesTooLate(t *testing.T) {
	t.Parallel()
	tiny := writeFile(t, "spec.yaml", testspec.Tiny)
	findings := writeFile(t, "spec.yaml", unresolvedRefSpec)
	unlowered := writeFile(t, "spec.yaml", "swagger: \"2.0\"\ninfo: {title: Old, version: \"1\"}\npaths: {}\n")
	tests := []struct {
		name     string
		args     []string
		onStdout bool // the signal comes with stdout's first write, rather than stderr's
		wantCode int
	}{
		{"validate's report", []string{"validate", findings}, false, exitFindings},
		{"the report of a spec lowering to no document", []string{"compile", unlowered}, false, exitFindings},
		{"the document to stdout", []string{"compile", tiny}, true, 0},
		{"an --explain report", []string{"compile", findings, "--explain", ""}, true, exitFindings},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var wantOut, wantErr bytes.Buffer
			require.Equal(t, tt.wantCode, run(t.Context(), tt.args, &wantOut, &wantErr), "unsignalled: %s", &wantErr)
			ctx, cancel := context.WithCancelCause(t.Context())
			last := &cancelOnWrite{cancel: cancel}
			var other bytes.Buffer
			var stdout, stderr io.Writer = &other, last
			gotOut, gotErr := &other, &last.buf
			if tt.onStdout {
				stdout, stderr = last, &other
				gotOut, gotErr = &last.buf, &other
			}

			code := run(ctx, tt.args, stdout, stderr)

			require.ErrorIs(t, context.Cause(ctx), errInterrupted, "the signal came")
			assert.Equal(t, tt.wantCode, code)
			assert.Equal(t, wantOut.String(), gotOut.String())
			assert.Equal(t, wantErr.String(), gotErr.String())
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
// in it and nothing else. Most hold the child in a write to a pipe the test
// has not read, so that the signal reaches it at a step the test chose. The
// one that signals -o's write cannot hold it there, and judges each attempt by
// what the child left.

// caughtSignals returns the signals morphic catches. They are spelled here
// rather than read from stopSignals, so that one left out of that list fails
// the tests below rather than shrinking them.
func caughtSignals() []syscall.Signal {
	return []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP}
}

// skipIfIgnored skips t when this process ignores sig. A child inherits the
// ignore, which morphic keeps, as TestMain_AnIgnoredSIGINTOrSIGHUPStaysIgnored
// pins, so sig cannot reach a run in it. A shell without job control starts a
// background job ignoring SIGINT, and nohup a command ignoring SIGHUP.
func skipIfIgnored(t *testing.T, sig os.Signal) {
	t.Helper()
	if signal.Ignored(sig) {
		t.Skipf("this process ignores %s, so a child it starts does too", sig)
	}
}

// firstOf returns the index in sigs of the signal to send first of several:
// the lowest-numbered one this process does not ignore. The kernel and then Go
// hand a process its pending signals lowest-numbered first, so one sent after
// it and pending beside it does not overtake it. SIGTERM, which sigs must
// hold, is never passed over, since Go keeps no inherited ignore of it.
func firstOf(t *testing.T, sigs []syscall.Signal) int {
	t.Helper()
	first := slices.Index(sigs, syscall.SIGTERM)
	require.NotEqual(t, -1, first, "SIGTERM is among the signals")
	for i, sig := range sigs {
		if sig < sigs[first] && !signal.Ignored(sig) {
			first = i
		}
	}
	return first
}

// morphic returns a command running the CLI with args in a child, which the
// test's childContext kills when it ends.
func morphic(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	return command(t, os.Args[0], args...)
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

// unmodeled is the code of the diagnostic each of reportingSpec's schemas
// draws.
const unmodeled = "openapi/unknown-schema-keyword"

// reportingSpec writes a spec of n object schemas, each carrying one long
// keyword no schema model has, and returns its path. Each keyword draws an info
// diagnostic naming it twice, so the spec compiles quickly to a large report,
// none of which reaches --fail-on: a run of it that finishes exits 0.
func reportingSpec(t *testing.T, n int) string {
	t.Helper()
	keyword := strings.Repeat("k", 1000)
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: Report, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n")
	for i := range n {
		fmt.Fprintf(&b, "    S%d: {type: object, %s: %d}\n", i, keyword, i)
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

// pipeHolds bounds what a pipe takes before a write to it blocks: sixteen
// pages, Linux's default, is 1 MiB on a kernel with 64 KiB pages, and macOS
// grows a pipe to 64 KiB at most.
const pipeHolds = 1 << 20

// heldWrite is a child held in a write to a pipe: it has written first there,
// and the test reads nothing more until it has signalled the child.
type heldWrite struct {
	first []byte
	pipe  *os.File
}

// holdIn starts cmd with stream, its Stdout or its Stderr, a pipe this test
// reads, and returns once the child has written its first byte there. A child
// writing more than pipeHolds there is then past every step before that write
// and short of every step after it, until the test reads the rest.
func holdIn(t *testing.T, cmd *exec.Cmd, stream *io.Writer) heldWrite {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, r.Close()) })
	*stream = w
	require.NoError(t, cmd.Start())
	require.NoError(t, w.Close())

	first := make([]byte, 1)
	_, err = io.ReadFull(r, first)
	require.NoError(t, err, "the child wrote nothing there")
	return heldWrite{first: first, pipe: r}
}

// drain reads the rest of the held write, until the child closes the pipe, and
// returns the whole of it. It fails the test when the write was too small to
// have held the child, which could then have finished it before the signal.
func (h heldWrite) drain(t *testing.T) string {
	t.Helper()
	rest, err := io.ReadAll(h.pipe)
	require.NoError(t, err)
	require.Greater(t, len(rest), pipeHolds, "the write was too small to hold the child")
	return string(h.first) + string(rest)
}

// lastLine returns the last line of s, for a message about a report too large
// to show whole.
func lastLine(s string) string {
	s = strings.TrimSuffix(s, "\n")
	return s[strings.LastIndexByte(s, '\n')+1:]
}

// requireWholeDocument fails the test unless doc is a whole IR document as
// compile writes it, to stdout or to a file.
func requireWholeDocument(t *testing.T, doc []byte) {
	t.Helper()
	require.True(t, bytes.HasSuffix(doc, []byte("}\n")), "the document was cut short at %d bytes", len(doc))
	var got ir.Document
	require.NoError(t, json.Unmarshal(doc, &got), "the document was cut short")
}

// writeAttempts bounds the children
// TestMain_AStopSignalDuringTheOutputWriteKeepsTheDestination starts for one
// signal. One whose write ends before the signal is handled is a miss.
const writeAttempts = 10

// TestMain_AStopSignalDuringTheOutputWriteKeepsTheDestination is the end-to-end
// check that each signal morphic catches stops -o's write: the destination
// keeps its previous content, the temporary file is gone, and the run reports
// the signal and exits 2 rather than being ended by it.
//
// The child is signalled the moment its temporary file appears. A write can
// still end before the signal is handled, and the run then finishes as though
// no signal had come, which is asserted too. That is a miss, so the case is
// tried again.
func TestMain_AStopSignalDuringTheOutputWriteKeepsTheDestination(t *testing.T) {
	t.Parallel()
	spec := specOf(t, 500)
	for _, sig := range caughtSignals() {
		t.Run(sig.String(), func(t *testing.T) {
			t.Parallel()
			skipIfIgnored(t, sig)
			for attempt := 1; attempt <= writeAttempts; attempt++ {
				if signalDuringWrite(t, spec, sig) {
					return
				}
				t.Logf("attempt %d: the write ended before the signal could stop it", attempt)
			}
			require.Fail(t, "every write ended before its signal could stop it", "%d attempts", writeAttempts)
		})
	}
}

// signalDuringWrite runs one attempt of the case above with sig, and reports
// whether the signal stopped the write. Whichever happened is asserted, and
// the destination says which it was: a stopped run leaves it as it was, and a
// run that replaced it finished.
func signalDuringWrite(t *testing.T, spec string, sig syscall.Signal) bool {
	t.Helper()
	dir, dest := destination(t)
	written, err := os.Stat(dest)
	require.NoError(t, err)
	cmd := morphic(t, "compile", spec, "-o", dest)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Start())
	var waited error
	exited := make(chan struct{})
	go func() { waited = cmd.Wait(); close(exited) }()

	if writing(t, dir, dest, written, exited) {
		// A child that saw no signal yet can finish and exit before this one.
		if err := cmd.Process.Signal(sig); !errors.Is(err, os.ErrProcessDone) {
			require.NoError(t, err)
		}
	}
	<-exited

	if readFile(t, dest) == previous {
		assert.Equal(t, exitFailure, exitCodeOf(t, waited), "stderr: %s", &stderr)
		assert.Equal(t, stoppedBy(sig), stderr.String())
		assertKept(t, dir, dest)
		return true
	}
	require.NoError(t, waited, "a run that replaced its destination finished: %s", &stderr)
	assert.Empty(t, stderr.String(), "a run that finished reports no stop")
	assert.Equal(t, []string{"ir.json"}, namesIn(t, dir), "no temporary file is left")
	requireWholeDocument(t, []byte(readFile(t, dest)))
	return false
}

// writing waits until dir holds a temporary file beside dest, and reports
// whether one appeared while the write was under way. It reports false once
// dest is not the file written, the test having missed the whole write, or
// once exited is closed.
func writing(t *testing.T, dir, dest string, written os.FileInfo, exited <-chan struct{}) bool {
	t.Helper()
	ctx := childContext(t)
	for ctx.Err() == nil {
		if len(namesIn(t, dir)) > 1 {
			return true
		}
		now, err := os.Stat(dest)
		require.NoError(t, err)
		if !os.SameFile(written, now) {
			return false
		}
		select {
		case <-exited:
			return false
		case <-time.After(100 * time.Microsecond):
		}
	}
	require.FailNow(t, "the child wrote nothing within its time")
	return false
}

// TestMain_LaterSignalsDoNotEndARunThatIsStopping pins that the first signal
// releases none: later ones, of every kind morphic catches, are caught too, as
// a copy a shell or a wrapper sends would be, so none cuts the stopping run
// short. The child is held in its report, which a run finishes before it
// stops short of the document, and then exits 2 naming the first signal, with
// -o's destination as it was.
func TestMain_LaterSignalsDoNotEndARunThatIsStopping(t *testing.T) {
	t.Parallel()
	const schemas = 1000
	dir, dest := destination(t)
	cmd := morphic(t, "compile", reportingSpec(t, schemas), "-o", dest)
	report := holdIn(t, cmd, &cmd.Stderr)
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	// The later signals begin with a copy of the first, so the first kind has
	// ten milliseconds to arrive before another is sent.
	caught := caughtSignals()
	start := firstOf(t, caught)
	first := caught[start]
	require.NoError(t, cmd.Process.Signal(first))
	for i := range laterSignals {
		sig := caught[(start+i)%len(caught)]
		require.NoError(t, cmd.Process.Signal(sig), "later signal %d", i+1)
		select {
		case err := <-waited:
			require.FailNow(t, "a later signal ended the run", "signal %d, %s: %v", i+1, sig, err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	all := report.drain(t)

	assert.Equal(t, exitFailure, exitCodeOf(t, <-waited), "stderr ends: %s", lastLine(all))
	assert.Equal(t, schemas, strings.Count(all, unmodeled), "the report under way is finished")
	assert.True(t, strings.HasSuffix(all, stoppedBy(first)), "stderr ends: %s", lastLine(all))
	assertKept(t, dir, dest)
}

// TestMain_AStopSignalDuringTheStdoutWriteComesTooLate pins a signal that
// arrives once the document has begun to reach stdout, past the run's last
// look at its context: of whichever kind morphic catches, and however many,
// it is too late to stop the run, which finishes the document and exits as
// though none had come.
func TestMain_AStopSignalDuringTheStdoutWriteComesTooLate(t *testing.T) {
	t.Parallel()
	cmd := morphic(t, "compile", specOf(t, 200))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	doc := holdIn(t, cmd, &cmd.Stdout)

	for _, sig := range caughtSignals() {
		require.NoError(t, cmd.Process.Signal(sig))
	}
	all := doc.drain(t)

	require.NoError(t, cmd.Wait(), "stderr: %s", &stderr)
	assert.Empty(t, stderr.String())
	requireWholeDocument(t, []byte(all))
}

// TestMain_AStopSignalAfterTheRunComesTooLate pins that morphic exits with
// the signals it catches still caught: one that comes once the run has
// returned can no longer stop it, and does not end the process instead. The
// child sends it to itself as it exits, through signalAtExit, which no test
// could time from outside, and exits as though it had not come.
func TestMain_AStopSignalAfterTheRunComesTooLate(t *testing.T) {
	t.Parallel()
	spec := writeFile(t, "spec.yaml", testspec.Tiny)
	for _, sig := range caughtSignals() {
		t.Run(sig.String(), func(t *testing.T) {
			t.Parallel()
			skipIfIgnored(t, sig)
			cmd := morphic(t, "compile", spec)
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%d", signalAtExit, sig))
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr

			require.NoError(t, cmd.Run(), "stderr: %s", &stderr)
			assert.Empty(t, stderr.String())
			requireWholeDocument(t, stdout.Bytes())
		})
	}
}

// TestMain_AnIgnoredSIGINTOrSIGHUPStaysIgnored pins that morphic catches
// neither SIGINT nor SIGHUP when it was started ignoring them, as a script's
// shell starts a background job with SIGINT and nohup a command with SIGHUP:
// catching one would undo that. The child is held in its report, where a
// caught signal would stop the run short of the document, and ends as if
// neither had been sent.
func TestMain_AnIgnoredSIGINTOrSIGHUPStaysIgnored(t *testing.T) {
	t.Parallel()
	const schemas = 1000
	cmd := command(t, "/bin/sh", "-c", `trap '' INT HUP; exec "$0" "$@"`,
		os.Args[0], "compile", reportingSpec(t, schemas))
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	report := holdIn(t, cmd, &cmd.Stderr)

	require.NoError(t, cmd.Process.Signal(syscall.SIGINT))
	require.NoError(t, cmd.Process.Signal(syscall.SIGHUP))
	all := report.drain(t)

	require.NoError(t, cmd.Wait(), "the ignored signals stopped the run: %s", lastLine(all))
	assert.Equal(t, schemas, strings.Count(all, unmodeled), "the report is whole")
	requireWholeDocument(t, stdout.Bytes())
}
