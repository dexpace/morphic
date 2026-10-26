package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"
	"time"
)

// raiseWait bounds how long raise waits for the signal it sends to end the
// process. Delivery takes far less; the bound decides only when a signal that
// did not end the process gives way to an exit status.
const raiseWait = time.Second

// stopSignals returns the signals that end a Go program by default, which
// morphic catches to stop a run cleanly instead: SIGINT from a terminal,
// SIGTERM from a supervisor or a timeout, and SIGHUP from a terminal closing.
func stopSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
}

// catchable returns the signals of sigs that are not ignored. A shell without
// job control, as a script's is, starts a background job with SIGINT ignored,
// and nohup a command with SIGHUP ignored, so that neither reaches it. Catching
// one would undo that: signal.Notify replaces an ignored disposition with a
// handler.
func catchable(sigs []os.Signal) []os.Signal {
	return slices.DeleteFunc(slices.Clone(sigs), signal.Ignored)
}

// signalled is the cause of a run a stop signal ended.
type signalled struct{ sig os.Signal }

func (s *signalled) Error() string { return "stopped: " + s.sig.String() + " signal received" }

// exitCode is the status a shell reports for a process the signal ended, 128
// plus its number, for when re-raising it leaves the process running. A signal
// with no number takes exitFailure.
func (s *signalled) exitCode() int {
	if n, ok := s.sig.(syscall.Signal); ok {
		return 128 + int(n)
	}
	return exitFailure
}

// signalledBy reports the stop signal that ended ctx, if one did.
func signalledBy(ctx context.Context) (*signalled, bool) {
	return errors.AsType[*signalled](context.Cause(ctx))
}

// watchSignals returns a context the first of sigs to arrive cancels, with a
// *signalled cause, and a stop function that ends the watch: it must be called,
// and calling it again does nothing, as calling a context's cancel again does.
// The first signal also releases them all, so a second takes its default
// action and ends the process at once: a run that does not stop can still be
// stopped. Given no sigs it watches none, where signal.Notify would relay all.
func watchSignals(parent context.Context, sigs []os.Signal) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	if len(sigs) == 0 {
		return ctx, func() { cancel(nil) }
	}
	w := newWatch(cancel)
	signal.Notify(w.caught, sigs...)
	go w.await()
	return ctx, sync.OnceFunc(w.stop)
}

// watch turns the first signal relayed to caught into the cancellation of the
// context cancel ends.
type watch struct {
	caught chan os.Signal
	cancel context.CancelCauseFunc
	// quit asks await to return; done is closed once it has.
	quit, done chan struct{}
}

// newWatch returns a watch that cancels with cancel, relayed no signal yet.
func newWatch(cancel context.CancelCauseFunc) *watch {
	return &watch{
		caught: make(chan os.Signal, 1),
		cancel: cancel,
		quit:   make(chan struct{}),
		done:   make(chan struct{}),
	}
}

// await waits for the first signal, and cancels with it, or for stop to ask
// it to return. It is the watch's goroutine, which stop waits for.
func (w *watch) await() {
	defer close(w.done)
	select {
	case sig := <-w.caught:
		signal.Stop(w.caught)
		w.cancel(&signalled{sig: sig})
	case <-w.quit:
	}
}

// stop ends the watch and cancels its context: with a signal that reached
// caught as the watch ended, which signal.Stop delivers rather than drops, or
// with none. A signal that came as the run ended therefore still ends the
// process, by being raised again or by its default action.
func (w *watch) stop() {
	close(w.quit)
	<-w.done
	signal.Stop(w.caught)
	select {
	case sig := <-w.caught:
		w.cancel(&signalled{sig: sig})
	default:
		w.cancel(nil)
	}
}

// raise ends the process by sig, which must no longer be caught, as sig's
// default action ends a process that never caught it. After a Ctrl-C, a shell
// stops the script running morphic only if SIGINT ended morphic, not if it
// exited 130. raise returns only when sig cannot be sent, or has not ended the
// process within raiseWait.
func raise(sig os.Signal) {
	p, err := os.FindProcess(os.Getpid())
	if err == nil {
		err = p.Signal(sig)
	}
	if err != nil {
		return
	}
	time.Sleep(raiseWait)
}

// exitStatus returns the status a process exits with when its run returned
// code under ctx, once ctx's watch has stopped. A run a stop signal ended is
// handed to end, which ends the process by that signal; the status is for
// when end returns, and is what a shell reports for a process that signal
// ended.
func exitStatus(ctx context.Context, code int, end func(os.Signal)) int {
	if s, ok := signalledBy(ctx); ok {
		end(s.sig)
		return s.exitCode()
	}
	return code
}
