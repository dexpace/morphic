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
// process-wide, so none of them runs in parallel. The signals they send
// themselves are SIGUSR1 and SIGUSR2, which a Go program that has not asked
// for them ignores, so one that arrives with no test watching does no harm.

// arrival bounds the wait for a signal a test sent itself, which takes far
// less, so only a failing test waits it out.
const arrival = 10 * time.Second

// settle is how long a test waits to show a signal it sent itself had no
// effect. A defect that shows later passes, so it only has to be long enough
// that one rarely does.
const settle = 200 * time.Millisecond

// noNumber is an os.Signal with no number, which no signal os/signal delivers
// on this platform is: what raise cannot send and exitCode cannot number.
type noNumber struct{}

func (noNumber) String() string { return "numberless" }
func (noNumber) Signal()        {}

// signalSelf sends this process SIGUSR1, the signal the watches here watch.
func signalSelf(t *testing.T) {
	t.Helper()
	require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGUSR1))
}

// TestSignalled_NamesTheSignalAndTheStatusItEndsWith pins the line a stopped
// run reports and the status a shell gives a process the signal ended, which
// is the one the run exits with when re-raising the signal does not end it.
func TestSignalled_NamesTheSignalAndTheStatusItEndsWith(t *testing.T) {
	tests := []struct {
		sig      os.Signal
		wantText string
		wantCode int
	}{
		{os.Interrupt, "stopped: interrupt signal received", 130},
		{syscall.SIGTERM, "stopped: terminated signal received", 143},
		{syscall.SIGHUP, "stopped: hangup signal received", 129},
		{noNumber{}, "stopped: numberless signal received", exitFailure},
	}
	for _, tt := range tests {
		s := &signalled{sig: tt.sig}

		assert.EqualError(t, s, tt.wantText)
		assert.Equal(t, tt.wantCode, s.exitCode(), "%s", tt.sig)
	}
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
// a watched signal cancels the context with itself as the cause, which stop
// then leaves in place.
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
	s, ok := signalledBy(ctx)
	require.True(t, ok, "the cause is the signal: %v", context.Cause(ctx))
	assert.Equal(t, syscall.SIGUSR1, s.sig)
}

// TestWatchSignals_StopEndsAWatchThatCaughtNothing pins that stop cancels the
// context with no signal as its cause, and that a signal after it is no longer
// the watch's to report.
func TestWatchSignals_StopEndsAWatchThatCaughtNothing(t *testing.T) {
	ctx, stop := watchSignals(t.Context(), []os.Signal{syscall.SIGUSR1})

	stop()
	signalSelf(t)
	time.Sleep(settle)

	assert.ErrorIs(t, context.Cause(ctx), context.Canceled)
	_, ok := signalledBy(ctx)
	assert.False(t, ok, "a signal after stop is not the run's")
}

// TestWatchSignals_NoSignalsWatchesNone pins the guard on an empty list:
// signal.Notify handed no signals relays every one, so without it any signal,
// unwatched ones included, would cancel the run.
func TestWatchSignals_NoSignalsWatchesNone(t *testing.T) {
	ctx, stop := watchSignals(t.Context(), nil)

	signalSelf(t)
	select {
	case <-ctx.Done():
		assert.Fail(t, "an unwatched signal cancelled the context", "cause: %v", context.Cause(ctx))
	case <-time.After(settle):
	}
	stop()

	assert.ErrorIs(t, context.Cause(ctx), context.Canceled, "stop still ends the watch")
}

// stoppedWatch returns a watch whose goroutine has already returned, as one
// that caught nothing has once stop asks it to, with the context it cancels.
func stoppedWatch(t *testing.T) (*watch, context.Context) {
	t.Helper()
	ctx, cancel := context.WithCancelCause(t.Context())
	w := newWatch(cancel)
	close(w.done)
	return w, ctx
}

// TestWatch_StopReadsASignalThatCameAsTheWatchEnded pins that a signal
// delivered to the watch's channel after its goroutine stopped reading is
// the run's cause, not lost: signal.Stop delivers a signal it races rather
// than dropping it. With none there, the run has no cause.
func TestWatch_StopReadsASignalThatCameAsTheWatchEnded(t *testing.T) {
	w, ctx := stoppedWatch(t)
	w.caught <- syscall.SIGUSR1

	w.stop()

	s, ok := signalledBy(ctx)
	require.True(t, ok, "a delivered signal is the cause: %v", context.Cause(ctx))
	assert.Equal(t, syscall.SIGUSR1, s.sig)

	w, ctx = stoppedWatch(t)
	w.stop()
	assert.ErrorIs(t, context.Cause(ctx), context.Canceled, "no signal, no cause")
}

// TestWatch_StopReleasesTheSignals pins that stop unregisters the watch's
// channel, so a signal after it takes its default action rather than going to
// a channel nobody reads.
func TestWatch_StopReleasesTheSignals(t *testing.T) {
	w, _ := stoppedWatch(t)
	signal.Notify(w.caught, syscall.SIGUSR1)

	w.stop()
	signalSelf(t)
	time.Sleep(settle)

	assert.Empty(t, w.caught, "a released channel is relayed nothing")
}

// TestRaise_SendsTheSignalToThisProcess pins that raise signals its own
// process. The signal is caught here, so the process survives it and raise
// returns once raiseWait has passed.
func TestRaise_SendsTheSignalToThisProcess(t *testing.T) {
	caught := make(chan os.Signal, 1)
	signal.Notify(caught, syscall.SIGUSR1)
	t.Cleanup(func() { signal.Stop(caught) })

	raise(syscall.SIGUSR1)

	select {
	case sig := <-caught:
		assert.Equal(t, syscall.SIGUSR1, sig)
	case <-time.After(arrival):
		assert.Fail(t, "raise sent this process nothing")
	}
}

// TestRaise_ReturnsASignalItCannotSend pins that a signal the platform cannot
// send leaves the caller to exit, rather than failing in some other way.
func TestRaise_ReturnsASignalItCannotSend(t *testing.T) {
	assert.NotPanics(t, func() { raise(noNumber{}) })
}

// TestExitStatus_EndsAStoppedRunByItsSignal pins the last step of main: a run
// a stop signal ended is handed to end with that signal, and exits as a shell
// reports a process the signal ended if end returns. Any other run, a
// cancelled one included, exits with its own code and ends nothing.
func TestExitStatus_EndsAStoppedRunByItsSignal(t *testing.T) {
	tests := []struct {
		name      string
		cause     error
		code      int
		wantEnded []os.Signal
		wantCode  int
	}{
		{"not cancelled", nil, exitFindings, nil, exitFindings},
		{"cancelled with no signal", context.Canceled, exitFailure, nil, exitFailure},
		{"stopped by SIGTERM", &signalled{sig: syscall.SIGTERM}, 0, []os.Signal{syscall.SIGTERM}, 143},
		{"stopped by a signal with no number", &signalled{sig: noNumber{}}, 0, []os.Signal{noNumber{}}, exitFailure},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			if tt.cause != nil {
				cancel(tt.cause)
			}
			var ended []os.Signal

			got := exitStatus(ctx, tt.code, func(sig os.Signal) { ended = append(ended, sig) })

			assert.Equal(t, tt.wantCode, got)
			assert.Equal(t, tt.wantEnded, ended)
		})
	}
}
