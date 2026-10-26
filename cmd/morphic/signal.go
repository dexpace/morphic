package main

import (
	"context"
	"os"
	"os/signal"
	"slices"
	"syscall"
)

// stopSignals returns the signals os/signal says make a Go program exit, as
// against those that make it exit with a stack dump. morphic catches them to
// stop a run cleanly instead: SIGINT from a terminal, SIGTERM from a
// supervisor or a timeout, and SIGHUP from a terminal closing.
func stopSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
}

// catchable returns the signals of sigs that are not ignored. A shell without
// job control, as a script's is, starts a background job with SIGINT ignored,
// and nohup a command with SIGHUP ignored, so that neither reaches it. Catching
// one would undo that: signal.Notify replaces an ignored disposition with a
// handler. Go keeps an inherited ignore for SIGINT and SIGHUP alone, so SIGTERM
// is caught however morphic was started.
func catchable(sigs []os.Signal) []os.Signal {
	return slices.DeleteFunc(slices.Clone(sigs), signal.Ignored)
}

// watchSignals returns a context the first of sigs to arrive cancels, as
// signal.NotifyContext does, and the stop function that ends the watch. Later
// signals are caught too until stop, so none can end the process before the
// run has cleaned up: a shell can send SIGHUP twice as its terminal closes, and
// a wrapper can pass on a Ctrl-C its child has had already. Given no sigs it
// watches none, where NotifyContext would relay every signal.
func watchSignals(parent context.Context, sigs []os.Signal) (context.Context, context.CancelFunc) {
	if len(sigs) == 0 {
		return context.WithCancel(parent)
	}
	return signal.NotifyContext(parent, sigs...)
}
