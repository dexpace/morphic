// Command morphic is the Morphic CLI: it lowers an API spec into Morphic IR.
//
// It parses flags, runs the engine and turns the run into an exit code. What
// it prints is the engine's to produce: engine/report renders the diagnostics
// and --explain, and engine/output writes the document. The CLI chooses only
// the streams; every stage below the engine emits typed ir.Diagnostic values
// and never writes to stderr (architecture §4).
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// osExit is os.Exit, a package var so tests can drive main without terminating
// the test process.
var osExit = os.Exit

// main runs the CLI under a context the first stop signal cancels, then exits.
func main() {
	ctx, stop := watchSignals(context.Background(), catchable(stopSignals()))
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	osExit(code)
}

// run builds the command tree, executes it against args under ctx and returns
// the process exit code. It exists so tests can drive the CLI without a
// subprocess; only main calls os.Exit.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return execute(ctx, newRootCmd(), args, stdout, stderr)
}

// execute runs root against args under ctx, reports what ended the run, and
// returns its exit code. args is copied and never nil: handed a nil slice,
// cobra runs os.Args[1:] instead. A long flag spelled with one dash is refused
// before cobra parses anything. A run whose ctx ended was stopped, whatever the
// command returned on seeing it, and fails with what ended ctx as its reason.
func execute(ctx context.Context, root *cobra.Command, args []string, stdout, stderr io.Writer) int {
	root.SetArgs(append([]string{}, args...))
	root.SetOut(stdout)
	root.SetErr(stderr)

	err := singleDashLongFlags(root, args)
	if err == nil {
		err = root.ExecuteContext(ctx)
	}
	if ctx.Err() != nil {
		err = failure(fmt.Errorf("stopped: %w", context.Cause(ctx)))
	}
	reportError(stderr, err)
	return exitCode(err)
}

// emitf writes a formatted line to w. Write errors on a human-facing stream are
// unactionable, so they are deliberately discarded.
func emitf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}
