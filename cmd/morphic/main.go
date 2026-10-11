// Command morphic is the Morphic CLI: it lowers an API spec into Morphic IR.
//
// It is the only layer that renders diagnostics for a human (architecture §4);
// every stage below it emits typed ir.Diagnostic values and never writes to
// stderr.
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"charm.land/fang/v2"
	"github.com/spf13/cobra"
)

// osExit is os.Exit, a package var so tests can drive main without terminating
// the test process.
var osExit = os.Exit

func main() {
	osExit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run builds the command tree, executes it against args and returns the process
// exit code. It exists so tests can drive the CLI without a subprocess; only
// main calls os.Exit.
func run(args []string, stdout, stderr io.Writer) int {
	return execute(newRootCmd(), args, stdout, stderr)
}

// execute runs root under fang and maps the outcome to an exit code. args is
// copied and never nil: handed a nil slice, cobra runs os.Args[1:] instead.
func execute(root *cobra.Command, args []string, stdout, stderr io.Writer) int {
	root.SetArgs(append([]string{}, args...))
	root.SetOut(stdout)
	root.SetErr(stderr)

	err := fang.Execute(context.Background(), root, fangOptions()...)
	return exitCode(err)
}

// emitf writes a formatted line to w. Write errors on a human-facing stream are
// unactionable, so they are deliberately discarded.
func emitf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}
