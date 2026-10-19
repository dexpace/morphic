package canary_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// childEnv names the child a re-run of this test binary runs in place of its
// tests, and inputEnv the file holding the document the child reads.
const (
	childEnv = "MORPHIC_CANARY_CHILD"
	inputEnv = "MORPHIC_CANARY_INPUT"
)

// What a child prints for probe to read, each opening a line of its own: the
// report of a child that returned, quoted, and the goroutines alive when a
// child crossed memoryCeiling.
const (
	reportMark = "canary-report: "
	memoryMark = "canary-memory: "
)

// memoryExit is the status a child exits with once it crosses memoryCeiling,
// distinct from the 2 a fatal error or an unrecovered panic exits with.
const memoryExit = 3

// childStack bounds a child's stack, so a recursion without end overflows in
// a fraction of a second rather than after a gigabyte of stack.
const childStack = 64 << 20

// memoryCeiling is how much memory a child's runtime may hold before the
// child is stopped as exhausting it: several times what any input here that
// ends takes, so only one that grows without end crosses it.
const memoryCeiling = 256 << 20

// childTimeout bounds one child. Every input here ends within a second or two
// at the pinned version, even under the race detector, so only a child that
// hangs waits it out. It is short so that a canary whose every child hangs
// still fails with its own message inside the gate's -timeout.
const childTimeout = 10 * time.Second

// watchInterval is how often a child reads its memory.
const watchInterval = time.Millisecond

// outcome is how a child ended.
type outcome string

const (
	returned   outcome = "returned"
	overflowed outcome = "overflowed its stack"
	panicked   outcome = "panicked where no recover reaches"
	exhausted  outcome = "exhausted its memory"
	hung       outcome = "hung"
)

// result is how one child ended and what it said: a returned child's report,
// a panic's message, or the goroutines alive at exhaustion. Output is all it
// printed, for a failure message.
type result struct {
	outcome outcome
	report  string
	output  string
}

// child is a call into the library a test cannot make in its own process,
// because it may end that process. It returns what it found, as text.
type child func(ctx context.Context, input []byte) string

// children returns the calls a child can run, by name.
func children() map[string]child {
	return map[string]child{
		"resolve":    resolveAll,
		"targets":    resolveTargets,
		"unmarshal":  unmarshal,
		"goroutines": goroutinesStarted,
		"alive":      goroutinesAlive,
		"orders":     unknownKeyOrders,
		"overlay":    applyOverlay,
		// Two whose ending is known, for the tests of probe itself.
		"panic here":      panicHere,
		"panic elsewhere": panicElsewhere,
	}
}

// panicHere panics on the child's own goroutine, where recovering reaches it.
func panicHere(context.Context, []byte) string {
	panic("here")
}

// panicElsewhere panics on a goroutine of its own, where nothing reaches it,
// and waits for the panic to end the process.
func panicElsewhere(context.Context, []byte) string {
	go func() { panic("elsewhere") }()
	time.Sleep(childTimeout)
	return "the panic did not end the process"
}

// probe runs the child name over input in a re-run of this test binary, with
// env added to its environment, and reads how it ended. An ending no outcome
// names fails the test.
func probe(t *testing.T, name, input string, env ...string) result {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.yaml")
	require.NoError(t, os.WriteFile(path, []byte(input), 0o600))

	ctx, cancel := context.WithTimeout(t.Context(), childTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0])
	cmd.Env = append(append(os.Environ(), env...), childEnv+"="+name, inputEnv+"="+path)
	out, err := cmd.CombinedOutput()

	code := 0
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		code = exit.ExitCode()
	} else if err != nil {
		require.NoError(t, ctx.Err(), "the child did not finish within %s", childTimeout)
		require.NoError(t, err, "the child did not start")
	}
	r, ok := classify(string(out), code, ctx.Err() != nil)
	require.True(t, ok, "child %q ended in a way no outcome names (%v):\n%.2000s", name, err, out)
	return r
}

// classify reads how a child ended from what it printed, the status it exited
// with, and whether its timeout fired first. It reports false for an ending no
// outcome names, such as a fatal error other than a stack overflow.
func classify(out string, code int, timedOut bool) (result, bool) {
	r := result{output: out}
	if timedOut {
		r.outcome = hung
		return r, true
	}

	var ok bool
	switch {
	case code == 0:
		r.outcome = returned
		r.report, ok = quotedAfter(out, reportMark)
	case code == memoryExit:
		r.outcome = exhausted
		r.report, ok = lineAfter(out, memoryMark)
	case code == 2 && strings.Contains(out, "fatal error: stack overflow"):
		r.outcome, ok = overflowed, true
	case code == 2:
		r.outcome = panicked
		r.report, ok = lineAfter(out, "panic: ")
	}
	return r, ok
}

// lineAfter returns the rest of the first line of out that opens with mark.
func lineAfter(out, mark string) (string, bool) {
	for line := range strings.Lines(out) {
		if rest, ok := strings.CutPrefix(line, mark); ok {
			return strings.TrimSpace(rest), true
		}
	}
	return "", false
}

// quotedAfter returns the Go-quoted string the first line opening with mark
// carries, unquoted.
func quotedAfter(out, mark string) (string, bool) {
	quoted, ok := lineAfter(out, mark)
	if !ok {
		return "", false
	}
	s, err := strconv.Unquote(quoted)
	return s, err == nil
}

// runChild is the child's side of probe: it runs the child name over the file
// at path on a stack of childStack, under a watch for memoryCeiling, prints
// its report and returns the status to exit with.
func runChild(name, path string) int {
	run, ok := children()[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "canary: no child %q\n", name)
		return 1
	}
	input, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "canary: %v\n", err)
		return 1
	}
	debug.SetMaxStack(childStack)
	go watchMemory()
	fmt.Printf("%s%q\n", reportMark, recovering(run, input))
	return 0
}

// recovering runs c over input, reporting a panic it recovers in this
// goroutine as what c found. A panic that still ends the process happened
// where no recover in the caller's goroutine reaches.
func recovering(c child, input []byte) (report string) {
	defer func() {
		if r := recover(); r != nil {
			report = fmt.Sprintf("recovered panic: %v", r)
		}
	}()
	return c(context.Background(), input)
}

// watchMemory ends the process with memoryExit once the runtime holds more
// than memoryCeiling, printing how many goroutines are alive. It reads for
// childTimeout at most, by which time probe has stopped the child.
func watchMemory() {
	sample := []metrics.Sample{{Name: "/memory/classes/total:bytes"}}
	for range childTimeout / watchInterval {
		metrics.Read(sample)
		if sample[0].Value.Uint64() > memoryCeiling {
			fmt.Printf("%s%d\n", memoryMark, runtime.NumGoroutine())
			os.Exit(memoryExit)
		}
		time.Sleep(watchInterval)
	}
}
