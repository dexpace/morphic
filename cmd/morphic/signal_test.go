package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tests here change how this process handles signals, which is
// process-wide, so none of them runs in parallel. The one signal they send
// themselves is SIGUSR1, which a Go program that has not asked for it ignores,
// so one that arrives with no test watching does no harm.

// arrival bounds the wait for a signal a test sent itself, which takes far
// less, so only a failing test waits it out.
const arrival = 10 * time.Second

// settle is how long a test waits to show a signal it sent itself had no
// effect. A defect that shows later passes, so it only has to be long enough
// that one rarely does.
const settle = 200 * time.Millisecond

// signalSelf sends this process SIGUSR1, the signal the watches here watch.
func signalSelf(t *testing.T) {
	t.Helper()
	require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGUSR1))
}

// TestCatchable_LeavesOutAnIgnoredSignal pins that a signal the process
// ignores is not caught, since catching it would stop ignoring it, and that
// the list handed in is left as it was.
func TestCatchable_LeavesOutAnIgnoredSignal(t *testing.T) {
	signal.Ignore(syscall.SIGUSR2)
	t.Cleanup(func() { signal.Reset(syscall.SIGUSR2) })
	sigs := []os.Signal{syscall.SIGUSR1, syscall.SIGUSR2}

	got := catchable(sigs)

	assert.Equal(t, []os.Signal{syscall.SIGUSR1}, got)
	assert.Equal(t, []os.Signal{syscall.SIGUSR1, syscall.SIGUSR2}, sigs, "the caller's list is not edited")
}

// TestWatchSignals_TheFirstSignalCancelsTheContext pins the watch's one job:
// a watched signal cancels the context with a cause naming it, which stop then
// leaves in place.
func TestWatchSignals_TheFirstSignalCancelsTheContext(t *testing.T) {
	ctx, stop := watchSignals(t.Context(), []os.Signal{syscall.SIGUSR1})

	signalSelf(t)

	select {
	case <-ctx.Done():
	case <-time.After(arrival):
		stop()
		require.FailNow(t, "the watched signal did not cancel the context")
	}
	stop()
	assert.ErrorContains(t, context.Cause(ctx), syscall.SIGUSR1.String())
}

// TestWatchSignals_StopEndsAWatchThatCaughtNothing pins that stop cancels the
// context with no signal as its cause when none came, and that calling it
// again does nothing, as calling a context's cancel again does.
func TestWatchSignals_StopEndsAWatchThatCaughtNothing(t *testing.T) {
	ctx, stop := watchSignals(t.Context(), []os.Signal{syscall.SIGUSR1})

	stop()
	require.NotPanics(t, func() { stop() }, "a second stop")

	assert.Equal(t, context.Canceled, context.Cause(ctx), "no signal came, so none is the cause")
}

// TestWatchSignals_NoSignalsWatchesNone pins the guard on an empty list:
// signal.NotifyContext handed no signals relays every one, so without it any
// signal, unwatched ones included, would cancel the run.
func TestWatchSignals_NoSignalsWatchesNone(t *testing.T) {
	ctx, stop := watchSignals(t.Context(), nil)

	signalSelf(t)
	select {
	case <-ctx.Done():
		assert.Fail(t, "an unwatched signal cancelled the context", "cause: %v", context.Cause(ctx))
	case <-time.After(settle):
	}
	stop()

	assert.Equal(t, context.Canceled, context.Cause(ctx), "stop still ends the watch")
}
